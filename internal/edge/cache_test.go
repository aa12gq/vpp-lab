package edge

import (
	"context"
	"path/filepath"
	"testing"
	"time"
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
	if stats.OldestPendingAt == nil {
		t.Fatalf("expected oldest pending timestamp")
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
	if stats.OldestPendingAt != nil {
		t.Fatalf("expected no oldest pending timestamp after mark: %+v", stats.OldestPendingAt)
	}
}

func TestCacheDeletesOnlySentMessagesBeforeCutoff(t *testing.T) {
	ctx := context.Background()
	cache, err := OpenCache(ctx, filepath.Join(t.TempDir(), "edge.db"))
	if err != nil {
		t.Fatalf("open cache: %v", err)
	}
	defer cache.Close()

	if err := cache.Put(ctx, "vpp/home-lab/load/load_01/telemetry", []byte(`{"seq":1}`)); err != nil {
		t.Fatalf("put sent old: %v", err)
	}
	if err := cache.Put(ctx, "vpp/home-lab/load/load_02/telemetry", []byte(`{"seq":2}`)); err != nil {
		t.Fatalf("put sent fresh: %v", err)
	}
	if err := cache.Put(ctx, "vpp/home-lab/load/load_03/telemetry", []byte(`{"seq":3}`)); err != nil {
		t.Fatalf("put pending old: %v", err)
	}

	rows, err := cache.Pending(ctx, 10)
	if err != nil {
		t.Fatalf("pending: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("expected 3 pending rows, got %d", len(rows))
	}
	for _, row := range rows[:2] {
		if err := cache.MarkSent(ctx, row.ID); err != nil {
			t.Fatalf("mark sent id=%d: %v", row.ID, err)
		}
	}

	old := time.Now().UTC().Add(-48 * time.Hour)
	if _, err := cache.db.ExecContext(ctx, `
UPDATE mqtt_messages
SET created_at = ?, sent_at = ?
WHERE id = ?`, old, old, rows[0].ID); err != nil {
		t.Fatalf("age old sent row: %v", err)
	}
	if _, err := cache.db.ExecContext(ctx, `
UPDATE mqtt_messages
SET created_at = ?
WHERE id = ?`, old, rows[2].ID); err != nil {
		t.Fatalf("age old pending row: %v", err)
	}

	deleted, err := cache.DeleteSentBefore(ctx, time.Now().UTC().Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("delete sent before: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("expected 1 deleted row, got %d", deleted)
	}

	stats, err := cache.Stats(ctx)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if stats.Pending != 1 || stats.Total != 2 {
		t.Fatalf("unexpected stats after cleanup: %+v", stats)
	}
	if stats.OldestPendingAt == nil || !stats.OldestPendingAt.Before(time.Now().UTC().Add(-24*time.Hour)) {
		t.Fatalf("expected old pending timestamp after cleanup: %+v", stats.OldestPendingAt)
	}
}
