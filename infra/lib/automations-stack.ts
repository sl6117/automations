import * as cdk from "aws-cdk-lib";
import * as lambda from "aws-cdk-lib/aws-lambda";
import { Platform } from "aws-cdk-lib/aws-ecr-assets";
import * as dynamodb from "aws-cdk-lib/aws-dynamodb";
import * as iam from "aws-cdk-lib/aws-iam";
import * as scheduler from "aws-cdk-lib/aws-scheduler";
import * as targets from "aws-cdk-lib/aws-scheduler-targets";
import { Construct } from "constructs";
import * as path from "path";

export class AutomationsStack extends cdk.Stack {
  constructor(scope: Construct, id: string, props?: cdk.StackProps) {
    super(scope, id, props);

    // One image for all projects (step 9 design): built from the repo-root
    // Dockerfile, bundling auto-lambda + digest-mcp + projects/* assets.
    const image = lambda.DockerImageCode.fromImageAsset(
      path.join(__dirname, "..", ".."),
      { platform: Platform.LINUX_ARM64 }
    );

    // The table pre-exists (created before this stack); import, don't own.
    const table = dynamodb.Table.fromTableName(this, "AutomationsTable", "automations");

    const makeWorker = (id: string, functionName: string, timeout: cdk.Duration) => {
      const fn = new lambda.DockerImageFunction(this, id, {
        functionName,
        code: image,
        architecture: lambda.Architecture.ARM_64,
        timeout,
        memorySize: 1024,
        environment: {
          STORAGE_BACKEND: "dynamo",
          DYNAMO_TABLE: "automations",
        },
      });
      table.grantReadWriteData(fn);
      fn.addToRolePolicy(
        new iam.PolicyStatement({
          actions: ["ssm:GetParametersByPath"],
          resources: [
            `arn:aws:ssm:${this.region}:${this.account}:parameter/automations`,
            `arn:aws:ssm:${this.region}:${this.account}:parameter/automations/*`,
          ],
        })
      );
      return fn;
    };

    const digest = makeWorker("Digest", "automations-digest", cdk.Duration.minutes(10));
    const deepdive = makeWorker("Deepdive", "automations-deepdive", cdk.Duration.minutes(15));

    // Cutover phase 1 (step 9 design): schedules fire with dryRun:true while
    // launchd stays live. Flip to dryRun:false + unschedule launchd together.
    const tz = cdk.TimeZone.AMERICA_LOS_ANGELES;

    new scheduler.Schedule(this, "DailyDigestSchedule", {
      schedule: scheduler.ScheduleExpression.cron({ minute: "0", hour: "9", timeZone: tz }),
      target: new targets.LambdaInvoke(digest, {
        input: scheduler.ScheduleTargetInput.fromObject({ project: "twitter-digest", dryRun: true }),
      }),
    });

    new scheduler.Schedule(this, "WeeklyDeepdiveSchedule", {
      schedule: scheduler.ScheduleExpression.cron({ minute: "0", hour: "10", weekDay: "SUN", timeZone: tz }),
      target: new targets.LambdaInvoke(deepdive, {
        input: scheduler.ScheduleTargetInput.fromObject({ project: "weekly-deepdive", dryRun: true }),
      }),
    });
  }
}