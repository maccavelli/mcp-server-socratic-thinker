package main

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/maccavelli/mcplib"
)

func TestServeCmdRun_GracefulShutdown(t *testing.T) {
	rIn, wIn, _ := os.Pipe()
	rOut, wOut, _ := os.Pipe()

	oldStdin := os.Stdin
	oldStdout := os.Stdout
	oldRealStdout := RealStdout
	os.Stdin = rIn
	os.Stdout = wOut
	RealStdout = wOut

	defer func() {
		os.Stdin = oldStdin
		os.Stdout = oldStdout
		RealStdout = oldRealStdout
		rIn.Close()
		wIn.Close()
		rOut.Close()
		wOut.Close()
	}()

	if Cfg == nil {
		initConfig()
	}

	done := make(chan struct{})
	go func() {
		serveCmd.Run(serveCmd, []string{})
		close(done)
	}()

	// Close stdin to trigger graceful shutdown
	wIn.Close()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("serveCmd.Run did not return gracefully")
	}
}

func TestCountingReaderAndWriter(t *testing.T) {
	// Reset counters
	bytesRead.Store(0)
	bytesWritten.Store(0)

	buf := bytes.NewBufferString("hello world")
	cr := &countingReader{r: io.NopCloser(buf)}
	p := make([]byte, 5)
	n, err := cr.Read(p)
	if err != nil {
		t.Fatal(err)
	}
	if n != 5 {
		t.Fatalf("expected 5 read, got %d", n)
	}
	if bytesRead.Load() != 5 {
		t.Fatalf("expected 5 bytesRead, got %d", bytesRead.Load())
	}
	cr.Close()

	outBuf := new(bytes.Buffer)
	cw := &countingWriter{w: nopWriteCloser{outBuf}}
	n, err = cw.Write([]byte("test"))
	if err != nil {
		t.Fatal(err)
	}
	if n != 4 {
		t.Fatalf("expected 4 written, got %d", n)
	}
	if bytesWritten.Load() != 4 {
		t.Fatalf("expected 4 bytesWritten, got %d", bytesWritten.Load())
	}
	cw.Close()
}

func TestPrintVersion(t *testing.T) {
	printVersion() // Just for coverage
}

type nopWriteCloser struct {
	io.Writer
}

func (nopWriteCloser) Close() error { return nil }

func TestServerProvider(t *testing.T) {
	sp := &serverProvider{srv: nil}
	if sp.MCPServer() != nil {
		t.Error("expected nil")
	}
	if sp.Session() != nil {
		t.Error("expected nil")
	}
}

type mockHandler struct {
	called bool
}

func (m *mockHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	m.called = true
}

func TestAuditMiddleware(t *testing.T) {
	mh := &mockHandler{}
	mw := &auditMiddleware{next: mh, machine: nil}
	mw.ServeHTTP(nil, nil)
	if !mh.called {
		t.Error("expected ServeHTTP to call next.ServeHTTP")
	}
}

func TestStartStreamableHTTPAPI(t *testing.T) {
	// Start the server on port 0 to let the OS choose
	errChan := make(chan error, 1)

	// Create dummy logger
	logBuffer := mcplib.NewLogBuffer()

	srv := startStreamableHTTPAPI(context.Background(), nil, errChan, 0, logBuffer, os.Stdout)
	// Just verifies it doesn't panic.
	if srv == nil {
		t.Fatal("expected server to be created")
	}
	srv.Close() // prevent leaking goroutine
}
