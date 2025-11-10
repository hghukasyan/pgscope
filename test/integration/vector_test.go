package integration_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/hghukasyan/pgscope/internal/config"
	"github.com/hghukasyan/pgscope/internal/database"
	"github.com/hghukasyan/pgscope/internal/explain"
	"github.com/hghukasyan/pgscope/internal/vector"
)

func vectorDSN(t *testing.T) string {
	t.Helper()
	if v := os.Getenv("PGSCOPE_VECTOR_DSN"); v != "" {
		return v
	}
	if v := os.Getenv("DATABASE_URL"); v != "" {
		// Prefer vector port if user points at primary without vector.
		return v
	}
	t.Skip("PGSCOPE_VECTOR_DSN or DATABASE_URL not set")
	return ""
}

func TestVectorDiscover(t *testing.T) {
	dsn := vectorDSN(t)
	cfg := config.Default()
	cfg.DSN = dsn
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := database.Connect(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	inv, err := vector.Discover(ctx, pool.Pool, vector.DiscoverOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !inv.Available {
		t.Skip("pgvector not installed on this DSN")
	}
	if !inv.Capabilities.HasHNSW && !inv.Capabilities.HasIVFFlat {
		t.Fatal("expected hnsw or ivfflat")
	}
	findings := vector.AnalyzeInventory(inv, "pgscope")
	_ = findings
	st := vector.EstimateStorage(inv)
	if !st.Collected {
		t.Fatal("storage report not collected")
	}
}

func TestVectorRecallAndTune(t *testing.T) {
	dsn := vectorDSN(t)
	cfg := config.Default()
	cfg.DSN = dsn
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	pool, err := database.Connect(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	inv, err := vector.Discover(ctx, pool.Pool, vector.DiscoverOptions{Table: "documents"})
	if err != nil {
		t.Fatal(err)
	}
	if !inv.Available || len(inv.Columns) == 0 {
		t.Skip("documents vector table not present")
	}

	minRec := 0.5
	w := vector.Workload{
		Target: vector.WorkloadTarget{
			Schema: "public", Table: "documents", IDColumn: "id", VectorColumn: "embedding",
		},
		Search: vector.WorkloadSearch{Metric: "cosine", K: []int{5, 10}},
		Benchmark: vector.WorkloadBenchmark{
			Samples: 8, Concurrency: 1, Timeout: 2 * time.Second,
			MaxDuration: 2 * time.Minute, Warmup: 1, Environment: "lab",
		},
		Objectives: vector.WorkloadObjectives{MinRecallAt10: &minRec},
		Tuning:     vector.WorkloadTuning{HNSWEfSearch: []int{20, 40}, MaxCandidates: 2},
		Queries:    vector.WorkloadQueries{SampleFromTable: true},
	}
	if err := w.Validate(); err != nil {
		t.Fatal(err)
	}

	rep, err := vector.RunRecall(ctx, pool.Pool, w, vector.RecallOptions{Authorize: true, OmitSampleDetails: true})
	if err != nil {
		t.Fatal(err)
	}
	if !rep.ExactBaselineVerified {
		t.Fatalf("exact baseline not verified: %+v", rep.Limitations)
	}
	if len(rep.Summaries) == 0 {
		t.Fatal("expected summaries")
	}

	dir := t.TempDir()
	path, err := vector.SaveRecallHistory(dir, rep)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := vector.LoadRecallReport(path)
	if err != nil {
		t.Fatal(err)
	}
	cmp := vector.CompareRecallReports(loaded, rep)
	if !cmp.Compatible {
		t.Fatalf("expected compatible: %s", cmp.Reason)
	}

	tune, err := vector.RunTune(ctx, pool.Pool, w, vector.TuneOptions{Authorize: true, Objective: "balanced"})
	if err != nil {
		t.Fatal(err)
	}
	if tune.Recommendation == "" && len(tune.Candidates) == 0 && len(tune.Limitations) == 0 {
		t.Fatal("expected tune output")
	}
}

func TestVectorExplain(t *testing.T) {
	dsn := vectorDSN(t)
	cfg := config.Default()
	cfg.DSN = dsn
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	pool, err := database.Connect(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	inv, err := vector.Discover(ctx, pool.Pool, vector.DiscoverOptions{})
	if err != nil || !inv.Available {
		t.Skip("pgvector unavailable")
	}
	sql := `SELECT id FROM documents ORDER BY embedding <=> '[0.1,0.2,0.3,0.4,0.5,0.6,0.7,0.8]'::vector LIMIT 5`
	res, err := explain.Run(ctx, pool.Pool, sql, explain.RunOptions{Timeout: 10 * time.Second, Database: "pgscope"})
	if err != nil {
		t.Fatal(err)
	}
	vector.AnalyzeVectorPlan(&res, inv, "pgscope")
	_ = filepath.Base(".")
}
