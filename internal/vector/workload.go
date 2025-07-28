package vector

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Workload is a validated opt-in vector benchmark configuration.
type Workload struct {
	Target    WorkloadTarget    `yaml:"target"`
	Search    WorkloadSearch    `yaml:"search"`
	Benchmark WorkloadBenchmark `yaml:"benchmark"`
	Objectives WorkloadObjectives `yaml:"objectives"`
	Tuning    WorkloadTuning    `yaml:"tuning"`
	Queries   WorkloadQueries   `yaml:"queries"`
}

type WorkloadTarget struct {
	Schema       string `yaml:"schema"`
	Table        string `yaml:"table"`
	IDColumn     string `yaml:"id_column"`
	VectorColumn string `yaml:"vector_column"`
	IndexName    string `yaml:"index_name"` // optional preferred ANN index
}

type WorkloadSearch struct {
	Metric string          `yaml:"metric"` // cosine|l2|ip|l1
	K      []int           `yaml:"k"`
	Filter *WorkloadFilter `yaml:"filter"`
}

type WorkloadFilter struct {
	Column   string   `yaml:"column"`
	Operator string   `yaml:"operator"` // = or IN
	Values   []string `yaml:"values"`
}

type WorkloadBenchmark struct {
	Samples      int           `yaml:"samples"`
	Concurrency  int           `yaml:"concurrency"`
	Timeout      time.Duration `yaml:"timeout"`
	MaxDuration  time.Duration `yaml:"max_duration"`
	Warmup       int           `yaml:"warmup"`
	Environment  string        `yaml:"environment"` // isolated|lab|production-forbidden
	IncludeSamples bool        `yaml:"include_samples"`
}

type WorkloadObjectives struct {
	MinRecallAt10    *float64 `yaml:"min_recall_at_10"`
	MaxP95LatencyMS  *float64 `yaml:"max_p95_latency_ms"`
	MaxErrorRate     *float64 `yaml:"max_error_rate"`
}

type WorkloadTuning struct {
	HNSWEfSearch       []int `yaml:"hnsw_ef_search"`
	IVFFlatProbes      []int `yaml:"ivfflat_probes"`
	MaxCandidates      int   `yaml:"max_candidates"`
	EnableIterativeScan *bool `yaml:"enable_iterative_scan"`
}

type WorkloadQueries struct {
	// FixtureFile points to a YAML/JSON file with query vectors as float arrays.
	FixtureFile string `yaml:"fixture_file"`
	// InlineVectors are optional small fixtures (never written to reports).
	InlineVectors [][]float64 `yaml:"inline_vectors"`
	// SampleFromTable draws random vectors from the target table (IDs only leave the DB).
	SampleFromTable bool `yaml:"sample_from_table"`
}

// LoadWorkload reads and validates a workload YAML file.
func LoadWorkload(path string) (Workload, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Workload{}, err
	}
	var w Workload
	if err := yaml.Unmarshal(data, &w); err != nil {
		return Workload{}, fmt.Errorf("parse workload: %w", err)
	}
	if err := w.Validate(); err != nil {
		return Workload{}, err
	}
	return w, nil
}

