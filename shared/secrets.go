package shared

import (
	"context"
	"encoding/json"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
)

// LoadSecret fetches the Secrets Manager secret whose ARN is in the named
// environment variable and unmarshals its JSON string into out.
//
// For local debugging, setting SECRET_JSON to the secret's JSON payload skips
// Secrets Manager (and AWS credentials) entirely. Lambda never sets it.
func LoadSecret(ctx context.Context, envVar string, out interface{}) error {
	if raw := os.Getenv("SECRET_JSON"); raw != "" {
		return json.Unmarshal([]byte(raw), out)
	}

	cfg, err := awsconfig.LoadDefaultConfig(ctx)
	if err != nil {
		return err
	}

	sm := secretsmanager.NewFromConfig(cfg)
	result, err := sm.GetSecretValue(ctx, &secretsmanager.GetSecretValueInput{
		SecretId: aws.String(os.Getenv(envVar)),
	})
	if err != nil {
		return err
	}

	return json.Unmarshal([]byte(aws.ToString(result.SecretString)), out)
}
