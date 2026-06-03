package monitor

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/hghukasyan/pgscope/internal/config"
)

func TestSkipOverlappingCycles(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	svc := New(cfg, nil, nil, slog.Default())
	if !svc.running.CompareAndSwap(false, true) {
		t.Fatal("setup")
	}
	svc.cycle(context.Background(), time.Second)
	st := svc.Stats()
	if st.SkippedOverlaps != 1 {
		t.Fatalf("expected skip, got %+v", st)
	}
	svc.running.Store(false)
}

func TestNilCollectorRecordsError(t *testing.T) {
	t.Parallel()
	svc := New(config.Default(), nil, nil, slog.Default())
	svc.cycle(context.Background(), time.Second)
	st := svc.Stats()
	if st.CollectionErrors != 1 || st.LastError == "" {
		t.Fatalf("stats=%+v", st)
	}
}

func TestRunCancels(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.Monitoring.DefaultInterval = time.Hour
	cfg.History.Enabled = false
	svc := New(cfg, nil, nil, slog.Default())
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- svc.Run(ctx) }()
	cancel()
	select {
	case err := <-done:
		if err != nil && err != context.Canceled {
			t.Fatalf("unexpected err: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
}
