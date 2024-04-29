package collector

import (
	"context"

	"github.com/hghukasyan/pgscope/pkg/models"
)

func (c *Collector) collectStorage(ctx context.Context, database string) (models.StorageStats, []models.Limitation) {
	var lims []models.Limitation
	stats := models.StorageStats{}

	var sizeBytes int64
	var sizePretty string
	err := c.pool.QueryRow(ctx, `
		SELECT pg_database_size(current_database()), pg_size_pretty(pg_database_size(current_database()))
	`).Scan(&sizeBytes, &sizePretty)
	if err != nil {
		reason := err.Error()
		hint := ""
		if isPermissionError(err) {
			reason = "Permission denied calling pg_database_size"
			hint = "Grant CONNECT and pg_read_all_stats or measure size as a privileged role"
		}
		lims = append(lims, models.Limitation{
			Area:   "storage",
			Reason: reason,
			Impact: "Database size unavailable",
			Hint:   hint,
		})
		return stats, lims
	}
	stats.DatabaseSizeBytes = sizeBytes
	stats.DatabaseSizePretty = sizePretty

	limit := c.cfg.TopRelations
	if limit <= 0 {
		limit = 10
	}

	rows, err := c.pool.Query(ctx, `
		SELECT
			n.nspname AS schema,
			c.relname AS name,
			CASE c.relkind
				WHEN 'r' THEN 'table'
				WHEN 'm' THEN 'materialized_view'
				WHEN 'p' THEN 'partitioned_table'
				WHEN 'i' THEN 'index'
				WHEN 'I' THEN 'partitioned_index'
				WHEN 't' THEN 'toast'
				ELSE c.relkind::text
			END AS kind,
			COALESCE(pg_total_relation_size(c.oid), 0) AS total_bytes,
			COALESCE(pg_relation_size(c.oid), 0) AS table_bytes,
			COALESCE(pg_indexes_size(c.oid), 0) AS index_bytes,
			COALESCE(pg_total_relation_size(c.reltoastrelid), 0) AS toast_bytes,
			pg_size_pretty(COALESCE(pg_total_relation_size(c.oid), 0)) AS pretty
		FROM pg_catalog.pg_class c
		JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
		WHERE c.relkind IN ('r', 'm', 'p')
		  AND n.nspname NOT IN ('pg_catalog', 'information_schema', 'pg_toast')
		  AND n.nspname NOT LIKE 'pg_temp_%'
		  AND n.nspname NOT LIKE 'pg_toast_temp_%'
		ORDER BY total_bytes DESC
		LIMIT $1
	`, limit)
	if err != nil {
		reason := err.Error()
		if isPermissionError(err) {
			reason = "Permission denied reading relation sizes"
		}
		lims = append(lims, models.Limitation{
			Area:   "relation_sizes",
			Reason: reason,
			Impact: "Top relation size list unavailable",
		})
		stats.Collected = true
		return stats, lims
	}
	defer rows.Close()

	for rows.Next() {
		var r models.RelationSize
		if err := rows.Scan(
			&r.Schema, &r.Name, &r.Kind,
			&r.TotalBytes, &r.TableBytes, &r.IndexBytes, &r.ToastBytes, &r.Pretty,
		); err != nil {
			lims = append(lims, models.Limitation{Area: "relation_sizes", Reason: err.Error(), Impact: "Partial relation sizes"})
			stats.Collected = true
			return stats, lims
		}
		stats.TopRelations = append(stats.TopRelations, r)
	}
	if err := rows.Err(); err != nil {
		lims = append(lims, models.Limitation{Area: "relation_sizes", Reason: err.Error(), Impact: "Partial relation sizes"})
	}

	_ = database
	stats.Collected = true
	return stats, lims
}
