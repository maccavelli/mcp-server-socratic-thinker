// Package handler provides functionality for the handler subsystem.
package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/go-playground/validator/v10"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/maccavelli/mcp-server-socratic-thinker/internal/config"
	"github.com/maccavelli/mcp-server-socratic-thinker/internal/socratic"
	"github.com/maccavelli/mcp-server-socratic-thinker/internal/util"
	"github.com/maccavelli/mcplib"
)

var validate = validator.New(validator.WithRequiredStructEnabled())

// Register explicitly maps the tools and resources to the MCP server.
// isHTTP indicates the server instance is bound to the Streamable HTTP API
// transport, enabling machine_mode autonomous execution. stdio-bound instances
// must pass false to enforce the single-session ephemeral state isolation guard.
func Register(sp util.SessionProvider, machine *socratic.Machine, logBuffer *mcplib.LogBuffer, sessionID string, cfg *config.Config, isHTTP bool) {
	// 1. Socratic Thinker Tool
	socraticTool := mcp.Tool{
		Name: "socratic_thinker",
		Description: `[ROLE: ARCHITECTURAL CRITIC] [DIRECTIVE: Socratic Dialectic Engine] Engage this STATEFUL reasoning engine when you need to rigorously stress-test an architecture, evaluate competing trade-offs, or synthesize opposing paradigms through structured adversarial debate. Note: Do NOT invoke this tool for general sequential thought or abstract logic topologies.
[MANDATORY WORKFLOW] Processes adversarial loops. This is a STATEFUL engine. DO NOT simulate the analysis in your own thought block.
1. INITIALIZE: Call with stage="INITIALIZE" and provide 'problem'.
2. EXECUTE: Read returned instructions for the next stage.
3. RECURSE: Pass analysis BACK INTO THIS TOOL one stage at a time (using 'stage', 'lemma', 'aporia_synthesis').
4. WAIT: Do not batch stages.
[FORBIDDEN FOR IDE AGENTS]: You MUST NEVER set 'machine_mode' or 'max_rounds'. These are strictly for backend server-to-server HTTP invocations. As an IDE agent, you must interact with this stateful engine manually, stage by stage.
Keywords: dialectic socratic adversarial trade-offs synthesis tension stress-test rebuttal skeptic thesis logic structural architecture evaluate debate quality gate machine mode
Intents: evaluate if this architecture is safe, find fatal flaws in this design pattern, what are the trade-offs of this approach, debate the merits of this solution, vet this code against edge cases, perform a semantic safety check, play devil's advocate on my design`,
	}

	mcplib.HardenedAddTool(sp.MCPServer(), &socraticTool, func(ctx context.Context, request *mcp.CallToolRequest, args util.SocraticToolArgs) (*mcp.CallToolResult, any, error) {
		return handleSocraticTool(ctx, machine, sessionID, isHTTP, request, args)
	}, mcplib.WithSerializedCalls(), mcplib.WithStackTrace())

	// 2. Internal Logs Tool
	mcplib.RegisterDiagnosticTool(
		sp.MCPServer(),
		logBuffer,
		"Socratic dialectic reasoning engine",
	)

	// 3. Telemetry Resource (Hybrid Observability)
	res := mcp.Resource{
		Name:        "Socratic Logs",
		URI:         "socratic-thinker://logs",
		Description: "Server diagnostic telemetry logs",
		MIMEType:    "text/plain",
	}
	sp.MCPServer().AddResource(&res, mcplib.HardenedResourceHandler(func(ctx context.Context, request *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		return handleLogResource(ctx, logBuffer, request)
	}))

	// 4. HFSC Log Retrieval Tool (Machine Mode Diagnostics)
	hfscTool := mcp.Tool{
		Name:        "fetch_hfsc_logs",
		Description: "[ROLE: DIAGNOSTIC] [DIRECTIVE: Transcript Retrieval] Retrieves the diagnostic lemma trail (transcript) from the in-memory High-Frequency State Channel (HFSC) by key. Used by peer MCP servers (like go-modernizer) after an autonomous machine_mode=true dialectic completes to debug rejected or UNSAFE mutations. Keywords: hfsc transcript lemma trail retrieve fetch diagnostic memory channel debug reject unsafe machine mode",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"key": map[string]any{
					"type":        "string",
					"description": "The HFSC key returned in DiagnosticLogHFSCKey from a machine_mode response.",
				},
			},
			"required": []string{"key"},
		},
	}

	type hfscArgs struct {
		Key string `json:"key"`
	}
	mcplib.HardenedAddTool(sp.MCPServer(), &hfscTool, func(ctx context.Context, request *mcp.CallToolRequest, args hfscArgs) (*mcp.CallToolResult, any, error) {
		return handleHFSCTool(ctx, request, args.Key)
	})

	slog.Info("all hardened tools and resources registered")
}

