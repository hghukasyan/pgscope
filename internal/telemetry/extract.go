// Package telemetry extracts historical metric samples from diagnostic snapshots
// and computes safe counter deltas.
package telemetry

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/hghukasyan/pgscope/pkg/models"
)

const (
	maxQueryMetrics   = 50
	maxVacuumMetrics  = 40
	maxIndexMetrics   = 40
	maxVectorMetrics  = 30
	maxReplicationMetrics = 40
)

// InstanceKey builds a stable identity for the monitored instance (no credentials).
func InstanceKey(inst models.Instance) string {
	parts := []string{inst.ServerAddr, inst.VersionString}
	key := strings.Join(parts, "|")
	if key == "|" || key == "" {
		return fmt.Sprintf("db:%s", inst.Database)
	}
	return key
}

// ResetToken captures identity that changes on restart/stats reset.
func ResetToken(snap models.Snapshot) string {
	var parts []string
	if snap.Instance.StartTime != nil {
		parts = append(parts, snap.Instance.StartTime.UTC().Format(time.RFC3339Nano))
	}
	if snap.Queries.StatsReset != nil {
		parts = append(parts, "q:"+snap.Queries.StatsReset.UTC().Format(time.RFC3339Nano))
	}
	if snap.Indexes.StatsReset != nil {
		parts = append(parts, "i:"+snap.Indexes.StatsReset.UTC().Format(time.RFC3339Nano))
	}
	if snap.WAL.StatsReset != nil {
		parts = append(parts, "w:"+snap.WAL.StatsReset.UTC().Format(time.RFC3339Nano))
	}
	if len(parts) == 0 {
		return "unknown"
	}
	return strings.Join(parts, "|")
}

