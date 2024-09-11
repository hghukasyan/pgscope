package models

import "time"

// QuerySortKey selects the ranking metric for top-query collection.
type QuerySortKey string

const (
	QuerySortTotalTime   QuerySortKey = "total-time"
	QuerySortMeanTime    QuerySortKey = "mean-time"
	QuerySortCalls       QuerySortKey = "calls"
	QuerySortSharedReads QuerySortKey = "shared-reads"
	QuerySortTempIO      QuerySortKey = "temp-io"
	QuerySortWAL         QuerySortKey = "wal"
)

// StatementStats is one normalized statement from pg_stat_statements.
// Optional fields use pointers so missing columns across versions are explicit.
type StatementStats struct {
	QueryID              int64    `json:"query_id"`
	QueryText            string   `json:"query_text,omitempty"`
	QueryTextRedacted    bool     `json:"query_text_redacted,omitempty"`
	DatabaseOID          uint32   `json:"dbid,omitempty"`
	UserOID              uint32   `json:"userid,omitempty"`
	Calls                int64    `json:"calls"`
	TotalExecTimeMS      float64  `json:"total_exec_time_ms"`
	MeanExecTimeMS       float64  `json:"mean_exec_time_ms"`
	MinExecTimeMS        *float64 `json:"min_exec_time_ms,omitempty"`
	MaxExecTimeMS        *float64 `json:"max_exec_time_ms,omitempty"`
	StddevExecTimeMS     *float64 `json:"stddev_exec_time_ms,omitempty"`
	Rows                 int64    `json:"rows"`
	SharedBlksHit        int64    `json:"shared_blks_hit"`
	SharedBlksRead       int64    `json:"shared_blks_read"`
	SharedBlksDirtied    *int64   `json:"shared_blks_dirtied,omitempty"`
	SharedBlksWritten    *int64   `json:"shared_blks_written,omitempty"`
	TempBlksRead         int64    `json:"temp_blks_read"`
	TempBlksWritten      int64    `json:"temp_blks_written"`
	WALRecords           *int64   `json:"wal_records,omitempty"`
	WALBytes             *int64   `json:"wal_bytes,omitempty"`
	TotalPlanTimeMS      *float64 `json:"total_plan_time_ms,omitempty"`
	MeanPlanTimeMS       *float64 `json:"mean_plan_time_ms,omitempty"`
	LocalBlksHit         *int64   `json:"local_blks_hit,omitempty"`
	LocalBlksRead        *int64   `json:"local_blks_read,omitempty"`
	BlkReadTimeMS        *float64 `json:"blk_read_time_ms,omitempty"`
	BlkWriteTimeMS       *float64 `json:"blk_write_time_ms,omitempty"`
}

// QueryWorkloadStats aggregates pg_stat_statements workload observations.
type QueryWorkloadStats struct {
	Collected      bool             `json:"collected"`
	Available      bool             `json:"available"`
	SortBy         QuerySortKey     `json:"sort_by,omitempty"`
	Limit          int              `json:"limit,omitempty"`
	Statements     []StatementStats `json:"statements,omitempty"`
	StatsReset     *time.Time       `json:"stats_reset,omitempty"`
	ColumnsPresent []string         `json:"columns_present,omitempty"`
	TrackSetting   string           `json:"track_setting,omitempty"`
}

// ExplainOptions controls EXPLAIN execution.
type ExplainOptions struct {
	Analyze          bool `json:"analyze"`
	Buffers          bool `json:"buffers"`
	Settings         bool `json:"settings"`
	Verbose          bool `json:"verbose"`
	Wal              bool `json:"wal"`
	TrustedIsolated  bool `json:"trusted_isolated,omitempty"`
	AllowSideEffects bool `json:"allow_side_effects,omitempty"`
}

