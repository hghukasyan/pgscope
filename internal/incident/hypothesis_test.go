package incident

import (
	"testing"
	"time"

	"github.com/hghukasyan/pgscope/pkg/models"
)

func TestLockContentionHypothesis(t *testing.T) {
	t.Parallel()
	inc := models.Incident{ID: "PG-1", Title: "locks"}
	regs := []models.Regression{
		{ID: "r1", MetricName: "locks.waiting_count", RelativeChange: 2, BaselineValue: 1, CurrentValue: 3},
		{ID: "r2", MetricName: "query.mean_exec_time_ms", RelativeChange: 3, BaselineValue: 10, CurrentValue: 40,
			Dimensions: map[string]string{"query_id": "99"}},
	}
	findings := []models.Finding{{RuleID: "LOCK.BLOCK", Title: "Blocking sessions detected"}}
	hyps := EvaluateHypotheses(inc, regs, findings, 10)
	if len(hyps) == 0 {
		t.Fatal("expected hypotheses")
	}
	if hyps[0].Type != "lock_contention" {
		t.Fatalf("primary=%s", hyps[0].Type)
	}
	if hyps[0].Confidence == models.ConfidenceInsufficient {
		t.Fatalf("expected evidence-backed confidence, got %s (%s)", hyps[0].Confidence, hyps[0].ConfidenceRationale)
	}
}

func TestUnknownWhenNoEvidence(t *testing.T) {
	t.Parallel()
	hyps := EvaluateHypotheses(models.Incident{ID: "x"}, nil, nil, 5)
	if len(hyps) != 1 || hyps[0].Type != "unknown" {
		t.Fatalf("%+v", hyps)
	}
}

func TestCorrelateDoesNotClaimCause(t *testing.T) {
	t.Parallel()
	inc := models.Incident{ID: "i1"}
	regs := []models.Regression{
		{ID: "a", MetricName: "locks.waiting_count"},
		{ID: "b", MetricName: "query.mean_exec_time_ms"},
	}
	edges := Correlate(inc, regs, nil, 100)
	for _, e := range edges {
		if string(e.Type) == "CAUSES" {
			t.Fatal("must not assert CAUSES")
		}
	}
	if len(edges) == 0 {
		t.Fatal("expected edges")
	}
}

func TestIncidentFingerprintDedup(t *testing.T) {
	t.Parallel()
	r1 := models.Regression{InstanceKey: "i", Database: "d", MetricName: "locks.waiting_count"}
	r2 := models.Regression{InstanceKey: "i", Database: "d", MetricName: "locks.blocking_chains"}
	r3 := models.Regression{InstanceKey: "i", Database: "d", MetricName: "wal.bytes"}
	if fingerprint(r1) != fingerprint(r2) {
		t.Fatal("same family should share fingerprint")
	}
	if fingerprint(r1) == fingerprint(r3) {
		t.Fatal("unrelated families must not share fingerprint")
	}
}

func TestTimelineObservedFlags(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	inc := models.Incident{ID: "i", StartedAt: now, Title: "t"}
	regs := []models.Regression{{
		ID: "r", MetricName: "locks.waiting_count", DetectedAt: now.Add(time.Minute),
		BaselineStart: now.Add(-time.Hour), BaselineEnd: now, BaselineValue: 1, CurrentValue: 5, SampleCount: 5,
	}}
	tl := BuildTimeline(inc, regs, nil, nil)
	var sawWindow bool
	for _, e := range tl {
		if !e.Observed {
			sawWindow = true
		}
	}
	if !sawWindow {
		t.Fatal("expected at least one window-derived (non-observed) event")
	}
}
