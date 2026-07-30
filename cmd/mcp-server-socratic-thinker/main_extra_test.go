package main

import (
	"os"
	"testing"
)

func TestMainAndExecute_Help(t *testing.T) {
	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()
	os.Args = []string{"mcp-server-socratic-thinker", "--help"}

	oldStdout := os.Stdout
	oldStderr := os.Stderr
	oldRealStdout := RealStdout

	rOut, wOut, _ := os.Pipe()
	os.Stdout = wOut
	os.Stderr = wOut
	RealStdout = wOut
	defer func() {
		os.Stdout = oldStdout
		os.Stderr = oldStderr
		RealStdout = oldRealStdout
		rOut.Close()
		wOut.Close()
	}()

	// Calling main() which calls Execute(), which should print help and return normally
	main()
}