// PlanNode is one node in a PostgreSQL JSON execution plan tree.
type PlanNode struct {
	NodeType           string             `json:"node_type"`
	ParentRelationship string             `json:"parent_relationship,omitempty"`
	RelationName       string             `json:"relation_name,omitempty"`
	Schema             string             `json:"schema,omitempty"`
	Alias              string             `json:"alias,omitempty"`
	IndexName          string             `json:"index_name,omitempty"`
	IndexCond          string             `json:"index_cond,omitempty"`
	Filter             string             `json:"filter,omitempty"`
	JoinFilter         string             `json:"join_filter,omitempty"`
	HashCond           string             `json:"hash_cond,omitempty"`
	MergeCond          string             `json:"merge_cond,omitempty"`
	RecheckCond        string             `json:"recheck_cond,omitempty"`
	StartupCost        float64            `json:"startup_cost"`
	TotalCost          float64            `json:"total_cost"`
	PlanRows           float64            `json:"plan_rows"`
	PlanWidth          int                `json:"plan_width,omitempty"`
	ActualStartupTime  *float64           `json:"actual_startup_time,omitempty"`
	ActualTotalTime    *float64           `json:"actual_total_time,omitempty"`
	ActualRows         *float64           `json:"actual_rows,omitempty"`
	ActualLoops        *float64           `json:"actual_loops,omitempty"`
	SharedHitBlocks    *int64             `json:"shared_hit_blocks,omitempty"`
	SharedReadBlocks   *int64             `json:"shared_read_blocks,omitempty"`
	SharedDirtiedBlocks *int64            `json:"shared_dirtied_blocks,omitempty"`
	SharedWrittenBlocks *int64            `json:"shared_written_blocks,omitempty"`
	TempReadBlocks     *int64             `json:"temp_read_blocks,omitempty"`
	TempWrittenBlocks  *int64             `json:"temp_written_blocks,omitempty"`
	SortMethod         string             `json:"sort_method,omitempty"`
	SortSpaceType      string             `json:"sort_space_type,omitempty"`
	SortSpaceUsedKB    *float64           `json:"sort_space_used_kb,omitempty"`
	HashBatches        *int64             `json:"hash_batches,omitempty"`
	HashBuckets        *int64             `json:"hash_buckets,omitempty"`
	PeakMemoryKB       *float64           `json:"peak_memory_usage_kb,omitempty"`
	WorkersPlanned     *int               `json:"workers_planned,omitempty"`
	WorkersLaunched    *int               `json:"workers_launched,omitempty"`
	ParallelAware      bool               `json:"parallel_aware,omitempty"`
	ParallelSafe       bool               `json:"parallel_safe,omitempty"`
	Output             []string           `json:"output,omitempty"`
	Extra              map[string]any     `json:"extra,omitempty"`
	Plans              []PlanNode         `json:"plans,omitempty"`
	Path               string             `json:"path,omitempty"`
}

// ExplainResult is the analyzed outcome of an EXPLAIN request.
type ExplainResult struct {
	SQLPreview     string     `json:"sql_preview"`
	Analyze        bool       `json:"analyze"`
	PlanningTimeMS *float64   `json:"planning_time_ms,omitempty"`
	ExecutionTimeMS *float64  `json:"execution_time_ms,omitempty"`
	Root           PlanNode   `json:"root"`
	Summary        PlanSummary `json:"summary"`
	Findings       []Finding  `json:"findings,omitempty"`
	Limitations    []Limitation `json:"limitations,omitempty"`
}

// PlanSummary aggregates notable plan characteristics.
type PlanSummary struct {
	NodeCount          int      `json:"node_count"`
	SeqScans           int      `json:"seq_scans"`
	IndexScans         int      `json:"index_scans"`
	IndexOnlyScans     int      `json:"index_only_scans"`
	BitmapScans        int      `json:"bitmap_scans"`
	NestedLoops        int      `json:"nested_loops"`
	HashJoins          int      `json:"hash_joins"`
	MergeJoins         int      `json:"merge_joins"`
	Sorts              int      `json:"sorts"`
	Aggregates         int      `json:"aggregates"`
	Materialize        int      `json:"materialize"`
	ParallelNodes      int      `json:"parallel_nodes"`
	MaxEstimatedRows   float64  `json:"max_estimated_rows"`
	TotalEstimatedCost float64  `json:"total_estimated_cost"`
	HasDiskSort        bool     `json:"has_disk_sort"`
	HasHashSpill       bool     `json:"has_hash_spill"`
	Relations          []string `json:"relations,omitempty"`
}

// IndexCandidate is a missing-index suggestion with confidence rationale.
type IndexCandidate struct {
	Schema           string    `json:"schema"`
	Table            string    `json:"table"`
	Columns          []string  `json:"columns,omitempty"`
	IncludeColumns   []string  `json:"include_columns,omitempty"`
	Predicate        string    `json:"predicate,omitempty"`
	CreateSQL        string    `json:"create_sql"`
	Reason           string    `json:"reason"`
	AffectedQueries  []string  `json:"affected_queries,omitempty"`
	WriteOverhead    string    `json:"write_overhead"`
	StorageNotes     string    `json:"storage_notes"`
	Confidence       string    `json:"confidence"`
	Validation       string    `json:"validation"`
	HypoCostBefore   *float64  `json:"hypo_cost_before,omitempty"`
	HypoCostAfter    *float64  `json:"hypo_cost_after,omitempty"`
	HypoNote         string    `json:"hypo_note,omitempty"`
}

