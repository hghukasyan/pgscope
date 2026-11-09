package vector

import (
	"testing"

	"github.com/hghukasyan/pgscope/pkg/models"
)

func TestParsePostgresSize(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		want int64
	}{
		{"128MB", 128 * 1024 * 1024},
		{"4GB", 4 * 1024 * 1024 * 1024},
		{"8192", 8192},
		{"1.5GB", int64(1.5 * 1024 * 1024 * 1024)},
		{"512 kB", 512 * 1024},
	}
	for _, tc := range cases {
		got, err := ParsePostgresSize(tc.in)
		if err != nil {
			t.Fatalf("%q: %v", tc.in, err)
		}
		if got != tc.want {
			t.Fatalf("%q: got %d want %d", tc.in, got, tc.want)
		}
	}
	if _, err := ParsePostgresSize("nope"); err == nil {
		t.Fatal("expected error")
	}
}

func TestHNSWMemoryFitFinding(t *testing.T) {
	t.Parallel()
	inv := models.VectorInventory{
		Collected: true,
		Available: true,
		Settings: map[string]string{
			"shared_buffers":      "128MB",
			"effective_cache_size": "1GB",
		},
		Indexes: []models.VectorIndex{
			{
				Schema: "public", Name: "big_hnsw", AccessMethod: "hnsw",
				IsValid: true, SizeBytes: 512 * 1024 * 1024, SizePretty: "512 MB",
			},
			{
				Schema: "public", Name: "small_hnsw", AccessMethod: "hnsw",
				IsValid: true, SizeBytes: 16 * 1024 * 1024, SizePretty: "16 MB",
			},
			{
				Schema: "public", Name: "ivf", AccessMethod: "ivfflat",
				IsValid: true, SizeBytes: 900 * 1024 * 1024, SizePretty: "900 MB",
			},
		},
	}
	findings := AnalyzeInventory(inv, "db")
	var hit *models.Finding
	for i := range findings {
		if findings[i].RuleID == "VEC.HNSW_MEMORY_FIT" {
			f := findings[i]
			hit = &f
			break
		}
	}
	if hit == nil {
		t.Fatal("expected VEC.HNSW_MEMORY_FIT for oversized HNSW")
	}
	if hit.Object != "public.big_hnsw" {
		t.Fatalf("object=%s", hit.Object)
	}
	if hit.Severity != models.SeverityWarning {
		t.Fatalf("severity=%s", hit.Severity)
	}
	for _, f := range findings {
		if f.RuleID == "VEC.HNSW_MEMORY_FIT" && f.Object == "public.small_hnsw" {
			t.Fatal("small HNSW should not emit memory-fit finding")
		}
		if f.RuleID == "VEC.HNSW_MEMORY_FIT" && f.Object == "public.ivf" {
			t.Fatal("IVFFlat should not emit HNSW memory-fit finding")
		}
	}
}

func TestHNSWMemoryFitSkippedWithoutSettings(t *testing.T) {
	t.Parallel()
	inv := models.VectorInventory{
		Collected: true,
		Available: true,
		Indexes: []models.VectorIndex{{
			Schema: "public", Name: "big_hnsw", AccessMethod: "hnsw",
			IsValid: true, SizeBytes: 512 * 1024 * 1024,
		}},
	}
	for _, f := range AnalyzeInventory(inv, "db") {
		if f.RuleID == "VEC.HNSW_MEMORY_FIT" {
			t.Fatal("should skip without memory settings")
		}
	}
}
