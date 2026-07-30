package config

import (
	"os"
	"testing"
)

func TestConfig(t *testing.T) {
	// Set some env vars to test binding
	os.Setenv("MCP_ORCHESTRATOR_OWNED", "true")
	os.Setenv("MCP_ENDPOINT_API_PORT", "12345")
	os.Setenv("MCP_REC_URL", "http://test-rec")
	os.Setenv("MCP_SOC_URL", "http://test-soc")
	defer func() {
		os.Unsetenv("MCP_ORCHESTRATOR_OWNED")
		os.Unsetenv("MCP_ENDPOINT_API_PORT")
		os.Unsetenv("MCP_REC_URL")
		os.Unsetenv("MCP_SOC_URL")
	}()

	cfg := New()

	if cfg.ResolveAPIPort() != 12345 {
		t.Errorf("expected port 12345, got %d", cfg.ResolveAPIPort())
	}
	if cfg.ResolveRecallURL() != "http://test-rec" {
		t.Errorf("expected http://test-rec, got %s", cfg.ResolveRecallURL())
	}
	if cfg.ResolveSocraticURL() != "http://test-soc" {
		t.Errorf("expected http://test-soc, got %s", cfg.ResolveSocraticURL())
	}
}

func TestConfigDefaults(t *testing.T) {
	os.Unsetenv("MCP_ORCHESTRATOR_OWNED")
	os.Unsetenv("MCP_ENDPOINT_API_PORT")
	os.Unsetenv("MCP_REC_URL")
	os.Unsetenv("MCP_SOC_URL")

	cfg := New()

	if cfg.ResolveAPIPort() != 47779 {
		t.Errorf("expected default port 47779, got %d", cfg.ResolveAPIPort())
	}
	if cfg.ResolveRecallURL() != "http://localhost:47669/mcp" {
		t.Errorf("expected default recall url, got %s", cfg.ResolveRecallURL())
	}
	if cfg.ResolveSocraticURL() != "http://localhost:47779/mcp" {
		t.Errorf("expected default socratic url, got %s", cfg.ResolveSocraticURL())
	}
}
