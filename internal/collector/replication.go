package collector

import (
	"context"
	"fmt"

	"github.com/hghukasyan/pgscope/pkg/models"
)

// CollectReplication gathers physical/logical replication diagnostics.
func (c *Collector) CollectReplication(ctx context.Context) (models.ReplicationStats, []models.Limitation) {
	return c.collectReplication(ctx)
}

func (c *Collector) collectReplication(ctx context.Context) (models.ReplicationStats, []models.Limitation) {
	var lims []models.Limitation
	stats := models.ReplicationStats{Settings: map[string]string{}, Role: "unknown"}

	if !c.cfg.EnableReplication {
		lims = append(lims, models.Limitation{
			Area: "replication", Reason: "Replication collector disabled by configuration",
			Impact: "Replication diagnostics skipped",
		})
		return stats, lims
	}

	var inRecovery bool
	if err := c.pool.QueryRow(ctx, `SELECT pg_is_in_recovery()`).Scan(&inRecovery); err != nil {
		lims = append(lims, models.Limitation{Area: "replication", Reason: err.Error(), Impact: "Unable to determine primary/standby role"})
		return stats, lims
	}
	stats.IsRecovery = inRecovery
	if inRecovery {
		stats.Role = "standby"
	} else {
		stats.Role = "primary"
	}

	for _, name := range []string{
		"wal_level", "max_wal_senders", "max_replication_slots",
		"hot_standby", "hot_standby_feedback", "synchronous_commit",
		"synchronous_standby_names", "max_slot_wal_keep_size",
	} {
		var val string
		if err := c.pool.QueryRow(ctx, `SELECT current_setting($1, true)`, name).Scan(&val); err == nil && val != "" {
			stats.Settings[name] = val
		}
	}

	if !inRecovery {
		var lsn string
		if err := c.pool.QueryRow(ctx, `SELECT pg_current_wal_lsn()::text`).Scan(&lsn); err == nil {
			stats.CurrentLSN = lsn
		}
		conns, clims := c.collectReplicationConns(ctx)
		lims = append(lims, clims...)
		stats.Connections = conns
	} else {
		var lsn string
		if err := c.pool.QueryRow(ctx, `SELECT COALESCE(pg_last_wal_replay_lsn()::text, '')`).Scan(&lsn); err == nil {
			stats.CurrentLSN = lsn
		}
		conflicts, clim := c.collectReplicaConflicts(ctx)
		lims = append(lims, clim...)
		stats.Conflicts = conflicts
	}

	slots, slims := c.collectReplicationSlots(ctx)
	lims = append(lims, slims...)
	stats.Slots = slots
	stats.Collected = true
	return stats, lims
}

func (c *Collector) collectReplicationConns(ctx context.Context) ([]models.ReplicationConn, []models.Limitation) {
	var lims []models.Limitation
	cols, err := c.relationColumns(ctx, "pg_stat_replication")
	if err != nil || len(cols) == 0 {
		lims = append(lims, models.Limitation{
			Area: "replication", Reason: "pg_stat_replication unavailable",
			Impact: "Replication connection state unavailable",
		})
		return nil, lims
	}

	q := fmt.Sprintf(`
		SELECT
			pid,
			COALESCE(usename::text, ''),
			COALESCE(application_name, ''),
			COALESCE(client_addr::text, ''),
			COALESCE(state, ''),
			COALESCE(sync_state, ''),
			%s, %s, %s, %s,
			%s, %s, %s,
			CASE WHEN sent_lsn IS NOT NULL THEN pg_wal_lsn_diff(pg_current_wal_lsn(), sent_lsn) ELSE NULL END,
			CASE WHEN replay_lsn IS NOT NULL THEN pg_wal_lsn_diff(pg_current_wal_lsn(), replay_lsn) ELSE NULL END,
			COALESCE(backend_xmin::text, '')
		FROM pg_stat_replication
		ORDER BY pid
	`,
		selectOrNullText(cols, "sent_lsn"),
		selectOrNullText(cols, "write_lsn"),
		selectOrNullText(cols, "flush_lsn"),
		selectOrNullText(cols, "replay_lsn"),
		selectOrNullInterval(cols, "write_lag"),
		selectOrNullInterval(cols, "flush_lag"),
		selectOrNullInterval(cols, "replay_lag"),
	)

	rows, err := c.pool.Query(ctx, q)
	if err != nil {
		reason := err.Error()
		if isPermissionError(err) {
			reason = "Permission denied reading pg_stat_replication"
		}
		lims = append(lims, models.Limitation{
			Area: "replication", Reason: reason,
			Impact: "Replication connection diagnostics unavailable",
			Hint:   "Requires role with pg_monitor or membership in replication roles on managed services",
		})
		return nil, lims
	}
	defer rows.Close()

	var out []models.ReplicationConn
	for rows.Next() {
		var r models.ReplicationConn
		var writeLag, flushLag, replayLag *string
		var byteLagSent, byteLagReplay *int64
		if err := rows.Scan(
			&r.PID, &r.Usename, &r.ApplicationName, &r.ClientAddr, &r.State, &r.SyncState,
			&r.SentLSN, &r.WriteLSN, &r.FlushLSN, &r.ReplayLSN,
			&writeLag, &flushLag, &replayLag,
			&byteLagSent, &byteLagReplay,
			&r.BackendXmin,
		); err != nil {
			lims = append(lims, models.Limitation{Area: "replication", Reason: err.Error(), Impact: "Partial replication stats"})
			return out, lims
		}
		// Preserve NULLs: do not coerce missing lag to zero.
		r.WriteLag = writeLag
		r.FlushLag = flushLag
		r.ReplayLag = replayLag
		r.ByteLagSent = byteLagSent
		r.ByteLagReplay = byteLagReplay
		out = append(out, r)
	}
	return out, lims
}

