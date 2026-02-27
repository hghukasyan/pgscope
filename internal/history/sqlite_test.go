package history

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/hghukasyan/pgscope/pkg/models"
)

func TestSQLiteRoundTripAndRetention(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "history.db")
	store, err := OpenSQLite(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	meta := models.HistorySnapshotMeta{
		ID: "snap1", CollectedAt: now, InstanceKey: "inst", Database: "app", PGVersion: "16.0", DurationMS: 12,
	}
	findings := []models.Finding{{
		RuleID: "CONN.HIGH", Severity: models.SeverityWarning, Title: "high connections", ObservedAt: now,
	}}
	metrics := []models.MetricSample{{
		SnapshotID: "snap1", CollectedAt: now, InstanceKey: "inst", Database: "app",
		Name: "connections.total", Value: 40, Kind: models.MetricGauge, Source: "test", Quality: models.QualityOK,
	}}
	if err := store.SaveSnapshot(ctx, meta, findings, metrics); err != nil {
		t.Fatal(err)
	}

	snaps, err := store.ListSnapshots(ctx, time.Time{}, time.Time{}, 10)
	if err != nil || len(snaps) != 1 {
		t.Fatalf("snaps=%v err=%v", snaps, err)
	}
	ms, err := store.ListMetrics(ctx, "connections.total", time.Time{}, time.Time{}, 10)
	if err != nil || len(ms) != 1 || ms[0].Value != 40 {
		t.Fatalf("metrics=%v err=%v", ms, err)
	}

	reg := models.Regression{
		ID: "reg1", DetectedAt: now, MetricName: "connections.total", InstanceKey: "inst",
		Database: "app", CurrentValue: 80, BaselineValue: 40, RelativeChange: 1, Severity: models.SeverityWarning,
		Reliability: "moderate", Method: "test",
	}
	if err := store.SaveRegressions(ctx, []models.Regression{reg}); err != nil {
		t.Fatal(err)
	}

	inc := models.Incident{
		ID: "PG-2026-AA", Fingerprint: "fp1", InstanceKey: "inst", Database: "app",
		Title: "test", Status: models.IncidentOpen, Severity: models.SeverityWarning,
		StartedAt: now, LastObservedAt: now, RegressionIDs: []string{"reg1"},
	}
	if err := store.SaveIncident(ctx, inc); err != nil {
		t.Fatal(err)
	}
	got, ok, err := store.FindOpenByFingerprint(ctx, "fp1")
	if err != nil || !ok || got.ID != inc.ID {
		t.Fatalf("find open: ok=%v err=%v got=%v", ok, err, got)
	}

	st, err := store.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if st.SnapshotCount != 1 || st.SchemaVersion != 1 {
		t.Fatalf("status=%+v", st)
	}

	// Retention: delete everything older than 1ns from a far-future cutoff by using tiny retention from "now"+1h perspective
	// Insert an old metric and clean.
	old := now.Add(-48 * time.Hour)
	oldMeta := models.HistorySnapshotMeta{ID: "old", CollectedAt: old, InstanceKey: "inst", Database: "app"}
	oldMetric := []models.MetricSample{{
		SnapshotID: "old", CollectedAt: old, InstanceKey: "inst", Name: "connections.total",
		Value: 1, Kind: models.MetricGauge, Quality: models.QualityOK,
	}}
	if err := store.SaveSnapshot(ctx, oldMeta, nil, oldMetric); err != nil {
		t.Fatal(err)
	}
	stats, err := store.Cleanup(ctx, RetentionPolicy{RawMetrics: 24 * time.Hour, Incidents: 365 * 24 * time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	if stats.MetricsDeleted < 1 {
		t.Fatalf("expected metrics deleted, got %+v", stats)
	}
}

func TestConcurrentWrites(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, err := OpenSQLite(ctx, filepath.Join(t.TempDir(), "c.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	errCh := make(chan error, 8)
	for i := 0; i < 8; i++ {
		i := i
		go func() {
			now := time.Now().UTC()
			id := time.Now().Format("150405.000000000") + string(rune('a'+i))
			meta := models.HistorySnapshotMeta{ID: id, CollectedAt: now, InstanceKey: "i", Database: "d"}
			m := []models.MetricSample{{
				SnapshotID: id, CollectedAt: now, Name: "x", Value: float64(i), Kind: models.MetricGauge, Quality: models.QualityOK,
			}}
			errCh <- store.SaveSnapshot(ctx, meta, nil, m)
		}()
	}
	for i := 0; i < 8; i++ {
		if err := <-errCh; err != nil {
			t.Fatal(err)
		}
	}
}
