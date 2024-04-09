// Package models defines the shared diagnostic domain types for PGScope.
package models

import "time"

// Severity classifies the urgency of a diagnostic finding.
type Severity string

const (
	SeverityInfo     Severity = "info"
	SeverityWarning  Severity = "warning"
	SeverityCritical Severity = "critical"
)

// RiskClass describes the operational risk of a recommended remediation.
type RiskClass string

const (
	RiskNone     RiskClass = "none"
	RiskLow      RiskClass = "low"
	RiskMedium   RiskClass = "medium"
	RiskHigh     RiskClass = "high"
	RiskUnknown  RiskClass = "unknown"
)

// EvidenceKind distinguishes measured facts from interpretive notes.
type EvidenceKind string

const (
	EvidenceFact        EvidenceKind = "fact"
	EvidenceObservation EvidenceKind = "observation"
	EvidenceHypothesis  EvidenceKind = "hypothesis"
)

// Evidence is a single supporting data point for a finding or hypothesis.
type Evidence struct {
	Kind        EvidenceKind `json:"kind"`
	Source      string       `json:"source"`
	Description string       `json:"description"`
	Value       string       `json:"value,omitempty"`
}

// Finding is one diagnostic conclusion produced by a rule.
type Finding struct {
	RuleID              string     `json:"rule_id"`
	Severity            Severity   `json:"severity"`
	Database            string     `json:"database,omitempty"`
	Object              string     `json:"object,omitempty"`
	ObservedAt          time.Time  `json:"observed_at"`
	Title               string     `json:"title"`
	Description         string     `json:"description"`
	Evidence            []Evidence `json:"evidence,omitempty"`
	ProbableCause       string     `json:"probable_cause,omitempty"`
	ConfidenceRationale string     `json:"confidence_rationale,omitempty"`
	Remediation         string     `json:"remediation,omitempty"`
	Risk                RiskClass  `json:"risk"`
	Validation          string     `json:"validation,omitempty"`
}

// Recommendation is an actionable suggestion derived from findings.
type Recommendation struct {
	ID          string    `json:"id"`
	FindingRefs []string  `json:"finding_refs,omitempty"`
	Title       string    `json:"title"`
	Summary     string    `json:"summary"`
	SQL         string    `json:"sql,omitempty"`
	Risk        RiskClass `json:"risk"`
	Validation  string    `json:"validation,omitempty"`
}

// Limitation records a collection or analysis gap (permissions, missing extension, etc.).
type Limitation struct {
	Area    string `json:"area"`
	Reason  string `json:"reason"`
	Impact  string `json:"impact"`
	Hint    string `json:"hint,omitempty"`
}

// Instance describes the connected PostgreSQL server and database.
type Instance struct {
	VersionString   string            `json:"version_string"`
	VersionNum      int               `json:"version_num"`
	MajorVersion    int               `json:"major_version"`
	Database        string            `json:"database"`
	User            string            `json:"user"`
	ServerAddr      string            `json:"server_addr,omitempty"`
	IsInRecovery    bool              `json:"is_in_recovery"`
	StartTime       *time.Time        `json:"start_time,omitempty"`
	Settings        map[string]string `json:"settings,omitempty"`
	Extensions      []Extension       `json:"extensions,omitempty"`
	Capabilities    Capabilities      `json:"capabilities"`
	CloudHints      []string          `json:"cloud_hints,omitempty"`
}

// Extension is an installed PostgreSQL extension.
type Extension struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Schema      string `json:"schema,omitempty"`
	Relocatable bool   `json:"relocatable,omitempty"`
}

// Capabilities describes features available for collection on this instance.
type Capabilities struct {
	MinSupportedMajor     int  `json:"min_supported_major"`
	MaxSupportedMajor     int  `json:"max_supported_major"`
	Supported             bool `json:"supported"`
	HasPgStatStatements   bool `json:"has_pg_stat_statements"`
	HasPgStatStatementsView bool `json:"has_pg_stat_statements_view"`
	HasPgVector           bool `json:"has_pgvector"`
	HasHypoPG             bool `json:"has_hypopg"`
	CanReadActivity       bool `json:"can_read_activity"`
	CanReadLocks          bool `json:"can_read_locks"`
	CanReadDatabaseSize   bool `json:"can_read_database_size"`
	CanReadRelationStats  bool `json:"can_read_relation_stats"`
	CanReadSettings       bool `json:"can_read_settings"`
	CanReadWAL            bool `json:"can_read_wal"`
	CanReadReplication    bool `json:"can_read_replication"`
	CanReadVacuumProgress bool `json:"can_read_vacuum_progress"`
}

