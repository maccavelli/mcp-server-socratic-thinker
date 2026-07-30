package socratic_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/maccavelli/mcp-server-socratic-thinker/internal/socratic"
)

// walkToEvaluate drives the pipeline from INITIALIZE through to the first ANTITHESIS_EVALUATE
// stage. Returns the pipeline at the point where ANTITHESIS_EVALUATE expects input.
func walkToEvaluate(t *testing.T, m *socratic.Machine) {
	t.Helper()
	ctx := context.Background()

	if _, err := m.Process(ctx, socratic.Request{Stage: "INITIALIZE", Problem: "test problem"}); err != nil {
		t.Fatalf("INITIALIZE: %v", err)
	}
	if _, err := m.Process(ctx, socratic.Request{Stage: "THESIS", Lemma: "Thesis position. Supporting evidence."}); err != nil {
		t.Fatalf("THESIS: %v", err)
	}
	if _, err := m.Process(ctx, socratic.Request{Stage: "ANTITHESIS_INITIAL", Lemma: "Challenge raised. Specific failure scenario."}); err != nil {
		t.Fatalf("ANTITHESIS_INITIAL: %v", err)
	}
	if _, err := m.Process(ctx, socratic.Request{Stage: "THESIS_DEFENSE", Lemma: "Defense holds. Metric validates it."}); err != nil {
		t.Fatalf("THESIS_DEFENSE: %v", err)
	}
}

// walkToConvergence drives the pipeline through the minimum dialectic rounds and
// achieves convergence, leaving the pipeline at StageChaos expecting CHAOS input.
func walkToConvergence(t *testing.T, m *socratic.Machine) {
	t.Helper()
	ctx := context.Background()
	walkToEvaluate(t, m)

	// Round 1: is_satisfied=true → should now succeed → CHAOS
	res, err := m.Process(ctx, socratic.Request{Stage: "ANTITHESIS_EVALUATE", Lemma: "Satisfied early. Addressed.", IsSatisfied: true})
	if err != nil {
		t.Fatalf("ANTITHESIS_EVALUATE R1: %v", err)
	}
	if !strings.Contains(res, "Chaos Architect") {
		t.Fatalf("expected Chaos Architect at R1, got: %s", res)
	}
}

func TestMachine_Process_HappyPath(t *testing.T) {
	m := socratic.NewMachine(nil)
	ctx := context.Background()

	// Walk through minimum 2 rounds to convergence (→ CHAOS)
	walkToConvergence(t, m)

	// CHAOS → routes to THESIS_DEFENSE (chaos rebuttal), NOT directly to APORIA
	res, err := m.Process(ctx, socratic.Request{Stage: "CHAOS", Lemma: "Black Swan event. Specific mechanism breaks consensus."})
	if err != nil {
		t.Fatalf("CHAOS failed: %v", err)
	}
	if !strings.Contains(res, "THESIS_DEFENSE") {
		t.Errorf("CHAOS should route to THESIS_DEFENSE for rebuttal, got: %s", res)
	}
	if !strings.Contains(res, "Black Swan") {
		t.Errorf("CHAOS directive should reference Black Swan, got: %s", res)
	}

	// Defend against Chaos Black Swan
	_, err = m.Process(ctx, socratic.Request{Stage: "THESIS_DEFENSE", Lemma: "Thesis survives the Black Swan. This component breaks under it."})
	if err != nil {
		t.Fatalf("THESIS_DEFENSE (chaos) failed: %v", err)
	}

	// Evaluate chaos rebuttal → satisfied → APORIA
	res, err = m.Process(ctx, socratic.Request{Stage: "ANTITHESIS_EVALUATE", Lemma: "Chaos defense adequate. Evidence confirms resilience.", IsSatisfied: true})
	if err != nil {
		t.Fatalf("ANTITHESIS_EVALUATE (chaos) failed: %v", err)
	}
	if !strings.Contains(res, "Aporia Engine") {
		t.Errorf("Expected Aporia Engine, got: %s", res)
	}

	// APORIA without synthesis_critique → should be rejected
	res, err = m.Process(ctx, socratic.Request{Stage: "APORIA", AporiaSynthesis: "Final synthesis."})
	if err != nil {
		t.Fatalf("APORIA (no critique) failed: %v", err)
	}
	if !strings.Contains(res, "synthesis_rejected") {
		t.Errorf("Expected synthesis_rejected without critique, got: %s", res)
	}

	// APORIA with both → success
	res, err = m.Process(ctx, socratic.Request{
		Stage:             "APORIA",
		AporiaSynthesis:   "Testing is only useful with users.",
		SynthesisCritique: "Handles core case well. Misses edge case of automated testing without users.",
	})
	if err != nil {
		t.Fatalf("APORIA failed: %v", err)
	}
	if !strings.Contains(res, "Socratic pipeline complete") {
		t.Errorf("Unexpected APORIA result: %s", res)
	}
	// Verify stage-tagged output
	if !strings.Contains(res, "[THESIS·R1]") {
		t.Errorf("Expected stage-tagged lemma trail, got: %s", res)
	}
	if !strings.Contains(res, "[CHAOS·R1]") {
		t.Errorf("Expected CHAOS stage tag in trail, got: %s", res)
	}
}

