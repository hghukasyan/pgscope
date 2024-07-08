package integration_test

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/hghukasyan/pgscope/internal/collector"
	"github.com/hghukasyan/pgscope/internal/config"
	"github.com/hghukasyan/pgscope/internal/database"
	"github.com/hghukasyan/pgscope/internal/diagnosis"
	"github.com/hghukasyan/pgscope/internal/report"
)

func TestScanAgainstLivePostgres(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = os.Getenv("PGSCOPE_TEST_DSN")
	}
	if dsn == "" {
		t.Skip("DATABASE_URL or PGSCOPE_TEST_DSN not set; skipping integration test")
	}

	cfg := config.Default()
	cfg.DSN = dsn
	cfg.Timeout = 20 * time.Second
	cfg.Color = false

	ctx, cancel := context.WithTimeout(context.Background(), cfg.Timeout)
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
	if snap.Instance.Database == "" {
		t.Fatal("expected database name")
	}
	if snap.Instance.MajorVersion == 0 {
		t.Fatal("expected major version")
	}
	if !snap.Connections.Collected {
		t.Fatal("expected connections collected")
	}

	eng := diagnosis.New()
	result, err := eng.Analyze(ctx, snap)
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}

	var buf bytes.Buffer
	if err := report.Write(&buf, result, "text", false); err != nil {
		t.Fatalf("report: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "PGScope Scan") {
		t.Fatalf("unexpected report output: %s", out)
	}
	if strings.Contains(out, "s3cret") {
		t.Fatal("report appears to leak a secret")
	}
}

func TestContextCancellation(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = os.Getenv("PGSCOPE_TEST_DSN")
	}
	if dsn == "" {
		t.Skip("DATABASE_URL or PGSCOPE_TEST_DSN not set; skipping integration test")
	}

	cfg := config.Default()
	cfg.DSN = dsn

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := database.Connect(ctx, cfg)
	if err == nil {
		t.Fatal("expected connect to fail on canceled context")
	}
}