func (c *Collector) collectReplicationSlots(ctx context.Context) ([]models.ReplicationSlot, []models.Limitation) {
	var lims []models.Limitation
	cols, err := c.relationColumns(ctx, "pg_replication_slots")
	if err != nil || len(cols) == 0 {
		lims = append(lims, models.Limitation{
			Area: "replication_slots", Reason: "pg_replication_slots unavailable",
			Impact: "Slot retention diagnostics unavailable",
		})
		return nil, lims
	}

	q := fmt.Sprintf(`
		SELECT
			slot_name,
			COALESCE(plugin, ''),
			slot_type,
			COALESCE(database, ''),
			active,
			COALESCE(restart_lsn::text, ''),
			%s,
			%s,
			%s,
			CASE
				WHEN restart_lsn IS NOT NULL THEN pg_wal_lsn_diff(
					CASE WHEN pg_is_in_recovery() THEN COALESCE(pg_last_wal_replay_lsn(), restart_lsn) ELSE pg_current_wal_lsn() END,
					restart_lsn
				)
				ELSE NULL
			END,
			%s
		FROM pg_replication_slots
		ORDER BY slot_name
	`,
		selectOrNullText(cols, "confirmed_flush_lsn"),
		selectOrNullText(cols, "wal_status"),
		selectOrNull(cols, "safe_wal_size", "bigint"),
		selectOrNullBool(cols, "two_phase"),
	)

	rows, err := c.pool.Query(ctx, q)
	if err != nil {
		reason := err.Error()
		if isPermissionError(err) {
			reason = "Permission denied reading pg_replication_slots"
		}
		lims = append(lims, models.Limitation{
			Area: "replication_slots", Reason: reason,
			Impact: "Replication slot diagnostics unavailable",
		})
		return nil, lims
	}
	defer rows.Close()

	var out []models.ReplicationSlot
	for rows.Next() {
		var s models.ReplicationSlot
		var confirmed, walStatus string
		var safe *int64
		var retained *int64
		var twoPhase bool
		if err := rows.Scan(
			&s.SlotName, &s.Plugin, &s.SlotType, &s.Database, &s.Active,
			&s.RestartLSN, &confirmed, &walStatus, &safe, &retained, &twoPhase,
		); err != nil {
			lims = append(lims, models.Limitation{Area: "replication_slots", Reason: err.Error(), Impact: "Partial slot stats"})
			return out, lims
		}
		s.ConfirmedFlushLSN = confirmed
		s.WalStatus = walStatus
		s.SafeWALSize = safe
		s.RetainedBytes = retained
		s.TwoPhase = twoPhase
		out = append(out, s)
	}
	return out, lims
}

func (c *Collector) collectReplicaConflicts(ctx context.Context) ([]models.ReplicaConflict, []models.Limitation) {
	var lims []models.Limitation
	rows, err := c.pool.Query(ctx, `
		SELECT
			datname,
			COALESCE(confl_tablespace, 0),
			COALESCE(confl_lock, 0),
			COALESCE(confl_snapshot, 0),
			COALESCE(confl_bufferpin, 0),
			COALESCE(confl_deadlock, 0)
		FROM pg_stat_database_conflicts
		WHERE datname = current_database()
	`)
	if err != nil {
		if isPermissionError(err) {
			lims = append(lims, models.Limitation{
				Area: "replica_conflicts", Reason: "Permission denied reading conflict stats",
				Impact: "Standby conflict counters unavailable",
			})
		}
		return nil, lims
	}
	defer rows.Close()
	var out []models.ReplicaConflict
	for rows.Next() {
		var cfl models.ReplicaConflict
		if err := rows.Scan(
			&cfl.Database, &cfl.ConflTablespace, &cfl.ConflLock, &cfl.ConflSnapshot,
			&cfl.ConflBufferpin, &cfl.ConflDeadlock,
		); err != nil {
			return out, lims
		}
		out = append(out, cfl)
	}
	return out, lims
}

func selectOrNullText(cols map[string]bool, name string) string {
	if cols[name] {
		return "COALESCE(" + name + "::text, '') AS " + name
	}
	return "''::text AS " + name
}

func selectOrNullInterval(cols map[string]bool, name string) string {
	if cols[name] {
		return name + "::text AS " + name
	}
	return "NULL::text AS " + name
}

func selectOrNullBool(cols map[string]bool, name string) string {
	if cols[name] {
		return "COALESCE(" + name + ", false) AS " + name
	}
	return "false AS " + name
}

// LSNByteDiff is a helper for tests / reporting when both LSNs are known.
func LSNByteDiff(a, b int64) int64 {
	if a > b {
		return a - b
	}
	return b - a
}
