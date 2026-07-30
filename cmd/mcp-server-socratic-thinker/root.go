package main

import (
	"fmt"
	"os"

	"github.com/maccavelli/mcp-server-socratic-thinker/internal/config"

	"github.com/spf13/cobra"
)

var Cfg *config.Config
var RealStdout *os.File

var rootCmd = &cobra.Command{
	Use:     cliName,
	Version: Version,
	Short:   "Stateful Socratic Dialectic Engine MCP Server",
	Long:    `Socratic Thinker is a STATEFUL MCP server that provides deep cognitive processing and paradox resolution via structured interactive dialectic stages.`,
	Run: func(cmd *cobra.Command, args []string) {
		// Default to serve if no args
		serveCmd.Run(serveCmd, args)
	},
}

// Execute adds all child commands to the root command and sets flags appropriately.
func Execute() {
	// CRITICAL: Steal os.Stdout to forbid Cobra usage-printing corruption over JSON-RPC
	RealStdout = os.Stdout
	os.Stdout = os.Stderr
	rootCmd.SetOut(os.Stderr)
	rootCmd.SetErr(os.Stderr)

	rootCmd.Version = Version
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Fatal execution error: %v\n", err)
		os.Exit(1)
	}
}

func init() {
	cobra.OnInitialize(initConfig)
	rootCmd.AddCommand(serveCmd)
	rootCmd.AddCommand(dashboardCmd)
}

func initConfig() {
	Cfg = config.New()
}
