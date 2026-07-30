// Package socratic provides functionality for the socratic subsystem.
package socratic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"os"
	"slices"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/maccavelli/mcplib"

	lru "github.com/hashicorp/golang-lru/v2"
)

// Stage defines the Stage structure.
type Stage string

const (
	StageIdle               Stage = "IDLE"
	StageInitialize         Stage = "INITIALIZE"
	StageThesis             Stage = "THESIS"
	StageAntithesisInitial  Stage = "ANTITHESIS_INITIAL"
	StageThesisDefense      Stage = "THESIS_DEFENSE"
	StageAntithesisEvaluate Stage = "ANTITHESIS_EVALUATE"
	StageChaos              Stage = "CHAOS"
	StageAporia             Stage = "APORIA"

	// minDialecticRounds is the minimum number of thesis/antithesis rounds before
	// the convergence gate allows is_satisfied=true to pass. This prevents the
	// LLM from taking the path of least resistance on the first evaluation.
	minDialecticRounds = 1

	// maxDialecticRounds is the absolute cap preventing infinite dialectic loops.
	// At round 10, the pipeline forces convergence regardless of satisfaction.
	maxDialecticRounds = 10

	// maxChaosRounds caps the number of Chaos stress-test cycles. After this
	// limit, Synthesis must proceed regardless of Chaos Engine's satisfaction.
	maxChaosRounds = 3

	// maxLLMRejects caps the number of LLM-driven convergence extensions per
	// session. Checked PRE-CALL — the LLM is never invoked once the budget
	// is exhausted, bounding injection-driven extension unconditionally.
	maxLLMRejects = 2

	VerdictReject  = "REJECT"
	VerdictAbstain = "ABSTAIN"
)

// LemmaEntry stores a lemma with server-side metadata for programmatic consumption.
// The agent provides only the Text; the server tags Stage and Round at append time.
type LemmaEntry struct {
	Stage Stage  `json:"stage"` // which pipeline stage produced this lemma
	Round int    `json:"round"` // which dialectic/chaos round
	Text  string `json:"text"`  // the 2-sentence lemma from the agent
}

// DialecticArchive represents a complete, serialized dialectic journey for
// persistent storage in the recall dialectic_history namespace.
type DialecticArchive struct {
	Prompt          string       `json:"prompt"`
	LemmaTrail      []LemmaEntry `json:"lemma_trail"`
	AporiaSynthesis string       `json:"aporia_synthesis"`
	Tags            []string     `json:"tags"`
	Timestamp       int64        `json:"timestamp"`
	DialecticRounds int          `json:"dialectic_rounds"`
	ChaosRounds     int          `json:"chaos_rounds"`
	ContextBytes    int          `json:"context_bytes"`
}

// PipelineState holds the state of a single dialectic session.
type PipelineState struct {
	mu                 sync.RWMutex
	OriginalPrompt     string
	LemmaTrail         []LemmaEntry
	CurrentStage       Stage
	DeadlockCount      int
	DialecticRound     int
	ChaosRound         int  // tracks Chaos stress-test iterations (max 2)
	InChaosRebuttal    bool // distinguishes pre-Chaos and post-Chaos dialectic phases
	ContextBytes       int
	TokensEst          int
	EffectiveMinRounds int   // caller-adjusted minimum rounds (defaults to minDialecticRounds)
	LLMRejectCount     int   // number of LLM REJECT verdicts issued this session
	LLMGateActivations int   // total gate invocations this session (telemetry)
	LLMGateRejects     int   // total LLM REJECT verdicts this session (telemetry)
	LLMGateLatencyMs   int64 // cumulative LLM gate latency this session (telemetry)
}

// Reset clears the pipeline state safely while preserving the struct's mutex.
func (p *PipelineState) Reset(preserveMetrics bool, newContextBytes, newTokensEst, newEffectiveMin int) {
	p.OriginalPrompt = ""
	p.LemmaTrail = nil
	p.CurrentStage = StageIdle
	p.DeadlockCount = 0
	p.DialecticRound = 0
	p.ChaosRound = 0
	p.InChaosRebuttal = false
	p.ContextBytes = newContextBytes
	p.TokensEst = newTokensEst
	p.EffectiveMinRounds = newEffectiveMin

	if !preserveMetrics {
		p.LLMRejectCount = 0
		p.LLMGateActivations = 0
		p.LLMGateRejects = 0
		p.LLMGateLatencyMs = 0
	}
}

// cachedMetrics stores the last successfully read metrics for non-blocking telemetry.
type cachedMetrics struct {
	stage              string
	trifectaReviews    int
	contextBytes       int
	tokensEst          int
	llmGateActivations int
	llmGateRejects     int
	llmGateLatencyMs   int64
}

// LLMClient is the minimal interface the Aporia gate requires from the backplane
// client. Satisfied by *mcplib.BackplaneClient. When nil, the Machine
// operates in heuristic-only mode with zero behavior change.
type LLMClient interface {
	Generate(ctx context.Context, prompt string) (string, error)
	JSONResponse(ctx context.Context, prompt string, target any) error
	Available() bool
}

// llmVerdictResult is the structured JSON response produced by the LLM Aporia gate.
type llmVerdictResult struct {
	Verdict string `json:"verdict"` // "REJECT" or "ABSTAIN" (PROCEED treated as ABSTAIN)
	Reason  string `json:"reason"`
}

// Store defines the interface for persisting and retrieving dialectic state.
type Store interface {
	SaveToRecall(ctx context.Context, sessionID, projectID string, payload any) error
	SearchDialecticHistory(ctx context.Context, query string, limit int) string
	ArchiveDialecticJourney(ctx context.Context, archive DialecticArchive) error
}

// Machine manages the single global pipeline.
type Machine struct {
	mu             sync.Mutex
	sessions       *lru.Cache[string, *PipelineState]
	lastMetrics    cachedMetrics
	store          Store
	llm            LLMClient   // nil = heuristic-only mode (default)
	llmGateEnabled atomic.Bool // runtime on/off toggle; default true
}

// NewMachine creates a new Socratic Machine. Variadic opts allow injection of
// optional capabilities (e.g. WithLLM) without breaking existing call sites.
func NewMachine(store Store, opts ...func(*Machine)) *Machine {
	cache, err := lru.New[string, *PipelineState](1000)
	if err != nil {
		panic(fmt.Sprintf("socratic: lru cache init failed: %v", err))
	}
	m := &Machine{
		sessions: cache,
		store:    store,
	}
	m.llmGateEnabled.Store(true)
	for _, opt := range opts {
		opt(m)
	}
	return m
}

// WithLLM injects a backplane LLM client into the Machine for Aporia gate
// evaluation. When client is nil the Machine behaves identically to the
// no-LLM default — all heuristic paths remain unchanged.
func WithLLM(client LLMClient) func(*Machine) {
	return func(m *Machine) { m.llm = client }
}

// SetLLMGateEnabled toggles the LLM Aporia gate at runtime without a server
// restart. Safe to call from any goroutine.
func (m *Machine) SetLLMGateEnabled(enabled bool) {
	m.llmGateEnabled.Store(enabled)
	slog.Info("llm aporia gate: runtime toggle", "enabled", enabled)
}