// DuplicateIndexGroup describes structurally similar indexes.
type DuplicateIndexGroup struct {
	TableSchema string      `json:"table_schema"`
	TableName   string      `json:"table_name"`
	Indexes     []IndexInfo `json:"indexes"`
	Reason      string      `json:"reason"`
}

// VacuumTableStats holds per-table maintenance observations.
type VacuumTableStats struct {
	Schema              string     `json:"schema"`
	Name                string     `json:"name"`
	LiveTuples          int64      `json:"n_live_tup"`
	DeadTuples          int64      `json:"n_dead_tup"`
	ModSinceAnalyze     int64      `json:"n_mod_since_analyze"`
	Inserts             int64      `json:"n_tup_ins"`
	Updates             int64      `json:"n_tup_upd"`
	Deletes             int64      `json:"n_tup_del"`
	HotUpdates          int64      `json:"n_tup_hot_upd"`
	LastVacuum          *time.Time `json:"last_vacuum,omitempty"`
	LastAutovacuum      *time.Time `json:"last_autovacuum,omitempty"`
	LastAnalyze         *time.Time `json:"last_analyze,omitempty"`
	LastAutoanalyze     *time.Time `json:"last_autoanalyze,omitempty"`
	VacuumCount         int64      `json:"vacuum_count"`
	AutovacuumCount     int64      `json:"autovacuum_count"`
	AnalyzeCount        int64      `json:"analyze_count"`
	AutoanalyzeCount    int64      `json:"autoanalyze_count"`
	TableBytes          int64      `json:"table_bytes"`
	TablePretty         string     `json:"table_pretty,omitempty"`
	RelFrozenXIDAge     int64      `json:"relfrozenxid_age,omitempty"`
	DeadTuplePct        float64    `json:"dead_tuple_pct,omitempty"`
	VacuumThreshold     int64      `json:"vacuum_threshold,omitempty"`
	AnalyzeThreshold    int64      `json:"analyze_threshold,omitempty"`
	WouldAutovacuum     bool       `json:"would_autovacuum,omitempty"`
	WouldAutoanalyze    bool       `json:"would_autoanalyze,omitempty"`
	ScaleFactor         float64    `json:"autovacuum_vacuum_scale_factor,omitempty"`
	VacuumBaseThreshold int64      `json:"autovacuum_vacuum_threshold,omitempty"`
	MaxThreshold        *int64     `json:"autovacuum_vacuum_max_threshold,omitempty"`
}

// VacuumProgress describes an in-progress vacuum when available.
type VacuumProgress struct {
	PID            int32   `json:"pid"`
	Database       string  `json:"datname,omitempty"`
	Schema         string  `json:"schema,omitempty"`
	Table          string  `json:"relname,omitempty"`
	Phase          string  `json:"phase,omitempty"`
	HeapBlksTotal  int64   `json:"heap_blks_total,omitempty"`
	HeapBlksScanned int64  `json:"heap_blks_scanned,omitempty"`
	HeapBlksVacuumed int64 `json:"heap_blks_vacuumed,omitempty"`
	IndexVacuumCount int64 `json:"index_vacuum_count,omitempty"`
	MaxDeadTuples  int64   `json:"max_dead_tuples,omitempty"`
	NumDeadTuples  int64   `json:"num_dead_tuples,omitempty"`
}

// VacuumStats aggregates maintenance diagnostics.
type VacuumStats struct {
	Collected           bool                `json:"collected"`
	Tables              []VacuumTableStats  `json:"tables,omitempty"`
	Risks               []VacuumTableStats  `json:"risks,omitempty"`
	Progress            []VacuumProgress    `json:"progress,omitempty"`
	Settings            map[string]string   `json:"settings,omitempty"`
	LongXactBlockingAge string              `json:"long_xact_blocking_age,omitempty"`
}