// ConnectionStats summarizes session state from pg_stat_activity.
type ConnectionStats struct {
	MaxConnections       int     `json:"max_connections"`
	Total                int     `json:"total"`
	Active               int     `json:"active"`
	Idle                 int     `json:"idle"`
	IdleInTransaction    int     `json:"idle_in_transaction"`
	IdleInTransactionAborted int `json:"idle_in_transaction_aborted"`
	Waiting              int     `json:"waiting"`
	Disabled             int     `json:"disabled"`
	UtilizationPct       float64 `json:"utilization_pct"`
	LongRunningSessions  []Session `json:"long_running_sessions,omitempty"`
	Collected            bool    `json:"collected"`
}

// Session is a bounded view of a backend process (no credentials or query params).
type Session struct {
	PID            int32      `json:"pid"`
	Usename        string     `json:"usename,omitempty"`
	ApplicationName string    `json:"application_name,omitempty"`
	ClientAddr     string     `json:"client_addr,omitempty"`
	State          string     `json:"state,omitempty"`
	WaitEventType  string     `json:"wait_event_type,omitempty"`
	WaitEvent      string     `json:"wait_event,omitempty"`
	XactStart      *time.Time `json:"xact_start,omitempty"`
	QueryStart     *time.Time `json:"query_start,omitempty"`
	BackendStart   *time.Time `json:"backend_start,omitempty"`
	Duration       string     `json:"duration,omitempty"`
	QueryPreview   string     `json:"query_preview,omitempty"`
}

// TransactionStats summarizes long-running / idle-in-transaction activity.
type TransactionStats struct {
	LongRunning         []Session `json:"long_running,omitempty"`
	IdleInTransaction   []Session `json:"idle_in_transaction,omitempty"`
	OldestXactAge       string    `json:"oldest_xact_age,omitempty"`
	XIDAge              int64     `json:"xid_age,omitempty"`
	DatfrozenxidAge     int64     `json:"datfrozenxid_age,omitempty"`
	Collected           bool      `json:"collected"`
}

// LockWait describes a blocked → blocking relationship.
type LockWait struct {
	BlockedPID       int32  `json:"blocked_pid"`
	BlockedUser      string `json:"blocked_user,omitempty"`
	BlockedQueryPreview string `json:"blocked_query_preview,omitempty"`
	BlockingPID      int32  `json:"blocking_pid"`
	BlockingUser     string `json:"blocking_user,omitempty"`
	BlockingQueryPreview string `json:"blocking_query_preview,omitempty"`
	LockType         string `json:"lock_type,omitempty"`
	Relation         string `json:"relation,omitempty"`
	Mode             string `json:"mode,omitempty"`
	BlockedDuration  string `json:"blocked_duration,omitempty"`
}

// LockStats captures contention and deadlock counters.
type LockStats struct {
	WaitingCount   int        `json:"waiting_count"`
	BlockingChains []LockWait `json:"blocking_chains,omitempty"`
	Deadlocks      int64      `json:"deadlocks"`
	Collected      bool       `json:"collected"`
}

// StorageStats summarizes database and relation sizes.
type StorageStats struct {
	DatabaseSizeBytes int64          `json:"database_size_bytes"`
	DatabaseSizePretty string        `json:"database_size_pretty,omitempty"`
	TopRelations      []RelationSize `json:"top_relations,omitempty"`
	Collected         bool           `json:"collected"`
}

// RelationSize is size metadata for a table or index.
type RelationSize struct {
	Schema     string `json:"schema"`
	Name       string `json:"name"`
	Kind       string `json:"kind"` // table, index, materialized_view, ...
	TotalBytes int64  `json:"total_bytes"`
	TableBytes int64  `json:"table_bytes,omitempty"`
	IndexBytes int64  `json:"index_bytes,omitempty"`
	ToastBytes int64  `json:"toast_bytes,omitempty"`
	Pretty     string `json:"pretty,omitempty"`
}