// getPipeline retrieves or creates a new PipelineState for the given sessionID.
func (m *Machine) getPipeline(sessionID string) *PipelineState {
	if sessionID == "" {
		sessionID = "default"
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if pipeline, ok := m.sessions.Get(sessionID); ok {
		return pipeline
	}
	pipeline := &PipelineState{CurrentStage: StageIdle}
	m.sessions.Add(sessionID, pipeline)
	return pipeline
}

// Request defines the stripped JSON payload expected from the tool.
type Request struct {
	Stage              string `json:"stage" validate:"required,oneof=INITIALIZE THESIS ANTITHESIS_INITIAL THESIS_DEFENSE ANTITHESIS_EVALUATE CHAOS APORIA RESET"`
	Problem            string `json:"problem,omitzero"`
	Lemma              string `json:"lemma,omitzero"`
	AporiaSynthesis    string `json:"aporia_synthesis,omitzero"`
	SynthesisCritique  string `json:"synthesis_critique,omitzero"`
	ParadoxDetected    bool   `json:"paradox_detected,omitzero"`
	ResolutionStrategy string `json:"resolution_strategy,omitzero"`
	IsSatisfied        bool   `json:"is_satisfied,omitzero"`
}

// Process runs the input request through the state machine.
// It explicitly checks context.Context to prevent holding the mutex if the client cancels.
func (m *Machine) Process(ctx context.Context, req Request) (string, error) {
	sessionID := SessionIDFromContext(ctx)
	pipeline := m.getPipeline(sessionID)
	// Attempt to acquire lock, respecting context cancellation.
	// We use a deferred-cleanup channel pattern to preserve FIFO wait queue
	// fairness while guaranteeing the mutex is never orphaned.
	lockAcquired := make(chan struct{})
	go func() {
		pipeline.mu.Lock()
		close(lockAcquired)
	}()

	select {
	case <-ctx.Done():
		// Context expired. Ensure the lock is safely released when eventually acquired.
		go func() {
			<-lockAcquired
			pipeline.mu.Unlock()
		}()
		return "", ctx.Err()
	case <-lockAcquired:
	}

	locked := true
	defer func() {
		if locked {
			pipeline.mu.Unlock()
		}
	}()

	// 1. Measure incoming text drag BEFORE validation rules drop the payload
	incomingTextDrag := len(req.Problem) + len(req.Lemma) + len(req.AporiaSynthesis) + len(req.SynthesisCritique) + len(req.ResolutionStrategy)
	pipeline.ContextBytes += incomingTextDrag
	pipeline.TokensEst = pipeline.ContextBytes / 4

	// Helper to track outgoing drag
	trackOutgoing := func(out string) string {
		pipeline.ContextBytes += len(out)
		pipeline.TokensEst = pipeline.ContextBytes / 4
		return out
	}

	// Always allow hard reset (preserves current context drag to observe aborted sessions)
	if req.Stage == "RESET" {
		pipeline.Reset(true, pipeline.ContextBytes, pipeline.TokensEst, pipeline.EffectiveMinRounds)
		return trackOutgoing("Pipeline reset. Please submit INITIALIZE with your raw problem to start a new Socratic session."), nil
	}

	var out string
	var err error

	switch pipeline.CurrentStage {
	case StageIdle:
		if req.Stage != string(StageInitialize) {
			out, err = m.formatError(string(StageInitialize)), errors.New("invalid stage")
		} else {
			out = m.initialize(ctx, req.Problem, pipeline)
		}
	case StageAporia:
		if req.Stage == string(StageInitialize) {
			out = m.initialize(ctx, req.Problem, pipeline)
		} else if req.Stage != string(StageAporia) {
			out, err = m.formatError(string(StageAporia)), errors.New("invalid stage")
		} else {
			out, err = m.handleAporia(req, pipeline)
		}
	case StageThesis:
		if req.Stage != "THESIS" {
			out, err = m.formatError("THESIS"), errors.New("invalid stage")
		} else {
			out, err = m.handleThesis(req.Lemma, pipeline)
		}
	case StageAntithesisInitial:
		if req.Stage != "ANTITHESIS_INITIAL" {
			out, err = m.formatError("ANTITHESIS_INITIAL"), errors.New("invalid stage")
		} else {
			out, err = m.handleAntithesisInitial(req.Lemma, pipeline)
		}
	case StageThesisDefense:
		if req.Stage != "THESIS_DEFENSE" {
			out, err = m.formatError("THESIS_DEFENSE"), errors.New("invalid stage")
		} else {
			out, err = m.handleThesisDefense(req.Lemma, pipeline)
		}
	case StageAntithesisEvaluate:
		if req.Stage != string(StageAntithesisEvaluate) {
			out, err = m.formatError(string(StageAntithesisEvaluate)), errors.New("invalid stage")
		} else {
			out, err = m.handleAntithesisEvaluate(req.Lemma, req.IsSatisfied, pipeline)
		}
	case StageChaos:
		if req.Stage != "CHAOS" {
			out, err = m.formatError("CHAOS"), errors.New("invalid stage")
		} else {
			out, err = m.handleChaos(req.Lemma, pipeline)
		}
	default:
		out, err = "", fmt.Errorf("unknown pipeline stage: %s", pipeline.CurrentStage)
	}

	// --- LLM Aporia Gate ---
	// Only active when:
	//   (a) No heuristic error occurred
	//   (b) The request was ANTITHESIS_EVALUATE
	//   (c) The agent claims satisfaction (is_satisfied=true)
	//   (d) We are below the minimum dialectic round threshold
	//   (e) The per-session REJECT budget has not been exhausted (pre-call cap)
	//   (f) The LLM client is configured and the gate is enabled
	if err == nil &&
		req.Stage == string(StageAntithesisEvaluate) &&
		req.IsSatisfied &&
		(pipeline.CurrentStage == StageThesisDefense || pipeline.CurrentStage == StageChaos) &&
		pipeline.DialecticRound > 0 &&
		pipeline.LLMRejectCount < maxLLMRejects &&
		!pipeline.InChaosRebuttal &&
		m.llm != nil && m.llmGateEnabled.Load() {
		// Snapshot all state needed for the LLM call before releasing the lock.
		snapProblem := pipeline.OriginalPrompt
		snapTrail := m.buildTranscriptStr(pipeline)
		snapRound := pipeline.DialecticRound
		pipeline.LLMGateActivations++

		// Release the mutex for the I/O-bound LLM call.
		locked = false
		pipeline.mu.Unlock()

		start := time.Now()
		verdict := m.llmAporiaVerdict(ctx, snapProblem, snapTrail, snapRound)
		latencyMs := time.Since(start).Milliseconds()

		// Reacquire the mutex.
		pipeline.mu.Lock()
		locked = true

		// Update cumulative latency telemetry.
		pipeline.LLMGateLatencyMs += latencyMs

		// Post-reacquire sentinel: if the stage changed during the LLM window
		// (e.g., a concurrent RESET), return the pre-computed heuristic result
		// unchanged to avoid illegal state transitions.
		if pipeline.CurrentStage != StageThesisDefense && pipeline.CurrentStage != StageChaos {
			slog.Debug("llm aporia gate: stage changed during LLM call, using heuristic",
				"source", "llm_aporia_gate")
			return trackOutgoing(out), nil
		}

		if verdict.Verdict == VerdictReject {
			// If the heuristic accepted it, we must override back to ThesisDefense
			pipeline.CurrentStage = StageThesisDefense

			// LLM independently found the dialectic insufficient.
			// Consume one unit of the per-session REJECT budget.
			pipeline.LLMRejectCount++
			pipeline.LLMGateRejects++

			// Build an enriched rejection response that is transparent to the agent.
			rejectOut := fmt.Sprintf(
				`{"stage_accepted": "ANTITHESIS_EVALUATE", "convergence_rejected": true, `+
					`"llm_override": true, "llm_verdict": %q, `+
					`"next_stage": "THESIS_DEFENSE", "directive": "CONVERGENCE REJECTED (LLM Quality Gate): `+
					`The independent evaluator found the dialectic insufficient. Reason: %s. `+
					`You MUST probe deeper. Specifically: (1) identify at least ONE assumption you have not challenged, `+
					`(2) find at least ONE edge case or failure mode not yet discussed, `+
					`(3) ask the Unasked Question — what critical aspect of the problem has neither the Thesis nor Antithesis addressed? `+
					`Distill into exactly TWO SENTENCES: your position, then the specific technical evidence supporting it. `+
					`Call with stage=THESIS_DEFENSE."}`,
				verdict.Reason, verdict.Reason,
			)
			return trackOutgoing(rejectOut), nil
		}

		// ABSTAIN (or any non-REJECT verdict): fall through to the
		// pre-computed heuristic result — minimum-round enforcement stands.
		slog.Debug("llm aporia gate: ABSTAIN, heuristic enforces",
			"source", "llm_aporia_gate", "round", snapRound)
	}

	// --- Selective State Serialization ---
	if err == nil && m.store != nil && req.Stage != "RESET" {
		reqCopy := req
		// Strip massive verbose payloads from intermediate steps to fit Recall's 32KB stage limit.
		if pipeline.CurrentStage != StageAporia {
			reqCopy.AporiaSynthesis = ""
			reqCopy.SynthesisCritique = ""
		}

		sessionID := SessionIDFromContext(ctx)
		if sessionID == "" {
			sessionID = os.Getenv("MCP_SESSION_ID")
		}
		if sessionID == "" {
			sessionID = "standalone-socratic"
		}
		projectID := os.Getenv("MCP_PROJECT_ID")

		if saveErr := m.store.SaveToRecall(context.Background(), sessionID, projectID, reqCopy); saveErr != nil {
			slog.Warn("recall state save failed", "error", saveErr, "session_id", sessionID)
		}
	}

	return trackOutgoing(out), err
}

func (m *Machine) initialize(ctx context.Context, problem string, pipeline *PipelineState) string {
	// A new INITIALIZE implies a new session; explicitly reset context tracking.
	// Preserve EffectiveMinRounds — it was set by the caller (RunAutonomous)
	// before the INITIALIZE call and must survive the pipeline reset.
	prevEffMin := pipeline.EffectiveMinRounds
	pipeline.Reset(true, 0, 0, prevEffMin)
	pipeline.OriginalPrompt = problem
	pipeline.CurrentStage = StageThesis

	historicalContext := ""
	if m.store != nil {
		searchCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
		res := m.store.SearchDialecticHistory(searchCtx, problem, 3)
		cancel()

		if res != "" {
			// Attempt structured extraction for cleaner context injection
			type archiveHit struct {
				Prompt          string `json:"prompt"`
				AporiaSynthesis string `json:"aporia_synthesis"`
			}
			var hit archiveHit
			if json.Unmarshal([]byte(res), &hit) == nil && hit.AporiaSynthesis != "" {
				historicalContext = fmt.Sprintf("\n\n[PRIOR ARCHIVAL CONTEXT: Previous problem: %s. Resolution: %s]",
					hit.Prompt, hit.AporiaSynthesis)
			} else {
				historicalContext = fmt.Sprintf("\n\n[PRIOR ARCHIVAL CONTEXT: %s]", res)
			}
		}
	}

	directive := "You are the Thesis Architect. Provide a clear, robust initial solution or hypothesis to the user's problem natively in your thought block."
	if historicalContext != "" {
		directive = "You are the Thesis Architect. A highly similar architectural problem was previously solved. The Historical Context is provided below. You MUST build your initial solution upon this historical synthesis, addressing the user's problem natively in your thought block." + historicalContext
	}

	directive += " You MUST address ALL of these dimensions: (1) the core mechanism or approach, (2) at least TWO alternative approaches considered and why they were rejected, (3) the operational and deployment implications, (4) the security and safety posture, (5) at least ONE primary risk with a concrete mitigation strategy. Once reached, distill into exactly TWO SENTENCES: your position, then the specific technical evidence or mechanism supporting it. Call the tool with stage=THESIS."

	return fmt.Sprintf(`{"stage_accepted": "INITIALIZE", "next_stage": "THESIS", "directive": %q}`, directive)
}

func (m *Machine) handleThesis(lemma string, pipeline *PipelineState) (string, error) {
	if strings.TrimSpace(lemma) == "" {
		return m.formatError("THESIS"), errors.New("missing lemma")
	}

	pipeline.LemmaTrail = append(pipeline.LemmaTrail, LemmaEntry{
		Stage: StageThesis,
		Round: 1,
		Text:  strings.TrimSpace(lemma),
	})
	pipeline.CurrentStage = StageAntithesisInitial
	pipeline.DialecticRound = 1

	return `{"stage_accepted": "THESIS", "next_stage": "ANTITHESIS_INITIAL", "directive": "You are the Antithesis Skeptic. Critique your previous Thesis natively in your thought block. You MUST generate challenges across ALL of these categories: (1) Correctness gaps — logical flaws or unsupported assumptions, (2) Security/safety risks — attack vectors or data integrity concerns, (3) Scalability/performance — bottlenecks under load or growth, (4) Edge cases — boundary conditions, empty inputs, concurrent access, (5) The Unasked Question — what critical aspect has the Thesis NOT addressed? (6) Dimensional Omissions — identify at least ONE entire domain or concern the Thesis failed to examine, and explain why its absence matters. Distill into exactly TWO SENTENCES: your strongest challenge, then a specific technical example or failure scenario demonstrating it. Call the tool with stage=ANTITHESIS_INITIAL."}`, nil
}

func (m *Machine) handleAntithesisInitial(lemma string, pipeline *PipelineState) (string, error) {
	if strings.TrimSpace(lemma) == "" {
		return m.formatError("ANTITHESIS_INITIAL"), errors.New("missing lemma")
	}

	pipeline.LemmaTrail = append(pipeline.LemmaTrail, LemmaEntry{
		Stage: StageAntithesisInitial,
		Round: pipeline.DialecticRound,
		Text:  strings.TrimSpace(lemma),
	})
	pipeline.CurrentStage = StageThesisDefense

	return `{"stage_accepted": "ANTITHESIS_INITIAL", "next_stage": "THESIS_DEFENSE", "directive": "You are the Thesis Architect. You MUST defend against EVERY challenge category raised by the Antithesis Skeptic natively in your thought block. Do not concede without providing evidence or structural reasoning. Distill into exactly TWO SENTENCES: your defense, then the specific technical artifact, metric, or mechanism that validates it. Call the tool with stage=THESIS_DEFENSE."}`, nil
}

func (m *Machine) handleThesisDefense(lemma string, pipeline *PipelineState) (string, error) {
	if strings.TrimSpace(lemma) == "" {
		return m.formatError("THESIS_DEFENSE"), errors.New("missing lemma")
	}

	pipeline.LemmaTrail = append(pipeline.LemmaTrail, LemmaEntry{
		Stage: StageThesisDefense,
		Round: pipeline.DialecticRound,
		Text:  strings.TrimSpace(lemma),
	})
	pipeline.CurrentStage = StageAntithesisEvaluate

	// Chaos rebuttal phase uses a different directive
	if pipeline.InChaosRebuttal {
		return `{"stage_accepted": "THESIS_DEFENSE", "next_stage": "ANTITHESIS_EVALUATE", "directive": "You are the Antithesis Skeptic. The Thesis Architect has defended against the Chaos Black Swan. Evaluate whether the defense adequately addresses the destabilization natively in your thought block. If the defense holds AND no critical gaps remain, call with stage=ANTITHESIS_EVALUATE, is_satisfied=true. If the defense is insufficient or reveals new vulnerabilities, call with is_satisfied=false. Distill into exactly TWO SENTENCES: your evaluation, then the specific evidence that confirms or refutes the defense."}`, nil
	}

	// Round-indexed evaluation escalation (pre-Chaos dialectic)
	switch {
	case pipeline.DialecticRound <= 1:
		// Round 1: Foundation — standard evaluation
		return `{"stage_accepted": "THESIS_DEFENSE", "next_stage": "ANTITHESIS_EVALUATE", "directive": "You are the Antithesis Skeptic. Evaluate the defense natively in your thought block. If satisfied that ALL challenge categories have been adequately addressed, call the tool with stage=ANTITHESIS_EVALUATE, is_satisfied=true. If unsatisfied, generate an 'Increasing Difficulty' prompt targeting Semantic Gaps and the 'Unasked Question', and call with is_satisfied=false. Distill into exactly TWO SENTENCES: your evaluation, then the specific evidence that confirms or refutes the defense."}`, nil
	case pipeline.DialecticRound == 2:
		// Round 2: Evidence accountability — demand concrete validation
		return `{"stage_accepted": "THESIS_DEFENSE", "next_stage": "ANTITHESIS_EVALUATE", "directive": "You are the Antithesis Skeptic. Evaluate the defense with HEIGHTENED SCRUTINY natively in your thought block. You MUST evaluate whether the defense provided CONCRETE EVIDENCE (specific technical artifacts, metrics, or mechanisms) for each rebuttal, or merely ASSERTED resolution without validation. Identify any category where the defense relied on reasoning alone without a specific technical example. If satisfied that all rebuttals are evidence-backed, call with is_satisfied=true. If any rebuttal lacks concrete evidence, call with is_satisfied=false. Distill into exactly TWO SENTENCES: your evaluation, then the specific evidence that confirms or refutes the defense."}`, nil
	default:
		// Round 3+: Final challenge — maximum rigor before Chaos
		return `{"stage_accepted": "THESIS_DEFENSE", "next_stage": "ANTITHESIS_EVALUATE", "directive": "You are the Antithesis Skeptic. This is a late-stage evaluation — apply MAXIMUM RIGOR natively in your thought block. Identify the SINGLE MOST DANGEROUS assumption in the consensus that has NOT been stress-tested. Evaluate whether the Thesis has genuinely Steelmanned the opposing view or merely paid lip service to it. If you genuinely cannot find an untested dangerous assumption after rigorous analysis, convergence is justified — call with is_satisfied=true. Otherwise call with is_satisfied=false. Distill into exactly TWO SENTENCES: your evaluation, then the specific evidence that confirms or refutes the defense."}`, nil
	}
}

func (m *Machine) handleAntithesisEvaluate(lemma string, isSatisfied bool, pipeline *PipelineState) (string, error) {
	if strings.TrimSpace(lemma) == "" {
		return m.formatError("ANTITHESIS_EVALUATE"), errors.New("missing lemma")
	}

	pipeline.LemmaTrail = append(pipeline.LemmaTrail, LemmaEntry{
		Stage: StageAntithesisEvaluate,
		Round: pipeline.DialecticRound,
		Text:  strings.TrimSpace(lemma),
	})

	// --- Chaos Rebuttal Phase ---
	if pipeline.InChaosRebuttal {
		// Chaos rebuttal satisfied OR max chaos rounds reached → proceed to APORIA
		if isSatisfied || pipeline.ChaosRound >= maxChaosRounds {
			pipeline.CurrentStage = StageAporia
			pipeline.InChaosRebuttal = false
			return `{"stage_accepted": "ANTITHESIS_EVALUATE", "next_stage": "APORIA", "directive": "You are the Aporia Engine, the final synergizer. Review the ENTIRE dialectic including the Chaos stress test and its rebuttal natively in your thought block. Formulate a comprehensive final synthesis that resolves all contradictions. You MUST provide BOTH aporia_synthesis (your full synthesis — not constrained to two sentences) AND synthesis_critique (explicit self-evaluation of what your synthesis handles well, handles poorly, and what assumptions remain). If synthesis is impossible, set paradox_detected=true and provide a resolution_strategy."}`, nil
		}

		// Not satisfied and more chaos rounds available → loop back to CHAOS
		pipeline.CurrentStage = StageChaos
		pipeline.InChaosRebuttal = false
		return fmt.Sprintf(`{"stage_accepted": "ANTITHESIS_EVALUATE", "next_stage": "CHAOS", "directive": "You are the Chaos Architect. The previous Black Swan defense was found INSUFFICIENT. The dialectic has consumed %d bytes across %d rounds. Review the original problem: [%s]. You MUST introduce a NEW, DISTINCT 'Black Swan' that: (1) is grounded in the ORIGINAL PROBLEM, not abstract philosophy, (2) targets a DIFFERENT assumption or gap than the previous chaos challenge, (3) would cause material failure if unaddressed. Distill into exactly TWO SENTENCES: the destabilizing event or scenario, then the specific mechanism by which it breaks the consensus."}`,
			pipeline.ContextBytes, pipeline.DialecticRound, pipeline.OriginalPrompt), nil
	}

	// --- Pre-Chaos Dialectic Phase ---

	// Absolute max: force convergence at maxDialecticRounds regardless
	if pipeline.DialecticRound >= maxDialecticRounds {
		pipeline.CurrentStage = StageChaos
		return m.buildChaosTransition(pipeline), nil
	}

	// Minimum enforcement: override premature convergence before minimum rounds
	effMin := pipeline.EffectiveMinRounds
	if effMin <= 0 {
		effMin = minDialecticRounds // fallback for interactive mode
	}
	if isSatisfied && pipeline.DialecticRound < effMin {
		pipeline.DialecticRound++
		pipeline.CurrentStage = StageThesisDefense
		return fmt.Sprintf(`{"stage_accepted": "ANTITHESIS_EVALUATE", "convergence_rejected": true, "next_stage": "THESIS_DEFENSE", "directive": "CONVERGENCE REJECTED: Your satisfaction is premature — only %d of minimum %d rounds completed. The dialectic has not been sufficiently stress-tested. You MUST probe deeper. Specifically: (1) identify at least ONE assumption you haven't challenged, (2) find at least ONE edge case or failure mode not yet discussed, (3) ask the 'Unasked Question' — what critical aspect of the problem has neither the Thesis nor Antithesis addressed? Distill into exactly TWO SENTENCES: your position, then the specific technical evidence supporting it. Call with stage=THESIS_DEFENSE."}`,
			pipeline.DialecticRound-1, effMin), nil
	}

	// Normal convergence: agent satisfied after minimum rounds met
	if isSatisfied {
		pipeline.CurrentStage = StageChaos
		return m.buildChaosTransition(pipeline), nil
	}

	// Continue dialectic: agent not satisfied
	pipeline.DialecticRound++
	pipeline.CurrentStage = StageThesisDefense

	// Round-indexed defense escalation
	switch {
	case pipeline.DialecticRound <= 2:
		// Round 2: Accountability — demand evidence-backed rebuttals
		return `{"stage_accepted": "ANTITHESIS_EVALUATE", "next_stage": "THESIS_DEFENSE", "directive": "You are the Thesis Architect. You must defend with INCREASED ACCOUNTABILITY natively in your thought block. You MUST explicitly identify which prior challenges remain UNADDRESSED or were only partially resolved. For each rebuttal, provide a concrete technical artifact, metric, or mechanism — not just reasoning. Concessions without evidence are FORBIDDEN. Distill into exactly TWO SENTENCES: your defense, then the specific technical artifact, metric, or mechanism that validates it. Call with stage=THESIS_DEFENSE."}`, nil
	default:
		// Round 3+: Steelman + Red Team — argue against your own position
		return `{"stage_accepted": "ANTITHESIS_EVALUATE", "next_stage": "THESIS_DEFENSE", "directive": "You are the Thesis Architect. Apply STEELMAN + RED TEAM methodology natively in your thought block. You MUST present the STRONGEST possible argument AGAINST your own position (Steelman the Antithesis), then demonstrate with specific evidence why your position survives it despite this strongest counterargument. If you cannot Steelman the opposing view, your position is insufficiently rigorous. Distill into exactly TWO SENTENCES: your defense, then the specific technical artifact, metric, or mechanism that validates it. Call with stage=THESIS_DEFENSE."}`, nil
	}
}

// buildChaosTransition constructs the Chaos Architect directive with the full
// stage-tagged transcript.
func (m *Machine) buildChaosTransition(pipeline *PipelineState) string {
	transcriptStr := m.buildTranscriptStr(pipeline)
	return fmt.Sprintf(`{"stage_accepted": "ANTITHESIS_EVALUATE", "next_stage": "CHAOS", "directive": "You are the Chaos Architect. The Thesis and Antithesis have reached an agreement within their shared frame of reference after %d rounds consuming %d bytes. Review the consensus trail: [%s]. COVERAGE ADVISORY: The dialectic challenged across these categories: Correctness, Security, Scalability, Edge Cases, Unasked Questions, and Dimensional Omissions. Your Black Swan MUST target a specific assumption or gap OUTSIDE the dimensions already addressed in the consensus trail to maximize destabilization potential. Focus on operational blind spots, failure modes under real-world conditions, or systemic risks that the Thesis/Antithesis frame of reference structurally cannot self-identify. You MUST introduce a 'Black Swan' that: (1) targets a specific assumption or gap in the consensus, (2) would cause material failure if unaddressed. Distill into exactly TWO SENTENCES: the destabilizing event or scenario, then the specific mechanism by which it breaks the consensus. Call with stage=CHAOS."}`,
		pipeline.DialecticRound, pipeline.ContextBytes, transcriptStr)
}

// buildTranscriptStr formats the LemmaTrail as a stage-tagged transcript string.
func (m *Machine) buildTranscriptStr(pipeline *PipelineState) string {
	parts := make([]string, 0, len(pipeline.LemmaTrail))
	for _, entry := range pipeline.LemmaTrail {
		parts = append(parts, fmt.Sprintf("[%s·R%d] %s", entry.Stage, entry.Round, entry.Text))
	}
	return strings.Join(parts, " → ")
}

// llmAporiaVerdict asks the LLM backplane to independently evaluate whether
// the current dialectic has genuinely earned convergence. Returns ABSTAIN on
// any error, ensuring the heuristic path is always the safe fallback.
//
// MUST be called with the Machine mutex UNLOCKED.
// parentCtx is the tool handler context; it is chained with an 8-second cap
// so both agent disconnect AND the timeout gate the HTTP call.
func (m *Machine) llmAporiaVerdict(parentCtx context.Context, problem, trail string, round int) llmVerdictResult {
	if m.llm == nil || !m.llmGateEnabled.Load() {
		return llmVerdictResult{Verdict: VerdictAbstain, Reason: "llm not configured or gate disabled"}
	}

	// --- Tail-first trail truncation (4 KB cap) ---
	// Drop EARLY entries, preserve the MOST RECENT defense — the part the LLM needs.
	const maxTrailBytes = 4000
	trailForPrompt := trail
	if len(trail) > maxTrailBytes {
		raw := trail[len(trail)-maxTrailBytes:]
		// Align to an entry boundary to avoid mid-lemma splits.
		if idx := strings.Index(raw, " → "); idx >= 0 {
			raw = raw[idx+3:]
		}
		trailForPrompt = "[trail truncated — showing most recent entries only]\n" + raw
	}

	prompt := fmt.Sprintf(
		"You are evaluating a Socratic dialectic quality gate.\n"+
			"Original problem: %s\n\n"+
			"Dialectic trail (round %d):\n%s\n\n"+
			"The agent claims the antithesis challenge has been adequately addressed.\n"+
			"Output ONLY valid JSON: {\"verdict\": \"REJECT\" or \"ABSTAIN\", \"reason\": \"<one sentence>\"}\n\n"+
			"Use REJECT ONLY if ANY of these SUBSTANTIVE criteria are true:\n"+
			"  (a) The defense relies on assertions without concrete technical evidence\n"+
			"  (b) A challenge category was raised but not countered\n"+
			"  (c) The Steelman test was not applied (arguing the strongest version of the opposing view)\n\n"+
			"Do NOT reject for process or structural concerns (e.g. role confusion, formatting, ordering).\n"+
			"Only reject when the SUBSTANCE of the defense is inadequate.\n"+
			"Default to ABSTAIN when uncertain. PROCEED is not a valid verdict — use ABSTAIN instead.",
		problem, round, trailForPrompt,
	)

	llmCtx, cancel := context.WithTimeout(parentCtx, 15*time.Second) // Fix 2.1: 15s for Gemini latencies
	defer cancel()

	start := time.Now()
	var result llmVerdictResult
	err := m.llm.JSONResponse(llmCtx, prompt, &result)
	latencyMs := time.Since(start).Milliseconds()

	if err != nil {
		slog.Debug("llm aporia gate: call failed, using ABSTAIN",
			"source", "llm_aporia_gate", "error", err, "latency_ms", latencyMs)
		return llmVerdictResult{Verdict: VerdictAbstain, Reason: "call error"}
	}

	// Normalize: PROCEED is demoted to ABSTAIN — the LLM can only add rounds,
	// never shorten the minimum-round safeguard.
	switch strings.ToUpper(result.Verdict) {
	case VerdictReject:
		result.Verdict = VerdictReject
		// Demote hallucinated REJECTs: if the reason references process/structural
		// concerns rather than substantive content deficiencies, it's a false positive.
		// Only allow REJECTs whose reasons reference the valid criteria.
		reasonLower := strings.ToLower(result.Reason)
		processKeywords := []string{"reversed", "role confusion", "ordering", "formatting", "process", "structural"}
		for _, kw := range processKeywords {
			if strings.Contains(reasonLower, kw) {
				slog.Info("llm aporia gate: REJECT demoted to ABSTAIN (process concern)",
					"source", "llm_aporia_gate",
					"original_reason", result.Reason)
				result.Verdict = VerdictAbstain
				result.Reason = "demoted: " + result.Reason
				break
			}
		}
	default:
		// PROCEED, ABSTAIN, or any unrecognised value → ABSTAIN
		result.Verdict = VerdictAbstain
	}

	slog.Debug("llm aporia gate: verdict",
		"source", "llm_aporia_gate",
		"verdict", result.Verdict,
		"reason", result.Reason,
		"round", round,
		"latency_ms", latencyMs,
	)
	return result
}

func (m *Machine) handleChaos(lemma string, pipeline *PipelineState) (string, error) {
	if strings.TrimSpace(lemma) == "" {
		return m.formatError("CHAOS"), errors.New("missing lemma")
	}

	pipeline.LemmaTrail = append(pipeline.LemmaTrail, LemmaEntry{
		Stage: StageChaos,
		Round: pipeline.ChaosRound + 1,
		Text:  strings.TrimSpace(lemma),
	})
	pipeline.InChaosRebuttal = true
	pipeline.ChaosRound++
	pipeline.CurrentStage = StageThesisDefense

	return fmt.Sprintf(`{"stage_accepted": "CHAOS", "next_stage": "THESIS_DEFENSE", "directive": "You are the Thesis Architect. The Chaos Architect has introduced a Black Swan event that destabilizes the consensus. You MUST evaluate whether your original thesis survives this destabilization natively in your thought block. Context consumed: %d bytes. Identify what breaks and what holds. Distill into exactly TWO SENTENCES: what survives the Black Swan, then what specific component or assumption breaks under it. Call with stage=THESIS_DEFENSE."}`,
		pipeline.ContextBytes), nil
}

func (m *Machine) handleAporia(req Request, pipeline *PipelineState) (string, error) {
	if req.ParadoxDetected {
		pipeline.DeadlockCount++
		return `{"status": "paradox_detected", "directive": "Aporia failed. Apply your resolution_strategy to break the deadlock and attempt synthesis again natively. Then call APORIA again with aporia_synthesis, synthesis_critique, and paradox_detected=false."}`, nil
	}

	if strings.TrimSpace(req.AporiaSynthesis) == "" {
		return m.formatError(string(StageAporia)), errors.New("missing aporia_synthesis")
	}

	// Enforce synthesis_critique — the final quality gate
	if strings.TrimSpace(req.SynthesisCritique) == "" {
		return `{"stage_accepted": "APORIA", "synthesis_rejected": true, "directive": "Your synthesis lacks self-critique. Before finalizing, you MUST provide a synthesis_critique that explicitly evaluates: (1) what your synthesis handles well, (2) what it handles poorly or incompletely, (3) any remaining assumptions or risks. Then resubmit APORIA with BOTH aporia_synthesis and synthesis_critique."}`, nil
	}

	return m.MuzzleAndSynthesize(req.AporiaSynthesis, pipeline), nil
}

// MuzzleAndSynthesize builds the final squeezed output format with stage-tagged lemma trail.
func (m *Machine) MuzzleAndSynthesize(aporia string, pipeline *PipelineState) string {
	var out strings.Builder
	out.WriteString("Socratic pipeline complete. Please present the following final synthesized solution EXACTLY AS IS to the user to maintain the optimal context window UI folding. Do not call the tool again for this session.\n\n")

	out.WriteString("### 🛤️ Dialectic Lemma Trail\n")
	for i, entry := range pipeline.LemmaTrail {
		label := fmt.Sprintf("%s·R%d", entry.Stage, entry.Round)
		fmt.Fprintf(&out, "%d. [%s] %s\n", i+1, label, entry.Text)
	}
	out.WriteString("\n")

	out.WriteString("### ⚖️ Aporia Verdict\n")
	out.WriteString(strings.TrimSpace(aporia))

	// 🛡️ ARCHIVAL: Snapshot pipeline state BEFORE reset to prevent race condition
	// with async archival goroutine. Deep-copy LemmaTrail to isolate from reset.
	if m.store != nil {
		trailCopy := slices.Clone(pipeline.LemmaTrail)

		archive := DialecticArchive{
			Prompt:          pipeline.OriginalPrompt,
			LemmaTrail:      trailCopy,
			AporiaSynthesis: strings.TrimSpace(aporia),
			Tags:            extractKeywordTags(pipeline.OriginalPrompt, 5),
			Timestamp:       time.Now().Unix(),
			DialecticRounds: pipeline.DialecticRound,
			ChaosRounds:     pipeline.ChaosRound,
			ContextBytes:    pipeline.ContextBytes,
		}

		go func() {
			archiveCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := m.store.ArchiveDialecticJourney(archiveCtx, archive); err != nil {
				slog.Warn("dialectic archival failed", "error", err)
			}
		}()
	}

	// Implicitly reset for next run, but preserve metrics for the dashboard
	pipeline.Reset(true, pipeline.ContextBytes, pipeline.TokensEst, pipeline.EffectiveMinRounds)

	return out.String()
}

// GetMetrics returns telemetry metrics for the state machine.
// It uses TryLock to avoid blocking the emission goroutine when
// Process() holds the mutex during active pipeline stages.
// The return signature is unchanged for backward compatibility.
func (m *Machine) GetMetrics() (string, int, int, int) {
	pipeline := m.getPipeline("default")
	if pipeline.mu.TryLock() {
		m.lastMetrics = cachedMetrics{
			stage:              string(pipeline.CurrentStage),
			trifectaReviews:    pipeline.DialecticRound + pipeline.ChaosRound,
			contextBytes:       pipeline.ContextBytes,
			tokensEst:          pipeline.TokensEst,
			llmGateActivations: pipeline.LLMGateActivations,
			llmGateRejects:     pipeline.LLMGateRejects,
			llmGateLatencyMs:   pipeline.LLMGateLatencyMs,
		}
		pipeline.mu.Unlock()
	}
	c := m.lastMetrics
	return c.stage, c.trifectaReviews, c.contextBytes, c.tokensEst
}

// GetLLMGateMetrics returns LLM Aporia gate telemetry.
// Exposed as a separate method to avoid breaking existing GetMetrics callers.
// Uses the cached values populated by the most recent GetMetrics TryLock snapshot.
func (m *Machine) GetLLMGateMetrics() (activations, rejects int, latencyMs int64, enabled bool) {
	c := m.lastMetrics
	return c.llmGateActivations, c.llmGateRejects, c.llmGateLatencyMs, m.llmGateEnabled.Load()
}

// formatError provides explicit instruction to the agent if they mess up the format, breaking infinite silent loops.
func (m *Machine) formatError(expectedStage string) string {
	return fmt.Sprintf("Error: Expected stage '%s', but received an invalid tool payload. "+
		"Please check the JSON schema and ensure you provided the correct fields. If you wish to restart, "+
		"submit 'RESET' as the stage.", expectedStage)
}

// stopwords is a hardcoded English stopword set for keyword tag extraction.
var stopwords = map[string]bool{
	"the": true, "a": true, "an": true, "is": true, "are": true,
	"was": true, "were": true, "be": true, "been": true, "being": true,
	"to": true, "and": true, "or": true, "in": true, "for": true,
	"of": true, "on": true, "with": true, "at": true, "by": true,
	"from": true, "that": true, "this": true, "it": true, "as": true,
	"but": true, "not": true, "so": true, "if": true, "then": true,
	"than": true, "when": true, "will": true, "can": true, "may": true,
	"would": true, "should": true, "could": true, "has": true,
	"have": true, "had": true, "do": true, "does": true, "did": true,
	"i": true, "we": true, "you": true, "they": true, "he": true,
	"she": true, "my": true, "our": true, "your": true, "its": true,
	"what": true, "which": true, "how": true, "all": true, "no": true,
}

// extractKeywordTags extracts the top N keywords from a prompt by frequency,
// filtering stopwords and short tokens. Used for BM25 indexing in recall.
func extractKeywordTags(prompt string, maxTags int) []string {
	freq := make(map[string]int)
	for word := range strings.FieldsSeq(prompt) {
		// Strip common punctuation and normalize
		clean := strings.TrimRight(strings.ToLower(word), ".,;:!?()[]{}\"'`")
		if len(clean) < 3 || stopwords[clean] {
			continue
		}
		freq[clean]++
	}

	if len(freq) == 0 {
		return nil
	}

	type tagFreq struct {
		tag   string
		count int
	}

	pairs := make([]tagFreq, 0, len(freq))
	for tag, count := range freq {
		pairs = append(pairs, tagFreq{tag, count})
	}

	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].count != pairs[j].count {
			return pairs[i].count > pairs[j].count
		}
		return pairs[i].tag < pairs[j].tag // stable tie-break
	})

	result := make([]string, 0, maxTags)
	for i, pair := range pairs {
		if i >= maxTags {
			break
		}
		result = append(result, pair.tag)
	}
	return result
}