func handleSocraticTool(ctx context.Context, machine *socratic.Machine, sessionID string, isHTTP bool, request *mcp.CallToolRequest, args util.SocraticToolArgs) (*mcp.CallToolResult, any, error) {
	// --- Machine Mode Branch ---
	if args.MachineMode != nil && *args.MachineMode {
		prompt := args.Prompt
		if prompt == "" {
			prompt = args.Problem
		}
		if prompt == "" {
			slog.Error("handler error", "error", "missing prompt")
			return &mcp.CallToolResult{
				IsError: true,
				Content: []mcp.Content{&mcp.TextContent{Text: "machine_mode=true requires a non-empty 'prompt' field"}},
			}, nil, nil
		}

		// Transport validation: machine_mode is restricted to HTTP sessions.
		// stdio transport cannot guarantee single-session ephemeral state isolation.
		if !isHTTP {
			slog.Error("handler error", "error", "machine_mode=true restricted to HTTP transport")
			return &mcp.CallToolResult{
				IsError: true,
				Content: []mcp.Content{&mcp.TextContent{Text: "machine_mode=true is restricted to HTTP transport. stdio sessions are not supported to ensure single-session ephemeral state isolation."}},
			}, nil, nil
		}

		maxRounds := 0
		if args.MaxRounds != nil {
			maxRounds = *args.MaxRounds
		}

		processCtx := socratic.ContextWithSessionID(ctx, sessionID)
		output, err := machine.RunAutonomous(processCtx, prompt, maxRounds)
		if err != nil {
			slog.Error("handler error", "error", err)
			return &mcp.CallToolResult{
				IsError: true,
				Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("machine_mode error: %v", err)}},
			}, nil, nil
		}

		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: output}},
		}, nil, nil
	}

	// --- Standard Interactive Mode ---
	// Re-parse from raw JSON to the internal socratic.Request for machine processing.
	var req socratic.Request
	if err := json.Unmarshal(request.Params.Arguments, &req); err != nil {
		slog.Error("handler error", "error", err)
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("JSON parse error: %v", err)}},
		}, nil, nil
	}
	if err := validate.Struct(req); err != nil {
		slog.Error("handler error", "error", err)
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Validation error: %v", err)}},
		}, nil, nil
	}

	processCtx := socratic.ContextWithSessionID(ctx, sessionID)
	output, err := machine.Process(processCtx, req)
	if err != nil && (err.Error() == "invalid stage" || err.Error() == "missing lemma" || err.Error() == "missing aporia_synthesis") {
		// Soft error from machine - return it as text so the agent sees the formatting hint
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: output}},
		}, nil, nil
	} else if err != nil {
		// e.g. context cancellation
		slog.Error("handler error", "error", err)
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}},
		}, nil, nil
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: output}},
	}, nil, nil
}

func handleLogResource(_ context.Context, logBuffer *mcplib.LogBuffer, request *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
	return &mcp.ReadResourceResult{
		Contents: []*mcp.ResourceContents{
			{
				URI:      request.Params.URI,
				MIMEType: "text/plain",
				Text:     logBuffer.String(),
			},
		},
	}, nil
}

func handleHFSCTool(_ context.Context, _ *mcp.CallToolRequest, key string) (*mcp.CallToolResult, any, error) {
	data := socratic.HFSCFetch(key)
	if data == "" {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: "HFSC key not found or expired (10-minute TTL)"}},
		}, nil, nil
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: data}},
	}, nil, nil
}
