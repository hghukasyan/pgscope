package collector

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/hghukasyan/pgscope/pkg/models"
)

const (
	minSupportedMajor = 14
	maxSupportedMajor = 18
)

func (c *Collector) collectInstance(ctx context.Context) (models.Instance, []models.Limitation, error) {
	var lims []models.Limitation
	inst := models.Instance{
		Settings: make(map[string]string),
		Capabilities: models.Capabilities{
			MinSupportedMajor: minSupportedMajor,
			MaxSupportedMajor: maxSupportedMajor,
		},
	}

	row := c.pool.QueryRow(ctx, `
		SELECT
			current_database(),
			current_user,
			version(),
			current_setting('server_version_num')::int,
			pg_is_in_recovery(),
			pg_postmaster_start_time()
	`)
	var start time.Time
	if err := row.Scan(
		&inst.Database,
		&inst.User,
		&inst.VersionString,
		&inst.VersionNum,
		&inst.IsInRecovery,
		&start,
	); err != nil {
		return inst, lims, fmt.Errorf("instance identity: %w", err)
	}
	inst.StartTime = &start
	inst.MajorVersion = majorFromVersionNum(inst.VersionNum)
	inst.Capabilities.Supported = inst.MajorVersion >= minSupportedMajor && inst.MajorVersion <= maxSupportedMajor
	if !inst.Capabilities.Supported {
		lims = append(lims, models.Limitation{
			Area:   "version",
			Reason: fmt.Sprintf("PostgreSQL major version %d is outside the Phase-1 support window (%d–%d)", inst.MajorVersion, minSupportedMajor, maxSupportedMajor),
			Impact: "Some collectors may fail or return incomplete results",
			Hint:   "PGScope targets PostgreSQL 14–18",
		})
	}

	settings := []string{
		"max_connections",
		"shared_buffers",
		"work_mem",
		"maintenance_work_mem",
		"effective_cache_size",
		"max_wal_size",
		"checkpoint_timeout",
		"autovacuum",
		"rds.extensions",
		"cloudsql.enable_google_adaptive_autovacuum",
	}
	readable := 0
	for _, name := range settings {
		var val string
		err := c.pool.QueryRow(ctx, `SELECT current_setting($1, true)`, name).Scan(&val)
		if err != nil || val == "" {
			continue
		}
		inst.Settings[name] = val
		readable++
		switch name {
		case "rds.extensions":
			inst.CloudHints = appendUnique(inst.CloudHints, "amazon_rds_or_aurora")
		case "cloudsql.enable_google_adaptive_autovacuum":
			inst.CloudHints = appendUnique(inst.CloudHints, "google_cloud_sql")
		}
	}
	inst.Capabilities.CanReadSettings = readable > 0
	if !inst.Capabilities.CanReadSettings {
		lims = append(lims, models.Limitation{
			Area:   "settings",
			Reason: "Unable to read GUC values via current_setting",
			Impact: "Configuration context will be incomplete",
		})
	}

	exts, err := c.collectExtensions(ctx)
	if err != nil {
		if isPermissionError(err) {
			lims = append(lims, models.Limitation{
				Area:   "extensions",
				Reason: "Permission denied reading pg_extension",
				Impact: "Extension detection (pgvector, pg_stat_statements) unavailable",
				Hint:   "Grant SELECT on pg_catalog.pg_extension",
			})
		} else {
			lims = append(lims, models.Limitation{
				Area:   "extensions",
				Reason: err.Error(),
				Impact: "Extension detection unavailable",
			})
		}
	} else {
		inst.Extensions = exts
		for _, e := range exts {
			switch e.Name {
			case "pg_stat_statements":
				inst.Capabilities.HasPgStatStatements = true
			case "vector":
				inst.Capabilities.HasPgVector = true
			case "hypopg":
				inst.Capabilities.HasHypoPG = true
			}
		}
	}

	if inst.Capabilities.HasPgStatStatements {
		var ok bool
		err := c.pool.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM information_schema.views
				WHERE table_schema = 'public' AND table_name = 'pg_stat_statements'
			) OR EXISTS (
				SELECT 1 FROM pg_catalog.pg_proc WHERE proname = 'pg_stat_statements'
			)
		`).Scan(&ok)
		if err == nil {
			// Prefer checking the view is selectable.
			var n int
			viewErr := c.pool.QueryRow(ctx, `SELECT COUNT(*) FROM pg_stat_statements LIMIT 1`).Scan(&n)
			if viewErr == nil {
				inst.Capabilities.HasPgStatStatementsView = true
			} else if isPermissionError(viewErr) {
				lims = append(lims, models.Limitation{
					Area:   "pg_stat_statements",
					Reason: "Extension installed but view not readable",
					Impact: "Query statistics unavailable in this scan",
					Hint:   "Grant pg_read_all_stats or SELECT on pg_stat_statements",
				})
			}
		}
	} else {
		lims = append(lims, models.Limitation{
			Area:   "pg_stat_statements",
			Reason: "Extension not installed",
			Impact: "Query intelligence (pgscope queries top) unavailable",
			Hint:   "Add shared_preload_libraries=pg_stat_statements, restart if required, then CREATE EXTENSION pg_stat_statements; grant pg_read_all_stats.",
		})
	}

	if !inst.Capabilities.HasPgVector {
		lims = append(lims, models.Limitation{
			Area:   "pgvector",
			Reason: "Extension not installed",
			Impact: "Vector diagnostics unavailable (Phase 3)",
			Hint:   "CREATE EXTENSION vector;",
		})
	}

	return inst, lims, nil
}

func (c *Collector) collectExtensions(ctx context.Context) ([]models.Extension, error) {
	rows, err := c.pool.Query(ctx, `
		SELECT e.extname, e.extversion, n.nspname, e.extrelocatable
		FROM pg_catalog.pg_extension e
		JOIN pg_catalog.pg_namespace n ON n.oid = e.extnamespace
		ORDER BY e.extname
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []models.Extension
	for rows.Next() {
		var e models.Extension
		if err := rows.Scan(&e.Name, &e.Version, &e.Schema, &e.Relocatable); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func majorFromVersionNum(versionNum int) int {
	// server_version_num: XXYYZZ → major is XX for PG 10+, historically different.
	if versionNum >= 100000 {
		return versionNum / 10000
	}
	return versionNum / 10000
}

func appendUnique(list []string, v string) []string {
	for _, x := range list {
		if x == v {
			return list
		}
	}
	return append(list, v)
}

// FormatVersionShort returns a compact version label.
func FormatVersionShort(inst models.Instance) string {
	parts := strings.Fields(inst.VersionString)
	if len(parts) >= 2 {
		return parts[0] + " " + parts[1]
	}
	return inst.VersionString
}