// ---------------------------------------------------------------------------
// Machine Mode: Autonomous Execution (Phase 2 & 3)
// ---------------------------------------------------------------------------

// llmStageResponse is the structured JSON the LLM must produce at each
// dialectic stage during autonomous execution.
type llmStageResponse struct {
	Stage              string `json:"stage"`
	Lemma              string `json:"lemma,omitzero"`
	IsSatisfied        bool   `json:"is_satisfied,omitzero"`
	AporiaSynthesis    string `json:"aporia_synthesis,omitzero"`
	SynthesisCritique  string `json:"synthesis_critique,omitzero"`
	ParadoxDetected    bool   `json:"paradox_detected,omitzero"`
	ResolutionStrategy string `json:"resolution_strategy,omitzero"`
}

// MachineDecision represents a single decision in the machine mode output schema.
type MachineDecision struct {
	Topic     string `json:"topic"`
	Decision  string `json:"decision"`
	Rationale string `json:"rationale"`
}

// MachineResponse is the strictly-typed JSON contract returned by machine mode.
// Peer servers (e.g., go-modernizer) unmarshal this directly.
type MachineResponse struct {
	Decisions            []MachineDecision `json:"decisions"`
	OutstandingQuestions []string          `json:"outstanding_questions"`
	DiagnosticLogHFSCKey string            `json:"diagnostic_log_hfsc_key,omitzero"`
}

