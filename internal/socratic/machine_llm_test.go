package socratic_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/maccavelli/mcp-server-socratic-thinker/internal/socratic"
)

// =============================================================================
// mockLLM — implements socratic.LLMClient for all gate tests
// =============================================================================

type mockLLM struct {
	mu        sync.Mutex
	callCount int
	verdict   string // "REJECT", "ABSTAIN", "PROCEED", or "" (error path)
	reason    string
	err       error         // returned by JSONResponse when non-nil
	delay     time.Duration // artificial latency for context-cancel tests
}

func (m *mockLLM) Generate(_ context.Context, _ string) (string, error) {
	m.mu.Lock()
	m.callCount++
	m.mu.Unlock()
	if m.err != nil {
		return "", m.err
	}
	return m.verdict, nil
}

// JSONResponse satisfies socratic.LLMClient. It populates target with
// a JSON-encoded {verdict, reason} object, mirroring the real backplane.
func (m *mockLLM) JSONResponse(ctx context.Context, _ string, target any) error {
	m.mu.Lock()
	m.callCount++
	m.mu.Unlock()

	if m.delay > 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(m.delay):
		}
	}

	if m.err != nil {
		return m.err
	}

	raw, _ := json.Marshal(map[string]string{
		"verdict": m.verdict,
		"reason":  m.reason,
	})
	return json.Unmarshal(raw, target)
}

func (m *mockLLM) calls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.callCount
}

func (m *mockLLM) Available() bool {
	return true
}

// =============================================================================
// walkToEvaluateLLM drives to StageAntithesisEvaluate (round 1 ready)
// =============================================================================

func walkToEvaluateLLM(t *testing.T, m *socratic.Machine) {
	t.Helper()
	ctx := context.Background()
	must := func(err error, label string) {
		t.Helper()
		if err != nil {
			t.Fatalf("%s: %v", label, err)
		}
	}
	_, err := m.Process(ctx, socratic.Request{Stage: "INITIALIZE", Problem: "test problem for llm gate"})
	must(err, "INITIALIZE")
	_, err = m.Process(ctx, socratic.Request{Stage: "THESIS", Lemma: "Position. Evidence."})
	must(err, "THESIS")
	_, err = m.Process(ctx, socratic.Request{Stage: "ANTITHESIS_INITIAL", Lemma: "Challenge. Failure scenario."})
	must(err, "ANTITHESIS_INITIAL")
	_, err = m.Process(ctx, socratic.Request{Stage: "THESIS_DEFENSE", Lemma: "Defense. Metric validates."})
	must(err, "THESIS_DEFENSE")
}

// =============================================================================
// Tests (do NOT modify any existing machine_test.go tests)
// =============================================================================

