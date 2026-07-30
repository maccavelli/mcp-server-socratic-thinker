package socratic

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/maccavelli/mcplib"
)

// RecallStore acts as a domain adapter over the standard mcplib.RecallClient,
// implementing the socratic.Store interface.
type RecallStore struct {
	rc *mcplib.RecallClient
}

// NewRecallStore initializes a new RecallStore.
func NewRecallStore(rc *mcplib.RecallClient) *RecallStore {
	return &RecallStore{rc: rc}
}

// SearchDialecticHistory queries the recall dialectic_history namespace with multi-dimensional
// BM25/Jaccard search. Returns empty string if recall is unavailable or no matches found.
func (s *RecallStore) SearchDialecticHistory(ctx context.Context, query string, limit int) string {
	args := map[string]any{"query": query}
	if limit > 0 {
		args["limit"] = limit
	}
	args["namespace"] = "dialectic_history"
	return s.rc.CallDatabaseTool(ctx, "search", args)
}

// ArchiveDialecticJourney persists a completed dialectic journey to the recall
// dialectic_history namespace via save_to_recall. Returns nil silently when recall is offline.
func (s *RecallStore) ArchiveDialecticJourney(ctx context.Context, archive DialecticArchive) error {
	if !s.rc.RecallEnabled() {
		return nil // Silent no-op when recall is offline
	}

	payloadBytes, err := json.Marshal(archive)
	if err != nil {
		return fmt.Errorf("marshal archive: %w", err)
	}

	key := fmt.Sprintf("dialectic:%d", archive.Timestamp)
	return s.rc.SaveToRecallWithNamespace(ctx, key, "socratic-thinker", "dialectic_history", map[string]any{
		"key":        key,
		"state_data": string(payloadBytes),
	})
}

// SaveToRecall is a direct pass-through for the Store interface.
func (s *RecallStore) SaveToRecall(ctx context.Context, sessionID, projectID string, payload any) error {
	return s.rc.SaveToRecall(ctx, sessionID, projectID, payload)
}
