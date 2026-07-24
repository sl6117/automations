import * as cdk from "aws-cdk-lib";
import * as lambda from "aws-cdk-lib/aws-lambda";
import { Platform } from "aws-cdk-lib/aws-ecr-assets";
import { Construct } from "constructs";
import * as path from "path";
import * as dynamodb from "aws-cdk-lib/aws-dynamodb";
import * as iam from "aws-cdk-lib/aws-iam";

export class AutomationsStack extends cdk.Stack {
  constructor(scope: Construct, id: string, props?: cdk.StackProps) {
    super(scope, id, props);

    // One image for all projects (step 9 design): built from the repo-root
    // Dockerfile, bundling auto-lambda + digest-mcp + projects/* assets.
    const image = lambda.DockerImageCode.fromImageAsset(
      path.join(__dirname, "..", ".."),
      { platform: Platform.LINUX_ARM64 }
    );

    const digest =new lambda.DockerImageFunction(this, "Digest", {
      functionName: "automations-digest",
      code: image,
      architecture: lambda.Architecture.ARM_64,
      timeout: cdk.Duration.minutes(10),
      memorySize: 1024,
      environment: {
        STORAGE_BACKEND: "dynamo",
        DYNAMO_TABLE: "automations",
      },
    });
    // The table pre-exists (created before this stack); import, don't own.
    const table = dynamodb.Table.fromTableName(this, "AutomationsTable", "automations");
    table.grantReadWriteData(digest);
    digest.addToRolePolicy(
      new iam.PolicyStatement({
        actions: ["ssm:GetParametersByPath"],
        resources: [
          `arn:aws:ssm:${this.region}:${this.account}:parameter/automations`,
          `arn:aws:ssm:${this.region}:${this.account}:parameter/automations/*`,
        ],
      })
    );
  }
}