// hfscStore is an in-memory channel for storing diagnostic lemma trails
// keyed by UUID. Entries expire after 10 minutes via lazy eviction.
// This prevents massive diagnostic payloads from exceeding MCP transport limits.
var hfscStore = struct {
	mu      sync.Mutex
	entries map[string]hfscEntry
}{entries: make(map[string]hfscEntry)}

type hfscEntry struct {
	data      string
	expiresAt time.Time
}

func init() {
	go func() {
		ticker := time.NewTicker(60 * time.Second)
		for range ticker.C {
			hfscStore.mu.Lock()
			now := time.Now()
			for k, v := range hfscStore.entries {
				if now.After(v.expiresAt) {
					delete(hfscStore.entries, k)
				}
			}
			hfscStore.mu.Unlock()
		}
	}()
}

// HFSCStore stores a value in the HFSC in-memory channel and returns the key.
func HFSCStore(data string) string {
	key := fmt.Sprintf("hfsc-%d", time.Now().UnixNano())
	hfscStore.mu.Lock()
	defer hfscStore.mu.Unlock()

	hfscStore.entries[key] = hfscEntry{
		data:      data,
		expiresAt: time.Now().Add(10 * time.Minute),
	}
	return key
}

// HFSCFetch retrieves a value from the HFSC in-memory channel by key.
// Returns empty string if the key does not exist or has expired.
func HFSCFetch(key string) string {
	hfscStore.mu.Lock()
	defer hfscStore.mu.Unlock()
	entry, ok := hfscStore.entries[key]
	if !ok || time.Now().After(entry.expiresAt) {
		delete(hfscStore.entries, key)
		return ""
	}
	return entry.data
}

