package collector

import (
	"context"
	"fmt"
	"strconv"

	"github.com/hghukasyan/pgscope/pkg/models"
)

// CollectVacuum gathers autovacuum / maintenance observations.
func (c *Collector) CollectVacuum(ctx context.Context, tableFilter string) (models.VacuumStats, []models.Limitation) {
	return c.collectVacuum(ctx, tableFilter)
}

func (c *Collector) collectVacuum(ctx context.Context, tableFilter string) (models.VacuumStats, []models.Limitation) {
	var lims []models.Limitation
	stats := models.VacuumStats{Settings: map[string]string{}}

	if !c.cfg.EnableVacuum {
		lims = append(lims, models.Limitation{
			Area: "vacuum", Reason: "Vacuum collector disabled by configuration",
			Impact: "Autovacuum diagnostics skipped",
		})
		return stats, lims
	}

	settings := []string{
		"autovacuum",
		"autovacuum_vacuum_threshold",
		"autovacuum_vacuum_scale_factor",
		"autovacuum_analyze_threshold",
		"autovacuum_analyze_scale_factor",
		"autovacuum_freeze_max_age",
		"autovacuum_multixact_freeze_max_age",
		"autovacuum_naptime",
		"autovacuum_max_workers",
		"vacuum_freeze_table_age",
		"vacuum_failsafe_age",
	}
	for _, name := range settings {
		var val string
		if err := c.pool.QueryRow(ctx, `SELECT current_setting($1, true)`, name).Scan(&val); err == nil && val != "" {
			stats.Settings[name] = val
		}
	}
	// PG16+ optional max threshold.
	var maxThresh string
	if err := c.pool.QueryRow(ctx, `SELECT current_setting('autovacuum_vacuum_max_threshold', true)`).Scan(&maxThresh); err == nil && maxThresh != "" {
		stats.Settings["autovacuum_vacuum_max_threshold"] = maxThresh
	}

	globalVacThresh := int64(50)
	globalVacScale := 0.2
	globalAnThresh := int64(50)
	globalAnScale := 0.1
	var globalMaxThresh *int64
	if v, ok := stats.Settings["autovacuum_vacuum_threshold"]; ok {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			globalVacThresh = n
		}
	}
	if v, ok := stats.Settings["autovacuum_vacuum_scale_factor"]; ok {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			globalVacScale = f
		}
	}
	if v, ok := stats.Settings["autovacuum_analyze_threshold"]; ok {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			globalAnThresh = n
		}
	}
	if v, ok := stats.Settings["autovacuum_analyze_scale_factor"]; ok {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			globalAnScale = f
		}
	}
	if v, ok := stats.Settings["autovacuum_vacuum_max_threshold"]; ok {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			globalMaxThresh = &n
		}
	}

	limit := c.cfg.TopVacuumTables
	if limit <= 0 {
		limit = 25
	}
	if c.cfg.DeepScan {
		limit = max(limit, 100)
	}

	query := `
		SELECT
			s.schemaname,
			s.relname,
			COALESCE(s.n_live_tup, 0),
			COALESCE(s.n_dead_tup, 0),
			COALESCE(s.n_mod_since_analyze, 0),
			COALESCE(s.n_tup_ins, 0),
			COALESCE(s.n_tup_upd, 0),
			COALESCE(s.n_tup_del, 0),
			COALESCE(s.n_tup_hot_upd, 0),
			s.last_vacuum,
			s.last_autovacuum,
			s.last_analyze,
			s.last_autoanalyze,
			COALESCE(s.vacuum_count, 0),
			COALESCE(s.autovacuum_count, 0),
			COALESCE(s.analyze_count, 0),
			COALESCE(s.autoanalyze_count, 0),
			COALESCE(pg_table_size(c.oid), 0),
			pg_size_pretty(COALESCE(pg_table_size(c.oid), 0)),
			COALESCE(age(c.relfrozenxid), 0),
			COALESCE( (SELECT option_value FROM pg_options_to_table(c.reloptions) WHERE option_name = 'autovacuum_vacuum_threshold' LIMIT 1), '' ),
			COALESCE( (SELECT option_value FROM pg_options_to_table(c.reloptions) WHERE option_name = 'autovacuum_vacuum_scale_factor' LIMIT 1), '' ),
			COALESCE( (SELECT option_value FROM pg_options_to_table(c.reloptions) WHERE option_name = 'autovacuum_analyze_threshold' LIMIT 1), '' ),
			COALESCE( (SELECT option_value FROM pg_options_to_table(c.reloptions) WHERE option_name = 'autovacuum_analyze_scale_factor' LIMIT 1), '' )
		FROM pg_stat_user_tables s
		JOIN pg_class c ON c.oid = s.relid
		WHERE ($1 = '' OR (s.schemaname || '.' || s.relname) = $1 OR s.relname = $1)
		ORDER BY s.n_dead_tup DESC, pg_table_size(c.oid) DESC
		LIMIT $2
	`
	rows, err := c.pool.Query(ctx, query, tableFilter, limit)
	if err != nil {
		reason := err.Error()
		hint := ""
		if isPermissionError(err) {
			reason = "Permission denied reading table vacuum statistics"
			hint = "Grant pg_monitor / pg_read_all_stats"
		}
		lims = append(lims, models.Limitation{Area: "vacuum", Reason: reason, Impact: "Autovacuum diagnostics unavailable", Hint: hint})
		return stats, lims
	}
	defer rows.Close()

	for rows.Next() {
		var t models.VacuumTableStats
		var vacThreshOverride, vacScaleOverride, anThreshOverride, anScaleOverride string
		if err := rows.Scan(
			&t.Schema, &t.Name,
			&t.LiveTuples, &t.DeadTuples, &t.ModSinceAnalyze,
			&t.Inserts, &t.Updates, &t.Deletes, &t.HotUpdates,
			&t.LastVacuum, &t.LastAutovacuum, &t.LastAnalyze, &t.LastAutoanalyze,
			&t.VacuumCount, &t.AutovacuumCount, &t.AnalyzeCount, &t.AutoanalyzeCount,
			&t.TableBytes, &t.TablePretty, &t.RelFrozenXIDAge,
			&vacThreshOverride, &vacScaleOverride, &anThreshOverride, &anScaleOverride,
		); err != nil {
			lims = append(lims, models.Limitation{Area: "vacuum", Reason: err.Error(), Impact: "Partial vacuum stats"})
			return stats, lims
		}

		vacThresh := globalVacThresh
		vacScale := globalVacScale
		anThresh := globalAnThresh
		anScale := globalAnScale
		if vacThreshOverride != "" {
			if n, err := strconv.ParseInt(vacThreshOverride, 10, 64); err == nil {
				vacThresh = n
			}
		}
		if vacScaleOverride != "" {
			if f, err := strconv.ParseFloat(vacScaleOverride, 64); err == nil {
				vacScale = f
			}
		}
		if anThreshOverride != "" {
			if n, err := strconv.ParseInt(anThreshOverride, 10, 64); err == nil {
				anThresh = n
			}
		}
		if anScaleOverride != "" {
			if f, err := strconv.ParseFloat(anScaleOverride, 64); err == nil {
				anScale = f
			}
		}

		t.VacuumBaseThreshold = vacThresh
		t.ScaleFactor = vacScale
		t.MaxThreshold = globalMaxThresh
		t.VacuumThreshold = ComputeVacuumThreshold(vacThresh, vacScale, t.LiveTuples, globalMaxThresh)
		t.AnalyzeThreshold = ComputeVacuumThreshold(anThresh, anScale, t.LiveTuples, nil)
		t.WouldAutovacuum = t.DeadTuples >= t.VacuumThreshold
		t.WouldAutoanalyze = t.ModSinceAnalyze >= t.AnalyzeThreshold
		if t.LiveTuples+t.DeadTuples > 0 {
			t.DeadTuplePct = float64(t.DeadTuples) / float64(t.LiveTuples+t.DeadTuples) * 100
		}
		stats.Tables = append(stats.Tables, t)
		if isVacuumRisk(t, stats.Settings) {
			stats.Risks = append(stats.Risks, t)
		}
	}
	if err := rows.Err(); err != nil {
		lims = append(lims, models.Limitation{Area: "vacuum", Reason: err.Error(), Impact: "Partial vacuum stats"})
		return stats, lims
	}

	progress, plims := c.collectVacuumProgress(ctx)
	lims = append(lims, plims...)
	stats.Progress = progress

	var age string
	if err := c.pool.QueryRow(ctx, `
		SELECT COALESCE(MAX(NOW() - xact_start)::text, '')
		FROM pg_stat_activity
		WHERE xact_start IS NOT NULL
		  AND pid <> pg_backend_pid()
		  AND (backend_type = 'client backend' OR backend_type IS NULL)
	`).Scan(&age); err == nil && age != "" {
		stats.LongXactBlockingAge = age
	}

	stats.Collected = true
	return stats, lims
}

