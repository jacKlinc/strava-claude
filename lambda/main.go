package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"regexp"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
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

type rawStream struct {
	Type         string        `json:"type"`
	Data         []json.Number `json:"data"`
	OriginalSize int           `json:"original_size"`
	SeriesType   string        `json:"series_type"`
	Resolution   string        `json:"resolution"`
}

type condensedStreams struct {
	OriginalSize int                    `json:"original_size"`
	NumSamples   int                    `json:"num_samples"`
	SampleEvery  int                    `json:"sample_every"`
	Streams      map[string]interface{} `json:"streams"`
}

func handler(ctx context.Context, req events.LambdaFunctionURLRequest) (events.LambdaFunctionURLResponse, error) {
	creds, err := loadCredentials(ctx)
	if err != nil {
		return errResp(500, "failed to load credentials: "+err.Error()), nil
	}

	if creds.SkillAuthKey != "" && req.Headers["x-claude-secret"] != creds.SkillAuthKey {
		return errResp(401, "unauthorized"), nil
	}

	accessToken, err := refreshAccessToken(creds)
	if err != nil {
		return errResp(500, "token refresh failed: "+err.Error()), nil
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
	default:
		return errResp(404, "endpoint not found"), nil
	}
}

func loadCredentials(ctx context.Context) (*credentials, error) {
	cfg, err := awsconfig.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, err
	}

	sm := secretsmanager.NewFromConfig(cfg)
	out, err := sm.GetSecretValue(ctx, &secretsmanager.GetSecretValueInput{
		SecretId: aws.String(os.Getenv("SECRET_ARN")),
	})
	if err != nil {
		return nil, err
	}

	var c credentials
	return &c, json.Unmarshal([]byte(aws.ToString(out.SecretString)), &c)
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

	body, _ := io.ReadAll(resp.Body)
	var tok tokenResponse
	if err := json.Unmarshal(body, &tok); err != nil || tok.AccessToken == "" {
		return "", fmt.Errorf("unexpected token response: %s", body)
	}
	return tok.AccessToken, nil
}

func proxyStrava(accessToken, path string, qp map[string]string) (events.LambdaFunctionURLResponse, error) {
	u, _ := url.Parse(stravaBase + path)
	if len(qp) > 0 {
		q := u.Query()
		for k, v := range qp {
			q.Set(k, v)
		}
		u.RawQuery = q.Encode()
	}

	req, _ := http.NewRequest(http.MethodGet, u.String(), nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return errResp(502, err.Error()), nil
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	return events.LambdaFunctionURLResponse{
		StatusCode: resp.StatusCode,
		Headers:    map[string]string{"Content-Type": "application/json"},
		Body:       string(body),
	}, nil
}

func fetchStreams(accessToken, activityID string) (events.LambdaFunctionURLResponse, error) {
	keys := "time,distance,heartrate,velocity_smooth,altitude,cadence,watts,temp"
	path := fmt.Sprintf("/activities/%s/streams?keys=%s&key_by_type=true", activityID, keys)

	req, _ := http.NewRequest(http.MethodGet, stravaBase+path, nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return errResp(502, err.Error()), nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return events.LambdaFunctionURLResponse{
			StatusCode: resp.StatusCode,
			Headers:    map[string]string{"Content-Type": "application/json"},
			Body:       string(body),
		}, nil
	}

	var streamsMap map[string]rawStream
	if err := json.NewDecoder(resp.Body).Decode(&streamsMap); err != nil {
		return errResp(502, "failed to decode streams: "+err.Error()), nil
	}
	streams := make([]rawStream, 0, len(streamsMap))
	for typ, s := range streamsMap {
		s.Type = typ
		streams = append(streams, s)
	}

	body, _ := json.Marshal(condenseStreams(streams))
	return events.LambdaFunctionURLResponse{
		StatusCode: 200,
		Headers:    map[string]string{"Content-Type": "application/json"},
		Body:       string(body),
	}, nil
}

// condenseStreams downsamples all stream arrays to at most maxStreamPts points,
// saving Claude's context window on long runs.
func condenseStreams(streams []rawStream) condensedStreams {
	if len(streams) == 0 {
		return condensedStreams{}
	}

	originalSize := 0
	for _, s := range streams {
		if s.OriginalSize > originalSize {
			originalSize = s.OriginalSize
		}
		if len(s.Data) > originalSize {
			originalSize = len(s.Data)
		}
	}

	step := 1
	if originalSize > maxStreamPts {
		step = int(math.Ceil(float64(originalSize) / maxStreamPts))
	}

	result := make(map[string]interface{}, len(streams))
	for _, s := range streams {
		sampled := make([]json.Number, 0, (len(s.Data)/step)+1)
		for i := 0; i < len(s.Data); i += step {
			sampled = append(sampled, s.Data[i])
		}
		result[s.Type] = sampled
	}

	numSamples := (originalSize + step - 1) / step

	return condensedStreams{
		OriginalSize: originalSize,
		NumSamples:   numSamples,
		SampleEvery:  step,
		Streams:      result,
	}
}

func errResp(code int, msg string) events.LambdaFunctionURLResponse {
	body, _ := json.Marshal(map[string]string{"error": msg})
	return events.LambdaFunctionURLResponse{
		StatusCode: code,
		Headers:    map[string]string{"Content-Type": "application/json"},
		Body:       string(body),
	}
}

func main() {
	lambda.Start(handler)
}
