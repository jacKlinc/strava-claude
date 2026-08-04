//go:build debug

package main

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/aws/aws-lambda-go/events"
)

// Debug-only entry points, excluded from normal builds and `go test ./...` by
// the `debug` build tag. In VS Code just click "debug test" above whichever
// route you care about — .vscode/settings.json supplies the tag and the .env,
// so there is nothing to configure per run.
//
// Change this to whatever activity you want to inspect.
const debugActivityID = "i170806275"

func TestDebugListActivities(t *testing.T) {
	run(t, "/activities", map[string]string{"oldest": "2026-07-01", "newest": "2026-08-04"}, "")
}

func TestDebugActivityDetail(t *testing.T) {
	run(t, "/activities/"+debugActivityID, nil, "")
}

func TestDebugStreams(t *testing.T) {
	run(t, "/activities/"+debugActivityID+"/streams", nil, "")
}

func TestDebugIntervals(t *testing.T) {
	run(t, "/activities/"+debugActivityID+"/intervals", nil, "")
}

func TestDebugMCP(t *testing.T) {
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get_activity","arguments":{"id":"` + debugActivityID + `"}}}`
	run(t, "/mcp", nil, body)
}

// run drives the real handler with a synthetic Function URL request. Breakpoints
// anywhere in handler (or in the shared module) are hit from here.
func run(t *testing.T, path string, query map[string]string, body string) {
	t.Helper()
	useEnvSecret(t)

	req := events.LambdaFunctionURLRequest{
		RawPath:               path,
		QueryStringParameters: query,
		Body:                  body,
		Headers:               map[string]string{"x-claude-secret": os.Getenv("SKILL_SECRET")},
	}

	resp, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	t.Logf("%s → status=%d\nbody=%s", path, resp.StatusCode, truncate(resp.Body, 2000))
}

// useEnvSecret makes LoadSecret short-circuit on SECRET_JSON built from .env,
// so debugging needs no AWS credentials. Set SECRET_ARN in .env instead if you
// want to exercise the real Secrets Manager path.
func useEnvSecret(t *testing.T) {
	t.Helper()
	if os.Getenv("SECRET_ARN") != "" || os.Getenv("SECRET_JSON") != "" {
		return
	}
	secret, _ := json.Marshal(credentials{
		APIKey:       os.Getenv("INTERVALS_API_KEY"),
		SkillAuthKey: os.Getenv("SKILL_SECRET"),
	})
	t.Setenv("SECRET_JSON", string(secret))
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "... (truncated)"
}
