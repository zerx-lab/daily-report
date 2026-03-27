package main

import (
	"context"
	"embed"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zerx-lab/daily-report/internal/model"
	"github.com/zerx-lab/daily-report/internal/service"
)

// emptyFS for EmailService init (MCP mode does not render HTML templates)
var emptyFS embed.FS

func main() {
	dbPath := flag.String("db", "data/daily_report.db", "SQLite 数据库路径")
	flag.Parse()

	// MCP stdio mode: logs must go to stderr to avoid polluting the JSON-RPC stdout stream
	log.SetOutput(os.Stderr)
	log.SetFlags(log.LstdFlags)

	// ==================== database ====================
	if err := model.InitDB(*dbPath); err != nil {
		log.Fatalf("[fatal] failed to init database: %v\n", err)
	}
	defer model.CloseDB()
	db := model.GetDB()

	// ==================== services ====================
	reportSvc := service.NewReportService(db)
	emailSvc := service.NewEmailService(db, emptyFS)
	siyuanSvc := service.NewSiyuanService(db)
	outingSvc := service.NewOutingService(db)
	aiSvc := service.NewAIService(db, reportSvc, outingSvc, emailSvc, siyuanSvc)

	// ==================== MCP server ====================
	s := mcp.NewServer(&mcp.Implementation{
		Name:    "daily-report",
		Version: "1.0.0",
	}, &mcp.ServerOptions{
		// Disable listChanged notifications so the client fetches tools/list
		// immediately after initialize, rather than waiting for a notification.
		Capabilities: &mcp.ServerCapabilities{
			Tools: &mcp.ToolCapabilities{ListChanged: false},
		},
	})

	// ==================== register tools ====================
	// AddTool[In, Out]: SDK auto-unmarshals Arguments JSON into In (map[string]any).
	for _, def := range service.ListToolDefinitions() {
		def := def // avoid loop variable capture

		tool := &mcp.Tool{
			Name:        def.Name,
			Description: def.Description,
			// InputSchema accepts any; passing map[string]any lets the SDK
			// remarshal it into an internal *jsonschema.Schema without conflict.
			InputSchema: def.InputSchema,
		}

		toolName := def.Name // capture for closure

		mcp.AddTool(s, tool, func(
			ctx context.Context,
			req *mcp.CallToolRequest,
			args map[string]any,
		) (*mcp.CallToolResult, any, error) {
			if args == nil {
				args = make(map[string]any)
			}

			result := aiSvc.CallTool(toolName, args)

			res := &mcp.CallToolResult{
				Content: []mcp.Content{
					&mcp.TextContent{Text: result.Content},
				},
				IsError: result.IsError,
			}
			return res, nil, nil
		})
	}

	// ==================== start stdio server ====================
	fmt.Fprintf(os.Stderr, "[MCP] daily-report v1.0.0 started, waiting for Claude Code...\n")

	if err := s.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Fatalf("[fatal] MCP stdio server error: %v\n", err)
	}
}
