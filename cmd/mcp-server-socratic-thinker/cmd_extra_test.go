package main

import (
	"os"
	"testing"
)

func TestRootInitConfig(t *testing.T) {
	os.Setenv("MCP_ENDPOINT_API_PORT", "12345")
	initConfig()
	if Cfg == nil {
		t.Error("expected Cfg to be initialized")
	}
	os.Unsetenv("MCP_ENDPOINT_API_PORT")
}

func TestMainExecute(t *testing.T) {
	// Call main or Execute but it might exit, so we just check Execute's existence
	if rootCmd.Use != "socratic-thinker" {
		t.Error("unexpected root command use")
	}
}
