// Package metrics provides a lightweight BuntDB-backed local metrics store
// for 30-day rolling operational telemetry aggregation. This is intentionally
// decoupled from the recall server's Badger-backed namespace — these are
// simple operational counters, not complex Socratic session histories.
package metrics

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/tidwall/buntdb"

	mcpbuntdb "github.com/maccavelli/mcp-buntdb"
)

// MetricSnapshot captures a point-in-time session metric observation.
type MetricSnapshot struct {
	Timestamp    int64 `json:"ts"`
	NetBytesIn   int64 `json:"net_in"`
	NetBytesOut  int64 `json:"net_out"`
	StagesRun    int   `json:"stages"`
	Trifecta     int   `json:"trifecta"`
	ContextBytes int   `json:"ctx_bytes"`
	TokensEst    int   `json:"tokens"`
}

// Aggregates holds the summed 30-day rolling totals.
type Aggregates struct {
	NetBytesIn   int64
	NetBytesOut  int64
	StagesRun    int
	Trifecta     int
	ContextBytes int
	TokensEst    int
}

// Store wraps a BuntDB instance for local metric persistence.
type Store struct {
	db *buntdb.DB
}

const (
	keyPrefix = "metric:"
	ttl       = 30 * 24 * time.Hour
)

// defaultDBPath returns ~/.socratic-thinker/metrics.db.
func defaultDBPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "/tmp"
	}
	return filepath.Join(home, ".socratic-thinker", "metrics.db")
}

// Open initializes the BuntDB store at the default path.
// Creates the parent directory if it does not exist.
func Open() (*Store, error) {
	return OpenPath(defaultDBPath())
}

// OpenPath initializes the BuntDB store at the given path.
func OpenPath(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, fmt.Errorf("metrics: create dir: %w", err)
	}

	db, err := mcpbuntdb.OpenBuntDB(path, nil)
	if err != nil {
		return nil, fmt.Errorf("metrics: open db: %w", err)
	}

	return &Store{db: db}, nil
}

// Close shuts down the BuntDB instance.
func (s *Store) Close() error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

// RecordSnapshot writes a timestamped metric snapshot with a 30-day TTL.
func (s *Store) RecordSnapshot(snap MetricSnapshot) error {
	snap.Timestamp = time.Now().Unix()

	data, err := json.Marshal(snap)
	if err != nil {
		return fmt.Errorf("metrics: marshal: %w", err)
	}

	key := keyPrefix + strconv.FormatInt(snap.Timestamp, 10)

	return s.db.Update(func(tx *buntdb.Tx) error {
		_, _, err := tx.Set(key, string(data), &buntdb.SetOptions{
			Expires: true,
			TTL:     ttl,
		})
		return err
	})
}

// Query30DayAggregates iterates all metric:* keys and sums their fields.
// Expired entries are automatically excluded by BuntDB's TTL eviction.
func (s *Store) Query30DayAggregates() (Aggregates, error) {
	var agg Aggregates

	err := s.db.View(func(tx *buntdb.Tx) error {
		return tx.AscendKeys(keyPrefix+"*", func(key, value string) bool {
			var snap MetricSnapshot
			if json.Unmarshal([]byte(value), &snap) == nil {
				agg.NetBytesIn += snap.NetBytesIn
				agg.NetBytesOut += snap.NetBytesOut
				agg.StagesRun += snap.StagesRun
				agg.Trifecta += snap.Trifecta
				agg.ContextBytes += snap.ContextBytes
				agg.TokensEst += snap.TokensEst
			}
			return true // keep scanning keys
		})
	})

	return agg, err
}

// StartAggregationTicker runs a background goroutine that records a snapshot
// every interval and returns a function to query the latest aggregates.
// The ticker stops when the provided done channel is closed.
func (s *Store) StartAggregationTicker(interval time.Duration, snapshotFn func() MetricSnapshot, done <-chan struct{}) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				snap := snapshotFn()
				if err := s.RecordSnapshot(snap); err != nil {
					slog.Warn("metrics: snapshot write failed", "error", err)
				}
			}
		}
	}()
}

// FormatBytes returns a human-readable string for byte counts.
func FormatBytes(b int64) string {
	switch {
	case b >= 1<<30:
		return strconv.FormatFloat(float64(b)/(1<<30), 'f', 2, 64) + " GB"
	case b >= 1<<20:
		return strconv.FormatFloat(float64(b)/(1<<20), 'f', 2, 64) + " MB"
	case b >= 1<<10:
		return strconv.FormatFloat(float64(b)/(1<<10), 'f', 2, 64) + " KB"
	default:
		return strconv.FormatInt(b, 10) + " B"
	}
}
