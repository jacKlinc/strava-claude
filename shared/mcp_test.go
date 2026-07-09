package shared

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/aws/aws-lambda-go/events"
)

var errTransport = errors.New("connection refused")

func okResponse(body string) events.LambdaFunctionURLResponse {
	return events.LambdaFunctionURLResponse{StatusCode: 200, Body: body}
}

func errResponse(code int, body string) events.LambdaFunctionURLResponse {
	return events.LambdaFunctionURLResponse{StatusCode: code, Body: body}
}

var stubTools = []map[string]interface{}{
	{"name": "echo", "description": "echoes its input", "inputSchema": map[string]interface{}{"type": "object"}},
}

func stubDispatch(name string, args map[string]interface{}) (interface{}, *MCPRPCError) {
	if name == "no_such_tool" {
		return nil, UnknownToolError(name)
	}
	return map[string]interface{}{"content": []map[string]interface{}{{"type": "text", "text": "ok"}}}, nil
}

func mustUnmarshal(t *testing.T, body string) map[string]interface{} {
	t.Helper()
	var out map[string]interface{}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("response body is not valid JSON: %s", body)
	}
	return out
}

func TestHandleMCPParseError(t *testing.T) {
	resp, err := HandleMCP("{not json", "test-mcp", stubTools, stubDispatch)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := mustUnmarshal(t, resp.Body)
	rpcErr, ok := out["error"].(map[string]interface{})
	if !ok || int(rpcErr["code"].(float64)) != -32700 {
		t.Fatalf("expected -32700 parse error, got %v", out)
	}
}

func TestHandleMCPNotification(t *testing.T) {
	resp, err := HandleMCP(`{"jsonrpc":"2.0","method":"notifications/initialized"}`, "test-mcp", stubTools, stubDispatch)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != 202 {
		t.Fatalf("expected 202 for notification, got %d", resp.StatusCode)
	}
	if resp.Body != "" {
		t.Fatalf("expected empty body for notification ack, got %q", resp.Body)
	}
}

func TestHandleMCPUnknownMethod(t *testing.T) {
	resp, err := HandleMCP(`{"jsonrpc":"2.0","id":1,"method":"no_such_method"}`, "test-mcp", stubTools, stubDispatch)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := mustUnmarshal(t, resp.Body)
	rpcErr, ok := out["error"].(map[string]interface{})
	if !ok || int(rpcErr["code"].(float64)) != -32601 {
		t.Fatalf("expected -32601 method not found, got %v", out)
	}
}

func TestHandleMCPPing(t *testing.T) {
	resp, err := HandleMCP(`{"jsonrpc":"2.0","id":1,"method":"ping"}`, "test-mcp", stubTools, stubDispatch)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := mustUnmarshal(t, resp.Body)
	if _, ok := out["result"]; !ok {
		t.Fatalf("expected empty result object, got %v", out)
	}
}

func TestHandleMCPInitialize(t *testing.T) {
	resp, err := HandleMCP(`{"jsonrpc":"2.0","id":1,"method":"initialize"}`, "test-mcp", stubTools, stubDispatch)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := mustUnmarshal(t, resp.Body)
	result := out["result"].(map[string]interface{})
	if result["protocolVersion"] != "2025-03-26" {
		t.Fatalf("unexpected protocolVersion: %v", result["protocolVersion"])
	}
	serverInfo := result["serverInfo"].(map[string]interface{})
	if serverInfo["name"] != "test-mcp" {
		t.Fatalf("unexpected serverInfo.name: %v", serverInfo["name"])
	}
}

func TestHandleMCPToolsList(t *testing.T) {
	resp, err := HandleMCP(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`, "test-mcp", stubTools, stubDispatch)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := mustUnmarshal(t, resp.Body)
	result := out["result"].(map[string]interface{})
	tools := result["tools"].([]interface{})
	if len(tools) != 1 || tools[0].(map[string]interface{})["name"] != "echo" {
		t.Fatalf("expected the stub tool list to pass through unchanged, got %v", tools)
	}
}

func TestHandleMCPToolsCallDispatches(t *testing.T) {
	resp, err := HandleMCP(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"echo","arguments":{}}}`, "test-mcp", stubTools, stubDispatch)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := mustUnmarshal(t, resp.Body)
	result := out["result"].(map[string]interface{})
	content := result["content"].([]interface{})
	if content[0].(map[string]interface{})["text"] != "ok" {
		t.Fatalf("expected dispatch's result to flow through, got %v", out)
	}
}

func TestHandleMCPToolsCallUnknownTool(t *testing.T) {
	resp, err := HandleMCP(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"no_such_tool","arguments":{}}}`, "test-mcp", stubTools, stubDispatch)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := mustUnmarshal(t, resp.Body)
	rpcErr, ok := out["error"].(map[string]interface{})
	if !ok || int(rpcErr["code"].(float64)) != -32602 {
		t.Fatalf("expected -32602 for unknown tool, got %v", out)
	}
}

func TestWrapProxyResultSuccess(t *testing.T) {
	result, rpcErr := WrapProxyResult(okResponse(`{"ok":true}`), nil)
	if rpcErr != nil {
		t.Fatalf("unexpected error: %v", rpcErr)
	}
	m := result.(map[string]interface{})
	if m["isError"] != nil {
		t.Fatalf("expected no isError field on success, got %v", m)
	}
}

func TestWrapProxyResultUpstreamError(t *testing.T) {
	result, rpcErr := WrapProxyResult(errResponse(404, "not found"), nil)
	if rpcErr != nil {
		t.Fatalf("unexpected rpc error: %v", rpcErr)
	}
	m := result.(map[string]interface{})
	if m["isError"] != true {
		t.Fatalf("expected isError true for non-200 upstream status, got %v", m)
	}
}

func TestWrapProxyResultTransportError(t *testing.T) {
	_, rpcErr := WrapProxyResult(okResponse(""), errTransport)
	if rpcErr == nil || rpcErr.Code != -32603 {
		t.Fatalf("expected -32603 for transport error, got %v", rpcErr)
	}
}