// IndexInfo is index metadata and usage counters when available.
type IndexInfo struct {
	Schema        string   `json:"schema"`
	Name          string   `json:"name"`
	TableSchema   string   `json:"table_schema"`
	TableName     string   `json:"table_name"`
	SizeBytes     int64    `json:"size_bytes"`
	SizePretty    string   `json:"size_pretty,omitempty"`
	TableBytes    int64    `json:"table_bytes,omitempty"`
	AccessMethod  string   `json:"access_method,omitempty"`
	IsUnique      bool     `json:"is_unique"`
	IsPrimary     bool     `json:"is_primary"`
	IsExclusion   bool     `json:"is_exclusion,omitempty"`
	IsValid       bool     `json:"is_valid"`
	IsReady       bool     `json:"is_ready"`
	IsLive        bool     `json:"is_live"`
	IsPartial     bool     `json:"is_partial,omitempty"`
	Predicate     string   `json:"predicate,omitempty"`
	IndexColumns  []string `json:"index_columns,omitempty"`
	IncludeColumns []string `json:"include_columns,omitempty"`
	IndexExprs    []string `json:"index_expressions,omitempty"`
	OpClasses     []string `json:"opclasses,omitempty"`
	IndOption     []int16  `json:"indoption,omitempty"`
	Collations    []string `json:"collations,omitempty"`
	ConstraintName string  `json:"constraint_name,omitempty"`
	IndexScans    int64    `json:"index_scans"`
	TuplesRead    int64    `json:"tuples_read"`
	TuplesFetched int64    `json:"tuples_fetched"`
	Definition    string   `json:"definition,omitempty"`
	StructuralKey string   `json:"structural_key,omitempty"`
}

// IndexStats aggregates index inventory for the current database.
type IndexStats struct {
	Indexes      []IndexInfo           `json:"indexes,omitempty"`
	Invalid      []IndexInfo           `json:"invalid,omitempty"`
	Largest      []IndexInfo           `json:"largest,omitempty"`
	NeverUsed    []IndexInfo           `json:"never_used_candidates,omitempty"`
	Duplicates   []DuplicateIndexGroup `json:"duplicates,omitempty"`
	Redundant    []DuplicateIndexGroup `json:"redundant_candidates,omitempty"`
	Candidates   []IndexCandidate      `json:"missing_candidates,omitempty"`
	Collected    bool                  `json:"collected"`
	DeepScan     bool                  `json:"deep_scan,omitempty"`
	StatsReset   *time.Time            `json:"stats_reset,omitempty"`
}

// Metric is a generic named observation (reserved for historical snapshots).
type Metric struct {
	Name      string            `json:"name"`
	Value     float64           `json:"value"`
	Unit      string            `json:"unit,omitempty"`
	Labels    map[string]string `json:"labels,omitempty"`
	Timestamp time.Time         `json:"timestamp"`
}

// Snapshot is a point-in-time collection of PostgreSQL observations.
type Snapshot struct {
	ID           string              `json:"id"`
	CollectedAt  time.Time           `json:"collected_at"`
	DurationMS   int64               `json:"duration_ms"`
	Instance     Instance            `json:"instance"`
	Connections  ConnectionStats     `json:"connections"`
	Transactions TransactionStats    `json:"transactions"`
	Locks        LockStats           `json:"locks"`
	Storage      StorageStats        `json:"storage"`
	Indexes      IndexStats          `json:"indexes"`
	Queries      QueryWorkloadStats  `json:"queries"`
	Vacuum       VacuumStats         `json:"vacuum"`
	WAL          WALStats            `json:"wal"`
	Replication  ReplicationStats    `json:"replication"`
	Vector       VectorInventory     `json:"vector"`
	Limitations  []Limitation        `json:"limitations,omitempty"`
	Metrics      []Metric            `json:"metrics,omitempty"`
	DeepScan     bool                `json:"deep_scan,omitempty"`
}

// ScanResult is the full output of a diagnostic scan.
type ScanResult struct {
	Snapshot        Snapshot         `json:"snapshot"`
	Findings        []Finding        `json:"findings"`
	Recommendations []Recommendation `json:"recommendations,omitempty"`
	OverallStatus   string           `json:"overall_status"`
	Summary         string           `json:"summary"`
}
