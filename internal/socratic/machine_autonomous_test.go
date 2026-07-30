package socratic

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

// mockAutoLLM is designed to return specific JSON responses for testing RunAutonomous
type mockAutoLLM struct {
	responses []any
	errs      []error
	callCount int
	available bool
}

func (m *mockAutoLLM) Generate(_ context.Context, _ string) (string, error) {
	return "", nil
}

func (m *mockAutoLLM) JSONResponse(_ context.Context, _ string, target any) error {
	if m.callCount < len(m.errs) && m.errs[m.callCount] != nil {
		err := m.errs[m.callCount]
		m.callCount++
		return err
	}
	if m.callCount < len(m.responses) {
		resp := m.responses[m.callCount]
		m.callCount++
		raw, _ := json.Marshal(resp)
		return json.Unmarshal(raw, target)
	}
	return errors.New("no more mock responses")
}

func (m *mockAutoLLM) Available() bool {
	return m.available
}

func TestRunAutonomous_Unreachable(t *testing.T) {
	m := NewMachine(nil, WithLLM(&mockAutoLLM{available: false}))
	_, err := m.RunAutonomous(context.Background(), "problem", 5)
	if err == nil {
		t.Fatal("expected unreachable error")
	}
}

func TestRunAutonomous_NilLLM(t *testing.T) {
	m := NewMachine(nil)
	_, err := m.RunAutonomous(context.Background(), "problem", 5)
	if err == nil {
		t.Fatal("expected nil llm error")
	}
}

func TestRunAutonomous_DeadlockAndMechanicalSynthesis(t *testing.T) {
	llm := &mockAutoLLM{
		available: true,
		responses: []any{
			llmStageResponse{}, // THESIS (fails missing lemma, deadlock=1)
			llmStageResponse{}, // THESIS (fails missing lemma, deadlock=2)
			llmStageResponse{}, // THESIS (fails missing lemma, deadlock=3)
			llmStageResponse{Stage: "APORIA", AporiaSynthesis: "final synthesis"},                      // for forceAporiaTransition
			MachineResponse{Decisions: []MachineDecision{{Topic: "T", Decision: "D", Rationale: "R"}}}, // for buildMachineResponseFromSynthesis
		},
	}
	m := NewMachine(nil, WithLLM(llm))

	_, err := m.RunAutonomous(context.Background(), "solve deadlock", 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestForceAporiaTransition_MechanicalFallback(t *testing.T) {
	m := NewMachine(nil, WithLLM(&mockAutoLLM{available: false}))
	m.getPipeline("test-ctx").LemmaTrail = []LemmaEntry{
		{Stage: "THESIS", Round: 1, Text: "A thesis"},
		{Stage: "ANTITHESIS_INITIAL", Round: 1, Text: string(make([]byte, 250))}, // long lemma
	}

	respStr, err := m.forceAporiaTransition(context.Background(), "fallback problem", m.getPipeline("test-ctx"))
	if err != nil {
		t.Fatalf("mechanical synthesis failed: %v", err)
	}

	var mr MachineResponse
	if err := json.Unmarshal([]byte(respStr), &mr); err != nil {
		t.Fatalf("mechanical synthesis returned invalid json: %v", err)
	}
	if len(mr.Decisions) == 0 {
		t.Errorf("expected decisions in mechanical synthesis")
	}
}

func TestBuildMachineResponse_StructureFailFallback(t *testing.T) {
	m := NewMachine(nil, WithLLM(&mockAutoLLM{
		available: true,
		errs:      []error{errors.New("structure fail")},
	}))

	respStr, err := m.buildMachineResponseFromSynthesis(context.Background(), "raw output")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var mr MachineResponse
	if err := json.Unmarshal([]byte(respStr), &mr); err != nil {
		t.Fatalf("failed to parse fallback response: %v", err)
	}
	if mr.Decisions[0].Rationale != "raw output" {
		t.Errorf("fallback did not include raw output: %q", mr.Decisions[0].Rationale)
	}
}
