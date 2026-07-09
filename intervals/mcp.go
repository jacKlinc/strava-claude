package main

import (
	"github.com/aws/aws-lambda-go/events"
	"github.com/jack/strava-claude/shared"
)

var mcpTools = []map[string]interface{}{
	{
		"name":        "list_activities",
		"description": "List recent intervals.icu activities (synced from Garmin).",
		"inputSchema": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"oldest": map[string]interface{}{"type": "string", "description": "ISO-8601 date, only activities on/after this date"},
				"newest": map[string]interface{}{"type": "string", "description": "ISO-8601 date, only activities on/before this date"},
			},
		},
	},
	{
		"name":        "get_activity",
		"description": "Full detail for a single intervals.icu activity.",
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
		"description": "Downsampled time-series for an activity (HR, power, pace, altitude, cadence). Returns ≤200 samples with sample_every indicating the stride.",
		"inputSchema": map[string]interface{}{
			"type":     "object",
			"required": []string{"id"},
			"properties": map[string]interface{}{
				"id": map[string]interface{}{"type": "string", "description": "Activity ID"},
			},
		},
	},
	{
		"name":        "get_intervals",
		"description": "intervals.icu's auto-detected interval segments for an activity, with per-interval average power/HR/pace.",
		"inputSchema": map[string]interface{}{
			"type":     "object",
			"required": []string{"id"},
			"properties": map[string]interface{}{
				"id": map[string]interface{}{"type": "string", "description": "Activity ID"},
			},
		},
	},
}

func handleMCP(apiKey, body string) (events.LambdaFunctionURLResponse, error) {
	return shared.HandleMCP(body, "intervals-icu-mcp", mcpTools, func(name string, args map[string]interface{}) (interface{}, *shared.MCPRPCError) {
		return dispatchTool(apiKey, name, args)
	})
}

func dispatchTool(apiKey, name string, args map[string]interface{}) (interface{}, *shared.MCPRPCError) {
	switch name {
	case "list_activities":
		qp := map[string]string{}
		for _, k := range []string{"oldest", "newest"} {
			if v, ok := args[k]; ok {
				qp[k] = shared.FormatArg(v)
			}
		}
		return shared.WrapProxyResult(proxyIntervals(apiKey, "/athlete/0/activities", qp))
	case "get_activity":
		id, ok := shared.StringArg(args, "id")
		if !ok {
			return nil, shared.MissingArgError("id")
		}
		return shared.WrapProxyResult(proxyIntervals(apiKey, "/activity/"+id, nil))
	case "get_streams":
		id, ok := shared.StringArg(args, "id")
		if !ok {
			return nil, shared.MissingArgError("id")
		}
		return shared.WrapProxyResult(fetchStreams(apiKey, id))
	case "get_intervals":
		id, ok := shared.StringArg(args, "id")
		if !ok {
			return nil, shared.MissingArgError("id")
		}
		return shared.WrapProxyResult(proxyIntervals(apiKey, "/activity/"+id+"/intervals", nil))
	default:
		return nil, shared.UnknownToolError(name)
	}
}