// ExtractMetrics converts a Snapshot into bounded MetricSample rows.
func ExtractMetrics(snap models.Snapshot) []models.MetricSample {
	ik := InstanceKey(snap.Instance)
	db := snap.Instance.Database
	at := snap.CollectedAt
	if at.IsZero() {
		at = time.Now().UTC()
	}
	reset := ResetToken(snap)
	base := func(name string, value float64, kind models.MetricKind, source, unit string, dims map[string]string) models.MetricSample {
		return models.MetricSample{
			SnapshotID:  snap.ID,
			CollectedAt: at,
			InstanceKey: ik,
			Database:    db,
			Name:        name,
			Value:       value,
			Kind:        kind,
			Unit:        unit,
			Dimensions:  dims,
			Source:      source,
			ResetToken:  reset,
			Quality:     models.QualityOK,
			DurationMS:  snap.DurationMS,
		}
	}

	var out []models.MetricSample

	if snap.Connections.Collected {
		out = append(out,
			base("connections.total", float64(snap.Connections.Total), models.MetricGauge, "pg_stat_activity", "count", nil),
			base("connections.active", float64(snap.Connections.Active), models.MetricGauge, "pg_stat_activity", "count", nil),
			base("connections.idle_in_transaction", float64(snap.Connections.IdleInTransaction), models.MetricGauge, "pg_stat_activity", "count", nil),
			base("connections.waiting", float64(snap.Connections.Waiting), models.MetricGauge, "pg_stat_activity", "count", nil),
			base("connections.utilization_pct", snap.Connections.UtilizationPct, models.MetricGauge, "pg_stat_activity", "percent", nil),
			base("connections.max", float64(snap.Connections.MaxConnections), models.MetricGauge, "pg_settings", "count", nil),
		)
	}

	if snap.Locks.Collected {
		out = append(out,
			base("locks.waiting_count", float64(snap.Locks.WaitingCount), models.MetricGauge, "pg_locks", "count", nil),
			base("locks.deadlocks", float64(snap.Locks.Deadlocks), models.MetricCounter, "pg_stat_database", "count", nil),
			base("locks.blocking_chains", float64(len(snap.Locks.BlockingChains)), models.MetricGauge, "pg_locks", "count", nil),
		)
	}

	if snap.Transactions.Collected {
		out = append(out,
			base("transactions.long_running_count", float64(len(snap.Transactions.LongRunning)), models.MetricGauge, "pg_stat_activity", "count", nil),
			base("transactions.idle_in_xact_count", float64(len(snap.Transactions.IdleInTransaction)), models.MetricGauge, "pg_stat_activity", "count", nil),
			base("transactions.xid_age", float64(snap.Transactions.XIDAge), models.MetricGauge, "pg_database", "xid", nil),
		)
	}

	if snap.Storage.Collected {
		out = append(out,
			base("storage.database_bytes", float64(snap.Storage.DatabaseSizeBytes), models.MetricGauge, "pg_database_size", "bytes", nil),
		)
	}

	if snap.Queries.Collected && snap.Queries.Available {
		n := 0
		for _, st := range snap.Queries.Statements {
			if n >= maxQueryMetrics {
				break
			}
			dims := map[string]string{"query_id": strconv.FormatInt(st.QueryID, 10)}
			out = append(out,
				base("query.mean_exec_time_ms", st.MeanExecTimeMS, models.MetricGauge, "pg_stat_statements", "ms", dims),
				base("query.calls", float64(st.Calls), models.MetricCounter, "pg_stat_statements", "count", dims),
				base("query.shared_blks_read", float64(st.SharedBlksRead), models.MetricCounter, "pg_stat_statements", "blocks", dims),
				base("query.temp_blks_written", float64(st.TempBlksWritten), models.MetricCounter, "pg_stat_statements", "blocks", dims),
			)
			n++
		}
	}

	if snap.Vacuum.Collected {
		n := 0
		var deadSum int64
		for _, t := range snap.Vacuum.Tables {
			deadSum += t.DeadTuples
			if n >= maxVacuumMetrics {
				continue
			}
			dims := map[string]string{"relation": t.Schema + "." + t.Name}
			out = append(out,
				base("vacuum.dead_tuples", float64(t.DeadTuples), models.MetricGauge, "pg_stat_user_tables", "count", dims),
				base("vacuum.dead_tuple_pct", t.DeadTuplePct, models.MetricGauge, "pg_stat_user_tables", "percent", dims),
			)
			n++
		}
		out = append(out, base("vacuum.dead_tuples_total", float64(deadSum), models.MetricGauge, "pg_stat_user_tables", "count", nil))
	}

	if snap.WAL.Collected {
		if snap.WAL.WALBytes != nil {
			out = append(out, base("wal.bytes", float64(*snap.WAL.WALBytes), models.MetricCounter, "pg_stat_wal", "bytes", nil))
		}
		if snap.WAL.WALRecords != nil {
			out = append(out, base("wal.records", float64(*snap.WAL.WALRecords), models.MetricCounter, "pg_stat_wal", "count", nil))
		}
		if snap.WAL.CheckpointsTimed != nil {
			out = append(out, base("wal.checkpoints_timed", float64(*snap.WAL.CheckpointsTimed), models.MetricCounter, "pg_stat_bgwriter", "count", nil))
		}
		if snap.WAL.CheckpointsReq != nil {
			out = append(out, base("wal.checkpoints_req", float64(*snap.WAL.CheckpointsReq), models.MetricCounter, "pg_stat_bgwriter", "count", nil))
		}
		if snap.WAL.WALBytesPerSec != nil && snap.WAL.RateValid {
			out = append(out, base("wal.bytes_per_sec", *snap.WAL.WALBytesPerSec, models.MetricDerived, "pg_stat_wal", "bytes/s", nil))
		}
	}

	if snap.Replication.Collected {
		n := 0
		for _, c := range snap.Replication.Connections {
			if n >= maxReplicationMetrics {
				break
			}
			dims := map[string]string{"application": c.ApplicationName, "pid": strconv.Itoa(int(c.PID))}
			if c.ByteLagReplay != nil {
				out = append(out, base("replication.byte_lag_replay", float64(*c.ByteLagReplay), models.MetricGauge, "pg_stat_replication", "bytes", dims))
			}
			if c.ByteLagSent != nil {
				out = append(out, base("replication.byte_lag_sent", float64(*c.ByteLagSent), models.MetricGauge, "pg_stat_replication", "bytes", dims))
			}
			n++
		}
		for _, slot := range snap.Replication.Slots {
			dims := map[string]string{"slot": slot.SlotName, "type": slot.SlotType}
			active := 0.0
			if slot.Active {
				active = 1
			}
			out = append(out, base("replication.slot_active", active, models.MetricGauge, "pg_replication_slots", "bool", dims))
			if slot.RetainedBytes != nil {
				out = append(out, base("replication.slot_retained_bytes", float64(*slot.RetainedBytes), models.MetricGauge, "pg_replication_slots", "bytes", dims))
			}
		}
	}

	if snap.Indexes.Collected {
		n := 0
		for _, idx := range snap.Indexes.Indexes {
			if n >= maxIndexMetrics {
				break
			}
			dims := map[string]string{"index": idx.Schema + "." + idx.Name, "table": idx.TableSchema + "." + idx.TableName}
			out = append(out, base("index.scans", float64(idx.IndexScans), models.MetricCounter, "pg_stat_user_indexes", "count", dims))
			n++
		}
	}

	if snap.Vector.Collected && snap.Vector.Available {
		out = append(out,
			base("vector.columns", float64(len(snap.Vector.Columns)), models.MetricGauge, "pgvector", "count", nil),
			base("vector.indexes", float64(len(snap.Vector.Indexes)), models.MetricGauge, "pgvector", "count", nil),
		)
		n := 0
		for _, idx := range snap.Vector.Indexes {
			if n >= maxVectorMetrics {
				break
			}
			dims := map[string]string{"index": idx.Schema + "." + idx.Name, "am": idx.AccessMethod}
			out = append(out, base("vector.index_scans", float64(idx.IndexScans), models.MetricCounter, "pgvector", "count", dims))
			n++
		}
	}

	out = append(out, base("pgscope.collection_duration_ms", float64(snap.DurationMS), models.MetricGauge, "pgscope", "ms", nil))
	out = append(out, base("pgscope.limitation_count", float64(len(snap.Limitations)), models.MetricGauge, "pgscope", "count", nil))

	return out
}

// DimKey returns a stable string for dimension maps (for grouping).
func DimKey(dims map[string]string) string {
	if len(dims) == 0 {
		return ""
	}
	keys := make([]string, 0, len(dims))
	for k := range dims {
		keys = append(keys, k)
	}
	// insertion-order independent
	for i := 0; i < len(keys); i++ {
		for j := i + 1; j < len(keys); j++ {
			if keys[j] < keys[i] {
				keys[i], keys[j] = keys[j], keys[i]
			}
		}
	}
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+dims[k])
	}
	return strings.Join(parts, ",")
}
