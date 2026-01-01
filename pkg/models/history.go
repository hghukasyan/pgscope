package models

import "time"

// MetricKind distinguishes how a metric should be interpreted over time.
type MetricKind string

const (
	MetricGauge    MetricKind = "gauge"
	MetricCounter  MetricKind = "counter"
	MetricDerived  MetricKind = "derived"  // rate or ratio already computed
	MetricBenchmark MetricKind = "benchmark"
)

// MetricQuality describes whether a sample/rate is trustworthy.
type MetricQuality string

const (
	QualityOK          MetricQuality = "ok"
	QualityUnavailable MetricQuality = "unavailable"
	QualityReset       MetricQuality = "reset"
	QualityInsufficient MetricQuality = "insufficient_samples"
	QualityAnomalous   MetricQuality = "anomalous"
)

// MetricSample is one persisted observation.
type MetricSample struct {
	SnapshotID   string            `json:"snapshot_id"`
	CollectedAt  time.Time         `json:"collected_at"`
	InstanceKey  string            `json:"instance_key"`
	Database     string            `json:"database,omitempty"`
	Name         string            `json:"name"`
	Value        float64           `json:"value"`
	Kind         MetricKind        `json:"kind"`
	Unit         string            `json:"unit,omitempty"`
	Dimensions   map[string]string `json:"dimensions,omitempty"`
	Source       string            `json:"source"`
	ResetToken   string            `json:"reset_token,omitempty"` // changes on stats reset / restart
	Quality      MetricQuality     `json:"quality"`
	DurationMS   int64             `json:"duration_ms,omitempty"`
}

// HistorySnapshotMeta is lightweight snapshot identity for listing.
type HistorySnapshotMeta struct {
	ID          string    `json:"id"`
	CollectedAt time.Time `json:"collected_at"`
	InstanceKey string    `json:"instance_key"`
	Database    string    `json:"database"`
	PGVersion   string    `json:"pg_version,omitempty"`
	DurationMS  int64     `json:"duration_ms"`
	FindingCount int      `json:"finding_count"`
	MetricCount int       `json:"metric_count"`
	Status      string    `json:"status,omitempty"`
}

// Regression is a detected performance change versus a baseline.
type Regression struct {
	ID              string        `json:"id"`
	DetectedAt      time.Time     `json:"detected_at"`
	MetricName      string        `json:"metric_name"`
	Dimensions      map[string]string `json:"dimensions,omitempty"`
	InstanceKey     string        `json:"instance_key"`
	Database        string        `json:"database,omitempty"`
	CurrentValue    float64       `json:"current_value"`
	BaselineValue   float64       `json:"baseline_value"`
	AbsoluteChange  float64       `json:"absolute_change"`
	RelativeChange  float64       `json:"relative_change"` // fraction, e.g. 0.5 = +50%
	WindowStart     time.Time     `json:"window_start"`
	WindowEnd       time.Time     `json:"window_end"`
	BaselineStart   time.Time     `json:"baseline_start"`
	BaselineEnd     time.Time     `json:"baseline_end"`
	SampleCount     int           `json:"sample_count"`
	BaselineSamples int           `json:"baseline_samples"`
	Method          string        `json:"method"`
	Threshold       string        `json:"threshold"`
	Severity        Severity      `json:"severity"`
	Reliability     string        `json:"reliability"` // strong|moderate|weak|insufficient
	Evidence        []Evidence    `json:"evidence,omitempty"`
	IncidentID      string        `json:"incident_id,omitempty"`
}

// IncidentStatus is the lifecycle state of an incident.
type IncidentStatus string

const (
	IncidentOpen         IncidentStatus = "open"
	IncidentInvestigating IncidentStatus = "investigating"
	IncidentMitigated    IncidentStatus = "mitigated"
	IncidentResolved     IncidentStatus = "resolved"
)

// Incident groups related regressions/findings over time.
type Incident struct {
	ID              string         `json:"id"`
	InstanceKey     string         `json:"instance_key"`
	Database        string         `json:"database,omitempty"`
	Title           string         `json:"title"`
	Status          IncidentStatus `json:"status"`
	Severity        Severity       `json:"severity"`
	StartedAt       time.Time      `json:"started_at"`
	LastObservedAt  time.Time      `json:"last_observed_at"`
	ResolvedAt      *time.Time     `json:"resolved_at,omitempty"`
	Fingerprint     string         `json:"fingerprint"` // dedup key
	TriggerMetric   string         `json:"trigger_metric,omitempty"`
	AffectedQueries []string       `json:"affected_queries,omitempty"`
	AffectedObjects []string       `json:"affected_objects,omitempty"`
	RegressionIDs   []string       `json:"regression_ids,omitempty"`
	FindingRefs     []string       `json:"finding_refs,omitempty"`
	Summary         string         `json:"summary,omitempty"`
}