// WALStats captures WAL and checkpoint observations.
type WALStats struct {
	Collected            bool              `json:"collected"`
	Settings             map[string]string `json:"settings,omitempty"`
	WALBytes             *int64            `json:"wal_bytes,omitempty"`
	WALRecords           *int64            `json:"wal_records,omitempty"`
	WALFPI               *int64            `json:"wal_fpi,omitempty"`
	WALBuffersFull       *int64            `json:"wal_buffers_full,omitempty"`
	WALWriteTimeMS       *float64          `json:"wal_write_time_ms,omitempty"`
	WALSyncTimeMS        *float64          `json:"wal_sync_time_ms,omitempty"`
	CheckpointsTimed     *int64            `json:"checkpoints_timed,omitempty"`
	CheckpointsReq       *int64            `json:"checkpoints_req,omitempty"`
	CheckpointWriteTimeMS *float64         `json:"checkpoint_write_time_ms,omitempty"`
	CheckpointSyncTimeMS *float64          `json:"checkpoint_sync_time_ms,omitempty"`
	BuffersCheckpoint    *int64            `json:"buffers_checkpoint,omitempty"`
	BuffersClean         *int64            `json:"buffers_clean,omitempty"`
	BuffersBackend       *int64            `json:"buffers_backend,omitempty"`
	StatsReset           *time.Time        `json:"stats_reset,omitempty"`
	CurrentLSN          string            `json:"current_lsn,omitempty"`
	// Rate fields populated when a prior sample is provided.
	IntervalSeconds      *float64          `json:"interval_seconds,omitempty"`
	WALBytesPerSec       *float64          `json:"wal_bytes_per_sec,omitempty"`
	CheckpointsReqRatio  *float64          `json:"checkpoints_req_ratio,omitempty"`
	RateValid            bool              `json:"rate_valid,omitempty"`
	RateInvalidReason    string            `json:"rate_invalid_reason,omitempty"`
}

// ReplicationSlot describes a physical or logical replication slot.
type ReplicationSlot struct {
	SlotName          string `json:"slot_name"`
	Plugin            string `json:"plugin,omitempty"`
	SlotType          string `json:"slot_type"`
	Database          string `json:"database,omitempty"`
	Active            bool   `json:"active"`
	RestartLSN        string `json:"restart_lsn,omitempty"`
	ConfirmedFlushLSN string `json:"confirmed_flush_lsn,omitempty"`
	WalStatus         string `json:"wal_status,omitempty"`
	SafeWALSize       *int64 `json:"safe_wal_size,omitempty"`
	RetainedBytes     *int64 `json:"retained_bytes,omitempty"`
	TwoPhase          bool   `json:"two_phase,omitempty"`
}

// ReplicationConn is one entry from pg_stat_replication.
type ReplicationConn struct {
	PID             int32    `json:"pid"`
	Usename         string   `json:"usename,omitempty"`
	ApplicationName string   `json:"application_name,omitempty"`
	ClientAddr      string   `json:"client_addr,omitempty"`
	State           string   `json:"state,omitempty"`
	SyncState       string   `json:"sync_state,omitempty"`
	SentLSN         string   `json:"sent_lsn,omitempty"`
	WriteLSN        string   `json:"write_lsn,omitempty"`
	FlushLSN        string   `json:"flush_lsn,omitempty"`
	ReplayLSN       string   `json:"replay_lsn,omitempty"`
	WriteLag        *string  `json:"write_lag,omitempty"`
	FlushLag        *string  `json:"flush_lag,omitempty"`
	ReplayLag       *string  `json:"replay_lag,omitempty"`
	ByteLagSent     *int64   `json:"byte_lag_sent,omitempty"`
	ByteLagReplay   *int64   `json:"byte_lag_replay,omitempty"`
	BackendXmin     string   `json:"backend_xmin,omitempty"`
}

// ReplicaConflict summarizes conflict counters on a standby.
type ReplicaConflict struct {
	Database   string `json:"database"`
	ConflTablespace int64 `json:"confl_tablespace"`
	ConflLock  int64  `json:"confl_lock"`
	ConflSnapshot int64 `json:"confl_snapshot"`
	ConflBufferpin int64 `json:"confl_bufferpin"`
	ConflDeadlock int64 `json:"confl_deadlock"`
}

// ReplicationStats aggregates replication diagnostics.
type ReplicationStats struct {
	Collected   bool               `json:"collected"`
	Role        string             `json:"role"` // primary | standby | unknown
	IsRecovery  bool               `json:"is_in_recovery"`
	Connections []ReplicationConn  `json:"connections,omitempty"`
	Slots       []ReplicationSlot  `json:"slots,omitempty"`
	Conflicts   []ReplicaConflict  `json:"conflicts,omitempty"`
	Settings    map[string]string  `json:"settings,omitempty"`
	CurrentLSN  string             `json:"current_lsn,omitempty"`
}
