package main

import (
	"os"

	"github.com/maccavelli/mcp-server-socratic-thinker/internal/config"

	"github.com/spf13/cobra"
)

// Cfg is the process configuration. It stays nil for commands annotated
// skipConfigAnnotation, which is how `update` avoids starting a config watcher.
var Cfg *config.Config

// RealStdout is the process stdout captured before Execute redirects it to
// stderr, so JSON-RPC output can be written deliberately rather than by
// accident.
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

// Execute runs the command tree and returns its error. It no longer calls
// os.Exit: exit mapping belongs to main, so `update --check` can report an
// available update as exit 10 instead of a generic failure (MADR 0005).
func Execute() error {
	// CRITICAL: Steal os.Stdout to forbid Cobra usage-printing corruption over JSON-RPC
	RealStdout = os.Stdout
	os.Stdout = os.Stderr
	rootCmd.SetOut(os.Stderr)
	rootCmd.SetErr(os.Stderr)

	rootCmd.Version = Version
	return rootCmd.Execute()
}

func init() {
	// cobra.OnInitialize ran initConfig for EVERY command, which would start a
	// config watcher just to check for an update. A pre-run hook can be scoped
	// instead: a command annotated selfupdate.skip-config opts out.
	rootCmd.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		if cmd.Annotations[skipConfigAnnotation] == skipConfigValue {
			return nil
		}
		initConfig()
		return nil
	}
	rootCmd.AddCommand(serveCmd)
	rootCmd.AddCommand(dashboardCmd)
	rootCmd.AddCommand(newUpdateCmd())
}

func initConfig() {
	Cfg = config.New()
}
