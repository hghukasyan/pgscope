package collector

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/hghukasyan/pgscope/internal/database"
	"github.com/hghukasyan/pgscope/pkg/models"
)

// CollectQueries gathers top statements from pg_stat_statements when available.
func (c *Collector) CollectQueries(ctx context.Context, sortBy models.QuerySortKey, limit int) (models.QueryWorkloadStats, []models.Limitation) {
	return c.collectQueries(ctx, sortBy, limit)
}

func (c *Collector) collectQueries(ctx context.Context, sortBy models.QuerySortKey, limit int) (models.QueryWorkloadStats, []models.Limitation) {
	var lims []models.Limitation
	stats := models.QueryWorkloadStats{SortBy: sortBy, Limit: limit}

	if !c.cfg.EnableQueries {
		lims = append(lims, models.Limitation{
			Area: "queries", Reason: "Query collector disabled by configuration",
			Impact: "pg_stat_statements workload analysis skipped",
		})
		return stats, lims
	}

	if limit <= 0 {
		limit = c.cfg.TopQueries
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 200 {
		limit = 200
	}
	if sortBy == "" {
		sortBy = models.QuerySortKey(c.cfg.QuerySort)
	}
	if sortBy == "" {
		sortBy = models.QuerySortTotalTime
	}
	stats.SortBy = sortBy
	stats.Limit = limit

	cols, err := c.pgStatStatementsColumns(ctx)
	if err != nil {
		reason := err.Error()
		hint := "CREATE EXTENSION pg_stat_statements; ensure shared_preload_libraries includes pg_stat_statements and grant pg_read_all_stats (or SELECT on the view)."
		if isPermissionError(err) {
			reason = "Permission denied reading pg_stat_statements metadata"
		} else if strings.Contains(strings.ToLower(reason), "does not exist") {
			reason = "pg_stat_statements is not available"
		}
		lims = append(lims, models.Limitation{
			Area: "queries", Reason: reason,
			Impact: "Query intelligence unavailable",
			Hint:   hint,
		})
		return stats, lims
	}
	if len(cols) == 0 {
		lims = append(lims, models.Limitation{
			Area: "queries", Reason: "pg_stat_statements view not found",
			Impact: "Query intelligence unavailable",
			Hint:   "CREATE EXTENSION pg_stat_statements; add it to shared_preload_libraries and restart if required.",
		})
		return stats, lims
	}
	stats.ColumnsPresent = sortedKeys(cols)
	stats.Available = true

	orderExpr, err := orderExpression(sortBy, cols)
	if err != nil {
		lims = append(lims, models.Limitation{
			Area: "queries", Reason: err.Error(),
			Impact: "Requested sort key unavailable on this server; falling back to total time",
		})
		orderExpr, _ = orderExpression(models.QuerySortTotalTime, cols)
		stats.SortBy = models.QuerySortTotalTime
	}

	selectCols := buildSelectList(cols)
	q := fmt.Sprintf(`
		SELECT %s
		FROM pg_stat_statements
		WHERE dbid = (SELECT oid FROM pg_database WHERE datname = current_database())
		   OR dbid IS NULL
		ORDER BY %s DESC NULLS LAST
		LIMIT $1
	`, selectCols, orderExpr)

	rows, err := c.pool.Query(ctx, q, limit)
	if err != nil {
		reason := err.Error()
		hint := ""
		if isPermissionError(err) {
			reason = "Permission denied reading pg_stat_statements"
			hint = "Grant pg_read_all_stats or SELECT on pg_stat_statements"
		}
		lims = append(lims, models.Limitation{
			Area: "queries", Reason: reason, Impact: "Query statistics unavailable", Hint: hint,
		})
		return stats, lims
	}
	defer rows.Close()

	includeText := c.cfg.CollectQueryText
	redact := c.cfg.SensitiveDataSafe

	for rows.Next() {
		st, scanErr := scanStatement(rows, cols)
		if scanErr != nil {
			lims = append(lims, models.Limitation{Area: "queries", Reason: scanErr.Error(), Impact: "Partial query statistics"})
			return stats, lims
		}
		if !includeText {
			st.QueryText = ""
			st.QueryTextRedacted = true
		} else if redact {
			st.QueryText = database.SanitizeQueryPreview(st.QueryText, c.cfg.QueryPreviewLen)
			st.QueryTextRedacted = true
		} else {
			st.QueryText = database.SanitizeQueryPreview(st.QueryText, max(c.cfg.QueryPreviewLen, 500))
		}
		stats.Statements = append(stats.Statements, st)
	}
	if err := rows.Err(); err != nil {
		lims = append(lims, models.Limitation{Area: "queries", Reason: err.Error(), Impact: "Partial query statistics"})
		return stats, lims
	}

	var reset *time.Time
	if err := c.pool.QueryRow(ctx, `
		SELECT stats_reset FROM pg_stat_database WHERE datname = current_database()
	`).Scan(&reset); err == nil {
		stats.StatsReset = reset
	}

	var track string
	if err := c.pool.QueryRow(ctx, `SELECT current_setting('pg_stat_statements.track', true)`).Scan(&track); err == nil {
		stats.TrackSetting = track
	}

	stats.Collected = true
	return stats, lims
}

func (c *Collector) pgStatStatementsColumns(ctx context.Context) (map[string]bool, error) {
	rows, err := c.pool.Query(ctx, `
		SELECT a.attname
		FROM pg_catalog.pg_attribute a
		JOIN pg_catalog.pg_class c ON c.oid = a.attrelid
		JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
		WHERE c.relname = 'pg_stat_statements'
		  AND a.attnum > 0 AND NOT a.attisdropped
		  AND n.nspname NOT IN ('pg_toast')
	`)
	if err != nil {
		// Fallback: try selecting from the view directly via information_schema.
		rows2, err2 := c.pool.Query(ctx, `
			SELECT column_name FROM information_schema.columns
			WHERE table_name = 'pg_stat_statements'
		`)
		if err2 != nil {
			return nil, err
		}
		defer rows2.Close()
		out := map[string]bool{}
		for rows2.Next() {
			var name string
			if err := rows2.Scan(&name); err != nil {
				return nil, err
			}
			out[name] = true
		}
		return out, rows2.Err()
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

func buildSelectList(cols map[string]bool) string {
	// Prefer PG14+ names (total_exec_time) with fallbacks to pre-13 names (total_time).
	parts := []string{
		coalesceCol(cols, "queryid", "0::bigint AS queryid"),
		coalesceCol(cols, "query", "''::text AS query"),
		coalesceCol(cols, "dbid", "0::oid AS dbid"),
		coalesceCol(cols, "userid", "0::oid AS userid"),
		coalesceCol(cols, "calls", "0::bigint AS calls"),
	}
	if cols["total_exec_time"] {
		parts = append(parts, "total_exec_time", "mean_exec_time")
	} else if cols["total_time"] {
		parts = append(parts, "total_time AS total_exec_time", "mean_time AS mean_exec_time")
	} else {
		parts = append(parts, "0::float8 AS total_exec_time", "0::float8 AS mean_exec_time")
	}
	parts = append(parts,
		optionalCol(cols, "min_exec_time", "min_time"),
		optionalCol(cols, "max_exec_time", "max_time"),
		optionalCol(cols, "stddev_exec_time", "stddev_time"),
		coalesceCol(cols, "rows", "0::bigint AS rows"),
		coalesceCol(cols, "shared_blks_hit", "0::bigint AS shared_blks_hit"),
		coalesceCol(cols, "shared_blks_read", "0::bigint AS shared_blks_read"),
		optionalCol(cols, "shared_blks_dirtied"),
		optionalCol(cols, "shared_blks_written"),
		coalesceCol(cols, "temp_blks_read", "0::bigint AS temp_blks_read"),
		coalesceCol(cols, "temp_blks_written", "0::bigint AS temp_blks_written"),
		optionalCol(cols, "wal_records"),
		optionalCol(cols, "wal_bytes"),
		optionalCol(cols, "total_plan_time"),
		optionalCol(cols, "mean_plan_time"),
		optionalCol(cols, "local_blks_hit"),
		optionalCol(cols, "local_blks_read"),
		optionalCol(cols, "blk_read_time"),
		optionalCol(cols, "blk_write_time"),
	)
	return strings.Join(parts, ", ")
}

func coalesceCol(cols map[string]bool, name, fallback string) string {
	if cols[name] {
		return name
	}
	return fallback
}

func optionalCol(cols map[string]bool, names ...string) string {
	for _, n := range names {
		if cols[n] {
			if n == names[0] {
				return n
			}
			return n + " AS " + names[0]
		}
	}
	return "NULL AS " + names[0]
}

func orderExpression(sort models.QuerySortKey, cols map[string]bool) (string, error) {
	total := "total_exec_time"
	if !cols["total_exec_time"] {
		if cols["total_time"] {
			total = "total_time"
		}
	}
	mean := "mean_exec_time"
	if !cols["mean_exec_time"] {
		if cols["mean_time"] {
			mean = "mean_time"
		}
	}
	switch sort {
	case models.QuerySortTotalTime:
		return total, nil
	case models.QuerySortMeanTime:
		return mean, nil
	case models.QuerySortCalls:
		return "calls", nil
	case models.QuerySortSharedReads:
		return "shared_blks_read", nil
	case models.QuerySortTempIO:
		return "(COALESCE(temp_blks_read,0) + COALESCE(temp_blks_written,0))", nil
	case models.QuerySortWAL:
		if cols["wal_bytes"] {
			return "wal_bytes", nil
		}
		if cols["wal_records"] {
			return "wal_records", nil
		}
		return "", fmt.Errorf("WAL metrics not present in pg_stat_statements on this version")
	default:
		return "", fmt.Errorf("unsupported sort key %q", sort)
	}
}

type scannable interface {
	Scan(dest ...any) error
}

func scanStatement(row scannable, cols map[string]bool) (models.StatementStats, error) {
	var st models.StatementStats
	var (
		minT, maxT, stdT, planTotal, planMean, blkR, blkW *float64
		sharedDirtied, sharedWritten, walRec, walBytes    *int64
		localHit, localRead                               *int64
		dbid, userid                                      uint32
	)
	err := row.Scan(
		&st.QueryID, &st.QueryText, &dbid, &userid, &st.Calls,
		&st.TotalExecTimeMS, &st.MeanExecTimeMS,
		&minT, &maxT, &stdT,
		&st.Rows, &st.SharedBlksHit, &st.SharedBlksRead,
		&sharedDirtied, &sharedWritten,
		&st.TempBlksRead, &st.TempBlksWritten,
		&walRec, &walBytes,
		&planTotal, &planMean,
		&localHit, &localRead,
		&blkR, &blkW,
	)
	if err != nil {
		return st, err
	}
	st.DatabaseOID = dbid
	st.UserOID = userid
	st.MinExecTimeMS = minT
	st.MaxExecTimeMS = maxT
	st.StddevExecTimeMS = stdT
	st.SharedBlksDirtied = sharedDirtied
	st.SharedBlksWritten = sharedWritten
	st.WALRecords = walRec
	st.WALBytes = walBytes
	st.TotalPlanTimeMS = planTotal
	st.MeanPlanTimeMS = planMean
	st.LocalBlksHit = localHit
	st.LocalBlksRead = localRead
	st.BlkReadTimeMS = blkR
	st.BlkWriteTimeMS = blkW
	_ = cols
	return st, nil
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j] < out[i] {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