func (c *Collector) collectVacuumProgress(ctx context.Context) ([]models.VacuumProgress, []models.Limitation) {
	var lims []models.Limitation
	rows, err := c.pool.Query(ctx, `
		SELECT
			p.pid,
			COALESCE(d.datname, ''),
			COALESCE(n.nspname, ''),
			COALESCE(c.relname, ''),
			COALESCE(p.phase, ''),
			COALESCE(p.heap_blks_total, 0),
			COALESCE(p.heap_blks_scanned, 0),
			COALESCE(p.heap_blks_vacuumed, 0),
			COALESCE(p.index_vacuum_count, 0),
			COALESCE(p.max_dead_tuples, 0),
			COALESCE(p.num_dead_tuples, 0)
		FROM pg_stat_progress_vacuum p
		LEFT JOIN pg_database d ON d.oid = p.datid
		LEFT JOIN pg_class c ON c.oid = p.relid
		LEFT JOIN pg_namespace n ON n.oid = c.relnamespace
	`)
	if err != nil {
		if isPermissionError(err) {
			lims = append(lims, models.Limitation{
				Area: "vacuum_progress", Reason: "Permission denied reading pg_stat_progress_vacuum",
				Impact: "Active vacuum progress unavailable",
			})
		}
		return nil, lims
	}
	defer rows.Close()
	var out []models.VacuumProgress
	for rows.Next() {
		var p models.VacuumProgress
		if err := rows.Scan(
			&p.PID, &p.Database, &p.Schema, &p.Table, &p.Phase,
			&p.HeapBlksTotal, &p.HeapBlksScanned, &p.HeapBlksVacuumed,
			&p.IndexVacuumCount, &p.MaxDeadTuples, &p.NumDeadTuples,
		); err != nil {
			lims = append(lims, models.Limitation{Area: "vacuum_progress", Reason: err.Error(), Impact: "Partial progress"})
			return out, lims
		}
		out = append(out, p)
	}
	return out, lims
}

