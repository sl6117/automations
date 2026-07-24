import * as cdk from "aws-cdk-lib";
import { AutomationsStack } from "../lib/automations-stack";
const app = new cdk.App();
new AutomationsStack(app, "AutomationsStack", {
  env: {
    account: process.env.CDK_DEFAULT_ACCOUNT,
    region: "us-east-2",
  },
});