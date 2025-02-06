package collector

import (
	"context"
	"fmt"
	"time"

	"github.com/hghukasyan/pgscope/pkg/models"
)

// CollectWAL gathers WAL and checkpoint observations.
func (c *Collector) CollectWAL(ctx context.Context) (models.WALStats, []models.Limitation) {
	return c.collectWAL(ctx)
}

func (c *Collector) collectWAL(ctx context.Context) (models.WALStats, []models.Limitation) {
	var lims []models.Limitation
	stats := models.WALStats{Settings: map[string]string{}}

	if !c.cfg.EnableWAL {
		lims = append(lims, models.Limitation{
			Area: "wal", Reason: "WAL collector disabled by configuration",
			Impact: "WAL and checkpoint diagnostics skipped",
		})
		return stats, lims
	}

	for _, name := range []string{
		"max_wal_size", "min_wal_size", "wal_compression", "wal_level",
		"checkpoint_timeout", "checkpoint_completion_target",
		"wal_buffers", "synchronous_commit", "archive_mode",
	} {
		var val string
		if err := c.pool.QueryRow(ctx, `SELECT current_setting($1, true)`, name).Scan(&val); err == nil && val != "" {
			stats.Settings[name] = val
		}
	}

	// Prefer pg_stat_wal (PG14+) when readable.
	walCols, err := c.relationColumns(ctx, "pg_stat_wal")
	if err == nil && len(walCols) > 0 {
		var (
			records, fpi, bytes, buffersFull *int64
			writeTime, syncTime              *float64
			reset                            *time.Time
		)
		q := fmt.Sprintf(`
			SELECT
				%s,
				%s,
				%s,
				%s,
				%s,
				%s,
				%s
			FROM pg_stat_wal
		`,
			selectOrNull(walCols, "wal_records", "bigint"),
			selectOrNull(walCols, "wal_fpi", "bigint"),
			selectOrNull(walCols, "wal_bytes", "bigint"),
			selectOrNull(walCols, "wal_buffers_full", "bigint"),
			selectOrNull(walCols, "wal_write_time", "float8"),
			selectOrNull(walCols, "wal_sync_time", "float8"),
			selectOrNull(walCols, "stats_reset", "timestamptz"),
		)
		if err := c.pool.QueryRow(ctx, q).Scan(
			&records, &fpi, &bytes, &buffersFull, &writeTime, &syncTime, &reset,
		); err != nil {
			if isPermissionError(err) {
				lims = append(lims, models.Limitation{
					Area: "wal", Reason: "Permission denied reading pg_stat_wal",
					Impact: "WAL counters unavailable", Hint: "Grant pg_monitor",
				})
			} else {
				lims = append(lims, models.Limitation{Area: "wal", Reason: err.Error(), Impact: "WAL counters unavailable"})
			}
		} else {
			stats.WALRecords = records
			stats.WALFPI = fpi
			stats.WALBytes = bytes
			stats.WALBuffersFull = buffersFull
			stats.WALWriteTimeMS = writeTime
			stats.WALSyncTimeMS = syncTime
			stats.StatsReset = reset
			stats.Collected = true
		}
	} else {
		lims = append(lims, models.Limitation{
			Area: "wal", Reason: "pg_stat_wal unavailable",
			Impact: "WAL generation counters unavailable on this instance",
		})
	}

	bgCols, err := c.relationColumns(ctx, "pg_stat_bgwriter")
	if err == nil && len(bgCols) > 0 {
		var (
			timed, req, bufChk, bufClean, bufBackend *int64
			writeTime, syncTime                      *float64
			reset                                    *time.Time
		)
		q := fmt.Sprintf(`
			SELECT
				%s, %s, %s, %s, %s, %s, %s, %s
			FROM pg_stat_bgwriter
		`,
			selectOrNull(bgCols, "checkpoints_timed", "bigint"),
			selectOrNull(bgCols, "checkpoints_req", "bigint"),
			selectOrNull(bgCols, "checkpoint_write_time", "float8"),
			selectOrNull(bgCols, "checkpoint_sync_time", "float8"),
			selectOrNull(bgCols, "buffers_checkpoint", "bigint"),
			selectOrNull(bgCols, "buffers_clean", "bigint"),
			selectOrNull(bgCols, "buffers_backend", "bigint"),
			selectOrNull(bgCols, "stats_reset", "timestamptz"),
		)
		if err := c.pool.QueryRow(ctx, q).Scan(
			&timed, &req, &writeTime, &syncTime, &bufChk, &bufClean, &bufBackend, &reset,
		); err != nil {
			if isPermissionError(err) {
				lims = append(lims, models.Limitation{
					Area: "checkpoints", Reason: "Permission denied reading pg_stat_bgwriter",
					Impact: "Checkpoint counters unavailable",
				})
			} else {
				lims = append(lims, models.Limitation{Area: "checkpoints", Reason: err.Error(), Impact: "Checkpoint counters unavailable"})
			}
		} else {
			stats.CheckpointsTimed = timed
			stats.CheckpointsReq = req
			stats.CheckpointWriteTimeMS = writeTime
			stats.CheckpointSyncTimeMS = syncTime
			stats.BuffersCheckpoint = bufChk
			stats.BuffersClean = bufClean
			stats.BuffersBackend = bufBackend
			if stats.StatsReset == nil {
				stats.StatsReset = reset
			}
			if timed != nil && req != nil {
				total := float64(*timed + *req)
				if total > 0 {
					ratio := float64(*req) / total
					stats.CheckpointsReqRatio = &ratio
				}
			}
			stats.Collected = true
		}
	}

	// PG17+ may expose checkpointer stats separately; attempt best-effort.
	if cpCols, err := c.relationColumns(ctx, "pg_stat_checkpointer"); err == nil && len(cpCols) > 0 {
		var timed, req *int64
		var writeTime, syncTime *float64
		q := fmt.Sprintf(`SELECT %s, %s, %s, %s FROM pg_stat_checkpointer`,
			selectOrNull(cpCols, "num_timed", "bigint"),
			selectOrNull(cpCols, "num_requested", "bigint"),
			selectOrNull(cpCols, "write_time", "float8"),
			selectOrNull(cpCols, "sync_time", "float8"),
		)
		if err := c.pool.QueryRow(ctx, q).Scan(&timed, &req, &writeTime, &syncTime); err == nil {
			if timed != nil {
				stats.CheckpointsTimed = timed
			}
			if req != nil {
				stats.CheckpointsReq = req
			}
			if writeTime != nil {
				stats.CheckpointWriteTimeMS = writeTime
			}
			if syncTime != nil {
				stats.CheckpointSyncTimeMS = syncTime
			}
			if timed != nil && req != nil {
				total := float64(*timed + *req)
				if total > 0 {
					ratio := float64(*req) / total
					stats.CheckpointsReqRatio = &ratio
				}
			}
			stats.Collected = true
		}
	}

	var lsn string
	if err := c.pool.QueryRow(ctx, `SELECT pg_current_wal_lsn()::text`).Scan(&lsn); err == nil {
		stats.CurrentLSN = lsn
		stats.Collected = true
	} else if err := c.pool.QueryRow(ctx, `SELECT pg_last_wal_receive_lsn()::text`).Scan(&lsn); err == nil {
		stats.CurrentLSN = lsn
		stats.Collected = true
	}

	return stats, lims
}

