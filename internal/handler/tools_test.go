package handler

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/maccavelli/mcp-server-socratic-thinker/internal/config"
	"github.com/maccavelli/mcp-server-socratic-thinker/internal/socratic"
	"github.com/maccavelli/mcp-server-socratic-thinker/internal/util"
	"github.com/maccavelli/mcplib"
)

func ptrBool(b bool) *bool { return &b }

func getContentText(c mcp.Content) string {
	if tc, ok := c.(*mcp.TextContent); ok {
		return tc.Text
	}
	return ""
}

type mockSessionProvider struct {
	server *mcp.Server
}

func (m *mockSessionProvider) MCPServer() *mcp.Server {
	return m.server
}

func (m *mockSessionProvider) Session() *mcp.ServerSession {
	return nil
}

type mockStore struct{}

func (m *mockStore) SearchDialecticHistory(ctx context.Context, query string, limit int) string {
	return ""
}
func (m *mockStore) ArchiveDialecticJourney(ctx context.Context, archive socratic.DialecticArchive) error {
	return nil
}
func (m *mockStore) SaveToRecall(ctx context.Context, content string, docType string, metadata any) error {
	return nil
}

func TestRegister(t *testing.T) {
	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1"}, &mcp.ServerOptions{})
	sp := &mockSessionProvider{server: server}
	cfg := &config.Config{}
	store := &mockStore{}
	machine := socratic.NewMachine(store)
	logBuffer := mcplib.NewLogBuffer()

	Register(sp, machine, logBuffer, "test-session", cfg, true)
	// Just verifies it doesn't panic.
}

func TestHandleHFSCTool(t *testing.T) {
	// Store something
	key := socratic.HFSCStore("test data")

	req := &mcp.CallToolRequest{}
	res, _, err := handleHFSCTool(context.Background(), req, key)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.Content) == 0 {
		t.Fatal("expected content")
	}
	if text := res.Content[0].(*mcp.TextContent).Text; text != "test data" {
		t.Errorf("expected 'test data', got %q", text)
	}

	// Missing key
	res, _, _ = handleHFSCTool(context.Background(), req, "invalid-key")
	if !strings.Contains(res.Content[0].(*mcp.TextContent).Text, "not found") {
		t.Errorf("expected not found error, got %v", res.Content[0].(*mcp.TextContent).Text)
	}
}

func TestHandleLogResource(t *testing.T) {
	logBuffer := mcplib.NewLogBuffer()
	logBuffer.Write([]byte("log1\n"))

	req := &mcp.ReadResourceRequest{
		Params: &mcp.ReadResourceParams{
			URI: "socratic-thinker://logs",
		},
	}

	res, err := handleLogResource(context.Background(), logBuffer, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.Contents) == 0 {
		t.Fatal("expected contents")
	}
	if res.Contents[0].Text != "log1\n" {
		t.Errorf("expected log1, got %q", res.Contents[0].Text)
	}
}

func TestHandleSocraticTool_MachineMode_NoPrompt(t *testing.T) {
	machine := socratic.NewMachine(nil)
	args := util.SocraticToolArgs{
		MachineMode: ptrBool(true),
	}
	res, _, _ := handleSocraticTool(context.Background(), machine, "sess", true, nil, args)
	if !res.IsError || !strings.Contains(getContentText(res.Content[0]), "requires a non-empty 'prompt' field") {
		t.Errorf("expected missing prompt error, got %v", res)
	}
}

func TestHandleSocraticTool_MachineMode_NotHTTP(t *testing.T) {
	machine := socratic.NewMachine(nil)
	args := util.SocraticToolArgs{
		MachineMode: ptrBool(true),
		Prompt:      "solve this",
	}
	res, _, _ := handleSocraticTool(context.Background(), machine, "sess", false, nil, args)
	if !res.IsError || !strings.Contains(getContentText(res.Content[0]), "HTTP transport") {
		t.Errorf("expected HTTP transport error, got %v", res)
	}
}

func TestHandleSocraticTool_MachineMode_Error(t *testing.T) {
	// Nil LLM will cause RunAutonomous to fail immediately
	machine := socratic.NewMachine(nil)
	args := util.SocraticToolArgs{
		MachineMode: ptrBool(true),
		Prompt:      "solve this",
	}
	res, _, _ := handleSocraticTool(context.Background(), machine, "sess", true, nil, args)
	if !res.IsError || !strings.Contains(getContentText(res.Content[0]), "machine_mode error") {
		t.Errorf("expected machine mode error, got %v", res)
	}
}

func TestHandleSocraticTool_Interactive_ParseError(t *testing.T) {
	machine := socratic.NewMachine(nil)
	req := &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{
			Arguments: []byte(`invalid json`),
		},
	}

	res, _, _ := handleSocraticTool(context.Background(), machine, "sess", true, req, util.SocraticToolArgs{})
	if !res.IsError || !strings.Contains(getContentText(res.Content[0]), "JSON parse error") {
		t.Errorf("expected JSON parse error, got %v", res)
	}
}

func TestHandleSocraticTool_Interactive_ValidationError(t *testing.T) {
	machine := socratic.NewMachine(nil)
	req := &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{
			Arguments: []byte(`{"stage": ""}`),
		},
	}

	res, _, _ := handleSocraticTool(context.Background(), machine, "sess", true, req, util.SocraticToolArgs{})
	if !res.IsError || !strings.Contains(getContentText(res.Content[0]), "Validation error") {
		t.Errorf("expected Validation error, got %v", res)
	}
}

func TestHandleSocraticTool_Interactive_SoftError(t *testing.T) {
	machine := socratic.NewMachine(nil)
	args, _ := json.Marshal(socratic.Request{Stage: "THESIS"})
	req := &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{
			Arguments: args,
		},
	}

	res, _, _ := handleSocraticTool(context.Background(), machine, "sess-new", true, req, util.SocraticToolArgs{})
	// Soft error should return NOT as IsError, but contain the error text.
	if res.IsError || !strings.Contains(getContentText(res.Content[0]), "Expected stage") {
		t.Errorf("expected soft invalid stage error, got %v", res)
	}
}
