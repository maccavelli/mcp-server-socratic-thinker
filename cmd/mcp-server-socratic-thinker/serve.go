package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/maccavelli/mcplib"

	"github.com/maccavelli/mcp-server-socratic-thinker/internal/handler"
	"github.com/maccavelli/mcp-server-socratic-thinker/internal/metrics"
	"github.com/maccavelli/mcp-server-socratic-thinker/internal/socratic"
	"github.com/maccavelli/mcp-server-socratic-thinker/internal/telemetry"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"
)

var (
	bytesRead    atomic.Int64
	bytesWritten atomic.Int64
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the Stateful Socratic Dialectic Engine MCP server",
	Run: func(cmd *cobra.Command, args []string) {
		apiPort := Cfg.ResolveAPIPort()

		// Defense-in-depth
		if _, exists := os.LookupEnv("GOMEMLIMIT"); !exists {
			if err := os.Setenv("GOMEMLIMIT", "1024MiB"); err != nil {
				slog.Warn("failed to set GOMEMLIMIT", "error", err)
			}
		}
		if _, exists := os.LookupEnv("GOMAXPROCS"); !exists {
			if err := os.Setenv("GOMAXPROCS", "2"); err != nil {
				slog.Warn("failed to set GOMAXPROCS", "error", err)
			}
		}

		logBuffer := telemetry.GlobalTracker.RingBuffer
		cleanupLogs := mcplib.SetupStandardLogging(cliName, logBuffer)
		defer cleanupLogs()
		slog.Info("Starting Socratic Thinker MCP Server")

		rootCtx := context.Background()
		ctx, stop := signal.NotifyContext(rootCtx, os.Interrupt, syscall.SIGTERM)
		defer stop()

		var wg sync.WaitGroup

		recallURL := Cfg.ResolveRecallURL()
		recallClient := mcplib.NewRecallClient(recallURL, cliName)
		store := socratic.NewRecallStore(recallClient)
		wg.Go(func() {
			recallClient.Start(ctx)
		})
		defer recallClient.Close()

		// Wire LLM backplane client (nil in standalone/heuristic-only mode).
		llmClient := mcplib.NewBackplaneClient(ctx, cliName, "thinking")
		machine := socratic.NewMachine(store, socratic.WithLLM(llmClient))

		mcpServer := mcp.NewServer(&mcp.Implementation{Name: cliName, Version: Version}, &mcp.ServerOptions{
			Logger: slog.Default(),
		})
		sp := &serverProvider{srv: mcpServer}

		handler.Register(sp, machine, logBuffer, "", Cfg, false)

		startTime := time.Now()

		// Background telemetry server (UDP listener — dashboard connects to us)
		telemetryServer := telemetry.NewServer()
		if telemetryServer != nil {
			telemetryServer.Start()
			defer telemetryServer.Close()
		}

		// Local BuntDB metrics store for 30-day rolling aggregation
		metricsStore, metricsErr := metrics.Open()
		if metricsErr != nil {
			slog.Warn("metrics store unavailable, historical aggregates disabled", "error", metricsErr)
		}
		if metricsStore != nil {
			defer func() {
				if closeErr := metricsStore.Close(); closeErr != nil {
					slog.Warn("metrics store close failed", "error", closeErr)
				}
			}()

			// Background 60-second aggregation ticker
			metricsStore.StartAggregationTicker(60*time.Second, func() metrics.MetricSnapshot {
				stage, trifectaCount, contextBytes, tokensEst := machine.GetMetrics()
				_ = stage
				return metrics.MetricSnapshot{
					NetBytesIn:   bytesRead.Load(),
					NetBytesOut:  bytesWritten.Load(),
					StagesRun:    trifectaCount,
					Trifecta:     trifectaCount,
					ContextBytes: contextBytes,
					TokensEst:    tokensEst,
				}
			}, ctx.Done())
		}

		// UDP emission goroutine — builds domain-specific payload and broadcasts
		wg.Go(func() {
			if telemetryServer == nil {
				return
			}
			ticker := time.NewTicker(telemetry.EmissionInterval)
			defer ticker.Stop()

			var memStats runtime.MemStats
			runtime.ReadMemStats(&memStats) // Initial hydration
			tickCount := 0

			var lastPayload telemetry.MetricPayload
			var lastEmit time.Time
			var cachedAgg metrics.Aggregates
			var lastAggQuery time.Time

			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					tickCount++
					// ReadMemStats every 4th tick (2s cadence) to reduce STW overhead
					if tickCount%4 == 0 {
						runtime.ReadMemStats(&memStats)
					}

					stage, trifectaCount, contextBytes, tokensEst := machine.GetMetrics()
					llmActivations, llmRejects, llmLatencyMs, _ := machine.GetLLMGateMetrics()

					payload := telemetry.MetricPayload{
						UptimeSeconds:       int64(time.Since(startTime).Seconds()),
						MemoryAllocBytes:    memStats.Alloc,
						ActiveGoroutines:    runtime.NumGoroutine(),
						GCPauseNs:           memStats.PauseTotalNs,
						NetworkBytesRead:    bytesRead.Load(),
						NetworkBytesWritten: bytesWritten.Load(),
						PipelineStage:       stage,
						TrifectaReviewCount: trifectaCount,
						SessionContextBytes: contextBytes,
						SessionTokensEst:    tokensEst,
						LLMGateActivations:  llmActivations,
						LLMGateRejects:      llmRejects,
						LLMGateLatencyMs:    llmLatencyMs,
						RecallConnected:     recallClient.RecallEnabled(),
						LLMConfigured:       llmClient != nil,
						LLMActive:           llmClient != nil && llmClient.Available(),
					}

					// Populate 30-day historical aggregates from local BuntDB
					if metricsStore != nil {
						if lastAggQuery.IsZero() || time.Since(lastAggQuery) >= 60*time.Second {
							if agg, err := metricsStore.Query30DayAggregates(); err == nil {
								cachedAgg = agg
								lastAggQuery = time.Now()
							}
						}
						payload.HistoryNetBytesIn = cachedAgg.NetBytesIn
						payload.HistoryNetBytesOut = cachedAgg.NetBytesOut
						payload.HistoryStagesRunCount = cachedAgg.StagesRun
						payload.HistoryTrifectaCount = cachedAgg.Trifecta
						payload.HistoryContextBytes = cachedAgg.ContextBytes
						payload.HistoryTokensEst = cachedAgg.TokensEst
					}

					if mcplib.IsOrchestratorOwned() {
						stateChanged := payload.PipelineStage != lastPayload.PipelineStage ||
							payload.TrifectaReviewCount != lastPayload.TrifectaReviewCount

						if !stateChanged && time.Since(lastEmit) < 5*time.Second {
							continue
						}
					}

					telemetryServer.Broadcast(payload)
					lastPayload = payload
					lastEmit = time.Now()
				}
			}
		})

		pipeline := mcplib.NewStdioPipeline(os.Stdin, RealStdout, stop)

		errChan := make(chan error, 1)

		var srv *http.Server
		if apiPort > 0 {
			srv = startStreamableHTTPAPI(ctx, machine, errChan, apiPort, logBuffer, RealStdout)
		}
		wg.Add(1)
		go func(threadCtx context.Context) {
			defer wg.Done()
			// Wrap mcplib building blocks with byte-counting decorators for telemetry.
			countReader := &countingReader{r: pipeline.Reader}
			countWriter := &countingWriter{w: pipeline.Writer}

			t := &mcp.IOTransport{
				Reader: countReader,
				Writer: countWriter,
			}
			if _, err := mcpServer.Connect(threadCtx, t, nil); err != nil {
				select {
				case errChan <- err:
				case <-threadCtx.Done():
				}
			}
		}(ctx)

		select {
		case <-ctx.Done():
			slog.Info("context cancelled; initiating graceful shutdown")
		case err := <-errChan:
			if mcplib.IsExpectedShutdownErr(err) {
				slog.Info("stdio transport closed gracefully", "reason", err.Error())
			} else {
				slog.Error("server fatal error", "error", err)
				os.Exit(1)
			}
		}

		if srv != nil {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := srv.Shutdown(shutdownCtx); err != nil {
				slog.Warn("Streamable HTTP server shutdown error", "error", err)
			}
		}

		drainCtx, drainCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer drainCancel()

		drainDone := make(chan struct{})
		go func() {
			wg.Wait()
			close(drainDone)
		}()

		select {
		case <-drainDone:
			slog.Info("[LIFECYCLE] All background goroutines exited cleanly")
		case <-drainCtx.Done():
			slog.Warn("[LIFECYCLE] Drain period expired, forcing exit")
		}

		if flushErr := pipeline.Flush(); flushErr != nil {
			slog.Warn("pipeline flush on exit failed", "error", flushErr)
		}
	},
}