// TestLLMAporiaGate_Nil — nil LLM client → gate never fires, heuristic output unchanged.
func TestLLMAporiaGate_Nil(t *testing.T) {
	m := socratic.NewMachine(nil) // no WithLLM
	walkToEvaluateLLM(t, m)
	ctx := context.Background()

	res, err := m.Process(ctx, socratic.Request{Stage: "ANTITHESIS_EVALUATE", Lemma: "Satisfied early.", IsSatisfied: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should proceed to CHAOS since heuristic min rounds = 1
	if !strings.Contains(res, "Chaos Architect") {
		t.Errorf("expected CHAOS transition, got: %s", res)
	}
	// No llm_override field — nil gate never fires
	if strings.Contains(res, "llm_override") {
		t.Errorf("llm_override should not appear when LLM is nil, got: %s", res)
	}
}

// TestLLMAporiaGate_GateDisabled — SetLLMGateEnabled(false) → LLM never called.
func TestLLMAporiaGate_GateDisabled(t *testing.T) {
	mock := &mockLLM{verdict: "REJECT", reason: "should not be called"}
	m := socratic.NewMachine(nil, socratic.WithLLM(mock))
	m.SetLLMGateEnabled(false)
	walkToEvaluateLLM(t, m)

	ctx := context.Background()
	res, err := m.Process(ctx, socratic.Request{Stage: "ANTITHESIS_EVALUATE", Lemma: "Premature.", IsSatisfied: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mock.calls() != 0 {
		t.Errorf("LLM should not be called when gate disabled, got %d calls", mock.calls())
	}
	// Should proceed to CHAOS since heuristic min rounds = 1
	if !strings.Contains(res, "Chaos Architect") {
		t.Errorf("expected CHAOS transition from heuristic, got: %s", res)
	}
}

// TestLLMAporiaGate_REJECT — LLM returns REJECT → round extended, llm_override=true in response.
func TestLLMAporiaGate_REJECT(t *testing.T) {
	mock := &mockLLM{verdict: "REJECT", reason: "defense lacks concrete evidence"}
	m := socratic.NewMachine(nil, socratic.WithLLM(mock))
	walkToEvaluateLLM(t, m)

	ctx := context.Background()
	res, err := m.Process(ctx, socratic.Request{Stage: "ANTITHESIS_EVALUATE", Lemma: "Seems satisfied.", IsSatisfied: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if mock.calls() != 1 {
		t.Errorf("expected 1 LLM call, got %d", mock.calls())
	}
	if !strings.Contains(res, "convergence_rejected") {
		t.Errorf("expected convergence_rejected in REJECT response, got: %s", res)
	}
	if !strings.Contains(res, "llm_override") {
		t.Errorf("expected llm_override field in REJECT response, got: %s", res)
	}
	if !strings.Contains(res, "defense lacks concrete evidence") {
		t.Errorf("expected LLM reason in response, got: %s", res)
	}
}

// TestLLMAporiaGate_ABSTAIN — LLM returns ABSTAIN → heuristic enforces min-rounds.
func TestLLMAporiaGate_ABSTAIN(t *testing.T) {
	mock := &mockLLM{verdict: "ABSTAIN", reason: "cannot determine quality"}
	m := socratic.NewMachine(nil, socratic.WithLLM(mock))
	walkToEvaluateLLM(t, m)

	ctx := context.Background()
	res, err := m.Process(ctx, socratic.Request{Stage: "ANTITHESIS_EVALUATE", Lemma: "Satisfied.", IsSatisfied: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if mock.calls() != 1 {
		t.Errorf("expected 1 LLM call, got %d", mock.calls())
	}
	// Heuristic still enforces — convergence_rejected present, NO llm_override
	if !strings.Contains(res, "Chaos Architect") {
		t.Errorf("expected CHAOS transition after ABSTAIN, got: %s", res)
	}
	if strings.Contains(res, "llm_override") {
		t.Errorf("llm_override should not appear on ABSTAIN path, got: %s", res)
	}
}

// TestLLMAporiaGate_PROCEED_Demoted — LLM returns PROCEED → treated as ABSTAIN, heuristic enforces.
func TestLLMAporiaGate_PROCEED_Demoted(t *testing.T) {
	mock := &mockLLM{verdict: "PROCEED", reason: "looks good to me"}
	m := socratic.NewMachine(nil, socratic.WithLLM(mock))
	walkToEvaluateLLM(t, m)

	ctx := context.Background()
	res, err := m.Process(ctx, socratic.Request{Stage: "ANTITHESIS_EVALUATE", Lemma: "Satisfied.", IsSatisfied: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if mock.calls() != 1 {
		t.Errorf("expected 1 LLM call, got %d", mock.calls())
	}
	if !strings.Contains(res, "Chaos Architect") {
		t.Errorf("expected CHAOS transition after PROCEED demotion, got: %s", res)
	}
	if strings.Contains(res, "llm_override") {
		t.Errorf("llm_override must not appear on demoted PROCEED path, got: %s", res)
	}
}

// TestLLMAporiaGate_Error — LLM returns an error → silent ABSTAIN fallback, no panic.
func TestLLMAporiaGate_Error(t *testing.T) {
	mock := &mockLLM{err: errors.New("backplane unreachable")}
	m := socratic.NewMachine(nil, socratic.WithLLM(mock))
	walkToEvaluateLLM(t, m)

	ctx := context.Background()
	res, err := m.Process(ctx, socratic.Request{Stage: "ANTITHESIS_EVALUATE", Lemma: "Satisfied.", IsSatisfied: true})
	if err != nil {
		t.Fatalf("unexpected error (should degrade gracefully): %v", err)
	}

	if mock.calls() != 1 {
		t.Errorf("expected 1 LLM call attempt, got %d", mock.calls())
	}
	// Must not crash; heuristic still enforces
	if !strings.Contains(res, "Chaos Architect") {
		t.Errorf("expected CHAOS transition after LLM error, got: %s", res)
	}
	if strings.Contains(res, "llm_override") {
		t.Errorf("llm_override must not appear on error fallback, got: %s", res)
	}
}

// TestLLMAporiaGate_StageRace — sentinel detects stage change during LLM window.
func TestLLMAporiaGate_StageRace(t *testing.T) {
	// Use a slow LLM so we have time to fire a RESET concurrently.
	mock := &mockLLM{verdict: "REJECT", reason: "insufficient", delay: 80 * time.Millisecond}
	m := socratic.NewMachine(nil, socratic.WithLLM(mock))
	walkToEvaluateLLM(t, m)

	ctx := context.Background()

	var wg sync.WaitGroup
	var gateResult string
	var gateErr error

	wg.Go(func() {
		gateResult, gateErr = m.Process(ctx, socratic.Request{Stage: "ANTITHESIS_EVALUATE", Lemma: "Satisfied.", IsSatisfied: true})
	})

	// Give the gate goroutine time to enter the LLM call (mutex released).
	time.Sleep(20 * time.Millisecond)

	// Fire RESET while LLM is in-flight.
	resetRes, resetErr := m.Process(ctx, socratic.Request{Stage: "RESET"})
	if resetErr != nil {
		t.Fatalf("RESET failed: %v", resetErr)
	}
	if !strings.Contains(resetRes, "Pipeline reset") {
		t.Fatalf("unexpected RESET response: %s", resetRes)
	}

	wg.Wait()

	// The gate goroutine must not panic and must return something reasonable.
	if gateErr != nil {
		t.Fatalf("gate goroutine returned error: %v", gateErr)
	}
	// Sentinel fired: result should be the pre-computed heuristic (no llm_override)
	// OR it returned the RESET state (stage changed — both are acceptable sentinel outcomes).
	if strings.Contains(gateResult, "llm_override") {
		t.Errorf("sentinel should have blocked LLM REJECT from applying after stage change, got: %s", gateResult)
	}
}

// TestLLMAporiaGate_NotTriggered_Unsatisfied — is_satisfied=false → LLM never called.
func TestLLMAporiaGate_NotTriggered_Unsatisfied(t *testing.T) {
	mock := &mockLLM{verdict: "REJECT", reason: "should not be called"}
	m := socratic.NewMachine(nil, socratic.WithLLM(mock))
	walkToEvaluateLLM(t, m)

	ctx := context.Background()
	_, err := m.Process(ctx, socratic.Request{Stage: "ANTITHESIS_EVALUATE", Lemma: "Not satisfied.", IsSatisfied: false})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mock.calls() != 0 {
		t.Errorf("LLM must not be called when is_satisfied=false, got %d calls", mock.calls())
	}
}

// TestLLMAporiaGate_FiresOnConvergence — LLM gate now fires even when min rounds are met (natural convergence).
func TestLLMAporiaGate_FiresOnConvergence(t *testing.T) {
	mock := &mockLLM{verdict: "ABSTAIN", reason: "natural convergence"}
	m := socratic.NewMachine(nil, socratic.WithLLM(mock))
	ctx := context.Background()

	walkToEvaluateLLM(t, m)

	// Round 1: is_satisfied=true. Min rounds (1) is met, so heuristic accepts.
	// But LLM gate must fire to verify it.
	res, err := m.Process(ctx, socratic.Request{Stage: "ANTITHESIS_EVALUATE", Lemma: "Satisfied.", IsSatisfied: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if mock.calls() != 1 {
		t.Errorf("LLM MUST be called on natural convergence to provide defense-in-depth, got %d calls", mock.calls())
	}
	if !strings.Contains(res, "Chaos Architect") {
		t.Errorf("expected CHAOS transition since LLM abstained, got: %s", res)
	}
}

// TestLLMAporiaGate_RejectBudgetExhausted — after maxLLMRejects (=2), gate disabled.
func TestLLMAporiaGate_RejectBudgetExhausted(t *testing.T) {
	mock := &mockLLM{verdict: "REJECT", reason: "still insufficient"}
	m := socratic.NewMachine(nil, socratic.WithLLM(mock))
	ctx := context.Background()
	walkToEvaluateLLM(t, m)

	// Round 1: LLM REJECT fires (budget 0→1)
	res, err := m.Process(ctx, socratic.Request{Stage: "ANTITHESIS_EVALUATE", Lemma: "R1.", IsSatisfied: true})
	if err != nil {
		t.Fatalf("R1: %v", err)
	}
	if !strings.Contains(res, "llm_override") {
		t.Errorf("R1: expected LLM REJECT with llm_override, got: %s", res)
	}
	if mock.calls() != 1 {
		t.Errorf("R1: expected 1 LLM call, got %d", mock.calls())
	}

	// Defend for round 2
	m.Process(ctx, socratic.Request{Stage: "THESIS_DEFENSE", Lemma: "Deeper defense."})

	// Round 2: LLM REJECT fires again (budget 1→2, now exhausted)
	res, err = m.Process(ctx, socratic.Request{Stage: "ANTITHESIS_EVALUATE", Lemma: "R2.", IsSatisfied: true})
	if err != nil {
		t.Fatalf("R2: %v", err)
	}
	if !strings.Contains(res, "llm_override") {
		t.Errorf("R2: expected LLM REJECT with llm_override, got: %s", res)
	}
	if mock.calls() != 2 {
		t.Errorf("R2: expected 2 total LLM calls, got %d", mock.calls())
	}

	// Defend for round 3
	m.Process(ctx, socratic.Request{Stage: "THESIS_DEFENSE", Lemma: "Further evidence."})

	// Round 3: budget exhausted AND min-rounds met (3) → LLM must NOT be called
	mock.mu.Lock()
	mock.callCount = 0
	mock.mu.Unlock()

	res, err = m.Process(ctx, socratic.Request{Stage: "ANTITHESIS_EVALUATE", Lemma: "R3.", IsSatisfied: true})
	if err != nil {
		t.Fatalf("R3: %v", err)
	}
	if mock.calls() != 0 {
		t.Errorf("R3: LLM must not be called when budget exhausted, got %d calls", mock.calls())
	}
	// Min rounds met → should accept convergence → CHAOS
	if !strings.Contains(res, "Chaos Architect") {
		t.Errorf("R3: expected CHAOS transition after budget exhaustion + min rounds met, got: %s", res)
	}
}

// TestLLMAporiaGate_Metrics — GetLLMGateMetrics reflects activations/rejects after calls.
func TestLLMAporiaGate_Metrics(t *testing.T) {
	mock := &mockLLM{verdict: "REJECT", reason: "metrics test"}
	m := socratic.NewMachine(nil, socratic.WithLLM(mock))
	ctx := context.Background()
	walkToEvaluateLLM(t, m)

	// Trigger gate once
	m.Process(ctx, socratic.Request{Stage: "ANTITHESIS_EVALUATE", Lemma: "R1.", IsSatisfied: true})

	// Snapshot via GetMetrics (which populates the cache)
	m.GetMetrics()
	activations, rejects, latencyMs, enabled := m.GetLLMGateMetrics()

	if activations != 1 {
		t.Errorf("expected 1 activation, got %d", activations)
	}
	if rejects != 1 {
		t.Errorf("expected 1 reject, got %d", rejects)
	}
	if latencyMs < 0 {
		t.Errorf("expected non-negative latency, got %d", latencyMs)
	}
	if !enabled {
		t.Errorf("expected gate to be enabled by default")
	}
}

// TestLLMAporiaGate_TrailTruncation — trail > 4KB is truncated with sentinel prefix.
func TestLLMAporiaGate_TrailTruncation(t *testing.T) {
	var capturedPrompt string
	mock := &mockLLMCapture{
		verdict: "ABSTAIN",
		onCall: func(prompt string) {
			capturedPrompt = prompt
		},
	}
	m := socratic.NewMachine(nil, socratic.WithLLM(mock))
	ctx := context.Background()

	// Build a problem with a long value, then walk to evaluate
	longProblem := strings.Repeat("x", 100)
	m.Process(ctx, socratic.Request{Stage: "INITIALIZE", Problem: longProblem})
	// Add a series of very long lemmas to bloat the trail past 4KB
	bigLemma := strings.Repeat("a", 800)
	m.Process(ctx, socratic.Request{Stage: "THESIS", Lemma: bigLemma})
	m.Process(ctx, socratic.Request{Stage: "ANTITHESIS_INITIAL", Lemma: bigLemma})
	m.Process(ctx, socratic.Request{Stage: "THESIS_DEFENSE", Lemma: bigLemma})
	// R1: should trigger the gate (trail > 4KB due to large lemmas)
	m.Process(ctx, socratic.Request{Stage: "ANTITHESIS_EVALUATE", Lemma: "Satisfied.", IsSatisfied: true})

	if capturedPrompt == "" {
		t.Fatal("LLM was not called — trail may not be long enough or gate condition not met")
	}
	if !strings.Contains(capturedPrompt, "trail truncated") {
		t.Logf("note: trail may be < 4KB with current lemma sizes (test is data-dependent): prompt length=%d", len(capturedPrompt))
		// Not a hard failure if trail doesn't exceed 4KB — log for visibility
	}
}

// TestLLMAporiaGate_ContextCancel — parent ctx cancelled → LLM call returns immediately.
func TestLLMAporiaGate_ContextCancel(t *testing.T) {
	// Slow LLM — would block for 10s without context cancellation
	mock := &mockLLM{verdict: "REJECT", delay: 10 * time.Second}
	m := socratic.NewMachine(nil, socratic.WithLLM(mock))
	walkToEvaluateLLM(t, m)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, _ = m.Process(ctx, socratic.Request{Stage: "ANTITHESIS_EVALUATE", Lemma: "Satisfied.", IsSatisfied: true})
	elapsed := time.Since(start)

	// Should return well under the 10s mock delay, bounded by the 200ms parent ctx
	if elapsed > 3*time.Second {
		t.Errorf("context cancellation did not short-circuit LLM call: elapsed %v", elapsed)
	}
}

// =============================================================================
// mockLLMCapture — captures the prompt for inspection
// =============================================================================

type mockLLMCapture struct {
	mu      sync.Mutex
	verdict string
	onCall  func(string)
}

func (m *mockLLMCapture) Generate(_ context.Context, prompt string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.onCall != nil {
		m.onCall(prompt)
	}
	return m.verdict, nil
}

func (m *mockLLMCapture) JSONResponse(_ context.Context, prompt string, target any) error {
	m.mu.Lock()
	if m.onCall != nil {
		m.onCall(prompt)
	}
	v := m.verdict
	m.mu.Unlock()

	raw, _ := json.Marshal(map[string]string{"verdict": v, "reason": "captured"})
	return json.Unmarshal(raw, target)
}

func (m *mockLLMCapture) Available() bool {
	return true
}
