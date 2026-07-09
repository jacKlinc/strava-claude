package shared

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/aws/aws-lambda-go/events"
)

// MCPRPCError is a JSON-RPC 2.0 error object.
type MCPRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

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
	Error   *MCPRPCError    `json:"error,omitempty"`
}

type toolCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// Dispatch handles a single tools/call invocation, given the plain tool name
// and already-decoded arguments.
type Dispatch func(name string, args map[string]interface{}) (interface{}, *MCPRPCError)

// HandleMCP handles the JSON-RPC 2.0 envelope for an MCP server over a Lambda
// Function URL: request parsing, notifications (no id — 202 no body), and the
// initialize/tools-list/tools-call/ping methods. dispatch is invoked only for
// tools/call, with tool-call param parsing already done.
func HandleMCP(body, serverName string, tools []map[string]interface{}, dispatch Dispatch) (events.LambdaFunctionURLResponse, error) {
	var req mcpRequest
	if err := json.Unmarshal([]byte(body), &req); err != nil {
		return mcpErrorResp(nil, -32700, "parse error"), nil
	}

	// Notifications have no id — acknowledge with 202, no body.
	if req.ID == nil {
		return events.LambdaFunctionURLResponse{StatusCode: 202}, nil
	}

	var result interface{}
	var rpcErr *MCPRPCError

	switch req.Method {
	case "initialize":
		result = map[string]interface{}{
			"protocolVersion": "2025-03-26",
			"capabilities":    map[string]interface{}{"tools": map[string]interface{}{}},
			"serverInfo":      map[string]interface{}{"name": serverName, "version": "1.0"},
		}
	case "tools/list":
		result = map[string]interface{}{"tools": tools}
	case "tools/call":
		result, rpcErr = handleToolCall(req.Params, dispatch)
	case "ping":
		result = map[string]interface{}{}
	default:
		rpcErr = &MCPRPCError{Code: -32601, Message: "method not found: " + req.Method}
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

func handleToolCall(rawParams json.RawMessage, dispatch Dispatch) (interface{}, *MCPRPCError) {
	var p toolCallParams
	if err := json.Unmarshal(rawParams, &p); err != nil {
		return nil, &MCPRPCError{Code: -32602, Message: "invalid params"}
	}

	var args map[string]interface{}
	if len(p.Arguments) > 0 {
		json.Unmarshal(p.Arguments, &args) //nolint:errcheck — empty args is fine
	}
	if args == nil {
		args = map[string]interface{}{}
	}

	return dispatch(p.Name, args)
}

func mcpErrorResp(id json.RawMessage, code int, msg string) events.LambdaFunctionURLResponse {
	b, _ := json.Marshal(mcpResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &MCPRPCError{Code: code, Message: msg},
	})
	return events.LambdaFunctionURLResponse{
		StatusCode: 200,
		Headers:    map[string]string{"Content-Type": "application/json"},
		Body:       string(b),
	}
}

// WrapProxyResult converts the result of a proxied upstream call into an MCP
// tools/call result: a transport error becomes a JSON-RPC internal error, a
// non-200 upstream status becomes an isError content block, and success wraps
// the raw body as a text content block.
func WrapProxyResult(resp events.LambdaFunctionURLResponse, err error) (interface{}, *MCPRPCError) {
	if err != nil {
		return nil, &MCPRPCError{Code: -32603, Message: err.Error()}
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

// StringArg returns the named arg as a string, handling both string and numeric JSON values.
func StringArg(args map[string]interface{}, key string) (string, bool) {
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

// FormatArg converts a JSON-decoded value to a query-parameter string.
// float64 integers are formatted without a decimal point.
func FormatArg(v interface{}) string {
	if f, ok := v.(float64); ok {
		return strconv.FormatInt(int64(f), 10)
	}
	return fmt.Sprintf("%v", v)
}

// MissingArgError builds the standard "required arg missing" JSON-RPC error.
func MissingArgError(name string) *MCPRPCError {
	return &MCPRPCError{Code: -32602, Message: name + " is required"}
}

// UnknownToolError builds the standard "no such tool" JSON-RPC error.
func UnknownToolError(name string) *MCPRPCError {
	return &MCPRPCError{Code: -32602, Message: "unknown tool: " + name}
}