func TestMachine_Process_Reset(t *testing.T) {
	m := socratic.NewMachine(nil)
	ctx := context.Background()

	m.Process(ctx, socratic.Request{Stage: "INITIALIZE", Problem: "prob"})
	res, err := m.Process(ctx, socratic.Request{Stage: "RESET"})
	if err != nil {
		t.Fatalf("RESET failed: %v", err)
	}
	if !strings.Contains(res, "Pipeline reset") {
		t.Errorf("Unexpected RESET result: %s", res)
	}

	_, err = m.Process(ctx, socratic.Request{Stage: "THESIS", Lemma: "foo"})
	if err == nil {
		t.Error("Expected error for THESIS when IDLE")
	}
}

func TestMachine_Process_InvalidState(t *testing.T) {
	m := socratic.NewMachine(nil)
	ctx := context.Background()

	_, err := m.Process(ctx, socratic.Request{Stage: "THESIS", Lemma: "foo"})
	if err == nil || !strings.Contains(err.Error(), "invalid stage") {
		t.Errorf("Expected invalid stage error, got: %v", err)
	}

	m.Process(ctx, socratic.Request{Stage: "INITIALIZE", Problem: "prob"})

	_, err = m.Process(ctx, socratic.Request{Stage: "ANTITHESIS_INITIAL", Lemma: "foo"})
	if err == nil || !strings.Contains(err.Error(), "invalid stage") {
		t.Errorf("Expected invalid stage error, got: %v", err)
	}
}

func TestMachine_Process_MissingLemma(t *testing.T) {
	m := socratic.NewMachine(nil)
	ctx := context.Background()

	m.Process(ctx, socratic.Request{Stage: "INITIALIZE", Problem: "prob"})

	_, err := m.Process(ctx, socratic.Request{Stage: "THESIS", Lemma: ""})
	if err == nil || !strings.Contains(err.Error(), "missing lemma") {
		t.Errorf("Expected missing lemma error for THESIS, got: %v", err)
	}

	m.Process(ctx, socratic.Request{Stage: "THESIS", Lemma: "t"})

	_, err = m.Process(ctx, socratic.Request{Stage: "ANTITHESIS_INITIAL", Lemma: ""})
	if err == nil || !strings.Contains(err.Error(), "missing lemma") {
		t.Errorf("Expected missing lemma error for ANTITHESIS_INITIAL, got: %v", err)
	}

	m.Process(ctx, socratic.Request{Stage: "ANTITHESIS_INITIAL", Lemma: "a"})

	_, err = m.Process(ctx, socratic.Request{Stage: "THESIS_DEFENSE", Lemma: ""})
	if err == nil || !strings.Contains(err.Error(), "missing lemma") {
		t.Errorf("Expected missing lemma error for THESIS_DEFENSE, got: %v", err)
	}

	m.Process(ctx, socratic.Request{Stage: "THESIS_DEFENSE", Lemma: "td"})

	_, err = m.Process(ctx, socratic.Request{Stage: "ANTITHESIS_EVALUATE", Lemma: "", IsSatisfied: true})
	if err == nil || !strings.Contains(err.Error(), "missing lemma") {
		t.Errorf("Expected missing lemma error for ANTITHESIS_EVALUATE, got: %v", err)
	}

	// Round 1: accepted → CHAOS
	m.Process(ctx, socratic.Request{Stage: "ANTITHESIS_EVALUATE", Lemma: "ae", IsSatisfied: true})

	_, err = m.Process(ctx, socratic.Request{Stage: "CHAOS", Lemma: ""})
	if err == nil || !strings.Contains(err.Error(), "missing lemma") {
		t.Errorf("Expected missing lemma error for CHAOS, got: %v", err)
	}

	// CHAOS now routes to THESIS_DEFENSE (chaos rebuttal), so walk through it
	m.Process(ctx, socratic.Request{Stage: "CHAOS", Lemma: "c"})
	m.Process(ctx, socratic.Request{Stage: "THESIS_DEFENSE", Lemma: "chaos defense"})
	m.Process(ctx, socratic.Request{Stage: "ANTITHESIS_EVALUATE", Lemma: "chaos eval", IsSatisfied: true})

	_, err = m.Process(ctx, socratic.Request{Stage: "APORIA", AporiaSynthesis: ""})
	if err == nil || !strings.Contains(err.Error(), "missing aporia_synthesis") {
		t.Errorf("Expected missing aporia_synthesis error, got: %v", err)
	}
}