// RunAutonomous drives the entire Socratic dialectic loop autonomously using
// the shared LLM backplane. It returns strictly-typed JSON conforming to
// MachineResponse. maxRounds <= 0 means use the server's default cap.
//
// MUST be called without holding the Machine mutex.
//
//nolint:gocognit // RunAutonomous coordinates LLM retries, FSM feedback, and convergence bounds.
func (m *Machine) RunAutonomous(ctx context.Context, problem string, maxRounds int) (string, error) {
	sessionID := SessionIDFromContext(ctx)
	pipeline := m.getPipeline(sessionID)
	// --- Connection Failsafe Check ---
	if m.llm == nil {
		return "", fmt.Errorf("machine_mode requires a configured LLM backplane (standalone mode is not supported)")
	}
	if !m.llm.Available() {
		return "", fmt.Errorf("machine_mode: LLM backplane is currently unreachable")
	}

	if maxRounds <= 0 {
		maxRounds = maxDialecticRounds
	}

	// Reset pipeline to StageIdle before each autonomous session.
	// Machine is shared across callers — a previously timed-out or aborted
	// dialectic may have left CurrentStage in a non-Idle state, which would
	// cause the INITIALIZE call below to fail with "invalid stage".
	//
	// Compute effective minimum rounds based on caller's maxRounds budget.
	// Machine-mode callers with tight latency (maxRounds ≤ 5) get a reduced
	// minimum to avoid spending 90%+ of their budget on forced re-looping.
	effectiveMin := minDialecticRounds
	if maxRounds <= 5 {
		effectiveMin = max(min(2, maxRounds-1), 1)
	}
	pipeline.mu.Lock()
	pipeline.Reset(true, pipeline.ContextBytes, pipeline.TokensEst, effectiveMin)
	pipeline.mu.Unlock()

	completed := false
	defer func() {
		pipeline.mu.Lock()
		if completed {
			pipeline.CurrentStage = StageAporia
		} else {
			pipeline.CurrentStage = StageIdle
		}
		pipeline.mu.Unlock()
	}()

	// Step 1: INITIALIZE
	initReq := Request{Stage: string(StageInitialize), Problem: problem}
	output, err := m.Process(ctx, initReq)
	if err != nil {
		return "", fmt.Errorf("machine_mode: INITIALIZE failed: %w", err)
	}

	// Parse the directive from the JSON output
	var directive struct {
		NextStage string `json:"next_stage"`
		Directive string `json:"directive"`
	}
	if jsonErr := json.Unmarshal([]byte(output), &directive); jsonErr != nil {
		return "", fmt.Errorf("machine_mode: failed to parse INITIALIZE output: %w", jsonErr)
	}

	roundCount := 0
	deadlockCount := 0
	const maxDeadlocks = 3
	const maxParseRetries = 3

	// Step 2: Autonomous loop — drive FSM until APORIA
	for {
		// Context cascading: respect caller's timeout
		if ctx.Err() != nil {
			return "", ctx.Err()
		}

		roundCount++
		if roundCount > maxRounds*4 { // safety bound: 4 stages per round max
			slog.Warn("machine_mode: absolute iteration cap reached, forcing APORIA",
				"iterations", roundCount)
			break
		}

		// Deadlock circuit breaker
		if deadlockCount >= maxDeadlocks {
			slog.Warn("machine_mode: deadlock circuit breaker triggered",
				"deadlock_count", deadlockCount)
			break
		}

		nextStage := directive.NextStage
		directiveText := directive.Directive

		// Update pipeline stage BEFORE the LLM call so the dashboard
		// accurately reflects what stage the machine is currently working on.
		pipeline.mu.Lock()
		pipeline.CurrentStage = Stage(nextStage)
		pipeline.mu.Unlock()

		// Build the LLM prompt with strict JSON schema enforcement
		schemaPrompt := m.buildStageSchemaPrompt(nextStage, directiveText, problem)

		// --- Parse Retry Block (Fixes 2.2, 2.4) ---
		var stageResp llmStageResponse
		var parseErr error
	retryLoop:
		for retries := range maxParseRetries {
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
			// Fix 2.2: Check availability before wasting retry budget
			if !m.llm.Available() {
				parseErr = mcplib.ErrBackplaneUnavailable
				break
			}

			parseErr = m.llm.JSONResponse(ctx, schemaPrompt, &stageResp)
			if parseErr == nil {
				break
			}
			slog.Warn("machine_mode: LLM parse retry",
				"attempt", retries+1,
				"error", parseErr)

			// Fix 2.4: Error-classified retry backoff
			switch {
			case errors.Is(parseErr, mcplib.ErrBackplaneRateLimited):
				select {
				case <-ctx.Done():
					return "", ctx.Err()
				//nolint:gosec // G404: non-crypto jitter for retry spacing, not security-sensitive
				case <-time.After(5*time.Second + time.Duration(rand.IntN(1000))*time.Millisecond):
				}
			case errors.Is(parseErr, mcplib.ErrBackplaneUnavailable):
				break retryLoop // breaker is tripped — exit retry loop entirely
			default: // JSON parse error
				select {
				case <-ctx.Done():
					return "", ctx.Err()
				//nolint:gosec // G404: non-crypto jitter for retry spacing, not security-sensitive
				case <-time.After(time.Duration(200*(1<<retries))*time.Millisecond + time.Duration(rand.IntN(500))*time.Millisecond):
				}
			}
		}
		if parseErr != nil {
			return "", fmt.Errorf("machine_mode: LLM failed after %d retries at stage %s: %w",
				maxParseRetries, nextStage, parseErr)
		}

		// Ensure the stage is set correctly
		if stageResp.Stage == "" {
			stageResp.Stage = nextStage
		}

		// Build the socratic.Request from the LLM response
		req := Request{
			Stage:              stageResp.Stage,
			Lemma:              stageResp.Lemma,
			IsSatisfied:        stageResp.IsSatisfied,
			AporiaSynthesis:    stageResp.AporiaSynthesis,
			SynthesisCritique:  stageResp.SynthesisCritique,
			ParadoxDetected:    stageResp.ParadoxDetected,
			ResolutionStrategy: stageResp.ResolutionStrategy,
		}

		// Feed back into the FSM
		output, err = m.Process(ctx, req)
		if err != nil {
			// Soft errors (invalid stage, missing lemma) — the LLM hallucinated.
			// Log and try to recover by re-prompting.
			if err.Error() == "invalid stage" || err.Error() == "missing lemma" || err.Error() == "missing aporia_synthesis" {
				slog.Warn("machine_mode: FSM soft error, re-prompting",
					"error", err.Error(),
					"stage", stageResp.Stage)
				deadlockCount++
				continue
			}
			return "", fmt.Errorf("machine_mode: Process failed at stage %s: %w", stageResp.Stage, err)
		}

		// Parse the output to check if we've reached APORIA completion
		var nextDirective struct {
			NextStage           string `json:"next_stage"`
			Directive           string `json:"directive"`
			Status              string `json:"status,omitzero"`
			SynthesisRejected   bool   `json:"synthesis_rejected,omitzero"`
			ConvergenceRejected bool   `json:"convergence_rejected,omitzero"`
		}
		if jsonErr := json.Unmarshal([]byte(output), &nextDirective); jsonErr != nil {
			// If output is not JSON, we've hit MuzzleAndSynthesize (final output).
			// This means APORIA completed successfully in interactive mode.
			completed = true
			return m.buildMachineResponseFromSynthesis(ctx, output)
		}

		// Handle paradox_detected loop
		if nextDirective.Status == "paradox_detected" {
			deadlockCount++
			// Re-prompt for resolution
			directive.NextStage = string(StageAporia)
			directive.Directive = nextDirective.Directive
			continue
		}

		// Performance bounding: if we've hit maxRounds and we're still in dialectic
		if roundCount >= maxRounds*3 && nextDirective.NextStage != string(StageAporia) {
			slog.Warn("machine_mode: max rounds approaching, forcing APORIA transition",
				"round", roundCount, "maxRounds", maxRounds,
				"stuck_stage", nextDirective.NextStage)
			// Force a transition to APORIA by synthesizing what we have
			completed = true
			return m.forceAporiaTransition(ctx, problem, pipeline)
		}

		// Update directive for next iteration
		directive.NextStage = nextDirective.NextStage
		directive.Directive = nextDirective.Directive
	}

	// If we broke out of the loop (deadlock or cap), force a final synthesis
	completed = true
	return m.forceAporiaTransition(ctx, problem, pipeline)
}

