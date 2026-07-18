package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/jack/strava-claude/shared"
)

const (
	stravaBase   = "https://www.strava.com/api/v3"
	tokenURL     = "https://www.strava.com/oauth/token"
	maxStreamPts = 200
)

var (
	reActivities     = regexp.MustCompile(`^/activities$`)
	reActivityDetail = regexp.MustCompile(`^/activities/(\d+)$`)
	reStreams        = regexp.MustCompile(`^/activities/(\d+)/streams$`)
	reLaps           = regexp.MustCompile(`^/activities/(\d+)/laps$`)
	reMCP            = regexp.MustCompile(`^/mcp$`)
)

// Secret layout in Secrets Manager: strava/oauth
// {
//   "client_id":     "...",
//   "client_secret": "...",
//   "refresh_token": "...",
//   "skill_auth_key": "..."   ← simple shared secret for the Function URL
// }
type credentials struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	RefreshToken string `json:"refresh_token"`
	SkillAuthKey string `json:"skill_auth_key"`
}

type tokenResponse struct {
	AccessToken string `json:"access_token"`
}

var corsHeaders = map[string]string{
	"Access-Control-Allow-Origin":  "*",
	"Access-Control-Allow-Methods": "GET, OPTIONS",
	"Access-Control-Allow-Headers": "Content-Type, x-claude-secret",
}

func handler(ctx context.Context, req events.LambdaFunctionURLRequest) (events.LambdaFunctionURLResponse, error) {
	if req.RequestContext.HTTP.Method == http.MethodOptions {
		return events.LambdaFunctionURLResponse{StatusCode: 200, Headers: corsHeaders}, nil
	}

	var creds credentials
	if err := shared.LoadSecret(ctx, "SECRET_ARN", &creds); err != nil {
		return shared.ErrResp(500, "failed to load credentials: "+err.Error()), nil
	}

	if !shared.Authorized(req, creds.SkillAuthKey) {
		return shared.ErrResp(401, "unauthorized"), nil
	}

	accessToken, err := refreshAccessToken(&creds)
	if err != nil {
		return shared.ErrResp(500, "token refresh failed: "+err.Error()), nil
	}

	path := req.RawPath
	qp := req.QueryStringParameters

	switch {
	case reActivities.MatchString(path):
		return proxyStrava(accessToken, "/athlete/activities", qp)
	case reActivityDetail.MatchString(path):
		m := reActivityDetail.FindStringSubmatch(path)
		return proxyStrava(accessToken, "/activities/"+m[1], qp)
	case reStreams.MatchString(path):
		m := reStreams.FindStringSubmatch(path)
		return fetchStreams(accessToken, m[1])
	case reLaps.MatchString(path):
		m := reLaps.FindStringSubmatch(path)
		return proxyStrava(accessToken, "/activities/"+m[1]+"/laps", qp)
	case reMCP.MatchString(path):
		return handleMCP(accessToken, req.Body)
	default:
		return shared.ErrResp(404, "endpoint not found"), nil
	}
}

func refreshAccessToken(c *credentials) (string, error) {
	resp, err := http.PostForm(tokenURL, url.Values{
		"client_id":     {c.ClientID},
		"client_secret": {c.ClientSecret},
		"refresh_token": {c.RefreshToken},
		"grant_type":    {"refresh_token"},
	})
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var tok tokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tok); err != nil || tok.AccessToken == "" {
		return "", fmt.Errorf("unexpected token response")
	}
	return tok.AccessToken, nil
}

func proxyStrava(accessToken, path string, qp map[string]string) (events.LambdaFunctionURLResponse, error) {
	return shared.Proxy(stravaBase, path, qp, func(r *http.Request) {
		r.Header.Set("Authorization", "Bearer "+accessToken)
	})
}

func fetchStreams(accessToken, activityID string) (events.LambdaFunctionURLResponse, error) {
	keys := "time,distance,heartrate,velocity_smooth,altitude,cadence,watts,temp"
	path := fmt.Sprintf("/activities/%s/streams?keys=%s&key_by_type=true", activityID, keys)

	resp, err := shared.Proxy(stravaBase, path, nil, func(r *http.Request) {
		r.Header.Set("Authorization", "Bearer "+accessToken)
	})
	if err != nil {
		return resp, err
	}
	if resp.StatusCode != http.StatusOK {
		return resp, nil
	}

	var streamsMap map[string]shared.RawStream
	if err := json.Unmarshal([]byte(resp.Body), &streamsMap); err != nil {
		return shared.ErrResp(502, "failed to decode streams: "+err.Error()), nil
	}
	streams := make([]shared.RawStream, 0, len(streamsMap))
	for typ, s := range streamsMap {
		s.Type = typ
		streams = append(streams, s)
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
