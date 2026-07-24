import * as cdk from "aws-cdk-lib";
import * as lambda from "aws-cdk-lib/aws-lambda";
import { Platform } from "aws-cdk-lib/aws-ecr-assets";
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

    new lambda.DockerImageFunction(this, "Digest", {
      functionName: "automations-digest",
      code: image,
      architecture: lambda.Architecture.ARM_64,
      timeout: cdk.Duration.minutes(10),
      memorySize: 1024,
    });
  }
}