func TestMachine_Process_ParadoxDetected(t *testing.T) {
	m := socratic.NewMachine(nil)
	ctx := context.Background()

	walkToConvergence(t, m)
	// Walk through chaos rebuttal to reach APORIA
	m.Process(ctx, socratic.Request{Stage: "CHAOS", Lemma: "chaos challenge"})
	m.Process(ctx, socratic.Request{Stage: "THESIS_DEFENSE", Lemma: "chaos defense"})
	m.Process(ctx, socratic.Request{Stage: "ANTITHESIS_EVALUATE", Lemma: "chaos eval", IsSatisfied: true})

	res, err := m.Process(ctx, socratic.Request{Stage: "APORIA", ParadoxDetected: true})
	if err != nil {
		t.Fatalf("Paradox failed: %v", err)
	}
	if !strings.Contains(res, "paradox_detected") {
		t.Errorf("Expected paradox detected response, got: %s", res)
	}
}

func TestMachine_Process_Cancel(t *testing.T) {
	m := socratic.NewMachine(nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // instantly cancel

	_, err := m.Process(ctx, socratic.Request{Stage: "INITIALIZE", Problem: "prob"})
	if err == nil || !errors.Is(err, context.Canceled) {
		t.Errorf("Expected context.Canceled, got: %v", err)
	}
}

func TestGetMetrics_NonBlocking(t *testing.T) {
	m := socratic.NewMachine(nil)

	// Simulate contention: hold the mutex in a goroutine
	ctx := context.Background()
	m.Process(ctx, socratic.Request{Stage: "INITIALIZE", Problem: "test"})

	done := make(chan struct{})
	go func() {
		// Call GetMetrics — should return immediately via TryLock fallback
		stage, _, _, _ := m.GetMetrics()
		_ = stage
		close(done)
	}()

	select {
	case <-done:
		// Success: GetMetrics returned without blocking
	case <-time.After(1 * time.Second):
		t.Fatal("GetMetrics blocked — TryLock fallback failed")
	}
}

func TestGetMetrics_CachesValues(t *testing.T) {
	m := socratic.NewMachine(nil)
	ctx := context.Background()

	// Progress to THESIS stage
	m.Process(ctx, socratic.Request{Stage: "INITIALIZE", Problem: "cache test"})

	// First call should populate cache with THESIS stage
	stage, _, _, _ := m.GetMetrics()
	if stage != "THESIS" {
		t.Errorf("expected THESIS, got %s", stage)
	}

	// Move to next stage
	m.Process(ctx, socratic.Request{Stage: "THESIS", Lemma: "test lemma"})

	// Should now return ANTITHESIS_INITIAL
	stage, _, _, _ = m.GetMetrics()
	if stage != "ANTITHESIS_INITIAL" {
		t.Errorf("expected ANTITHESIS_INITIAL, got %s", stage)
	}
}

// --- New tests for hardened pipeline ---

func TestChaosRebuttalLoop(t *testing.T) {
	m := socratic.NewMachine(nil)
	ctx := context.Background()
	walkToConvergence(t, m)

	// CHAOS should route to THESIS_DEFENSE, not APORIA
	res, err := m.Process(ctx, socratic.Request{Stage: "CHAOS", Lemma: "Black Swan event. Breaks the consensus."})
	if err != nil {
		t.Fatalf("CHAOS: %v", err)
	}
	if !strings.Contains(res, "THESIS_DEFENSE") {
		t.Fatalf("CHAOS should route to THESIS_DEFENSE, got: %s", res)
	}

	// Defend against chaos
	_, err = m.Process(ctx, socratic.Request{Stage: "THESIS_DEFENSE", Lemma: "Defense holds. Evidence supports it."})
	if err != nil {
		t.Fatalf("THESIS_DEFENSE (chaos): %v", err)
	}

	// Evaluate → satisfied → should go to APORIA
	res, err = m.Process(ctx, socratic.Request{Stage: "ANTITHESIS_EVALUATE", Lemma: "Defense adequate. Resilience confirmed.", IsSatisfied: true})
	if err != nil {
		t.Fatalf("ANTITHESIS_EVALUATE (chaos): %v", err)
	}
	if !strings.Contains(res, "Aporia Engine") {
		t.Errorf("Expected Aporia Engine after chaos rebuttal, got: %s", res)
	}
}

func TestChaosMaxRounds(t *testing.T) {
	m := socratic.NewMachine(nil)
	ctx := context.Background()
	walkToConvergence(t, m)

	// Chaos round 1
	m.Process(ctx, socratic.Request{Stage: "CHAOS", Lemma: "Black Swan 1. Mechanism 1."})
	m.Process(ctx, socratic.Request{Stage: "THESIS_DEFENSE", Lemma: "Defense 1. Evidence 1."})
	// Not satisfied → loop back to CHAOS for round 2
	res, err := m.Process(ctx, socratic.Request{Stage: "ANTITHESIS_EVALUATE", Lemma: "Insufficient. Gap remains.", IsSatisfied: false})
	if err != nil {
		t.Fatalf("ANTITHESIS_EVALUATE (chaos R1 not satisfied): %v", err)
	}
	if !strings.Contains(res, "Chaos Architect") {
		t.Fatalf("Expected route back to CHAOS, got: %s", res)
	}

	// Chaos round 2
	m.Process(ctx, socratic.Request{Stage: "CHAOS", Lemma: "Black Swan 2. Mechanism 2."})
	m.Process(ctx, socratic.Request{Stage: "THESIS_DEFENSE", Lemma: "Defense 2. Evidence 2."})
	res, err = m.Process(ctx, socratic.Request{Stage: "ANTITHESIS_EVALUATE", Lemma: "Still insufficient.", IsSatisfied: false})
	if err != nil {
		t.Fatalf("ANTITHESIS_EVALUATE (chaos R2 not satisfied): %v", err)
	}
	if !strings.Contains(res, "Chaos Architect") {
		t.Fatalf("Expected route back to CHAOS, got: %s", res)
	}

	// Chaos round 3
	m.Process(ctx, socratic.Request{Stage: "CHAOS", Lemma: "Black Swan 3. Mechanism 3."})
	m.Process(ctx, socratic.Request{Stage: "THESIS_DEFENSE", Lemma: "Defense 3. Evidence 3."})
	// Not satisfied BUT ChaosRound=3 (max) → should force APORIA
	res, err = m.Process(ctx, socratic.Request{Stage: "ANTITHESIS_EVALUATE", Lemma: "Still insufficient. But max reached.", IsSatisfied: false})
	if err != nil {
		t.Fatalf("ANTITHESIS_EVALUATE (chaos R3 forced): %v", err)
	}
	if !strings.Contains(res, "Aporia Engine") {
		t.Errorf("Expected forced APORIA at max chaos rounds, got: %s", res)
	}
}

func TestMaxDialecticRounds(t *testing.T) {
	m := socratic.NewMachine(nil)
	ctx := context.Background()
	walkToEvaluate(t, m)

	// Drive through rounds 1-9 with is_satisfied=false each time
	for round := 1; round < 10; round++ {
		res, err := m.Process(ctx, socratic.Request{Stage: "ANTITHESIS_EVALUATE", Lemma: "Not satisfied. More analysis needed.", IsSatisfied: false})
		if err != nil {
			t.Fatalf("ANTITHESIS_EVALUATE round %d: %v", round, err)
		}
		if !strings.Contains(res, "THESIS_DEFENSE") {
			t.Fatalf("Expected THESIS_DEFENSE at round %d, got: %s", round, res)
		}
		// Verify round-indexed escalation in defense directives
		if round == 1 {
			if !strings.Contains(res, "ACCOUNTABILITY") {
				t.Errorf("Expected ACCOUNTABILITY directive at round 2 defense, got: %s", res)
			}
		} else if round >= 2 {
			if !strings.Contains(res, "STEELMAN") {
				t.Errorf("Expected STEELMAN directive at round %d defense, got: %s", round+1, res)
			}
		}
		if _, err := m.Process(ctx, socratic.Request{Stage: "THESIS_DEFENSE", Lemma: "Defense deepened. New evidence found."}); err != nil {
			t.Fatalf("THESIS_DEFENSE round %d: %v", round+1, err)
		}
	}

	// Round 10: should force convergence → CHAOS regardless of is_satisfied
	res, err := m.Process(ctx, socratic.Request{Stage: "ANTITHESIS_EVALUATE", Lemma: "Still not satisfied. But max reached.", IsSatisfied: false})
	if err != nil {
		t.Fatalf("ANTITHESIS_EVALUATE R10: %v", err)
	}
	if !strings.Contains(res, "Chaos Architect") {
		t.Errorf("Expected forced CHAOS at max dialectic rounds, got: %s", res)
	}
}

func TestSynthesisCritiqueRequired(t *testing.T) {
	m := socratic.NewMachine(nil)
	ctx := context.Background()
	walkToConvergence(t, m)

	// Walk through chaos rebuttal to APORIA
	m.Process(ctx, socratic.Request{Stage: "CHAOS", Lemma: "chaos event. breaks things."})
	m.Process(ctx, socratic.Request{Stage: "THESIS_DEFENSE", Lemma: "defense holds. evidence supports."})
	m.Process(ctx, socratic.Request{Stage: "ANTITHESIS_EVALUATE", Lemma: "adequate. confirmed.", IsSatisfied: true})

	// APORIA without synthesis_critique → rejected
	res, err := m.Process(ctx, socratic.Request{Stage: "APORIA", AporiaSynthesis: "My synthesis."})
	if err != nil {
		t.Fatalf("APORIA (no critique): %v", err)
	}
	if !strings.Contains(res, "synthesis_rejected") {
		t.Errorf("Expected synthesis_rejected, got: %s", res)
	}

	// APORIA with critique → accepted
	res, err = m.Process(ctx, socratic.Request{
		Stage:             "APORIA",
		AporiaSynthesis:   "My complete synthesis.",
		SynthesisCritique: "Handles core well. Misses edge case.",
	})
	if err != nil {
		t.Fatalf("APORIA (with critique): %v", err)
	}
	if !strings.Contains(res, "Socratic pipeline complete") {
		t.Errorf("Expected completion, got: %s", res)
	}
}

func TestLemmaEntryMetadata(t *testing.T) {
	m := socratic.NewMachine(nil)
	ctx := context.Background()

	m.Process(ctx, socratic.Request{Stage: "INITIALIZE", Problem: "metadata test"})
	m.Process(ctx, socratic.Request{Stage: "THESIS", Lemma: "Thesis position. Evidence."})
	m.Process(ctx, socratic.Request{Stage: "ANTITHESIS_INITIAL", Lemma: "Challenge raised. Example."})
	m.Process(ctx, socratic.Request{Stage: "THESIS_DEFENSE", Lemma: "Defense. Proof."})

	// Reach convergence through min rounds (1)
	m.Process(ctx, socratic.Request{Stage: "ANTITHESIS_EVALUATE", Lemma: "Satisfied now. Confirmed.", IsSatisfied: true}) // accepted R1 → CHAOS

	// CHAOS
	m.Process(ctx, socratic.Request{Stage: "CHAOS", Lemma: "Black Swan. Breaks things."})
	m.Process(ctx, socratic.Request{Stage: "THESIS_DEFENSE", Lemma: "Survives. This breaks."})
	m.Process(ctx, socratic.Request{Stage: "ANTITHESIS_EVALUATE", Lemma: "Adequate. Confirmed.", IsSatisfied: true})

	// Final APORIA — check the MuzzleAndSynthesize output for stage tags
	res, err := m.Process(ctx, socratic.Request{
		Stage:             "APORIA",
		AporiaSynthesis:   "Final synthesis.",
		SynthesisCritique: "Good coverage. Minor gaps.",
	})
	if err != nil {
		t.Fatalf("APORIA: %v", err)
	}

	// Verify stage tags in output
	expectedTags := []string{
		"[THESIS·R1]",
		"[ANTITHESIS_INITIAL·R1]",
		"[THESIS_DEFENSE·R1]",
		"[ANTITHESIS_EVALUATE·R1]",
		"[CHAOS·R1]",
	}
	for _, tag := range expectedTags {
		if !strings.Contains(res, tag) {
			t.Errorf("Expected tag %q in output, got: %s", tag, res)
		}
	}
}

func TestMuzzleAndSynthesize_StageLabels(t *testing.T) {
	m := socratic.NewMachine(nil)
	ctx := context.Background()
	walkToConvergence(t, m)

	m.Process(ctx, socratic.Request{Stage: "CHAOS", Lemma: "Chaos event. Mechanism."})
	m.Process(ctx, socratic.Request{Stage: "THESIS_DEFENSE", Lemma: "Defense. Evidence."})
	m.Process(ctx, socratic.Request{Stage: "ANTITHESIS_EVALUATE", Lemma: "Satisfied. Done.", IsSatisfied: true})

	res, err := m.Process(ctx, socratic.Request{
		Stage:             "APORIA",
		AporiaSynthesis:   "Complete synthesis.",
		SynthesisCritique: "Self-evaluation complete.",
	})
	if err != nil {
		t.Fatalf("APORIA: %v", err)
	}

	// Verify the output format
	if !strings.Contains(res, "Dialectic Lemma Trail") {
		t.Error("Missing 'Dialectic Lemma Trail' header")
	}
	if !strings.Contains(res, "Aporia Verdict") {
		t.Error("Missing 'Aporia Verdict' header")
	}
	if !strings.Contains(res, "Complete synthesis.") {
		t.Error("Missing synthesis content in output")
	}

	// Check numbered format with stage labels
	if !strings.Contains(res, "1. [THESIS·R1]") {
		t.Errorf("Expected numbered format with stage label, got: %s", res)
	}
}

func TestEnhancedThesisDirective(t *testing.T) {
	m := socratic.NewMachine(nil)
	ctx := context.Background()

	res, err := m.Process(ctx, socratic.Request{Stage: "INITIALIZE", Problem: "test problem"})
	if err != nil {
		t.Fatalf("INITIALIZE: %v", err)
	}

	// Verify multi-dimensional mandate keywords
	if !strings.Contains(res, "operational") {
		t.Errorf("THESIS directive should require 'operational' dimension, got: %s", res)
	}
	if !strings.Contains(res, "security") {
		t.Errorf("THESIS directive should require 'security' dimension, got: %s", res)
	}
	if !strings.Contains(res, "TWO alternative") {
		t.Errorf("THESIS directive should require TWO alternatives, got: %s", res)
	}
}

func TestEnhancedAntithesisDirective(t *testing.T) {
	m := socratic.NewMachine(nil)
	ctx := context.Background()

	m.Process(ctx, socratic.Request{Stage: "INITIALIZE", Problem: "test problem"})
	res, err := m.Process(ctx, socratic.Request{Stage: "THESIS", Lemma: "Position stated. Evidence provided."})
	if err != nil {
		t.Fatalf("THESIS: %v", err)
	}

	// Verify 6th category
	if !strings.Contains(res, "Dimensional Omissions") {
		t.Errorf("ANTITHESIS directive should contain 'Dimensional Omissions', got: %s", res)
	}
}

func TestRoundEscalationDirectives(t *testing.T) {
	m := socratic.NewMachine(nil)
	ctx := context.Background()
	walkToEvaluate(t, m)

	// Round 1 evaluation: standard
	// (evaluation directive comes from handleThesisDefense which was already called in walkToEvaluate)

	// Round 1: not satisfied → should get Round 2 ACCOUNTABILITY defense directive
	res, err := m.Process(ctx, socratic.Request{Stage: "ANTITHESIS_EVALUATE", Lemma: "Not satisfied. Gaps remain.", IsSatisfied: false})
	if err != nil {
		t.Fatalf("ANTITHESIS_EVALUATE R1: %v", err)
	}
	if !strings.Contains(res, "ACCOUNTABILITY") {
		t.Errorf("Round 2 defense should contain 'ACCOUNTABILITY', got: %s", res)
	}

	// Defend at Round 2
	res, err = m.Process(ctx, socratic.Request{Stage: "THESIS_DEFENSE", Lemma: "Evidence-backed defense. Concrete artifact."})
	if err != nil {
		t.Fatalf("THESIS_DEFENSE R2: %v", err)
	}
	// Round 2 evaluation directive should demand HEIGHTENED SCRUTINY
	if !strings.Contains(res, "HEIGHTENED SCRUTINY") {
		t.Errorf("Round 2 evaluation should contain 'HEIGHTENED SCRUTINY', got: %s", res)
	}

	// Round 2: not satisfied → should get Round 3 STEELMAN defense directive
	res, err = m.Process(ctx, socratic.Request{Stage: "ANTITHESIS_EVALUATE", Lemma: "Insufficient evidence. More needed.", IsSatisfied: false})
	if err != nil {
		t.Fatalf("ANTITHESIS_EVALUATE R2: %v", err)
	}
	if !strings.Contains(res, "STEELMAN") {
		t.Errorf("Round 3 defense should contain 'STEELMAN', got: %s", res)
	}

	// Defend at Round 3
	res, err = m.Process(ctx, socratic.Request{Stage: "THESIS_DEFENSE", Lemma: "Steelman presented. Position survives."})
	if err != nil {
		t.Fatalf("THESIS_DEFENSE R3: %v", err)
	}
	// Round 3 evaluation directive should demand MAXIMUM RIGOR
	if !strings.Contains(res, "MAXIMUM RIGOR") {
		t.Errorf("Round 3+ evaluation should contain 'MAXIMUM RIGOR', got: %s", res)
	}
}

func TestChaosHandoffAnnotation(t *testing.T) {
	m := socratic.NewMachine(nil)
	ctx := context.Background()
	walkToConvergence(t, m)

	// The walkToConvergence result should contain COVERAGE ADVISORY
	// Re-initialize and walk to check the chaos transition output directly
	m2 := socratic.NewMachine(nil)
	walkToConvergence(t, m2)

	// The last output from walkToConvergence was the Chaos transition
	// We need to check it contained the advisory — but walkToConvergence only
	// verifies "Chaos Architect". Let's walk manually to capture the output.
	m3 := socratic.NewMachine(nil)
	walkToEvaluate(t, m3)

	// Round 1: satisfied → CHAOS with coverage advisory
	res, err := m3.Process(ctx, socratic.Request{Stage: "ANTITHESIS_EVALUATE", Lemma: "Satisfied R1. All addressed.", IsSatisfied: true})
	if err != nil {
		t.Fatalf("ANTITHESIS_EVALUATE R3: %v", err)
	}
	if !strings.Contains(res, "COVERAGE ADVISORY") {
		t.Errorf("Chaos handoff should contain 'COVERAGE ADVISORY', got: %s", res)
	}
	if !strings.Contains(res, "Dimensional Omissions") {
		t.Errorf("Chaos handoff should reference 'Dimensional Omissions' category, got: %s", res)
	}
}

// --- Mock Store for archival tests ---

type mockStore struct {
	mu            sync.Mutex
	archiveCalled bool
	lastArchive   socratic.DialecticArchive
	sessionCalled bool
	searchResult  string
}

func (s *mockStore) SaveToRecall(_ context.Context, _, _ string, _ any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessionCalled = true
	return nil
}

func (s *mockStore) SearchDialecticHistory(_ context.Context, _ string, _ int) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.searchResult
}

