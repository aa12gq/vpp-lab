package edge

import (
	"context"
	"path/filepath"
	"testing"
)

func TestCacheStoresPendingAndMarksSent(t *testing.T) {
	ctx := context.Background()
	cache, err := OpenCache(ctx, filepath.Join(t.TempDir(), "edge.db"))
	if err != nil {
		t.Fatalf("open cache: %v", err)
	}
	defer cache.Close()

	if err := cache.Put(ctx, "vpp/home-lab/load/load_01/telemetry", []byte(`{"seq":1}`)); err != nil {
		t.Fatalf("put: %v", err)
	}
	stats, err := cache.Stats(ctx)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if stats.Pending != 1 || stats.Total != 1 {
		t.Fatalf("unexpected stats: %+v", stats)
	}

	rows, err := cache.Pending(ctx, 10)
	if err != nil {
		t.Fatalf("pending: %v", err)
	}
	if len(rows) != 1 || rows[0].Topic != "vpp/home-lab/load/load_01/telemetry" {
		t.Fatalf("unexpected pending rows: %+v", rows)
	}

	if err := cache.MarkSent(ctx, rows[0].ID); err != nil {
		t.Fatalf("mark sent: %v", err)
	}
	stats, err = cache.Stats(ctx)
	if err != nil {
		t.Fatalf("stats after mark: %v", err)
	}
	if stats.Pending != 0 || stats.Total != 1 {
		t.Fatalf("unexpected stats after mark: %+v", stats)
	}
}