// countingReader wraps an io.ReadCloser and tracks total bytes read for telemetry.
type countingReader struct {
	r io.ReadCloser
}

func (c *countingReader) Read(p []byte) (n int, err error) {
	n, err = c.r.Read(p)
	bytesRead.Add(int64(n))
	return n, err
}

func (c *countingReader) Close() error {
	return c.r.Close()
}

// countingWriter wraps an io.WriteCloser and tracks total bytes written for telemetry.
type countingWriter struct {
	w io.WriteCloser
}

func (c *countingWriter) Write(p []byte) (n int, err error) {
	n, err = c.w.Write(p)
	bytesWritten.Add(int64(n))
	return n, err
}

func (c *countingWriter) Close() error {
	return c.w.Close()
}

// serverProvider wraps an mcp.Server to implement the SessionProvider interface.
type serverProvider struct {
	srv *mcp.Server
}

func (s *serverProvider) MCPServer() *mcp.Server      { return s.srv }
func (s *serverProvider) Session() *mcp.ServerSession { return nil }

func startStreamableHTTPAPI(ctx context.Context, machine *socratic.Machine, errChan chan<- error, port int, logBuffer *mcplib.LogBuffer, realStdout *os.File) *http.Server {
	slog.Info("starting Streamable HTTP API", "port", port)

	if _, err := fmt.Fprintf(realStdout, "[INFO] Socratic HTTP API available on port %d\n", port); err != nil {
		slog.Warn("failed to write HTTP API banner", "error", err)
	}

	streamHandler := mcp.NewStreamableHTTPHandler(func(req *http.Request) *mcp.Server {
		sessionID := req.Header.Get("Mcp-Session-Id")
		mcpServer := mcp.NewServer(&mcp.Implementation{
			Name:    cliName,
			Version: Version,
		}, &mcp.ServerOptions{Logger: slog.Default()})
		sp := &serverProvider{srv: mcpServer}

		handler.Register(sp, machine, logBuffer, sessionID, Cfg, true)
		return mcpServer
	}, nil)

	mux := http.NewServeMux()
	mux.Handle("/mcp", &auditMiddleware{next: streamHandler, machine: machine})

	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", port),
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		BaseContext:       func(_ net.Listener) context.Context { return ctx },
	}

	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("Streamable HTTP server exited", "error", err)
			errChan <- err
		}
	}()

	return srv
}

type auditMiddleware struct {
	next    http.Handler
	machine *socratic.Machine
}

func (m *auditMiddleware) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	m.next.ServeHTTP(w, r)
}
