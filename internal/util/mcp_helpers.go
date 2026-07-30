// Package util provides MCP registration types and shared logging utilities.
package util

import (
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// SessionProvider defines the SessionProvider structure.
type SessionProvider interface {
	MCPServer() *mcp.Server
	Session() *mcp.ServerSession
}

// MockSessionProvider defines the MockSessionProvider structure.
type MockSessionProvider struct{ Srv *mcp.Server }

// MCPServer performs the MCPServer operation.
func (m *MockSessionProvider) MCPServer() *mcp.Server { return m.Srv }

// Session performs the Session operation.
func (m *MockSessionProvider) Session() *mcp.ServerSession { return nil }

// SocraticToolArgs defines the typed input schema for the socratic_thinker tool.
// The handcrafted InputSchema map on the mcp.Tool is retained for precise enum
// and description control; this struct handles deserialization only.
type SocraticToolArgs struct {
	Stage              string `json:"stage" validate:"required" jsonschema:"The exact Socratic pipeline stage you are invoking." jsonschema_extras:"enum=INITIALIZE,enum=THESIS,enum=ANTITHESIS_INITIAL,enum=THESIS_DEFENSE,enum=ANTITHESIS_EVALUATE,enum=CHAOS,enum=APORIA,enum=RESET"`
	Problem            string `json:"problem,omitzero" jsonschema:"The raw string problem. Only provide this during the INITIALIZE stage."`
	Lemma              string `json:"lemma,omitzero" jsonschema:"[REQUIRES: Native markdown thought block] Your isolated two-sentence summary for the active stage. Sentence 1: your position or finding. Sentence 2: the specific technical evidence, mechanism, or example supporting it. Do not include your detailed 'Chain of Thought' here; perform all detailed analysis natively in your markdown response before invoking this tool."`
	IsSatisfied        *bool  `json:"is_satisfied,omitzero" jsonschema:"Used only in ANTITHESIS_EVALUATE to signal if the Thesis defense successfully resolved the tension. Acts as the Convergence Gate."`
	AporiaSynthesis    string `json:"aporia_synthesis,omitzero" jsonschema:"[REQUIRES: Native markdown thought block] Your final synthesized solution. This is not constrained to two sentences — provide a comprehensive synthesis. Only provide this during the APORIA stage. Do not include your detailed 'Chain of Thought' here; perform all detailed analysis natively in your markdown response before invoking this tool."`
	SynthesisCritique  string `json:"synthesis_critique,omitzero" jsonschema:"Explicit self-correction and rigorous evaluation of the synthesis attempt. Only provide this during the APORIA stage."`
	ParadoxDetected    *bool  `json:"paradox_detected,omitzero" jsonschema:"Must be set to true if SynthesisCritique identifies a paradox, flaw, or assumption that breaks the synthesis. Only provide this during the APORIA stage."`
	ResolutionStrategy string `json:"resolution_strategy,omitzero" jsonschema:"Mandatory step outlining how to fix the synthesis loop ONLY if paradox_detected is true."`
	MachineMode        *bool  `json:"machine_mode,omitzero" jsonschema:"[FORBIDDEN FOR IDE AGENTS: DO NOT USE] Strictly for server-to-server internal API calls. You MUST NEVER set this field. When true, the server autonomously drives the entire Socratic dialectic loop using the shared LLM backplane. RESTRICTED to HTTP transport only."`
	MaxRounds          *int   `json:"max_rounds,omitzero" jsonschema:"[FORBIDDEN FOR IDE AGENTS: DO NOT USE] Internal server parameter only. Optional cap on dialectic rounds for machine_mode."`
	Prompt             string `json:"prompt,omitzero" jsonschema:"The raw problem statement for machine_mode autonomous execution. Only used when machine_mode=true."`
}
