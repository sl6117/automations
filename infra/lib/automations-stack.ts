import * as cdk from "aws-cdk-lib";
import * as lambda from "aws-cdk-lib/aws-lambda";
import { Construct } from "constructs";

export class AutomationsStack extends cdk.Stack {
  constructor(scope: Construct, id: string, props?: cdk.StackProps) {
    super(scope, id, props);

    // Bite-1 smoke test: proves bootstrap + deploy + invoke before any real
    // packaging. Replaced by the container-image functions in bite 2.
    new lambda.Function(this, "Hello", {
      functionName: "automations-hello",
      runtime: lambda.Runtime.NODEJS_22_X,
      handler: "index.handler",
      code: lambda.Code.fromInline(
        "exports.handler = async (event) => { console.log('hello from automations', event); return { ok: true }; };"
      ),
    });
  }
}