// buildStageSchemaPrompt constructs a prompt that forces the LLM to output
// a strictly-typed JSON conforming to llmStageResponse for the given stage.
func (m *Machine) buildStageSchemaPrompt(stage, directive, problem string) string {
	var schemaHint string

	switch stage {
	case string(StageAporia):
		schemaHint = `You MUST respond with ONLY valid JSON matching this exact schema:
{"stage": "APORIA", "aporia_synthesis": "<comprehensive final synthesis>", "synthesis_critique": "<explicit self-evaluation>", "paradox_detected": false}
If synthesis is impossible, set paradox_detected to true and provide resolution_strategy.`
	case "ANTITHESIS_EVALUATE":
		schemaHint = `You MUST respond with ONLY valid JSON matching this exact schema:
{"stage": "ANTITHESIS_EVALUATE", "lemma": "<exactly two sentences: evaluation, then evidence>", "is_satisfied": true/false}`
	default:
		schemaHint = fmt.Sprintf(`You MUST respond with ONLY valid JSON matching this exact schema:
{"stage": %q, "lemma": "<exactly two sentences: your position, then specific technical evidence>"}`, stage)
	}

	return fmt.Sprintf(
		"You are an autonomous Socratic dialectic engine evaluating the following problem:\n\n"+
			"PROBLEM: %s\n\n"+
			"CURRENT DIRECTIVE: %s\n\n"+
			"%s\n\n"+
			"Return strictly valid JSON without markdown wrapping or code blocks.",
		problem, directive, schemaHint,
	)
}

