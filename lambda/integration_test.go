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

func TestQueryParamAuth(t *testing.T) {
	baseURL, secret := apiClient(t)

	// Auth via ?key= query param (used by Claude.ai MCP connector which has no custom header support).
	body, _ := json.Marshal(map[string]interface{}{
		"jsonrpc": "2.0", "id": 1, "method": "tools/list",
	})
	req, _ := http.NewRequest(http.MethodPost, baseURL+"/mcp?key="+secret, strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	// Deliberately no x-claude-secret header.

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, b)
	}
}

// ── MCP tests ────────────────────────────────────────────────────────────────

func mcpCall(t *testing.T, baseURL, secret, method string, params interface{}) map[string]interface{} {
	t.Helper()
	body, _ := json.Marshal(map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  method,
		"params":  params,
	})
	req, _ := http.NewRequest(http.MethodPost, baseURL+"/mcp", strings.NewReader(string(body)))
	req.Header.Set("x-claude-secret", secret)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("MCP %s: %v", method, err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("MCP %s: HTTP %d: %s", method, resp.StatusCode, raw)
	}

	var out map[string]interface{}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("MCP %s: non-JSON: %s", method, raw)
	}
	if rpcErr, ok := out["error"]; ok {
		t.Fatalf("MCP %s: RPC error: %v", method, rpcErr)
	}
	return out["result"].(map[string]interface{})
}

func mcpToolCall(t *testing.T, baseURL, secret, tool string, args map[string]interface{}) string {
	t.Helper()
	result := mcpCall(t, baseURL, secret, "tools/call", map[string]interface{}{
		"name":      tool,
		"arguments": args,
	})
	if result["isError"] == true {
		t.Fatalf("tool %s returned isError: %v", tool, result["content"])
	}
	content := result["content"].([]interface{})
	if len(content) == 0 {
		t.Fatalf("tool %s: empty content", tool)
	}
	return content[0].(map[string]interface{})["text"].(string)
}

func TestMCPInitialize(t *testing.T) {
	baseURL, secret := apiClient(t)
	result := mcpCall(t, baseURL, secret, "initialize", map[string]interface{}{
		"protocolVersion": "2025-03-26",
		"capabilities":    map[string]interface{}{},
		"clientInfo":      map[string]interface{}{"name": "test", "version": "1.0"},
	})
	if result["protocolVersion"] != "2025-03-26" {
		t.Fatalf("unexpected protocolVersion: %v", result["protocolVersion"])
	}
	t.Logf("server: %v", result["serverInfo"])
}

func TestMCPToolsList(t *testing.T) {
	baseURL, secret := apiClient(t)
	result := mcpCall(t, baseURL, secret, "tools/list", nil)
	tools := result["tools"].([]interface{})
	if len(tools) != 4 {
		t.Fatalf("expected 4 tools, got %d", len(tools))
	}
	names := make([]string, len(tools))
	for i, tool := range tools {
		names[i] = tool.(map[string]interface{})["name"].(string)
	}
	t.Logf("tools: %v", names)
}

func TestMCPListActivities(t *testing.T) {
	baseURL, secret := apiClient(t)
	text := mcpToolCall(t, baseURL, secret, "list_activities", map[string]interface{}{"per_page": 1})
	var activities []interface{}
	if err := json.Unmarshal([]byte(text), &activities); err != nil || len(activities) == 0 {
		t.Fatalf("list_activities: expected JSON array, got: %s", text)
	}
	a := activities[0].(map[string]interface{})
	t.Logf("most recent: %v — %v", a["name"], a["start_date_local"])
}

func TestMCPGetActivity(t *testing.T) {
	baseURL, secret := apiClient(t)
	id := firstActivityID(t, baseURL, secret)
	text := mcpToolCall(t, baseURL, secret, "get_activity", map[string]interface{}{"id": id})
	var activity map[string]interface{}
	if err := json.Unmarshal([]byte(text), &activity); err != nil || activity["id"] == nil {
		t.Fatalf("get_activity: expected activity JSON, got: %s", text)
	}
	t.Logf("activity: %v (%v m)", activity["name"], activity["distance"])
}

func TestMCPGetStreams(t *testing.T) {
	baseURL, secret := apiClient(t)
	id := firstActivityID(t, baseURL, secret)
	text := mcpToolCall(t, baseURL, secret, "get_streams", map[string]interface{}{"id": id})
	var streams map[string]interface{}
	if err := json.Unmarshal([]byte(text), &streams); err != nil || streams["streams"] == nil {
		t.Fatalf("get_streams: expected streams JSON, got: %s", text)
	}
	t.Logf("streams: original_size=%v, num_samples=%v", streams["original_size"], streams["num_samples"])
}

func TestMCPGetLaps(t *testing.T) {
	baseURL, secret := apiClient(t)
	id := firstActivityID(t, baseURL, secret)
	text := mcpToolCall(t, baseURL, secret, "get_laps", map[string]interface{}{"id": id})
	var laps []interface{}
	if err := json.Unmarshal([]byte(text), &laps); err != nil || len(laps) == 0 {
		t.Fatalf("get_laps: expected JSON array, got: %s", text)
	}
	t.Logf("laps: %d", len(laps))
}

func TestMCPUnknownMethod(t *testing.T) {
	baseURL, secret := apiClient(t)
	body, _ := json.Marshal(map[string]interface{}{
		"jsonrpc": "2.0", "id": 1, "method": "no_such_method",
	})
	req, _ := http.NewRequest(http.MethodPost, baseURL+"/mcp", strings.NewReader(string(body)))
	req.Header.Set("x-claude-secret", secret)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&out)
	if out["error"] == nil {
		t.Fatalf("expected RPC error for unknown method, got: %v", out)
	}
}

func TestMCPUnknownTool(t *testing.T) {
	baseURL, secret := apiClient(t)
	body, _ := json.Marshal(map[string]interface{}{
		"jsonrpc": "2.0", "id": 1, "method": "tools/call",
		"params": map[string]interface{}{"name": "no_such_tool", "arguments": map[string]interface{}{}},
	})
	req, _ := http.NewRequest(http.MethodPost, baseURL+"/mcp", strings.NewReader(string(body)))
	req.Header.Set("x-claude-secret", secret)
	req.Header.Set("Content-Type", "application/json")
	resp, _ := http.DefaultClient.Do(req)
	defer resp.Body.Close()
	var out map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&out)
	if out["error"] == nil {
		t.Fatalf("expected RPC error for unknown tool, got: %v", out)
	}
}

func TestMCPNotification(t *testing.T) {
	baseURL, secret := apiClient(t)
	// Notifications have no id — server must return 202 with no body.
	body, _ := json.Marshal(map[string]interface{}{
		"jsonrpc": "2.0", "method": "notifications/initialized",
	})
	req, _ := http.NewRequest(http.MethodPost, baseURL+"/mcp", strings.NewReader(string(body)))
	req.Header.Set("x-claude-secret", secret)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("expected 202 for notification, got %d", resp.StatusCode)
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
