package main

import (
	"github.com/aws/aws-cdk-go/awscdk/v2"
	"github.com/aws/aws-cdk-go/awscdk/v2/awslambda"
	"github.com/aws/aws-cdk-go/awscdk/v2/awssecretsmanager"
	"github.com/aws/aws-cdk-go/awscdk/v2/awsapigatewayv2"
	"github.com/aws/aws-cdk-go/awscdk/v2/awsapigatewayv2integrations"
	"github.com/aws/constructs-go/constructs/v10"
	"github.com/aws/jsii-runtime-go"
)

type StravaSkillStackProps struct {
	awscdk.StackProps
}

func StravaSkillStack(scope constructs.Construct, id string, props *StravaSkillStackProps) awscdk.Stack {
	stack := awscdk.NewStack(scope, &id, &props.StackProps)

	// Reference the manually-created secret. Create it first with:
	// aws secretsmanager create-secret --name StravaSkillStack/strava/oauth \
	//   --secret-string '{"client_id":"...","client_secret":"...","refresh_token":"...","skill_auth_key":"..."}'
	stravaSecret := awssecretsmanager.Secret_FromSecretNameV2(
		stack, jsii.String("StravaSkillStack-Secret"), jsii.String("StravaSkillStack/strava/oauth"),
	)

	fn := awslambda.NewFunction(stack, jsii.String("StravaSkillStack-ProxyHandler"), &awslambda.FunctionProps{
		Runtime:      awslambda.Runtime_PROVIDED_AL2023(),
		Architecture: awslambda.Architecture_ARM_64(),
		Handler:      jsii.String("bootstrap"),
		Code:         awslambda.Code_FromAsset(jsii.String("../lambda/bootstrap.zip"), nil),
		Timeout:      awscdk.Duration_Seconds(jsii.Number(29)),
		MemorySize:   jsii.Number(256),
		Environment: &map[string]*string{
			"SECRET_ARN": stravaSecret.SecretArn(),
		},
	})

	stravaSecret.GrantRead(fn, nil)

	api := awsapigatewayv2.NewHttpApi(stack, jsii.String("StravaSkillStack-Api"), &awsapigatewayv2.HttpApiProps{
		ApiName: jsii.String("StravaSkillStack-Api"),
	})

	integration := awsapigatewayv2integrations.NewHttpLambdaIntegration(
		jsii.String("StravaSkillStack-Integration"), fn, nil,
	)

	api.AddRoutes(&awsapigatewayv2.AddRoutesOptions{
		Path:        jsii.String("/{proxy+}"),
		Methods:     &[]awsapigatewayv2.HttpMethod{awsapigatewayv2.HttpMethod_ANY},
		Integration: integration,
	})

	awscdk.NewCfnOutput(stack, jsii.String("StravaSkillStack-ApiURL"), &awscdk.CfnOutputProps{
		Value:       api.ApiEndpoint(),
		Description: jsii.String("Paste this as BASE_URL in ~/.claude/skills/strava-analyzer/SKILL.md"),
	})

	return stack
}

func main() {
	defer jsii.Close()

	app := awscdk.NewApp(nil)

	StravaSkillStack(app, "StravaSkillStack", &StravaSkillStackProps{
		StackProps: awscdk.StackProps{},
	})

	app.Synth(nil)
}
