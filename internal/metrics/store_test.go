package metrics

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStore(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "metrics-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "metrics.db")
	store, err := OpenPath(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	snap := MetricSnapshot{
		NetBytesIn:   100,
		NetBytesOut:  200,
		StagesRun:    1,
		Trifecta:     2,
		ContextBytes: 300,
		TokensEst:    400,
	}

	if err := store.RecordSnapshot(snap); err != nil {
		t.Fatal(err)
	}

	agg, err := store.Query30DayAggregates()
	if err != nil {
		t.Fatal(err)
	}
	if agg.NetBytesIn != 100 || agg.NetBytesOut != 200 || agg.StagesRun != 1 || agg.Trifecta != 2 || agg.ContextBytes != 300 || agg.TokensEst != 400 {
		t.Errorf("Unexpected aggregates: %+v", agg)
	}
}

func TestStore_TickerAndHelpers(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "metrics-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "metrics.db")
	store, err := OpenPath(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	done := make(chan struct{})
	store.StartAggregationTicker(10*time.Millisecond, func() MetricSnapshot {
		return MetricSnapshot{StagesRun: 1}
	}, done)

	time.Sleep(25 * time.Millisecond)
	close(done)

	agg, err := store.Query30DayAggregates()
	if err != nil {
		t.Fatal(err)
	}
	if agg.StagesRun == 0 {
		t.Errorf("Ticker didn't run, got 0 stages")
	}

	if FormatBytes(100) != "100 B" {
		t.Errorf("FormatBytes(100) = %v", FormatBytes(100))
	}
	if FormatBytes(1<<10) != "1.00 KB" {
		t.Errorf("FormatBytes(1024) = %v", FormatBytes(1<<10))
	}
	if FormatBytes(1<<20) != "1.00 MB" {
		t.Errorf("FormatBytes(1048576) = %v", FormatBytes(1<<20))
	}
	if FormatBytes(1<<30) != "1.00 GB" {
		t.Errorf("FormatBytes(1073741824) = %v", FormatBytes(1<<30))
	}
}

func TestDefaultDBPath(t *testing.T) {
	p := defaultDBPath()
	if p == "" {
		t.Errorf("defaultDBPath is empty")
	}
}
