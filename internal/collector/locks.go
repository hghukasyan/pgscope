package collector

import (
	"context"
	"time"

	"github.com/hghukasyan/pgscope/internal/database"
	"github.com/hghukasyan/pgscope/pkg/models"
)

func (c *Collector) collectLocks(ctx context.Context) (models.LockStats, []models.Limitation) {
	var lims []models.Limitation
	stats := models.LockStats{}

	var waiting int
	err := c.pool.QueryRow(ctx, `
		SELECT COUNT(*)::int
		FROM pg_locks
		WHERE NOT granted
	`).Scan(&waiting)
	if err != nil {
		reason := err.Error()
		hint := ""
		if isPermissionError(err) {
			reason = "Permission denied reading pg_locks"
			hint = "Grant pg_monitor"
		}
		lims = append(lims, models.Limitation{
			Area:   "locks",
			Reason: reason,
			Impact: "Lock contention analysis unavailable",
			Hint:   hint,
		})
		return stats, lims
	}
	stats.WaitingCount = waiting

	chains, err := c.queryBlockingChains(ctx)
	if err != nil {
		lims = append(lims, models.Limitation{
			Area:   "lock_chains",
			Reason: err.Error(),
			Impact: "Blocking session chains unavailable",
		})
	} else {
		stats.BlockingChains = chains
	}

	var deadlocks int64
	err = c.pool.QueryRow(ctx, `
		SELECT deadlocks
		FROM pg_stat_database
		WHERE datname = current_database()
	`).Scan(&deadlocks)
	if err != nil {
		if isPermissionError(err) {
			lims = append(lims, models.Limitation{
				Area:   "deadlocks",
				Reason: "Permission denied reading pg_stat_database.deadlocks",
				Impact: "Deadlock counter unavailable",
			})
		}
	} else {
		stats.Deadlocks = deadlocks
	}

	stats.Collected = true
	return stats, lims
}

func (c *Collector) queryBlockingChains(ctx context.Context) ([]models.LockWait, error) {
	rows, err := c.pool.Query(ctx, `
		SELECT
			blocked.pid AS blocked_pid,
			COALESCE(blocked.usename::text, '') AS blocked_user,
			COALESCE(blocked.query, '') AS blocked_query,
			blocking.pid AS blocking_pid,
			COALESCE(blocking.usename::text, '') AS blocking_user,
			COALESCE(blocking.query, '') AS blocking_query,
			COALESCE(blocked_locks.locktype, '') AS lock_type,
			COALESCE(blocked_locks.relation::regclass::text, '') AS relation,
			COALESCE(blocked_locks.mode, '') AS mode,
			COALESCE(EXTRACT(EPOCH FROM (NOW() - blocked.query_start)), 0) AS blocked_seconds
		FROM pg_catalog.pg_locks blocked_locks
		JOIN pg_catalog.pg_stat_activity blocked
			ON blocked.pid = blocked_locks.pid
		JOIN pg_catalog.pg_locks blocking_locks
			ON blocking_locks.locktype = blocked_locks.locktype
			AND blocking_locks.DATABASE IS NOT DISTINCT FROM blocked_locks.DATABASE
			AND blocking_locks.relation IS NOT DISTINCT FROM blocked_locks.relation
			AND blocking_locks.page IS NOT DISTINCT FROM blocked_locks.page
			AND blocking_locks.tuple IS NOT DISTINCT FROM blocked_locks.tuple
			AND blocking_locks.virtualxid IS NOT DISTINCT FROM blocked_locks.virtualxid
			AND blocking_locks.transactionid IS NOT DISTINCT FROM blocked_locks.transactionid
			AND blocking_locks.classid IS NOT DISTINCT FROM blocked_locks.classid
			AND blocking_locks.objid IS NOT DISTINCT FROM blocked_locks.objid
			AND blocking_locks.objsubid IS NOT DISTINCT FROM blocked_locks.objsubid
			AND blocking_locks.pid <> blocked_locks.pid
		JOIN pg_catalog.pg_stat_activity blocking
			ON blocking.pid = blocking_locks.pid
		WHERE NOT blocked_locks.granted
		  AND blocking_locks.granted
		LIMIT 50
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []models.LockWait
	for rows.Next() {
		var lw models.LockWait
		var blockedQ, blockingQ string
		var blockedSeconds float64
		if err := rows.Scan(
			&lw.BlockedPID, &lw.BlockedUser, &blockedQ,
			&lw.BlockingPID, &lw.BlockingUser, &blockingQ,
			&lw.LockType, &lw.Relation, &lw.Mode, &blockedSeconds,
		); err != nil {
			return nil, err
		}
		lw.BlockedQueryPreview = database.SanitizeQueryPreview(blockedQ, c.cfg.QueryPreviewLen)
		lw.BlockingQueryPreview = database.SanitizeQueryPreview(blockingQ, c.cfg.QueryPreviewLen)
		lw.BlockedDuration = (time.Duration(blockedSeconds) * time.Second).Round(time.Second).String()
		out = append(out, lw)
	}
	return out, rows.Err()
}
