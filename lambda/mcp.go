package main

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/aws/aws-lambda-go/events"
)

// mcpRequest is a JSON-RPC 2.0 request. ID is nil for notifications (no response expected).
type mcpRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type mcpResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  interface{}     `json:"result,omitempty"`
	Error   *mcpRPCError    `json:"error,omitempty"`
}

type mcpRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type toolCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

var mcpTools = []map[string]interface{}{
	{
		"name":        "list_activities",
		"description": "List recent Strava activities.",
		"inputSchema": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"per_page": map[string]interface{}{"type": "integer", "description": "Number of activities (default 5, max 30)"},
				"page":     map[string]interface{}{"type": "integer", "description": "Page number"},
				"before":   map[string]interface{}{"type": "integer", "description": "Only activities before this Unix epoch"},
				"after":    map[string]interface{}{"type": "integer", "description": "Only activities after this Unix epoch"},
			},
		},
	},
	{
		"name":        "get_activity",
		"description": "Full detail for a single Strava activity: splits, gear, segment efforts.",
		"inputSchema": map[string]interface{}{
			"type":     "object",
			"required": []string{"id"},
			"properties": map[string]interface{}{
				"id": map[string]interface{}{"type": "string", "description": "Activity ID"},
			},
		},
	},
	{
		"name":        "get_streams",
		"description": "Downsampled time-series for an activity (HR, pace, altitude, cadence, etc). Returns ≤200 samples with sample_every indicating the stride.",
		"inputSchema": map[string]interface{}{
			"type":     "object",
			"required": []string{"id"},
			"properties": map[string]interface{}{
				"id": map[string]interface{}{"type": "string", "description": "Activity ID"},
			},
		},
	},
	{
		"name":        "get_laps",
		"description": "Lap-by-lap splits for a Strava activity.",
		"inputSchema": map[string]interface{}{
			"type":     "object",
			"required": []string{"id"},
			"properties": map[string]interface{}{
				"id": map[string]interface{}{"type": "string", "description": "Activity ID"},
			},
		},
	},
}

func handleMCP(accessToken, body string) (events.LambdaFunctionURLResponse, error) {
	var req mcpRequest
	if err := json.Unmarshal([]byte(body), &req); err != nil {
		return mcpErrorResp(nil, -32700, "parse error"), nil
	}

	// Notifications have no id — acknowledge with 202, no body.
	if req.ID == nil {
		return events.LambdaFunctionURLResponse{StatusCode: 202}, nil
	}

	var result interface{}
	var rpcErr *mcpRPCError

	switch req.Method {
	case "initialize":
		result = map[string]interface{}{
			"protocolVersion": "2025-03-26",
			"capabilities":    map[string]interface{}{"tools": map[string]interface{}{}},
			"serverInfo":      map[string]interface{}{"name": "strava-mcp", "version": "1.0"},
		}
	case "tools/list":
		result = map[string]interface{}{"tools": mcpTools}
	case "tools/call":
		result, rpcErr = dispatchTool(accessToken, req.Params)
	case "ping":
		result = map[string]interface{}{}
	default:
		rpcErr = &mcpRPCError{Code: -32601, Message: "method not found: " + req.Method}
	}

	resp := mcpResponse{JSONRPC: "2.0", ID: req.ID}
	if rpcErr != nil {
		resp.Error = rpcErr
	} else {
		resp.Result = result
	}

	b, _ := json.Marshal(resp)
	return events.LambdaFunctionURLResponse{
		StatusCode: 200,
		Headers:    map[string]string{"Content-Type": "application/json"},
		Body:       string(b),
	}, nil
}

func dispatchTool(accessToken string, rawParams json.RawMessage) (interface{}, *mcpRPCError) {
	var p toolCallParams
	if err := json.Unmarshal(rawParams, &p); err != nil {
		return nil, &mcpRPCError{Code: -32602, Message: "invalid params"}
	}

	var args map[string]interface{}
	if len(p.Arguments) > 0 {
		json.Unmarshal(p.Arguments, &args) //nolint:errcheck — empty args is fine
	}
	if args == nil {
		args = map[string]interface{}{}
	}

	var resp events.LambdaFunctionURLResponse
	var err error

	switch p.Name {
	case "list_activities":
		qp := map[string]string{}
		for _, k := range []string{"per_page", "page", "before", "after"} {
			if v, ok := args[k]; ok {
				qp[k] = formatArg(v)
			}
		}
		resp, err = proxyStrava(accessToken, "/athlete/activities", qp)
	case "get_activity":
		id, ok := stringArg(args, "id")
		if !ok {
			return nil, &mcpRPCError{Code: -32602, Message: "id is required"}
		}
		resp, err = proxyStrava(accessToken, "/activities/"+id, nil)
	case "get_streams":
		id, ok := stringArg(args, "id")
		if !ok {
			return nil, &mcpRPCError{Code: -32602, Message: "id is required"}
		}
		resp, err = fetchStreams(accessToken, id)
	case "get_laps":
		id, ok := stringArg(args, "id")
		if !ok {
			return nil, &mcpRPCError{Code: -32602, Message: "id is required"}
		}
		resp, err = proxyStrava(accessToken, "/activities/"+id+"/laps", nil)
	default:
		return nil, &mcpRPCError{Code: -32602, Message: "unknown tool: " + p.Name}
	}

	if err != nil {
		return nil, &mcpRPCError{Code: -32603, Message: err.Error()}
	}
	if resp.StatusCode != 200 {
		return map[string]interface{}{
			"content": []map[string]interface{}{{"type": "text", "text": resp.Body}},
			"isError": true,
		}, nil
	}
	return map[string]interface{}{
		"content": []map[string]interface{}{{"type": "text", "text": resp.Body}},
	}, nil
}

func mcpErrorResp(id json.RawMessage, code int, msg string) events.LambdaFunctionURLResponse {
	b, _ := json.Marshal(mcpResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &mcpRPCError{Code: code, Message: msg},
	})
	return events.LambdaFunctionURLResponse{
		StatusCode: 200,
		Headers:    map[string]string{"Content-Type": "application/json"},
		Body:       string(b),
	}
}

// stringArg returns the named arg as a string, handling both string and numeric JSON values.
func stringArg(args map[string]interface{}, key string) (string, bool) {
	v, ok := args[key]
	if !ok {
		return "", false
	}
	switch t := v.(type) {
	case string:
		return t, true
	case float64:
		return strconv.FormatInt(int64(t), 10), true
	default:
		return fmt.Sprintf("%v", v), true
	}
}

// formatArg converts a JSON-decoded value to a query-parameter string.
// float64 integers are formatted without a decimal point.
func formatArg(v interface{}) string {
	if f, ok := v.(float64); ok {
		return strconv.FormatInt(int64(f), 10)
	}
	return fmt.Sprintf("%v", v)
}