// ComputeVacuumThreshold implements PostgreSQL's effective threshold formula:
// threshold + scale_factor * reltuples, optionally capped by max_threshold (PG16+).
func ComputeVacuumThreshold(threshold int64, scaleFactor float64, liveTuples int64, maxThreshold *int64) int64 {
	v := threshold + int64(scaleFactor*float64(liveTuples))
	if maxThreshold != nil && *maxThreshold >= 0 && v > *maxThreshold {
		v = *maxThreshold
	}
	return v
}

func isVacuumRisk(t models.VacuumTableStats, settings map[string]string) bool {
	if t.WouldAutovacuum && t.DeadTuplePct >= 10 {
		return true
	}
	if t.WouldAutoanalyze && t.ModSinceAnalyze > 0 && t.LastAutoanalyze == nil && t.LastAnalyze == nil {
		return true
	}
	freezeMax := int64(200_000_000)
	if v, ok := settings["autovacuum_freeze_max_age"]; ok {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			freezeMax = n
		}
	}
	// Warn when within 20% of freeze max age.
	if t.RelFrozenXIDAge > 0 && float64(t.RelFrozenXIDAge) >= float64(freezeMax)*0.8 {
		return true
	}
	return false
}

// FormatVacuumThresholdExplain returns a human explanation of threshold math.
func FormatVacuumThresholdExplain(t models.VacuumTableStats) string {
	base := fmt.Sprintf("threshold=%d + scale_factor=%.4f * n_live_tup=%d => %d",
		t.VacuumBaseThreshold, t.ScaleFactor, t.LiveTuples, t.VacuumThreshold)
	if t.MaxThreshold != nil {
		base += fmt.Sprintf(" (max_threshold=%d applied when lower)", *t.MaxThreshold)
	}
	return base
}