func (s *mockStore) ArchiveDialecticJourney(_ context.Context, archive socratic.DialecticArchive) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.archiveCalled = true
	s.lastArchive = archive
	return nil
}

func (s *mockStore) getArchiveState() (bool, socratic.DialecticArchive) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.archiveCalled, s.lastArchive
}

// --- Archival Tests ---

func TestArchiveTriggersOnAporia(t *testing.T) {
	store := &mockStore{}
	m := socratic.NewMachine(store)
	ctx := context.Background()

	// Walk full pipeline to APORIA
	walkToConvergence(t, m)
	m.Process(ctx, socratic.Request{Stage: "CHAOS", Lemma: "Black Swan. Mechanism."})
	m.Process(ctx, socratic.Request{Stage: "THESIS_DEFENSE", Lemma: "Defense. Evidence."})
	m.Process(ctx, socratic.Request{Stage: "ANTITHESIS_EVALUATE", Lemma: "Adequate. Confirmed.", IsSatisfied: true})

	// Trigger APORIA → should fire archival
	res, err := m.Process(ctx, socratic.Request{
		Stage:             "APORIA",
		AporiaSynthesis:   "Final synthesis for archival test.",
		SynthesisCritique: "Good coverage. Minor gaps.",
	})
	if err != nil {
		t.Fatalf("APORIA: %v", err)
	}
	if !strings.Contains(res, "Socratic pipeline complete") {
		t.Fatalf("Expected pipeline complete, got: %s", res)
	}

	// Wait for async archival goroutine to complete
	time.Sleep(100 * time.Millisecond)

	called, archive := store.getArchiveState()
	if !called {
		t.Fatal("ArchiveDialecticJourney was not called after APORIA")
	}

	// Verify archive contents
	if archive.AporiaSynthesis != "Final synthesis for archival test." {
		t.Errorf("Expected aporia synthesis in archive, got: %q", archive.AporiaSynthesis)
	}
	if archive.Prompt != "test problem" {
		t.Errorf("Expected original prompt in archive, got: %q", archive.Prompt)
	}
	if len(archive.LemmaTrail) == 0 {
		t.Error("Expected non-empty lemma trail in archive")
	}
	if archive.Timestamp == 0 {
		t.Error("Expected non-zero timestamp in archive")
	}
	if archive.DialecticRounds == 0 {
		t.Error("Expected non-zero dialectic rounds in archive")
	}
	if len(archive.Tags) == 0 {
		t.Error("Expected non-empty tags in archive")
	}
}