// Validate enforces safety bounds and whitelist rules.
func (w *Workload) Validate() error {
	if w.Target.Schema == "" {
		w.Target.Schema = "public"
	}
	for _, id := range []string{w.Target.Schema, w.Target.Table, w.Target.IDColumn, w.Target.VectorColumn} {
		if err := ValidateSimpleIdent(id); err != nil {
			return fmt.Errorf("target: %w", err)
		}
	}
	if w.Target.IndexName != "" {
		if err := ValidateSimpleIdent(w.Target.IndexName); err != nil {
			return fmt.Errorf("index_name: %w", err)
		}
	}
	metric := strings.ToLower(w.Search.Metric)
	switch metric {
	case "cosine", "l2", "ip", "l1":
		w.Search.Metric = metric
	case "":
		w.Search.Metric = "cosine"
	default:
		return fmt.Errorf("unsupported metric %q", w.Search.Metric)
	}
	if len(w.Search.K) == 0 {
		w.Search.K = []int{10}
	}
	for _, k := range w.Search.K {
		if k <= 0 || k > 1000 {
			return fmt.Errorf("k=%d out of allowed range 1..1000", k)
		}
	}
	if w.Search.Filter != nil {
		if err := ValidateSimpleIdent(w.Search.Filter.Column); err != nil {
			return fmt.Errorf("filter.column: %w", err)
		}
		op := strings.ToUpper(strings.TrimSpace(w.Search.Filter.Operator))
		if op == "" {
			op = "="
		}
		if op != "=" && op != "IN" {
			return fmt.Errorf("filter.operator %q not allowed; only = and IN are supported", w.Search.Filter.Operator)
		}
		w.Search.Filter.Operator = op
		if len(w.Search.Filter.Values) == 0 {
			return fmt.Errorf("filter.values required")
		}
		if len(w.Search.Filter.Values) > 64 {
			return fmt.Errorf("filter.values exceeds 64 entries")
		}
	}
	if w.Benchmark.Samples <= 0 {
		w.Benchmark.Samples = 20
	}
	if w.Benchmark.Samples > 5000 {
		return fmt.Errorf("benchmark.samples exceeds hard limit 5000")
	}
	if w.Benchmark.Concurrency <= 0 {
		w.Benchmark.Concurrency = 1
	}
	if w.Benchmark.Concurrency > 8 {
		return fmt.Errorf("benchmark.concurrency exceeds hard limit 8")
	}
	if w.Benchmark.Timeout <= 0 {
		w.Benchmark.Timeout = 2 * time.Second
	}
	if w.Benchmark.MaxDuration <= 0 {
		w.Benchmark.MaxDuration = 5 * time.Minute
	}
	if w.Benchmark.MaxDuration > 30*time.Minute {
		return fmt.Errorf("benchmark.max_duration exceeds hard limit 30m")
	}
	if w.Benchmark.Warmup < 0 || w.Benchmark.Warmup > 100 {
		return fmt.Errorf("benchmark.warmup out of range")
	}
	env := strings.ToLower(w.Benchmark.Environment)
	switch env {
	case "", "isolated", "lab":
		if env == "" {
			w.Benchmark.Environment = "isolated"
		}
	case "production", "production-forbidden":
		return fmt.Errorf("benchmark.environment %q refused: recall/tuning requires isolated/lab", w.Benchmark.Environment)
	default:
		return fmt.Errorf("benchmark.environment %q unsupported (want isolated|lab)", w.Benchmark.Environment)
	}
	if w.Tuning.MaxCandidates <= 0 {
		w.Tuning.MaxCandidates = 8
	}
	if w.Tuning.MaxCandidates > 32 {
		return fmt.Errorf("tuning.max_candidates exceeds 32")
	}
	if len(w.Queries.InlineVectors) == 0 && w.Queries.FixtureFile == "" && !w.Queries.SampleFromTable {
		w.Queries.SampleFromTable = true
	}
	return nil
}

// Fingerprint returns a stable identity for regression compatibility checks (no embeddings).
func (w Workload) Fingerprint() string {
	parts := []string{
		w.Target.Schema, w.Target.Table, w.Target.IDColumn, w.Target.VectorColumn,
		w.Target.IndexName, w.Search.Metric, fmt.Sprint(w.Search.K),
		fmt.Sprintf("samples=%d", w.Benchmark.Samples),
	}
	if w.Search.Filter != nil {
		parts = append(parts, w.Search.Filter.Column, w.Search.Filter.Operator, strings.Join(w.Search.Filter.Values, ","))
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "|")))
	return hex.EncodeToString(sum[:8])
}

func (w Workload) Operator() string {
	return operatorFromMetric(w.Search.Metric)
}
