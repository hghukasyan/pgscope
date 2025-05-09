package models

import "time"

// VectorCapabilities describes discovered pgvector features.
type VectorCapabilities struct {
	Installed            bool     `json:"installed"`
	ExtensionVersion     string   `json:"extension_version,omitempty"`
	ExtensionSchema      string   `json:"extension_schema,omitempty"`
	Types                []string `json:"types,omitempty"`
	AccessMethods        []string `json:"access_methods,omitempty"`
	HasHNSW              bool     `json:"has_hnsw"`
	HasIVFFlat           bool     `json:"has_ivfflat"`
	OperatorClasses      []string `json:"operator_classes,omitempty"`
	DistanceOperators    []string `json:"distance_operators,omitempty"`
	SettingsAvailable    []string `json:"settings_available,omitempty"`
	HasIterativeScanHNSW bool     `json:"has_iterative_scan_hnsw"`
	HasIterativeScanIVF  bool     `json:"has_iterative_scan_ivfflat"`
}

// VectorColumn describes a column with a pgvector-related type.
type VectorColumn struct {
	Schema         string `json:"schema"`
	Table          string `json:"table"`
	Column         string `json:"column"`
	TypeName       string `json:"type_name"` // vector, halfvec, sparsevec
	TypeSchema     string `json:"type_schema,omitempty"`
	Dimensions     *int   `json:"dimensions,omitempty"` // nil when unconstrained
	Nullable       bool   `json:"nullable"`
	EstimatedRows  int64  `json:"estimated_rows,omitempty"`
	TableBytes     int64  `json:"table_bytes,omitempty"`
	TablePretty    string `json:"table_pretty,omitempty"`
	HasVectorIndex bool   `json:"has_vector_index"`
}

// VectorIndex describes an HNSW or IVFFlat index.
type VectorIndex struct {
	Schema           string            `json:"schema"`
	Name             string            `json:"name"`
	TableSchema      string            `json:"table_schema"`
	TableName        string            `json:"table_name"`
	AccessMethod     string            `json:"access_method"` // hnsw | ivfflat
	OperatorClass    string            `json:"operator_class,omitempty"`
	DistanceMetric   string            `json:"distance_metric,omitempty"` // l2, cosine, ip, l1, hamming, jaccard
	DistanceOperator string            `json:"distance_operator,omitempty"`
	IndexedColumns   []string          `json:"indexed_columns,omitempty"`
	IndexedExpr      string            `json:"indexed_expression,omitempty"`
	Dimensions       *int              `json:"dimensions,omitempty"`
	Options          map[string]string `json:"options,omitempty"` // m, ef_construction, lists, ...
	SizeBytes        int64             `json:"size_bytes"`
	SizePretty       string            `json:"size_pretty,omitempty"`
	TableBytes       int64             `json:"table_bytes,omitempty"`
	IsValid          bool              `json:"is_valid"`
	IsReady          bool              `json:"is_ready"`
	IsUnique         bool              `json:"is_unique"`
	IndexScans       int64             `json:"index_scans"`
	TuplesRead       int64             `json:"tuples_read"`
	TuplesFetched    int64             `json:"tuples_fetched"`
	Definition       string            `json:"definition,omitempty"`
	EstimatedRows    int64             `json:"estimated_rows,omitempty"`
}

// VectorInventory is the discovery snapshot for pgvector.
type VectorInventory struct {
	Collected    bool               `json:"collected"`
	Available    bool               `json:"available"`
	Capabilities VectorCapabilities `json:"capabilities"`
	Columns      []VectorColumn     `json:"columns,omitempty"`
	Indexes      []VectorIndex      `json:"indexes,omitempty"`
	Settings     map[string]string  `json:"settings,omitempty"`
	Limitations  []Limitation       `json:"limitations,omitempty"`
}

// VectorStorageEstimate is a labeled capacity estimate (not measured physical size).
type VectorStorageEstimate struct {
	Schema              string  `json:"schema"`
	Table               string  `json:"table"`
	Column              string  `json:"column"`
	TypeName            string  `json:"type_name"`
	Dimensions          *int    `json:"dimensions,omitempty"`
	EstimatedRows       int64   `json:"estimated_rows"`
	MeasuredTableBytes  int64   `json:"measured_table_bytes"`
	MeasuredIndexBytes  int64   `json:"measured_index_bytes"`
	EstVectorPayloadB   int64   `json:"estimated_vector_payload_bytes"`
	EstHalfvecPayloadB  int64   `json:"estimated_halfvec_payload_bytes,omitempty"`
	EstIndexOverheadB   int64   `json:"estimated_index_overhead_bytes,omitempty"`
	Notes               string  `json:"notes"`
	IsEstimate          bool    `json:"is_estimate"`
}

// VectorStorageReport aggregates storage intelligence.
type VectorStorageReport struct {
	Collected bool                    `json:"collected"`
	Estimates []VectorStorageEstimate `json:"estimates,omitempty"`
	Notes     string                  `json:"notes,omitempty"`
}

