package main

import (
	"github.com/aws/aws-cdk-go/awscdk/v2"
	"github.com/aws/aws-cdk-go/awscdk/v2/awsapigatewayv2"
	"github.com/aws/aws-cdk-go/awscdk/v2/awsapigatewayv2integrations"
	"github.com/aws/aws-cdk-go/awscdk/v2/awslambda"
	"github.com/aws/aws-cdk-go/awscdk/v2/awssecretsmanager"
	"github.com/aws/constructs-go/constructs/v10"
	"github.com/aws/jsii-runtime-go"
)

// skillStackConfig parameterizes the identical shape shared by every
// Strava-claude proxy Lambda stack: one Secrets Manager secret (created
// out-of-band via `make create-secret`/`make create-intervals-secret`), one
// Lambda behind an HTTP API with an ANY /{proxy+} route.
type skillStackConfig struct {
	// IDPrefix names every construct in the stack (e.g. "StravaSkillStack"),
	// kept stable across refactors so CloudFormation logical IDs don't change.
	IDPrefix          string
	SecretName        string
	CodeAssetPath     string
	OutputDescription string
}

func newSkillStack(scope constructs.Construct, id string, cfg skillStackConfig) awscdk.Stack {
	stack := awscdk.NewStack(scope, &id, &awscdk.StackProps{})

	// Reference the manually-created secret — see cfg.SecretName's Makefile target.
	secret := awssecretsmanager.Secret_FromSecretNameV2(
		stack, jsii.String(cfg.IDPrefix+"-Secret"), jsii.String(cfg.SecretName),
	)

	fn := awslambda.NewFunction(stack, jsii.String(cfg.IDPrefix+"-ProxyHandler"), &awslambda.FunctionProps{
		Runtime:      awslambda.Runtime_PROVIDED_AL2023(),
		Architecture: awslambda.Architecture_ARM_64(),
		Handler:      jsii.String("bootstrap"),
		Code:         awslambda.Code_FromAsset(jsii.String(cfg.CodeAssetPath), nil),
		Timeout:      awscdk.Duration_Seconds(jsii.Number(29)),
		MemorySize:   jsii.Number(256),
		Environment: &map[string]*string{
			"SECRET_ARN": secret.SecretArn(),
		},
	})

	secret.GrantRead(fn, nil)

	api := awsapigatewayv2.NewHttpApi(stack, jsii.String(cfg.IDPrefix+"-Api"), &awsapigatewayv2.HttpApiProps{
		ApiName: jsii.String(cfg.IDPrefix + "-Api"),
		CorsPreflight: &awsapigatewayv2.CorsPreflightOptions{
			AllowOrigins: jsii.Strings("*"),
			AllowMethods: &[]awsapigatewayv2.CorsHttpMethod{awsapigatewayv2.CorsHttpMethod_GET},
			AllowHeaders: jsii.Strings("content-type", "x-claude-secret"),
		},
	})

	integration := awsapigatewayv2integrations.NewHttpLambdaIntegration(
		jsii.String(cfg.IDPrefix+"-Integration"), fn, nil,
	)

	api.AddRoutes(&awsapigatewayv2.AddRoutesOptions{
		Path:        jsii.String("/{proxy+}"),
		Methods:     &[]awsapigatewayv2.HttpMethod{awsapigatewayv2.HttpMethod_ANY},
		Integration: integration,
	})

	awscdk.NewCfnOutput(stack, jsii.String(cfg.IDPrefix+"-ApiURL"), &awscdk.CfnOutputProps{
		Value:       api.ApiEndpoint(),
		Description: jsii.String(cfg.OutputDescription),
	})

	return stack
}

func main() {
	defer jsii.Close()

	app := awscdk.NewApp(nil)

	newSkillStack(app, "StravaSkillStack", skillStackConfig{
		IDPrefix:          "StravaSkillStack",
		SecretName:        "StravaSkillStack/strava/oauth",
		CodeAssetPath:     "../lambda/bootstrap.zip",
		OutputDescription: "Paste this as BASE_URL in ~/.claude/skills/strava-analyzer/SKILL.md",
	})

	newSkillStack(app, "IntervalsSkillStack", skillStackConfig{
		IDPrefix:          "IntervalsSkillStack",
		SecretName:        "IntervalsSkillStack/intervals/api",
		CodeAssetPath:     "../intervals/bootstrap.zip",
		OutputDescription: "Paste this as INTERVALS_BASE_URL in .env, then add https://<url>/mcp?key=<SKILL_SECRET> in Claude.ai",
	})

	app.Synth(nil)
}
