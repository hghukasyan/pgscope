// Package history provides persistent local storage for PGScope telemetry,
// regressions, and incidents.
package history

import (
	"context"
	"time"

	"github.com/hghukasyan/pgscope/pkg/models"
)

// Store is the persistence interface for Phase 4 history.
type Store interface {
	Close() error
	Status(ctx context.Context) (models.HistoryStatus, error)

	SaveSnapshot(ctx context.Context, meta models.HistorySnapshotMeta, findings []models.Finding, metrics []models.MetricSample) error
	ListSnapshots(ctx context.Context, since, until time.Time, limit int) ([]models.HistorySnapshotMeta, error)
	ListMetrics(ctx context.Context, name string, since, until time.Time, limit int) ([]models.MetricSample, error)
	ListMetricSeries(ctx context.Context, name string, dims map[string]string, since, until time.Time) ([]models.MetricSample, error)

	SaveRegressions(ctx context.Context, regs []models.Regression) error
	ListRegressions(ctx context.Context, since, until time.Time, limit int) ([]models.Regression, error)
	GetRegression(ctx context.Context, id string) (models.Regression, error)

	SaveIncident(ctx context.Context, inc models.Incident) error
	ListIncidents(ctx context.Context, status models.IncidentStatus, limit int) ([]models.Incident, error)
	GetIncident(ctx context.Context, id string) (models.Incident, error)
	FindOpenByFingerprint(ctx context.Context, fingerprint string) (models.Incident, bool, error)

	SaveHypotheses(ctx context.Context, hyps []models.Hypothesis) error
	ListHypotheses(ctx context.Context, incidentID string) ([]models.Hypothesis, error)

	SaveTimeline(ctx context.Context, incidentID string, events []models.TimelineEvent) error
	ListTimeline(ctx context.Context, incidentID string) ([]models.TimelineEvent, error)

	SaveEdges(ctx context.Context, incidentID string, edges []models.EvidenceEdge) error
	ListEdges(ctx context.Context, incidentID string) ([]models.EvidenceEdge, error)

	SaveVectorBenchmark(ctx context.Context, summary VectorBenchmarkSummary) error
	ListVectorBenchmarks(ctx context.Context, fingerprint string, since, until time.Time, limit int) ([]VectorBenchmarkSummary, error)

	Cleanup(ctx context.Context, retention RetentionPolicy) (CleanupStats, error)
}

// RetentionPolicy mirrors config retention windows.
type RetentionPolicy struct {
	RawMetrics time.Duration
	Incidents  time.Duration
}

// CleanupStats reports rows removed.
type CleanupStats struct {
	SnapshotsDeleted   int64 `json:"snapshots_deleted"`
	MetricsDeleted     int64 `json:"metrics_deleted"`
	RegressionsDeleted int64 `json:"regressions_deleted"`
	IncidentsDeleted   int64 `json:"incidents_deleted"`
}

// VectorBenchmarkSummary is a compact, comparable vector recall result.
type VectorBenchmarkSummary struct {
	ID                  string    `json:"id"`
	CollectedAt         time.Time `json:"collected_at"`
	InstanceKey         string    `json:"instance_key"`
	Database            string    `json:"database"`
	WorkloadFingerprint string    `json:"workload_fingerprint"`
	ConfigFingerprint   string    `json:"config_fingerprint"`
	RecallAtK           float64   `json:"recall_at_k"`
	K                   int       `json:"k"`
	P50LatencyMS        float64   `json:"p50_latency_ms"`
	P95LatencyMS        float64   `json:"p95_latency_ms"`
	SampleCount         int       `json:"sample_count"`
	IndexName           string    `json:"index_name,omitempty"`
	Notes               string    `json:"notes,omitempty"`
}
