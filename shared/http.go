package shared

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"

	"github.com/aws/aws-lambda-go/events"
)

// Proxy performs a GET against baseURL+path with the given query params,
// applying setAuth to attach whatever auth scheme the upstream needs, and
// returns the raw upstream response as a Lambda Function URL response.
func Proxy(baseURL, path string, qp map[string]string, setAuth func(*http.Request)) (events.LambdaFunctionURLResponse, error) {
	u, _ := url.Parse(baseURL + path)
	if len(qp) > 0 {
		q := u.Query()
		for k, v := range qp {
			q.Set(k, v)
		}
		u.RawQuery = q.Encode()
	}

	req, _ := http.NewRequest(http.MethodGet, u.String(), nil)
	setAuth(req)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return ErrResp(502, err.Error()), nil
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	return events.LambdaFunctionURLResponse{
		StatusCode: resp.StatusCode,
		Headers:    map[string]string{"Content-Type": "application/json"},
		Body:       string(body),
	}, nil
}

// ErrResp builds a uniform {"error": msg} JSON response.
func ErrResp(code int, msg string) events.LambdaFunctionURLResponse {
	body, _ := json.Marshal(map[string]string{"error": msg})
	return events.LambdaFunctionURLResponse{
		StatusCode: code,
		Headers:    map[string]string{"Content-Type": "application/json"},
		Body:       string(body),
	}
}