func TestNoArchiveOnReset(t *testing.T) {
	store := &mockStore{}
	m := socratic.NewMachine(store)
	ctx := context.Background()

	// Start pipeline then RESET
	m.Process(ctx, socratic.Request{Stage: "INITIALIZE", Problem: "test problem"})
	m.Process(ctx, socratic.Request{Stage: "THESIS", Lemma: "Position. Evidence."})
	m.Process(ctx, socratic.Request{Stage: "RESET"})

	// Wait to ensure no async archival fires
	time.Sleep(100 * time.Millisecond)

	called, _ := store.getArchiveState()
	if called {
		t.Fatal("ArchiveDialecticJourney should NOT be called on RESET")
	}
}

func TestArchiveWithNilStore(t *testing.T) {
	m := socratic.NewMachine(nil)
	ctx := context.Background()

	// Walk full pipeline to APORIA with nil store — should not panic
	walkToConvergence(t, m)
	m.Process(ctx, socratic.Request{Stage: "CHAOS", Lemma: "Black Swan. Mechanism."})
	m.Process(ctx, socratic.Request{Stage: "THESIS_DEFENSE", Lemma: "Defense. Evidence."})
	m.Process(ctx, socratic.Request{Stage: "ANTITHESIS_EVALUATE", Lemma: "Adequate. Confirmed.", IsSatisfied: true})

	res, err := m.Process(ctx, socratic.Request{
		Stage:             "APORIA",
		AporiaSynthesis:   "Synthesis with nil store.",
		SynthesisCritique: "Self-critique complete.",
	})
	if err != nil {
		t.Fatalf("APORIA with nil store: %v", err)
	}
	if !strings.Contains(res, "Socratic pipeline complete") {
		t.Errorf("Expected pipeline complete with nil store, got: %s", res)
	}
}

