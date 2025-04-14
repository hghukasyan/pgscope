package rules

import (
	"context"
	"testing"
	"time"

	"github.com/hghukasyan/pgscope/pkg/models"
)

func TestConnectionSaturationRule(t *testing.T) {
	t.Parallel()
	rule := ConnectionSaturationRule{}
	snap := models.Snapshot{
		CollectedAt: time.Now().UTC(),
		Instance:    models.Instance{Database: "app"},
		Connections: models.ConnectionStats{
			Collected:      true,
			MaxConnections: 100,
			Total:          80,
			UtilizationPct: 80,
		},
	}
	fs, err := rule.Evaluate(context.Background(), snap)
	if err != nil {
		t.Fatal(err)
	}
	if len(fs) != 1 || fs[0].Severity != models.SeverityWarning {
		t.Fatalf("unexpected findings: %+v", fs)
	}

	snap.Connections.UtilizationPct = 50
	snap.Connections.Total = 50
	fs, err = rule.Evaluate(context.Background(), snap)
	if err != nil {
		t.Fatal(err)
	}
	if len(fs) != 0 {
		t.Fatalf("expected no findings, got %+v", fs)
	}
}

func TestLockContentionRule(t *testing.T) {
	t.Parallel()
	rule := LockContentionRule{}
	snap := models.Snapshot{
		CollectedAt: time.Now().UTC(),
		Locks: models.LockStats{
			Collected:    true,
			WaitingCount: 2,
			BlockingChains: []models.LockWait{
				{BlockedPID: 10, BlockingPID: 11, Relation: "public.orders", Mode: "ShareLock"},
			},
		},
	}
	fs, err := rule.Evaluate(context.Background(), snap)
	if err != nil {
		t.Fatal(err)
	}
	if len(fs) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(fs))
	}
	if fs[0].RuleID != "LOCK.CONTENTION" {
		t.Fatalf("rule id %q", fs[0].RuleID)
	}
}

func TestInvalidIndexRule(t *testing.T) {
	t.Parallel()
	rule := InvalidIndexRule{}
	snap := models.Snapshot{
		CollectedAt: time.Now().UTC(),
		Indexes: models.IndexStats{
			Collected: true,
			Invalid: []models.IndexInfo{
				{Schema: "public", Name: "orders_ix", TableSchema: "public", TableName: "orders", IsValid: false},
			},
		},
	}
	fs, err := rule.Evaluate(context.Background(), snap)
	if err != nil {
		t.Fatal(err)
	}
	if len(fs) != 1 || fs[0].Severity != models.SeverityCritical {
		t.Fatalf("unexpected: %+v", fs)
	}
}

func TestAllPhase1IDsUnique(t *testing.T) {
	t.Parallel()
	seen := map[string]struct{}{}
	for _, r := range AllPhase1() {
		if _, ok := seen[r.ID()]; ok {
			t.Fatalf("duplicate rule id %q", r.ID())
		}
		seen[r.ID()] = struct{}{}
	}
}
