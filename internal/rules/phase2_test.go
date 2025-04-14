package rules

import (
	"context"
	"testing"
	"time"

	"github.com/hghukasyan/pgscope/pkg/models"
)

func TestAllPhase2IDsUnique(t *testing.T) {
	t.Parallel()
	seen := map[string]struct{}{}
	for _, r := range AllPhase3() {
		if _, ok := seen[r.ID()]; ok {
			t.Fatalf("duplicate rule id %q", r.ID())
		}
		seen[r.ID()] = struct{}{}
	}
}

func TestQueryHighTotalTimeRequiresContext(t *testing.T) {
	t.Parallel()
	rule := QueryHighTotalTimeRule{}
	snap := models.Snapshot{
		CollectedAt: time.Now().UTC(),
		Instance:    models.Instance{Database: "app"},
		Queries: models.QueryWorkloadStats{
			Collected: true,
			Statements: []models.StatementStats{
				{QueryID: 1, Calls: 100, TotalExecTimeMS: 900, MeanExecTimeMS: 9},
				{QueryID: 2, Calls: 100, TotalExecTimeMS: 100, MeanExecTimeMS: 1},
			},
		},
	}
	fs, err := rule.Evaluate(context.Background(), snap)
	if err != nil {
		t.Fatal(err)
	}
	if len(fs) != 1 || fs[0].Object != "queryid=1" {
		t.Fatalf("unexpected %+v", fs)
	}
}

func TestQueryHighTotalTimeIgnoresLoneMetric(t *testing.T) {
	t.Parallel()
	rule := QueryHighTotalTimeRule{}
	snap := models.Snapshot{
		Queries: models.QueryWorkloadStats{
			Collected: true,
			Statements: []models.StatementStats{
				{QueryID: 1, Calls: 1, TotalExecTimeMS: 1000, MeanExecTimeMS: 1000},
			},
		},
	}
	fs, err := rule.Evaluate(context.Background(), snap)
	if err != nil {
		t.Fatal(err)
	}
	if len(fs) != 0 {
		t.Fatalf("single-call domination should not flag without call context: %+v", fs)
	}
}

func TestReplicationNULLLagNotZero(t *testing.T) {
	t.Parallel()
	rule := ReplicationLagRule{}
	byteLag := int64(32 * 1024 * 1024)
	snap := models.Snapshot{
		CollectedAt: time.Now().UTC(),
		Replication: models.ReplicationStats{
			Collected: true,
			Role:      "primary",
			Connections: []models.ReplicationConn{
				{PID: 1, ApplicationName: "standby", State: "streaming", ByteLagReplay: &byteLag, ReplayLag: nil},
			},
		},
	}
	fs, err := rule.Evaluate(context.Background(), snap)
	if err != nil {
		t.Fatal(err)
	}
	if len(fs) != 1 {
		t.Fatalf("expected lag finding from byte lag: %+v", fs)
	}
	foundNULLNote := false
	for _, e := range fs[0].Evidence {
		if e.Description == "replay_lag" && e.Value == "NULL (not interpreted as zero)" {
			foundNULLNote = true
		}
	}
	if !foundNULLNote {
		t.Fatalf("expected NULL lag evidence note: %+v", fs[0].Evidence)
	}
}

func TestVacuumDeadTuples(t *testing.T) {
	t.Parallel()
	rule := VacuumDeadTuplesRule{}
	snap := models.Snapshot{
		CollectedAt: time.Now().UTC(),
		Instance:    models.Instance{Database: "app"},
		Vacuum: models.VacuumStats{
			Collected: true,
			Tables: []models.VacuumTableStats{
				{Schema: "public", Name: "events", LiveTuples: 1000, DeadTuples: 500, DeadTuplePct: 33, VacuumThreshold: 250, WouldAutovacuum: true},
			},
		},
	}
	fs, err := rule.Evaluate(context.Background(), snap)
	if err != nil || len(fs) != 1 {
		t.Fatalf("err=%v fs=%+v", err, fs)
	}
}
