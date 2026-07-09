package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/jack/strava-claude/shared"
)

const (
	intervalsBase = "https://intervals.icu/api/v1"
	maxStreamPts  = 200
)

var (
	reActivities     = regexp.MustCompile(`^/activities$`)
	reActivityDetail = regexp.MustCompile(`^/activities/(\d+)$`)
	reStreams        = regexp.MustCompile(`^/activities/(\d+)/streams$`)
	reIntervals      = regexp.MustCompile(`^/activities/(\d+)/intervals$`)
	reMCP            = regexp.MustCompile(`^/mcp$`)
)

// Secret layout in Secrets Manager: intervals/api
// {
//   "api_key":        "...",
//   "skill_auth_key": "..."   ← simple shared secret for the Function URL
// }
type credentials struct {
	APIKey       string `json:"api_key"`
	SkillAuthKey string `json:"skill_auth_key"`
}

func handler(ctx context.Context, req events.LambdaFunctionURLRequest) (events.LambdaFunctionURLResponse, error) {
	var creds credentials
	if err := shared.LoadSecret(ctx, "SECRET_ARN", &creds); err != nil {
		return shared.ErrResp(500, "failed to load credentials: "+err.Error()), nil
	}

	if !shared.Authorized(req, creds.SkillAuthKey) {
		return shared.ErrResp(401, "unauthorized"), nil
	}

	path := req.RawPath
	qp := req.QueryStringParameters

	switch {
	case reActivities.MatchString(path):
		return proxyIntervals(creds.APIKey, "/athlete/0/activities", qp)
	case reActivityDetail.MatchString(path):
		m := reActivityDetail.FindStringSubmatch(path)
		return proxyIntervals(creds.APIKey, "/activity/"+m[1], qp)
	case reStreams.MatchString(path):
		m := reStreams.FindStringSubmatch(path)
		return fetchStreams(creds.APIKey, m[1])
	case reIntervals.MatchString(path):
		m := reIntervals.FindStringSubmatch(path)
		return proxyIntervals(creds.APIKey, "/activity/"+m[1]+"/intervals", qp)
	case reMCP.MatchString(path):
		return handleMCP(creds.APIKey, req.Body)
	default:
		return shared.ErrResp(404, "endpoint not found"), nil
	}
}

func proxyIntervals(apiKey, path string, qp map[string]string) (events.LambdaFunctionURLResponse, error) {
	return shared.Proxy(intervalsBase, path, qp, func(r *http.Request) {
		r.SetBasicAuth("API_KEY", apiKey)
	})
}

func fetchStreams(apiKey, activityID string) (events.LambdaFunctionURLResponse, error) {
	types := "time,distance,heartrate,watts,velocity_smooth,altitude,cadence"
	path := fmt.Sprintf("/activity/%s/streams?types=%s", activityID, types)

	resp, err := shared.Proxy(intervalsBase, path, nil, func(r *http.Request) {
		r.SetBasicAuth("API_KEY", apiKey)
	})
	if err != nil {
		return resp, err
	}
	if resp.StatusCode != http.StatusOK {
		return resp, nil
	}

	var streams []shared.RawStream
	if err := json.Unmarshal([]byte(resp.Body), &streams); err != nil {
		return shared.ErrResp(502, "failed to decode streams: "+err.Error()), nil
	}

	body, _ := json.Marshal(shared.CondenseStreams(streams, maxStreamPts))
	return events.LambdaFunctionURLResponse{
		StatusCode: 200,
		Headers:    map[string]string{"Content-Type": "application/json"},
		Body:       string(body),
	}, nil
}

func main() {
	lambda.Start(handler)
}
