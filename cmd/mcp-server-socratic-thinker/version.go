package main

import (
	"fmt"
	"os"
	"strings"
)

// Version is the current version of the Socratic Thinker MCP server.
var RawVersion = "v4.4.4"
var Version = strings.TrimPrefix(RawVersion, "v")

func printVersion() {
	fmt.Fprintf(os.Stderr, "mcp-server-socratic-thinker version %s\n", Version)
}
