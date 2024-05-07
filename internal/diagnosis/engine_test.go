package diagnosis

import (
	"context"
	"testing"
	"time"

	"github.com/hghukasyan/pgscope/pkg/models"
)

func TestAnalyzeHealthy(t *testing.T) {
	t.Parallel()
	eng := New()
	snap := models.Snapshot{
		CollectedAt: time.Now().UTC(),
		Instance: models.Instance{
			Database:     "app",
			MajorVersion: 16,
			Capabilities: models.Capabilities{Supported: true, MinSupportedMajor: 14, MaxSupportedMajor: 18},
		},
		Connections: models.ConnectionStats{
			Collected: true, MaxConnections: 100, Total: 10, UtilizationPct: 10,
		},
		Transactions: models.TransactionStats{Collected: true},
		Locks:        models.LockStats{Collected: true},
		Storage:      models.StorageStats{Collected: true, DatabaseSizePretty: "10 MB"},
		Indexes:      models.IndexStats{Collected: true},
	}
	res, err := eng.Analyze(context.Background(), snap)
	if err != nil {
		t.Fatal(err)
	}
	if res.OverallStatus != "ok" {
		t.Fatalf("status=%s findings=%+v", res.OverallStatus, res.Findings)
	}
}

func TestAnalyzeCritical(t *testing.T) {
	t.Parallel()
	eng := New()
	snap := models.Snapshot{
		CollectedAt: time.Now().UTC(),
		Instance: models.Instance{
			Database:     "app",
			MajorVersion: 16,
			Capabilities: models.Capabilities{Supported: true, MinSupportedMajor: 14, MaxSupportedMajor: 18},
		},
		Connections: models.ConnectionStats{
			Collected: true, MaxConnections: 100, Total: 95, UtilizationPct: 95,
		},
		Transactions: models.TransactionStats{Collected: true},
		Locks: models.LockStats{
			Collected: true, WaitingCount: 10,
			BlockingChains: []models.LockWait{{BlockedPID: 1, BlockingPID: 2}},
		},
		Indexes: models.IndexStats{Collected: true},
	}
	res, err := eng.Analyze(context.Background(), snap)
	if err != nil {
		t.Fatal(err)
	}
	if res.OverallStatus != "critical" {
		t.Fatalf("status=%s", res.OverallStatus)
	}
	if len(res.Findings) == 0 {
		t.Fatal("expected findings")
	}
}