func TestExtractKeywordTags(t *testing.T) {
	// Use a realistic prompt
	m := socratic.NewMachine(nil)
	ctx := context.Background()

	prompt := "Design and implement a Kubernetes deployment strategy for the Go microservice using BadgerDB as the persistence layer with BadgerDB replication"
	res, err := m.Process(ctx, socratic.Request{Stage: "INITIALIZE", Problem: prompt})
	if err != nil {
		t.Fatalf("INITIALIZE: %v", err)
	}
	_ = res // We can't inspect extractKeywordTags directly from external test,
	// but we can verify archival captures tags correctly via the mock store.

	store := &mockStore{}
	m2 := socratic.NewMachine(store)

	// Walk a full pipeline with the keyword-rich prompt
	m2.Process(ctx, socratic.Request{Stage: "INITIALIZE", Problem: prompt})
	m2.Process(ctx, socratic.Request{Stage: "THESIS", Lemma: "Position. Evidence."})
	m2.Process(ctx, socratic.Request{Stage: "ANTITHESIS_INITIAL", Lemma: "Challenge. Example."})
	m2.Process(ctx, socratic.Request{Stage: "THESIS_DEFENSE", Lemma: "Defense. Proof."})

	// 3 rounds to convergence
	m2.Process(ctx, socratic.Request{Stage: "ANTITHESIS_EVALUATE", Lemma: "R1.", IsSatisfied: true})
	m2.Process(ctx, socratic.Request{Stage: "THESIS_DEFENSE", Lemma: "D2."})
	m2.Process(ctx, socratic.Request{Stage: "ANTITHESIS_EVALUATE", Lemma: "R2.", IsSatisfied: true})
	m2.Process(ctx, socratic.Request{Stage: "THESIS_DEFENSE", Lemma: "D3."})
	m2.Process(ctx, socratic.Request{Stage: "ANTITHESIS_EVALUATE", Lemma: "R3.", IsSatisfied: true})

	// CHAOS
	m2.Process(ctx, socratic.Request{Stage: "CHAOS", Lemma: "Chaos. Break."})
	m2.Process(ctx, socratic.Request{Stage: "THESIS_DEFENSE", Lemma: "Survives. Breaks."})
	m2.Process(ctx, socratic.Request{Stage: "ANTITHESIS_EVALUATE", Lemma: "OK.", IsSatisfied: true})

	// APORIA
	m2.Process(ctx, socratic.Request{
		Stage:             "APORIA",
		AporiaSynthesis:   "Final.",
		SynthesisCritique: "Good.",
	})

	time.Sleep(100 * time.Millisecond)

	called, archive := store.getArchiveState()
	if !called {
		t.Fatal("ArchiveDialecticJourney was not called")
	}

	// Verify tags were extracted
	if len(archive.Tags) == 0 {
		t.Fatal("Expected non-empty tags from keyword-rich prompt")
	}
	if len(archive.Tags) > 5 {
		t.Errorf("Expected max 5 tags, got %d", len(archive.Tags))
	}

	// "badgerdb" should appear in tags (it appears twice in the prompt)
	found := slices.Contains(archive.Tags, "badgerdb")
	if !found {
		t.Errorf("Expected 'badgerdb' in tags (appears twice in prompt), got: %v", archive.Tags)
	}
}

