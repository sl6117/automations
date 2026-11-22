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

    // Every fetched post lives here, apart from the main table, so the main table's
    // full-table List scans never read corpus items. Owned by this stack (unlike the main
    // table) but retained on stack deletion: it holds history that cannot be re-fetched.
    const corpusTable = new dynamodb.Table(this, "CorpusTable", {
      tableName: "automations-corpus",
      partitionKey: { name: "pk", type: dynamodb.AttributeType.STRING },
      sortKey: { name: "sk", type: dynamodb.AttributeType.STRING },
      billingMode: dynamodb.BillingMode.PAY_PER_REQUEST,
      pointInTimeRecoverySpecification: { pointInTimeRecoveryEnabled: true },
      removalPolicy: cdk.RemovalPolicy.RETAIN,
    });
    // digest only: it is the sole writer, and the deepdive has no reason to read posts
    corpusTable.grantReadWriteData(digest);
    digest.addEnvironment("CORPUS_TABLE", corpusTable.tableName);
    const deepdive = makeWorker("Deepdive", "automations-deepdive", cdk.Duration.minutes(15));

    // Seals the day's desk feed from the corpus and, when its flag is on, publishes it
    // to GitHub. It reads the corpus the digest wrote, so it runs half an hour later.
    const knowledgeFactory = makeWorker("KnowledgeFactory", "automations-knowledge-factory", cdk.Duration.minutes(5));
    corpusTable.grantReadWriteData(knowledgeFactory);
    knowledgeFactory.addEnvironment("CORPUS_TABLE", corpusTable.tableName);

   // These schedules are the sole triggers for both projects; launchd is retired.
    const tz = cdk.TimeZone.AMERICA_LOS_ANGELES;

    new scheduler.Schedule(this, "DailyDigestSchedule", {
      schedule: scheduler.ScheduleExpression.cron({ minute: "0", hour: "9", timeZone: tz }),
      target: new targets.LambdaInvoke(digest, {
        input: scheduler.ScheduleTargetInput.fromObject({ project: "twitter-digest", dryRun: false }),
      }),
    });

    new scheduler.Schedule(this, "WeeklyDeepdiveSchedule", {
      schedule: scheduler.ScheduleExpression.cron({ minute: "0", hour: "10", weekDay: "SUN", timeZone: tz }),
      target: new targets.LambdaInvoke(deepdive, {
        input: scheduler.ScheduleTargetInput.fromObject({ project: "weekly-deepdive", dryRun: false }),
      }),
    });

    new scheduler.Schedule(this, "DailyKnowledgeFactorySchedule", {
      schedule: scheduler.ScheduleExpression.cron({ minute: "30", hour: "9", timeZone: tz }),
      target: new targets.LambdaInvoke(knowledgeFactory, {
        input: scheduler.ScheduleTargetInput.fromObject({ project: "knowledge-factory", dryRun: false }),
      }),
    });
  }
}