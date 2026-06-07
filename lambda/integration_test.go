//go:build integration

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
)

// Run with: go test -v -tags integration ./...
// Required env vars: STRAVA_BASE_URL, STRAVA_SECRET

func apiClient(t *testing.T) (baseURL, secret string) {
	t.Helper()
	baseURL = os.Getenv("STRAVA_BASE_URL")
	secret = os.Getenv("STRAVA_SECRET")
	if baseURL == "" || secret == "" {
		t.Fatal("STRAVA_BASE_URL and STRAVA_SECRET must be set")
	}
	baseURL = strings.TrimRight(baseURL, "/")
	return
}

func get(t *testing.T, baseURL, secret, path string) map[string]interface{} {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, baseURL+path, nil)
	req.Header.Set("x-claude-secret", secret)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: status %d, body: %s", path, resp.StatusCode, body)
	}

	var out map[string]interface{}
	if err := json.Unmarshal(body, &out); err != nil {
		// might be an array — wrap it so callers get a consistent type
		var arr []interface{}
		if err2 := json.Unmarshal(body, &arr); err2 != nil {
			t.Fatalf("GET %s: non-JSON response: %s", path, body)
		}
		return map[string]interface{}{"_array": arr}
	}
	if errMsg, ok := out["message"]; ok {
		t.Fatalf("GET %s: API error: %v", path, errMsg)
	}
	return out
}

func TestActivitiesList(t *testing.T) {
	baseURL, secret := apiClient(t)

	raw := get(t, baseURL, secret, "/activities?per_page=1")
	arr, ok := raw["_array"].([]interface{})
	if !ok || len(arr) == 0 {
		t.Fatal("/activities returned no activities")
	}
	activity := arr[0].(map[string]interface{})
	t.Logf("most recent activity: %v — %v", activity["name"], activity["start_date_local"])
}

func TestActivityDetail(t *testing.T) {
	baseURL, secret := apiClient(t)

	id := firstActivityID(t, baseURL, secret)
	result := get(t, baseURL, secret, fmt.Sprintf("/activities/%s", id))
	if result["id"] == nil {
		t.Fatal("/activities/:id response missing id field")
	}
	t.Logf("activity detail: %v (%v m, %v s)", result["name"], result["distance"], result["moving_time"])
}

func TestActivityStreams(t *testing.T) {
	baseURL, secret := apiClient(t)

	id := firstActivityID(t, baseURL, secret)
	result := get(t, baseURL, secret, fmt.Sprintf("/activities/%s/streams", id))

	if result["streams"] == nil {
		t.Fatal("/streams response missing streams field")
	}
	t.Logf("streams: original_size=%v, num_samples=%v, sample_every=%v",
		result["original_size"], result["num_samples"], result["sample_every"])
}

func TestActivityLaps(t *testing.T) {
	baseURL, secret := apiClient(t)

	id := firstActivityID(t, baseURL, secret)
	raw := get(t, baseURL, secret, fmt.Sprintf("/activities/%s/laps", id))
	arr, ok := raw["_array"].([]interface{})
	if !ok || len(arr) == 0 {
		t.Fatal("/laps returned no laps")
	}
	t.Logf("laps: %d laps for activity %s", len(arr), id)
}

func TestUnauthorized(t *testing.T) {
	baseURL, _ := apiClient(t)

	req, _ := http.NewRequest(http.MethodGet, baseURL+"/activities", nil)
	req.Header.Set("x-claude-secret", "wrong-secret")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

// firstActivityID fetches the most recent activity and returns its ID as a string.
func firstActivityID(t *testing.T, baseURL, secret string) string {
	t.Helper()
	raw := get(t, baseURL, secret, "/activities?per_page=1")
	arr, ok := raw["_array"].([]interface{})
	if !ok || len(arr) == 0 {
		t.Fatal("no activities found")
	}
	activity := arr[0].(map[string]interface{})
	// JSON numbers decode to float64; use %d on the int64 value to avoid scientific notation.
	return fmt.Sprintf("%d", int64(activity["id"].(float64)))
}
