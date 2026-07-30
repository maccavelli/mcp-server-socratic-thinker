package telemetry

import "github.com/maccavelli/mcplib"

// GlobalRingBuffer is the canonical type alias for mcplib.LogBuffer.
// This ensures downward compatibility for existing dependent packages
// while strictly adhering to STD-MCP-TELEMETRY-ALIAS-001.
type GlobalRingBuffer = mcplib.LogBuffer

// Tracker wraps the global diagnostic ring buffer.
type Tracker struct {
	RingBuffer *GlobalRingBuffer
}

// GlobalTracker exposes the centralized telemetry logging plane per STD-MCP-LOG-TELEMETRY-001.
var GlobalTracker = &Tracker{
	RingBuffer: mcplib.NewLogBuffer(),
}
