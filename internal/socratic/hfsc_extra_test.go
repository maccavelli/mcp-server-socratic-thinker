package socratic

import (
	"testing"
	"time"
)

func TestHFSCStoreAndFetch(t *testing.T) {
	key := HFSCStore("test data")
	if key == "" {
		t.Fatal("expected non-empty key")
	}
	val := HFSCFetch(key)
	if val != "test data" {
		t.Errorf("expected 'test data', got '%s'", val)
	}

	// Fetching a non-existent key should return ""
	if HFSCFetch("unknown") != "" {
		t.Error("expected empty string for unknown key")
	}

	// Test cleanup logic minimally
	hfscStore.mu.Lock()
	hfscStore.entries["expired"] = hfscEntry{
		data:      "expired data",
		expiresAt: time.Now().Add(-1 * time.Hour), // Set it in the past
	}
	hfscStore.mu.Unlock()

	// Fetching an expired key should return "" and clean it up
	if HFSCFetch("expired") != "" {
		t.Error("expected empty string for expired key")
	}

	hfscStore.mu.Lock()
	_, exists := hfscStore.entries["expired"]
	hfscStore.mu.Unlock()
	if exists {
		t.Error("expected expired key to be cleaned up")
	}
}
