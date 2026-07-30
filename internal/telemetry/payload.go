package telemetry

import (
	"os"
	"strconv"
	"strings"
	"time"
)

type MetricPayload struct {
	// System Metrics
	UptimeSeconds    int64  `json:"uptime_seconds,omitempty,omitzero"`
	MemoryAllocBytes uint64 `json:"memory_alloc_bytes,omitempty,omitzero"`
	ActiveGoroutines int    `json:"active_goroutines,omitempty,omitzero"`
	GCPauseNs        uint64 `json:"gc_pause_ns,omitempty,omitzero"`

	// Session Metrics
	NetworkBytesRead    int64  `json:"network_bytes_read,omitempty,omitzero"`
	NetworkBytesWritten int64  `json:"network_bytes_written,omitempty,omitzero"`
	PipelineStage       string `json:"pipeline_stage"`
	TrifectaReviewCount int    `json:"trifecta_review_count,omitempty,omitzero"`
	SessionContextBytes int    `json:"session_context_bytes,omitempty,omitzero"`
	SessionTokensEst    int    `json:"session_tokens_est,omitempty,omitzero"`

	// Enhanced Intelligence Metrics
	LLMGateActivations int   `json:"llm_gate_activations,omitempty,omitzero"`
	LLMGateRejects     int   `json:"llm_gate_rejects,omitempty,omitzero"`
	LLMGateLatencyMs   int64 `json:"llm_gate_latency_ms,omitempty,omitzero"`
	RecallConnected    bool  `json:"recall_connected"`

	// LLM Backplane State
	LLMConfigured bool `json:"llm_configured"` // true if shared LLM env vars are set (MCP_ORCHESTRATOR_OWNED mode)
	LLMActive     bool `json:"llm_active"`     // true if last LLM backplane probe succeeded (circuit breaker)

	// Historical 30-Day Metrics
	HistoryNetBytesIn     int64 `json:"history_net_bytes_in,omitempty,omitzero"`
	HistoryNetBytesOut    int64 `json:"history_net_bytes_out,omitempty,omitzero"`
	HistoryStagesRunCount int   `json:"history_stages_run_count,omitempty,omitzero"`
	HistoryTrifectaCount  int   `json:"history_trifecta_count,omitempty,omitzero"`
	HistoryContextBytes   int   `json:"history_context_bytes,omitempty,omitzero"`
	HistoryTokensEst      int   `json:"history_tokens_est,omitempty,omitzero"`
}

var (
	// TelemetryPorts are the UDP ports used for dashboard telemetry (serve listens, dashboard connects).
	DefaultTelemetryPorts = []int{49151, 49152, 49153, 49154, 49155}
	// EmissionInterval controls how frequently the serve process pushes metrics to the dashboard.
	// 500ms provides near-real-time updates without excessive ReadMemStats overhead.
	EmissionInterval = 500 * time.Millisecond
)

// GetTelemetryPorts parses the environment variable or falls back to the server's specific default ports.
func GetTelemetryPorts() []int {
	env := os.Getenv("MCP_TELEMETRY_UDP_PORTS")
	if env == "" {
		return DefaultTelemetryPorts
	}

	var ports []int
	for part := range strings.SplitSeq(env, ",") {
		part = strings.TrimSpace(part)
		if strings.Contains(part, "-") {
			bounds := strings.Split(part, "-")
			if len(bounds) == 2 {
				start, err1 := strconv.Atoi(strings.TrimSpace(bounds[0]))
				end, err2 := strconv.Atoi(strings.TrimSpace(bounds[1]))
				if err1 == nil && err2 == nil && start <= end {
					for i := start; i <= end; i++ {
						ports = append(ports, i)
					}
				}
			}
		} else {
			if port, err := strconv.Atoi(part); err == nil {
				ports = append(ports, port)
			}
		}
	}

	if len(ports) == 0 {
		return DefaultTelemetryPorts // Fallback on malformed environment variable
	}
	return ports
}
