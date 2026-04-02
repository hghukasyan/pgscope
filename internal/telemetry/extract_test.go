package telemetry

import (
	"testing"
	"time"

	"github.com/hghukasyan/pgscope/pkg/models"
)

func TestExtractMetricsBounded(t *testing.T) {
	t.Parallel()
	snap := models.Snapshot{
		ID: "s1", CollectedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), DurationMS: 5,
		Instance: models.Instance{Database: "app", VersionString: "16.2", ServerAddr: "127.0.0.1"},
		Connections: models.ConnectionStats{Collected: true, Total: 10, Active: 2, MaxConnections: 100, UtilizationPct: 10},
		Locks: models.LockStats{Collected: true, WaitingCount: 3, Deadlocks: 1},
	}
	ms := ExtractMetrics(snap)
	if len(ms) < 5 {
		t.Fatalf("expected metrics, got %d", len(ms))
	}
	for _, m := range ms {
		if m.InstanceKey == "" || m.Quality == "" {
			t.Fatalf("incomplete sample: %+v", m)
		}
		if m.ResetToken == "" {
			t.Fatal("reset token required")
		}
	}
}

func TestDimKeyStable(t *testing.T) {
	t.Parallel()
	a := DimKey(map[string]string{"b": "2", "a": "1"})
	b := DimKey(map[string]string{"a": "1", "b": "2"})
	if a != b {
		t.Fatalf("%q vs %q", a, b)
	}
}
