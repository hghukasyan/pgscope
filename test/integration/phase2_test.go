package integration_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/hghukasyan/pgscope/internal/collector"
	"github.com/hghukasyan/pgscope/internal/config"
	"github.com/hghukasyan/pgscope/internal/database"
	"github.com/hghukasyan/pgscope/internal/explain"
	"github.com/hghukasyan/pgscope/internal/sqlsafe"
	"github.com/hghukasyan/pgscope/pkg/models"
)

func testDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = os.Getenv("PGSCOPE_TEST_DSN")
	}
	if dsn == "" {
		t.Skip("DATABASE_URL or PGSCOPE_TEST_DSN not set; skipping integration test")
	}
	return dsn
}

func TestPhase2Collectors(t *testing.T) {
	dsn := testDSN(t)
	cfg := config.Default()
	cfg.DSN = dsn
	cfg.SensitiveDataSafe = true
	cfg.CollectQueryText = true

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool, err := database.Connect(ctx, cfg)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer pool.Close()

	col := collector.New(pool, cfg, nil)
	snap, err := col.Collect(ctx)
	if err != nil {
		t.Fatalf("collect: %v", err)
	}

	// Seed some statements so ranking has data when extension exists.
	_, _ = pool.Exec(ctx, `SELECT 1`)
	_, _ = pool.Exec(ctx, `SELECT 1`)
	_, _ = pool.Exec(ctx, `SELECT count(*) FROM pg_class`)

	q, lims := col.CollectQueries(ctx, models.QuerySortTotalTime, 10)
	if !q.Available {
		t.Logf("pg_stat_statements unavailable (ok if not installed): %+v", lims)
	} else if !q.Collected {
		t.Fatalf("available but not collected: %+v", lims)
	} else if cfg.SensitiveDataSafe {
		for _, s := range q.Statements {
			if s.QueryText != "" && !s.QueryTextRedacted {
				t.Fatal("expected query text redaction flag in sensitive mode")
			}
		}
	}

	idx, _ := col.CollectIndexes(ctx, "")
	if !idx.Collected {
		t.Fatal("expected indexes collected")
	}

	vac, _ := col.CollectVacuum(ctx, "")
	if !vac.Collected {
		t.Fatal("expected vacuum collected")
	}

	wal, _ := col.CollectWAL(ctx)
	if !wal.Collected {
		t.Fatal("expected wal collected")
	}

	repl, _ := col.CollectReplication(ctx)
	if !repl.Collected {
		t.Fatal("expected replication collected")
	}
	if repl.Role != "primary" && repl.Role != "standby" {
		t.Fatalf("unexpected role %q", repl.Role)
	}

	_ = snap
}

func TestExplainSafeAndUnsafe(t *testing.T) {
	dsn := testDSN(t)
	cfg := config.Default()
	cfg.DSN = dsn
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	pool, err := database.Connect(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	dir := t.TempDir()
	okFile := filepath.Join(dir, "ok.sql")
	badFile := filepath.Join(dir, "bad.sql")
	if err := os.WriteFile(okFile, []byte("SELECT 1"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(badFile, []byte("DELETE FROM pg_class"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := sqlsafe.ValidateReadOnlySelect("DELETE FROM pg_class"); err == nil {
		t.Fatal("expected unsafe rejection")
	}

	res, err := explain.Run(ctx, pool.Pool, "SELECT 1", explain.RunOptions{
		Timeout: cfg.StatementTimeout, PreviewLen: 80, Database: "pgscope",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Root.NodeType == "" {
		t.Fatal("expected plan root")
	}

	_, err = explain.Run(ctx, pool.Pool, "SELECT 1; SELECT 2", explain.RunOptions{})
	if err == nil {
		t.Fatal("expected multi-statement rejection")
	}
}

func TestExplainCancellation(t *testing.T) {
	dsn := testDSN(t)
	cfg := config.Default()
	cfg.DSN = dsn
	pool, err := database.Connect(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = explain.Run(ctx, pool.Pool, "SELECT 1", explain.RunOptions{Timeout: time.Second})
	if err == nil {
		t.Fatal("expected cancellation error")
	}
}
