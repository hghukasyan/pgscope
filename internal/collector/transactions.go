package collector

import (
	"context"
	"fmt"
	"time"

	"github.com/hghukasyan/pgscope/internal/database"
	"github.com/hghukasyan/pgscope/pkg/models"
)

func (c *Collector) collectTransactions(ctx context.Context, inst models.Instance) (models.TransactionStats, []models.Limitation) {
	var lims []models.Limitation
	stats := models.TransactionStats{}

	long, err := c.queryLongTransactions(ctx, c.cfg.LongXactThreshold)
	if err != nil {
		reason := err.Error()
		hint := ""
		if isPermissionError(err) {
			reason = "Permission denied reading transaction activity"
			hint = "Grant pg_monitor or pg_read_all_stats"
		}
		lims = append(lims, models.Limitation{
			Area:   "transactions",
			Reason: reason,
			Impact: "Long-running transaction detection unavailable",
			Hint:   hint,
		})
		return stats, lims
	}
	stats.LongRunning = long

	idle, err := c.queryIdleInTransaction(ctx, c.cfg.LongXactThreshold)
	if err != nil {
		lims = append(lims, models.Limitation{
			Area:   "idle_in_transaction",
			Reason: err.Error(),
			Impact: "Idle-in-transaction detection unavailable",
		})
	} else {
		stats.IdleInTransaction = idle
	}

	if len(long) > 0 && long[0].XactStart != nil {
		stats.OldestXactAge = time.Since(*long[0].XactStart).Round(time.Second).String()
	} else if len(idle) > 0 && idle[0].XactStart != nil {
		stats.OldestXactAge = time.Since(*idle[0].XactStart).Round(time.Second).String()
	}

	var age int64
	err = c.pool.QueryRow(ctx, `
		SELECT age(datfrozenxid)::bigint
		FROM pg_database
		WHERE datname = current_database()
	`).Scan(&age)
	if err != nil {
		if isPermissionError(err) {
			lims = append(lims, models.Limitation{
				Area:   "xid_age",
				Reason: "Permission denied reading datfrozenxid age",
				Impact: "Transaction ID wraparound risk cannot be assessed",
			})
		}
	} else {
		stats.DatfrozenxidAge = age
		stats.XIDAge = age
	}

	_ = inst // reserved for version-specific txn collectors
	stats.Collected = true
	return stats, lims
}

func (c *Collector) queryLongTransactions(ctx context.Context, threshold time.Duration) ([]models.Session, error) {
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
		  AND xact_start IS NOT NULL
		  AND NOW() - xact_start > $1::interval
		  AND state <> 'idle'
		ORDER BY xact_start
		LIMIT 25
	`, fmt.Sprintf("%d milliseconds", threshold.Milliseconds()))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSessions(rows, c.cfg.QueryPreviewLen)
}

func (c *Collector) queryIdleInTransaction(ctx context.Context, threshold time.Duration) ([]models.Session, error) {
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
		  AND state IN ('idle in transaction', 'idle in transaction (aborted)')
		  AND xact_start IS NOT NULL
		  AND NOW() - xact_start > $1::interval
		ORDER BY xact_start
		LIMIT 25
	`, fmt.Sprintf("%d milliseconds", threshold.Milliseconds()))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSessions(rows, c.cfg.QueryPreviewLen)
}

type sessionRows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}

func scanSessions(rows sessionRows, previewLen int) ([]models.Session, error) {
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
		if s.XactStart != nil {
			s.Duration = now.Sub(*s.XactStart).Round(time.Second).String()
		}
		s.QueryPreview = database.SanitizeQueryPreview(query, previewLen)
		out = append(out, s)
	}
	return out, rows.Err()
}
