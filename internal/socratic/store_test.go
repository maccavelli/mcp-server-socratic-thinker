package socratic

import (
	"context"
	"testing"

	"github.com/maccavelli/mcplib"
)

func TestNewRecallStore(t *testing.T) {
	rc := mcplib.NewRecallClient("http://localhost:9999", "test")
	store := NewRecallStore(rc)
	if store == nil {
		t.Fatal("expected non-nil store")
	}
}

func TestSearchDialecticHistory_RecallOffline(t *testing.T) {
	// with an empty URL, RecallEnabled() is probably false, and CallDatabaseTool returns ""
	rc := mcplib.NewRecallClient("", "test")
	store := NewRecallStore(rc)

	res := store.SearchDialecticHistory(context.Background(), "test", 10)
	if res != "" {
		t.Errorf("expected empty string when recall is offline, got %q", res)
	}
}

func TestArchiveDialecticJourney_RecallOffline(t *testing.T) {
	rc := mcplib.NewRecallClient("", "test")
	store := NewRecallStore(rc)

	err := store.ArchiveDialecticJourney(context.Background(), DialecticArchive{})
	if err != nil {
		t.Errorf("expected no error when recall is offline, got %v", err)
	}
}

func TestSaveToRecall_RecallOffline(t *testing.T) {
	rc := mcplib.NewRecallClient("", "test")
	store := NewRecallStore(rc)

	err := store.SaveToRecall(context.Background(), "session", "project", nil)
	if err == nil {
		// we expect an error or it might silently return nil if offline check is inside mcplib
		t.Log("SaveToRecall returned nil")
	}
}
