package service

import (
	"encoding/json"
	"fmt"
)

// ToolResult is the result of an MCP tool call.
type ToolResult struct {
	Content string
	IsError bool
}

// CallTool is the unified tool dispatch entry point used by the MCP server.
func (s *AIService) CallTool(name string, args map[string]any) ToolResult {
	var raw string

	switch name {
	case "get_today_report":
		raw = s.toolGetTodayReport()

	case "create_or_update_report":
		raw = s.toolCreateOrUpdateReport(args)

	case "list_recent_reports":
		raw = s.toolListRecentReports(args)

	case "send_report":
		raw = s.toolSendReport(args)

	case "create_outing":
		raw = s.toolCreateOuting(args)

	case "send_outing":
		raw = s.toolSendOuting(args)

	default:
		return ToolResult{
			Content: fmt.Sprintf("unknown tool: %s", name),
			IsError: true,
		}
	}

	isError := jsonHasError(raw)
	return ToolResult{Content: raw, IsError: isError}
}

// ToolDefinition describes a single MCP tool (name, description, JSON Schema).
type ToolDefinition struct {
	Name        string
	Description string
	InputSchema map[string]any
}

// ListToolDefinitions returns all available tool definitions for MCP server registration.
// Keep this in sync with AIService.buildTools().
func ListToolDefinitions() []ToolDefinition {
	return []ToolDefinition{
		{
			Name:        "get_today_report",
			Description: "Get today's work report. Returns the date, weekday, full content, and current status (draft / pending / sent).",
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
				"required":   []string{},
			},
		},
		{
			Name: "create_or_update_report",
			Description: "Create or update a daily work report for the given date. " +
				"If a report already exists for that date it will be overwritten; otherwise a new one is created. " +
				"'content' must contain the complete report text (existing content + new additions). " +
				"Each work item should be on its own line with no numbering prefix.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"date": map[string]any{
						"type":        "string",
						"description": "Report date in yyyy-MM-dd format, e.g. 2026-03-24. Defaults to today.",
					},
					"content": map[string]any{
						"type":        "string",
						"description": "Full report content. Separate multiple work items with newlines; no numbering prefix needed.",
					},
				},
				"required": []string{"date", "content"},
			},
		},
		{
			Name:        "list_recent_reports",
			Description: "List recent daily work reports. Returns date, weekday, status, and a content preview (first 100 characters) for each report.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"limit": map[string]any{
						"type":        "integer",
						"description": "Number of reports to return. Default 5, max 20.",
					},
				},
				"required": []string{},
			},
		},
		{
			Name: "send_report",
			Description: "Send the daily report email. " +
				"All non-draft reports are merged into a single Excel attachment and sent in one email. " +
				"Reports with draft status are excluded. No date parameter is needed.",
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
				"required":   []string{},
			},
		},
		{
			Name: "create_outing",
			Description: "Create an out-of-office request. " +
				"The applicant name and department are read automatically from system settings. " +
				"All times must be in yyyy-MM-dd HH:mm format.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"out_time": map[string]any{
						"type":        "string",
						"description": "Departure time in yyyy-MM-dd HH:mm format, e.g. 2026-03-20 09:00.",
					},
					"return_time": map[string]any{
						"type":        "string",
						"description": "Expected return time in yyyy-MM-dd HH:mm format, e.g. 2026-03-20 18:00.",
					},
					"destination": map[string]any{
						"type":        "string",
						"description": "Destination of the outing, e.g. client office, government building.",
					},
					"reason": map[string]any{
						"type":        "string",
						"description": "Purpose of the outing; a brief description of the business reason.",
					},
					"remarks": map[string]any{
						"type":        "string",
						"description": "Optional additional remarks.",
					},
				},
				"required": []string{"out_time", "return_time", "destination", "reason"},
			},
		},
		{
			Name: "send_outing",
			Description: "Send an out-of-office request email. " +
				"If 'id' is provided, that specific request is sent. " +
				"If 'id' is omitted, the most recent pending (or previously failed) request is sent automatically.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id": map[string]any{
						"type":        "integer",
						"description": "ID of the outing request to send. Optional — omit to auto-select the latest pending request.",
					},
				},
				"required": []string{},
			},
		},
	}
}

// jsonHasError reports whether the top-level JSON object contains an "error" key.
func jsonHasError(raw string) bool {
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return false
	}
	_, has := m["error"]
	return has
}