// RecallSample is one query's exact vs approximate comparison (IDs only — no embeddings).
type RecallSample struct {
	QueryIndex       int      `json:"query_index"`
	FilterLabel      string   `json:"filter_label,omitempty"`
	K                int      `json:"k"`
	ExactIDs         []string `json:"exact_ids,omitempty"`
	ApproxIDs        []string `json:"approx_ids,omitempty"`
	Intersection     int      `json:"intersection"`
	ExactCount       int      `json:"exact_count"`
	ApproxCount      int      `json:"approx_count"`
	Recall           float64  `json:"recall"`
	ExactLatencyMS   float64  `json:"exact_latency_ms"`
	ApproxLatencyMS  float64  `json:"approx_latency_ms"`
	ExactUsedIndex   bool     `json:"exact_used_index"`
	ApproxUsedIndex  bool     `json:"approx_used_index"`
	ExactPlanNode    string   `json:"exact_plan_node,omitempty"`
	ApproxPlanNode   string   `json:"approx_plan_node,omitempty"`
	Error            string   `json:"error,omitempty"`
	IncompleteResult bool     `json:"incomplete_result,omitempty"`
	EligibleRows     *int64   `json:"eligible_rows,omitempty"`
}

// LatencyStats summarizes observed latencies.
type LatencyStats struct {
	Count      int     `json:"count"`
	MeanMS     float64 `json:"mean_ms"`
	MinMS      float64 `json:"min_ms"`
	MaxMS      float64 `json:"max_ms"`
	P50MS      float64 `json:"p50_ms"`
	P90MS      float64 `json:"p90_ms"`
	P95MS      float64 `json:"p95_ms"`
	P99MS      float64 `json:"p99_ms"`
	P99Warning string  `json:"p99_warning,omitempty"`
}

// RecallSummary aggregates Recall@K and latency.
type RecallSummary struct {
	K                    int          `json:"k"`
	MeanRecall           float64      `json:"mean_recall"`
	MinRecall            float64      `json:"min_recall"`
	Samples              int          `json:"samples"`
	IncompleteResults    int          `json:"incomplete_results"`
	Errors               int          `json:"errors"`
	Timeouts             int          `json:"timeouts"`
	ApproxLatency        LatencyStats `json:"approx_latency"`
	ExactLatency         LatencyStats `json:"exact_latency"`
	DenominatorNote      string       `json:"denominator_note,omitempty"`
}

// RecallReport is the full opt-in benchmark output.
type RecallReport struct {
	CollectedAt          time.Time            `json:"collected_at"`
	Database             string               `json:"database"`
	PGVersion            string               `json:"pg_version,omitempty"`
	PgvectorVersion      string               `json:"pgvector_version,omitempty"`
	Target               string               `json:"target"`
	Metric               string               `json:"metric"`
	IndexName            string               `json:"index_name,omitempty"`
	AccessMethod         string               `json:"access_method,omitempty"`
	WorkloadFingerprint  string               `json:"workload_fingerprint"`
	Environment          string               `json:"environment"`
	ConfigFingerprint    string               `json:"config_fingerprint"`
	SampleSize           int                  `json:"sample_size"`
	SearchSettings       map[string]string    `json:"search_settings,omitempty"`
	Summaries            []RecallSummary      `json:"summaries,omitempty"`
	FilterSummaries      []FilterRecallGroup  `json:"filter_summaries,omitempty"`
	Samples              []RecallSample       `json:"samples,omitempty"` // may be omitted when large
	Findings             []Finding            `json:"findings,omitempty"`
	Limitations          []Limitation         `json:"limitations,omitempty"`
	ExactBaselineVerified bool                `json:"exact_baseline_verified"`
	DurationMS           int64                `json:"duration_ms"`
}

// FilterRecallGroup summarizes recall for one filter cohort.
type FilterRecallGroup struct {
	Label      string  `json:"label"`
	K          int     `json:"k"`
	MeanRecall float64 `json:"mean_recall"`
	MinRecall  float64 `json:"min_recall"`
	Samples    int     `json:"samples"`
	Incomplete int     `json:"incomplete_results"`
}

// TuneCandidate is one measured search-parameter configuration.
type TuneCandidate struct {
	Settings     map[string]string `json:"settings"`
	Summaries    []RecallSummary   `json:"summaries,omitempty"`
	MeetsObjectives bool           `json:"meets_objectives"`
	Notes        string            `json:"notes,omitempty"`
	Error        string            `json:"error,omitempty"`
}

// TuneReport is the output of a bounded tuning experiment.
type TuneReport struct {
	CollectedAt         time.Time       `json:"collected_at"`
	Target              string          `json:"target"`
	AccessMethod        string          `json:"access_method"`
	Objective           string          `json:"objective"`
	WorkloadFingerprint string          `json:"workload_fingerprint"`
	Baseline            *TuneCandidate  `json:"baseline,omitempty"`
	Candidates          []TuneCandidate `json:"candidates,omitempty"`
	Recommendation      string          `json:"recommendation,omitempty"`
	ParetoNotes         string          `json:"pareto_notes,omitempty"`
	Findings            []Finding       `json:"findings,omitempty"`
	Limitations         []Limitation    `json:"limitations,omitempty"`
	DurationMS          int64           `json:"duration_ms"`
}

// VectorRegressionCompare compares two compatible recall reports.
type VectorRegressionCompare struct {
	Compatible bool      `json:"compatible"`
	Reason     string    `json:"reason,omitempty"`
	Baseline   string    `json:"baseline_fingerprint,omitempty"`
	Current    string    `json:"current_fingerprint,omitempty"`
	Findings   []Finding `json:"findings,omitempty"`
}
