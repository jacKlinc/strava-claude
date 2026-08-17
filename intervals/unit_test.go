package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func mustUnmarshalMCP(t *testing.T, body string) map[string]interface{} {
	t.Helper()
	var out map[string]interface{}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("response body is not valid JSON: %s", body)
	}
	return out
}

func TestHandleMCPInitialize(t *testing.T) {
	resp, err := handleMCP("", `{"jsonrpc":"2.0","id":1,"method":"initialize"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := mustUnmarshalMCP(t, resp.Body)
	result := out["result"].(map[string]interface{})
	serverInfo := result["serverInfo"].(map[string]interface{})
	if serverInfo["name"] != "intervals-icu-mcp" {
		t.Fatalf("unexpected serverInfo.name: %v", serverInfo["name"])
	}
}

func TestHandleMCPToolsList(t *testing.T) {
	resp, err := handleMCP("", `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := mustUnmarshalMCP(t, resp.Body)
	result := out["result"].(map[string]interface{})
	tools := result["tools"].([]interface{})
	if len(tools) != 4 {
		t.Fatalf("expected 4 tools, got %d", len(tools))
	}
	want := map[string]bool{"list_activities": true, "get_activity": true, "get_streams": true, "get_intervals": true}
	for _, tool := range tools {
		name := tool.(map[string]interface{})["name"].(string)
		if !want[name] {
			t.Fatalf("unexpected tool name: %s", name)
		}
		delete(want, name)
	}
	if len(want) != 0 {
		t.Fatalf("missing expected tools: %v", want)
	}
}

// The activity tools must keep warning consumers that average_temp is a device
// sensor reading, not ambient temperature — see shared.AnnotateTemp.
func TestHandleMCPActivityToolsDocumentTempSource(t *testing.T) {
	resp, err := handleMCP("", `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := mustUnmarshalMCP(t, resp.Body)
	tools := out["result"].(map[string]interface{})["tools"].([]interface{})

	need := map[string]bool{"list_activities": true, "get_activity": true}
	for _, tool := range tools {
		m := tool.(map[string]interface{})
		name := m["name"].(string)
		if !need[name] {
			continue
		}
		delete(need, name)
		desc := m["description"].(string)
		for _, want := range []string{"temp_source", "average_weather_temp", "sensor"} {
			if !strings.Contains(desc, want) {
				t.Fatalf("tool %s: description missing %q: %s", name, want, desc)
			}
		}
	}
	if len(need) != 0 {
		t.Fatalf("tools not found in tools/list: %v", need)
	}
}

func TestDispatchToolUnknownTool(t *testing.T) {
	_, rpcErr := dispatchTool("", "no_such_tool", map[string]interface{}{})
	if rpcErr == nil || rpcErr.Code != -32602 {
		t.Fatalf("expected -32602 for unknown tool, got %v", rpcErr)
	}
}

func TestDispatchToolMissingID(t *testing.T) {
	for _, tool := range []string{"get_activity", "get_streams", "get_intervals"} {
		_, rpcErr := dispatchTool("", tool, map[string]interface{}{})
		if rpcErr == nil || rpcErr.Code != -32602 {
			t.Fatalf("tool %s: expected -32602 for missing id, got %v", tool, rpcErr)
		}
	}
}
