package collector

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/hghukasyan/pgscope/internal/database"
	"github.com/hghukasyan/pgscope/pkg/models"
)

func (c *Collector) collectConnections(ctx context.Context, inst models.Instance) (models.ConnectionStats, []models.Limitation) {
	var lims []models.Limitation
	stats := models.ConnectionStats{}

	if v, ok := inst.Settings["max_connections"]; ok {
		if n, err := strconv.Atoi(v); err == nil {
			stats.MaxConnections = n
		}
	}
	if stats.MaxConnections == 0 {
		var n int
		if err := c.pool.QueryRow(ctx, `SELECT setting::int FROM pg_settings WHERE name = 'max_connections'`).Scan(&n); err == nil {
			stats.MaxConnections = n
		}
	}

	rows, err := c.pool.Query(ctx, `
		SELECT
			COALESCE(state, 'unknown') AS state,
			COUNT(*)::int
		FROM pg_stat_activity
		WHERE backend_type = 'client backend' OR backend_type IS NULL
		GROUP BY 1
	`)
	if err != nil {
		reason := err.Error()
		hint := ""
		if isPermissionError(err) {
			reason = "Permission denied reading pg_stat_activity"
			hint = "Grant pg_monitor or pg_read_all_stats"
		}
		lims = append(lims, models.Limitation{
			Area:   "connections",
			Reason: reason,
			Impact: "Connection utilization and session state unavailable",
			Hint:   hint,
		})
		return stats, lims
	}
	defer rows.Close()

	for rows.Next() {
		var state string
		var count int
		if err := rows.Scan(&state, &count); err != nil {
			lims = append(lims, models.Limitation{Area: "connections", Reason: err.Error(), Impact: "Partial connection stats"})
			return stats, lims
		}
		stats.Total += count
		switch state {
		case "active":
			stats.Active = count
		case "idle":
			stats.Idle = count
		case "idle in transaction":
			stats.IdleInTransaction = count
		case "idle in transaction (aborted)":
			stats.IdleInTransactionAborted = count
		case "disabled":
			stats.Disabled = count
		}
	}
	if err := rows.Err(); err != nil {
		lims = append(lims, models.Limitation{Area: "connections", Reason: err.Error(), Impact: "Partial connection stats"})
		return stats, lims
	}

	var waiting int
	if err := c.pool.QueryRow(ctx, `
		SELECT COUNT(*)::int
		FROM pg_stat_activity
		WHERE wait_event_type = 'Lock'
		  AND (backend_type = 'client backend' OR backend_type IS NULL)
	`).Scan(&waiting); err == nil {
		stats.Waiting = waiting
	}

	if stats.MaxConnections > 0 {
		stats.UtilizationPct = float64(stats.Total) / float64(stats.MaxConnections) * 100
	}

	long, err := c.queryLongSessions(ctx, c.cfg.LongQueryThreshold)
	if err != nil {
		if isPermissionError(err) {
			lims = append(lims, models.Limitation{
				Area:   "long_sessions",
				Reason: "Permission denied reading long-running sessions",
				Impact: "Long-running session details unavailable",
			})
		} else {
			lims = append(lims, models.Limitation{Area: "long_sessions", Reason: err.Error(), Impact: "Long-running session details unavailable"})
		}
	} else {
		stats.LongRunningSessions = long
	}

	stats.Collected = true
	return stats, lims
}

func (c *Collector) queryLongSessions(ctx context.Context, threshold time.Duration) ([]models.Session, error) {
	rows, err := c.pool.Query(ctx, `
		SELECT
			pid,
			COALESCE(usename::text, ''),
			COALESCE(application_name, ''),
			COALESCE(client_addr::text, ''),
			COALESCE(state, ''),
			COALESCE(wait_event_type, ''),
			COALESCE(wait_event, ''),
			xact_start,
			query_start,
			backend_start,
			COALESCE(query, '')
		FROM pg_stat_activity
		WHERE (backend_type = 'client backend' OR backend_type IS NULL)
		  AND pid <> pg_backend_pid()
		  AND state = 'active'
		  AND query_start IS NOT NULL
		  AND NOW() - query_start > $1::interval
		ORDER BY query_start
		LIMIT 25
	`, fmt.Sprintf("%d milliseconds", threshold.Milliseconds()))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []models.Session
	now := time.Now()
	for rows.Next() {
		var s models.Session
		var query string
		if err := rows.Scan(
			&s.PID, &s.Usename, &s.ApplicationName, &s.ClientAddr,
			&s.State, &s.WaitEventType, &s.WaitEvent,
			&s.XactStart, &s.QueryStart, &s.BackendStart, &query,
		); err != nil {
			return nil, err
		}
		if s.QueryStart != nil {
			s.Duration = now.Sub(*s.QueryStart).Round(time.Second).String()
		}
		s.QueryPreview = database.SanitizeQueryPreview(query, c.cfg.QueryPreviewLen)
		out = append(out, s)
	}
	return out, rows.Err()
}