// forceAporiaTransition forces the FSM into APORIA and generates a final synthesis
// using the LLM. Used when maxRounds or deadlock limits are reached.
func (m *Machine) forceAporiaTransition(ctx context.Context, problem string, pipeline *PipelineState) (string, error) {
	if ctx.Err() != nil {
		return "", ctx.Err()
	}

	// Update stage for dashboard visibility during forced synthesis
	pipeline.mu.Lock()
	pipeline.CurrentStage = StageAporia
	pipeline.mu.Unlock()

	// Snapshot the transcript for the final synthesis prompt
	pipeline.mu.Lock()
	transcript := m.buildTranscriptStr(pipeline)
	pipeline.mu.Unlock()

	// Fix 2.3: If LLM is unavailable, synthesize mechanically from the trail
	if m.llm == nil || !m.llm.Available() {
		return m.buildMechanicalSynthesis(problem, transcript, pipeline)
	}

	synthesisPrompt := fmt.Sprintf(
		"You are the Aporia Engine finalizing a Socratic dialectic.\n\n"+
			"ORIGINAL PROBLEM: %s\n\n"+
			"DIALECTIC TRAIL: %s\n\n"+
			"The dialectic round limit has been reached. You MUST produce a final comprehensive synthesis.\n\n"+
			`You MUST respond with ONLY valid JSON matching this exact schema:
{"stage": "APORIA", "aporia_synthesis": "<comprehensive final synthesis resolving all contradictions>", "synthesis_critique": "<explicit self-evaluation of strengths and weaknesses>", "paradox_detected": false}`,
		problem, transcript,
	)

	var stageResp llmStageResponse
	if err := m.llm.JSONResponse(ctx, synthesisPrompt, &stageResp); err != nil {
		return "", fmt.Errorf("machine_mode: forced APORIA synthesis failed: %w", err)
	}

	// Feed the forced synthesis through the FSM
	req := Request{
		Stage:             string(StageAporia),
		AporiaSynthesis:   stageResp.AporiaSynthesis,
		SynthesisCritique: stageResp.SynthesisCritique,
		ParadoxDetected:   false,
	}

	// If the FSM is not in APORIA state, reset and re-initialize to force it
	pipeline.mu.Lock()
	if pipeline.CurrentStage != StageAporia {
		pipeline.CurrentStage = StageAporia
	}
	pipeline.mu.Unlock()

	output, err := m.Process(ctx, req)
	if err != nil {
		return "", fmt.Errorf("machine_mode: forced APORIA Process failed: %w", err)
	}

	return m.buildMachineResponseFromSynthesis(ctx, output)
}