func TestHistoricalContextInjection(t *testing.T) {
	store := &mockStore{
		searchResult: `{"prompt":"previous kubernetes problem","aporia_synthesis":"Use StatefulSets with PVC templates"}`,
	}
	m := socratic.NewMachine(store)
	ctx := context.Background()

	res, err := m.Process(ctx, socratic.Request{Stage: "INITIALIZE", Problem: "kubernetes deployment"})
	if err != nil {
		t.Fatalf("INITIALIZE: %v", err)
	}

	// Should contain structured context extraction
	if !strings.Contains(res, "PRIOR ARCHIVAL CONTEXT") {
		t.Errorf("Expected PRIOR ARCHIVAL CONTEXT in directive, got: %s", res)
	}
	if !strings.Contains(res, "StatefulSets") {
		t.Errorf("Expected extracted aporia_synthesis in context, got: %s", res)
	}
}

func TestHistoricalContextFallback(t *testing.T) {
	store := &mockStore{
		searchResult: `some raw non-json text from recall`,
	}
	m := socratic.NewMachine(store)
	ctx := context.Background()

	res, err := m.Process(ctx, socratic.Request{Stage: "INITIALIZE", Problem: "test"})
	if err != nil {
		t.Fatalf("INITIALIZE: %v", err)
	}

	// Should fallback to raw text injection
	if !strings.Contains(res, "PRIOR ARCHIVAL CONTEXT") {
		t.Errorf("Expected PRIOR ARCHIVAL CONTEXT in directive, got: %s", res)
	}
	if !strings.Contains(res, "some raw non-json text") {
		t.Errorf("Expected raw fallback text in context, got: %s", res)
	}
}