// EdgeType is a typed relationship in the correlation graph.
type EdgeType string

const (
	EdgeBlocks               EdgeType = "BLOCKS"
	EdgeAffects              EdgeType = "AFFECTS"
	EdgeCorrelatesWith       EdgeType = "CORRELATES_WITH"
	EdgePrecedes             EdgeType = "PRECEDES"
	EdgeCausedByHypothesis   EdgeType = "CAUSED_BY_HYPOTHESIS"
	EdgeContradicts          EdgeType = "CONTRADICTS"
	EdgeSupports             EdgeType = "SUPPORTS"
)

// EvidenceEdge links two evidence nodes.
type EvidenceEdge struct {
	From string   `json:"from"`
	To   string   `json:"to"`
	Type EdgeType `json:"type"`
	Note string   `json:"note,omitempty"`
}

// HypothesisConfidence is an explainable category (not a fake percentage).
type HypothesisConfidence string

const (
	ConfidenceStrong       HypothesisConfidence = "strong_evidence"
	ConfidenceModerate     HypothesisConfidence = "moderate_evidence"
	ConfidenceWeak         HypothesisConfidence = "weak_evidence"
	ConfidenceInsufficient HypothesisConfidence = "insufficient_evidence"
)

// Hypothesis is one competing root-cause explanation.
type Hypothesis struct {
	ID                   string               `json:"id"`
	IncidentID           string               `json:"incident_id"`
	Type                 string               `json:"type"`
	Title                string               `json:"title"`
	Description          string               `json:"description"`
	SupportingEvidence   []Evidence           `json:"supporting_evidence,omitempty"`
	ContradictingEvidence []Evidence          `json:"contradicting_evidence,omitempty"`
	MissingEvidence      []string             `json:"missing_evidence,omitempty"`
	Confidence           HypothesisConfidence `json:"confidence"`
	ConfidenceRationale  string               `json:"confidence_rationale"`
	Rank                 int                  `json:"rank"`
	Validation           string               `json:"validation,omitempty"`
	Remediation          string               `json:"remediation,omitempty"`
}

// TimelineEvent is an observed (not fabricated) incident timeline point.
type TimelineEvent struct {
	At          time.Time `json:"at"`
	Kind        string    `json:"kind"` // snapshot|regression|finding|status|note
	Summary     string    `json:"summary"`
	SourceID    string    `json:"source_id,omitempty"`
	Observed    bool      `json:"observed"` // true = exact sample time; false = window-derived
	Details     string    `json:"details,omitempty"`
}

// InvestigationReport is the full incident investigation output.
type InvestigationReport struct {
	Incident     Incident         `json:"incident"`
	Regressions  []Regression     `json:"regressions,omitempty"`
	Hypotheses   []Hypothesis     `json:"hypotheses,omitempty"`
	Timeline     []TimelineEvent  `json:"timeline,omitempty"`
	Edges        []EvidenceEdge   `json:"edges,omitempty"`
	Findings     []Finding        `json:"findings,omitempty"`
	Limitations  []Limitation     `json:"limitations,omitempty"`
	GeneratedAt  time.Time        `json:"generated_at"`
}

// HistoryStatus summarizes local history backend health.
type HistoryStatus struct {
	Enabled        bool      `json:"enabled"`
	Backend        string    `json:"backend"`
	Path           string    `json:"path,omitempty"`
	SchemaVersion  int       `json:"schema_version"`
	SnapshotCount  int64     `json:"snapshot_count"`
	MetricCount    int64     `json:"metric_count"`
	IncidentCount  int64     `json:"incident_count"`
	RegressionCount int64    `json:"regression_count"`
	DBSizeBytes    int64     `json:"db_size_bytes,omitempty"`
	OldestSnapshot *time.Time `json:"oldest_snapshot,omitempty"`
	NewestSnapshot *time.Time `json:"newest_snapshot,omitempty"`
	LastError      string    `json:"last_error,omitempty"`
}