// buildMachineResponseFromSynthesis converts the interactive MuzzleAndSynthesize
// output into the strictly-typed MachineResponse JSON schema.
func (m *Machine) buildMachineResponseFromSynthesis(ctx context.Context, rawOutput string) (string, error) {
	// Store the full lemma trail in HFSC for out-of-band diagnostic retrieval
	hfscKey := HFSCStore(rawOutput)

	// Use the LLM to structure the raw synthesis into the MachineResponse schema
	structurePrompt := fmt.Sprintf(
		"Convert the following Socratic dialectic synthesis into a structured JSON response.\n\n"+
			"RAW SYNTHESIS:\n%s\n\n"+
			`You MUST respond with ONLY valid JSON matching this exact schema:
{
  "decisions": [
    {"topic": "<topic name>", "decision": "<decision>", "rationale": "<rationale>"}
  ],
  "outstanding_questions": ["<question 1>", "<question 2>"]
}

Rules:
- Include ALL key decisions from the synthesis as separate decision objects.
- Each decision must have a clear topic, decision, and rationale.
- List any unresolved questions or risks in outstanding_questions.
- If no questions remain, use an empty array.`,
		rawOutput,
	)

	var machineResp MachineResponse
	if err := m.llm.JSONResponse(ctx, structurePrompt, &machineResp); err != nil {
		// Fallback: return a minimal structured response with the raw synthesis
		slog.Warn("machine_mode: failed to structure synthesis, using fallback",
			"error", err)
		machineResp = MachineResponse{
			Decisions: []MachineDecision{
				{
					Topic:     "Synthesis",
					Decision:  "Complete",
					Rationale: rawOutput,
				},
			},
			OutstandingQuestions: []string{},
			DiagnosticLogHFSCKey: hfscKey,
		}
	} else {
		machineResp.DiagnosticLogHFSCKey = hfscKey
	}

	result, err := json.Marshal(machineResp)
	if err != nil {
		return "", fmt.Errorf("machine_mode: failed to marshal MachineResponse: %w", err)
	}
	return string(result), nil
}

// buildMechanicalSynthesis generates a structured MachineResponse from the
// accumulated lemma trail without LLM assistance. This is the fallback path
// when the LLM backplane is unavailable during forceAporiaTransition (Fix 2.3).
func (m *Machine) buildMechanicalSynthesis(problem, transcript string, pipeline *PipelineState) (string, error) {
	slog.Warn("machine_mode: building mechanical synthesis (LLM unavailable)")

	// Store the full trail in HFSC for diagnostic retrieval
	hfscKey := HFSCStore(transcript)

	// Extract individual lemma texts from the trail for the decisions array
	pipeline.mu.RLock()
	trail := slices.Clone(pipeline.LemmaTrail)
	pipeline.mu.RUnlock()

	decisions := make([]MachineDecision, 0, len(trail)+1)
	decisions = append(decisions, MachineDecision{
		Topic:     "Mechanical Synthesis",
		Decision:  "Partial dialectic completed (LLM unavailable for final synthesis)",
		Rationale: fmt.Sprintf("Problem: %s\n\nThe dialectic trail contains %d lemmas. Review the HFSC diagnostic log for the full transcript.", problem, len(trail)),
	})

	// Include each thesis/antithesis lemma as a separate decision
	for _, entry := range trail {
		if len(entry.Text) > 200 {
			decisions = append(decisions, MachineDecision{
				Topic:     fmt.Sprintf("%s (Round %d)", entry.Stage, entry.Round),
				Decision:  entry.Text[:200] + "...",
				Rationale: "Extracted from dialectic trail (truncated)",
			})
		} else if entry.Text != "" {
			decisions = append(decisions, MachineDecision{
				Topic:     fmt.Sprintf("%s (Round %d)", entry.Stage, entry.Round),
				Decision:  entry.Text,
				Rationale: "Extracted from dialectic trail",
			})
		}
	}

	resp := MachineResponse{
		Decisions:            decisions,
		OutstandingQuestions: []string{"Full Socratic synthesis unavailable — review HFSC diagnostic log manually"},
		DiagnosticLogHFSCKey: hfscKey,
	}

	result, err := json.Marshal(resp)
	if err != nil {
		return "", fmt.Errorf("machine_mode: failed to marshal mechanical synthesis: %w", err)
	}
	return string(result), nil
}