// DeriveWALRates computes rate metrics from two samples. Invalid windows
// (counter reset, non-positive interval) are reported without fabricating rates.
func DeriveWALRates(prev, cur models.WALStats, interval time.Duration) models.WALStats {
	out := cur
	secs := interval.Seconds()
	if secs <= 0 {
		out.RateValid = false
		out.RateInvalidReason = "non-positive sample interval"
		return out
	}
	out.IntervalSeconds = &secs

	if prev.StatsReset != nil && cur.StatsReset != nil && !prev.StatsReset.Equal(*cur.StatsReset) {
		out.RateValid = false
		out.RateInvalidReason = "statistics reset between samples"
		return out
	}
	if prev.WALBytes != nil && cur.WALBytes != nil {
		if *cur.WALBytes < *prev.WALBytes {
			out.RateValid = false
			out.RateInvalidReason = "WAL byte counter decreased (restart or wrap)"
			return out
		}
		rate := float64(*cur.WALBytes-*prev.WALBytes) / secs
		out.WALBytesPerSec = &rate
		out.RateValid = true
	} else {
		out.RateValid = false
		out.RateInvalidReason = "WAL byte counters unavailable on one or both samples"
	}
	return out
}

func (c *Collector) relationColumns(ctx context.Context, relname string) (map[string]bool, error) {
	rows, err := c.pool.Query(ctx, `
		SELECT a.attname
		FROM pg_catalog.pg_attribute a
		JOIN pg_catalog.pg_class c ON c.oid = a.attrelid
		JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
		WHERE c.relname = $1
		  AND n.nspname IN ('pg_catalog', 'public')
		  AND a.attnum > 0 AND NOT a.attisdropped
	`, relname)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out[name] = true
	}
	return out, rows.Err()
}

func selectOrNull(cols map[string]bool, name, typ string) string {
	if cols[name] {
		return name
	}
	return "NULL::" + typ + " AS " + name
}
