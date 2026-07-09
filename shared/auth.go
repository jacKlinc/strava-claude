package shared

import "github.com/aws/aws-lambda-go/events"

// Authorized checks the shared-secret protecting the Function URL, via the
// x-claude-secret header or a ?key= query param (the latter for clients,
// like Claude.ai's MCP connector, that can't set custom headers).
func Authorized(req events.LambdaFunctionURLRequest, expectedKey string) bool {
	if expectedKey == "" {
		return true
	}
	providedKey := req.Headers["x-claude-secret"]
	if providedKey == "" {
		providedKey = req.QueryStringParameters["key"]
	}
	return providedKey == expectedKey
}
