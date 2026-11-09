package vector

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/hghukasyan/pgscope/pkg/models"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DiscoverOptions filters inventory collection.
type DiscoverOptions struct {
	Schema string
	Table  string
}

// Discover collects pgvector capabilities, vector columns, and ANN indexes.
func Discover(ctx context.Context, pool *pgxpool.Pool, opts DiscoverOptions) (models.VectorInventory, error) {
	inv := models.VectorInventory{
		Settings: map[string]string{},
	}

	caps, lims, err := discoverCapabilities(ctx, pool)
	inv.Limitations = append(inv.Limitations, lims...)
	inv.Capabilities = caps
	if err != nil {
		return inv, err
	}
	if !caps.Installed {
		inv.Collected = true
		inv.Available = false
		inv.Limitations = append(inv.Limitations, models.Limitation{
			Area:   "pgvector",
			Reason: "Extension not installed",
			Impact: "Vector discovery, recall, and tuning unavailable",
			Hint:   "CREATE EXTENSION vector;",
		})
		return inv, nil
	}
	inv.Available = true

	for _, name := range caps.SettingsAvailable {
		var val string
		if err := pool.QueryRow(ctx, `SELECT current_setting($1, true)`, name).Scan(&val); err == nil && val != "" {
			inv.Settings[name] = val
		}
	}
	// Memory GUCs contextualize HNSW residency: latency jumps when the graph spills to disk.
	for _, name := range []string{"shared_buffers", "effective_cache_size"} {
		var val string
		if err := pool.QueryRow(ctx, `SELECT current_setting($1, true)`, name).Scan(&val); err == nil && val != "" {
			inv.Settings[name] = val
		}
	}

	cols, clim, err := discoverColumns(ctx, pool, opts, caps)
	inv.Limitations = append(inv.Limitations, clim...)
	if err != nil {
		return inv, err
	}
	inv.Columns = cols

	idxs, ilim, err := discoverIndexes(ctx, pool, opts, caps)
	inv.Limitations = append(inv.Limitations, ilim...)
	if err != nil {
		return inv, err
	}
	inv.Indexes = idxs

	indexed := map[string]bool{}
	for _, idx := range idxs {
		for _, col := range idx.IndexedColumns {
			indexed[idx.TableSchema+"."+idx.TableName+"."+col] = true
		}
	}
	for i := range inv.Columns {
		key := inv.Columns[i].Schema + "." + inv.Columns[i].Table + "." + inv.Columns[i].Column
		inv.Columns[i].HasVectorIndex = indexed[key]
	}

	inv.Collected = true
	return inv, nil
}

func discoverCapabilities(ctx context.Context, pool *pgxpool.Pool) (models.VectorCapabilities, []models.Limitation, error) {
	var lims []models.Limitation
	caps := models.VectorCapabilities{}

	var name, version, schema string
	err := pool.QueryRow(ctx, `
		SELECT e.extname, e.extversion, n.nspname
		FROM pg_catalog.pg_extension e
		JOIN pg_catalog.pg_namespace n ON n.oid = e.extnamespace
		WHERE e.extname = 'vector'
	`).Scan(&name, &version, &schema)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return caps, lims, nil
		}
		return caps, lims, fmt.Errorf("detect vector extension: %w", err)
	}
	caps.Installed = true
	caps.ExtensionVersion = version
	caps.ExtensionSchema = schema

	typeRows, err := pool.Query(ctx, `
		SELECT n.nspname || '.' || t.typname
		FROM pg_catalog.pg_type t
		JOIN pg_catalog.pg_namespace n ON n.oid = t.typnamespace
		WHERE t.typname IN ('vector', 'halfvec', 'sparsevec')
		ORDER BY t.typname
	`)
	if err == nil {
		defer typeRows.Close()
		for typeRows.Next() {
			var tn string
			if err := typeRows.Scan(&tn); err == nil {
				caps.Types = append(caps.Types, tn)
			}
		}
	}

	amRows, err := pool.Query(ctx, `
		SELECT amname FROM pg_catalog.pg_am
		WHERE amname IN ('hnsw', 'ivfflat')
		ORDER BY amname
	`)
	if err == nil {
		defer amRows.Close()
		for amRows.Next() {
			var am string
			if err := amRows.Scan(&am); err == nil {
				caps.AccessMethods = append(caps.AccessMethods, am)
				switch am {
				case "hnsw":
					caps.HasHNSW = true
				case "ivfflat":
					caps.HasIVFFlat = true
				}
			}
		}
	}

	opcRows, err := pool.Query(ctx, `
		SELECT opc.opcname
		FROM pg_catalog.pg_opclass opc
		JOIN pg_catalog.pg_am am ON am.oid = opc.opcmethod
		WHERE am.amname IN ('hnsw', 'ivfflat')
		ORDER BY opc.opcname
	`)
	if err == nil {
		defer opcRows.Close()
		for opcRows.Next() {
			var opc string
			if err := opcRows.Scan(&opc); err == nil {
				caps.OperatorClasses = append(caps.OperatorClasses, opc)
			}
		}
	}

	opRows, err := pool.Query(ctx, `
		SELECT DISTINCT oprname
		FROM pg_catalog.pg_operator
		WHERE oprname IN ('<->', '<=>', '<#>', '<+>', '<~>', '<%>')
		  AND oprleft::regtype::text IN ('vector', 'halfvec', 'sparsevec', 'bit')
		ORDER BY 1
	`)
	if err == nil {
		defer opRows.Close()
		for opRows.Next() {
			var op string
			if err := opRows.Scan(&op); err == nil {
				caps.DistanceOperators = append(caps.DistanceOperators, op)
			}
		}
	}

	candidateSettings := []string{
		"hnsw.ef_search",
		"hnsw.iterative_scan",
		"hnsw.max_scan_tuples",
		"hnsw.scan_mem_multiplier",
		"ivfflat.probes",
		"ivfflat.iterative_scan",
		"ivfflat.max_probes",
	}
	for _, s := range candidateSettings {
		var exists bool
		// Probe via SET LOCAL in a subtransaction would mutate; use pg_settings + try current_setting.
		err := pool.QueryRow(ctx, `
			SELECT EXISTS (SELECT 1 FROM pg_catalog.pg_settings WHERE name = $1)
		`, s).Scan(&exists)
		if err == nil && exists {
			caps.SettingsAvailable = append(caps.SettingsAvailable, s)
			continue
		}
		// Custom GUCs may not appear in pg_settings until first set; try reading with missing_ok.
		var val *string
		if err := pool.QueryRow(ctx, `SELECT current_setting($1, true)`, s).Scan(&val); err == nil {
			caps.SettingsAvailable = append(caps.SettingsAvailable, s)
		}
	}
	for _, s := range caps.SettingsAvailable {
		if s == "hnsw.iterative_scan" {
			caps.HasIterativeScanHNSW = true
		}
		if s == "ivfflat.iterative_scan" {
			caps.HasIterativeScanIVF = true
		}
	}
	// Iterative scan may still be settable even if not listed; attempt SET probe in savepoint.
	caps.HasIterativeScanHNSW = caps.HasIterativeScanHNSW || settingSettable(ctx, pool, "hnsw.iterative_scan", "off")
	caps.HasIterativeScanIVF = caps.HasIterativeScanIVF || settingSettable(ctx, pool, "ivfflat.iterative_scan", "off")
	if caps.HasIterativeScanHNSW && !contains(caps.SettingsAvailable, "hnsw.iterative_scan") {
		caps.SettingsAvailable = append(caps.SettingsAvailable, "hnsw.iterative_scan")
	}
	if caps.HasIterativeScanIVF && !contains(caps.SettingsAvailable, "ivfflat.iterative_scan") {
		caps.SettingsAvailable = append(caps.SettingsAvailable, "ivfflat.iterative_scan")
	}
	if settingSettable(ctx, pool, "hnsw.ef_search", "40") && !contains(caps.SettingsAvailable, "hnsw.ef_search") {
		caps.SettingsAvailable = append(caps.SettingsAvailable, "hnsw.ef_search")
	}
	if settingSettable(ctx, pool, "ivfflat.probes", "1") && !contains(caps.SettingsAvailable, "ivfflat.probes") {
		caps.SettingsAvailable = append(caps.SettingsAvailable, "ivfflat.probes")
	}

	return caps, lims, nil
}

func settingSettable(ctx context.Context, pool *pgxpool.Pool, name, value string) bool {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return false
	}
	defer func() { _ = tx.Rollback(ctx) }()
	_, err = tx.Exec(ctx, fmt.Sprintf("SET LOCAL %s = %s", name, quoteSettingValue(value)))
	return err == nil
}

func quoteSettingValue(v string) string {
	if _, err := strconv.Atoi(v); err == nil {
		return v
	}
	switch strings.ToLower(v) {
	case "on", "off", "true", "false":
		return v
	default:
		return "'" + strings.ReplaceAll(v, "'", "''") + "'"
	}
}

func discoverColumns(ctx context.Context, pool *pgxpool.Pool, opts DiscoverOptions, caps models.VectorCapabilities) ([]models.VectorColumn, []models.Limitation, error) {
	var lims []models.Limitation
	rows, err := pool.Query(ctx, `
		SELECT
			n.nspname AS schema,
			c.relname AS table,
			a.attname AS column,
			tn.nspname AS type_schema,
			t.typname AS type_name,
			CASE
				WHEN t.typname IN ('vector', 'halfvec') AND a.atttypmod > 0 THEN a.atttypmod
				ELSE NULL
			END AS dimensions,
			NOT a.attnotnull AS nullable,
			COALESCE(c.reltuples::bigint, 0) AS estimated_rows,
			COALESCE(pg_table_size(c.oid), 0) AS table_bytes,
			pg_size_pretty(COALESCE(pg_table_size(c.oid), 0)) AS table_pretty
		FROM pg_catalog.pg_attribute a
		JOIN pg_catalog.pg_class c ON c.oid = a.attrelid
		JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
		JOIN pg_catalog.pg_type t ON t.oid = a.atttypid
		JOIN pg_catalog.pg_namespace tn ON tn.oid = t.typnamespace
		WHERE a.attnum > 0 AND NOT a.attisdropped
		  AND c.relkind IN ('r', 'p', 'm')
		  AND t.typname IN ('vector', 'halfvec', 'sparsevec')
		  AND n.nspname NOT IN ('pg_catalog', 'information_schema')
		  AND ($1 = '' OR n.nspname = $1)
		  AND ($2 = '' OR c.relname = $2)
		ORDER BY n.nspname, c.relname, a.attnum
	`, opts.Schema, opts.Table)
	if err != nil {
		lims = append(lims, models.Limitation{Area: "vector_columns", Reason: err.Error(), Impact: "Vector column inventory unavailable"})
		return nil, lims, nil
	}
	defer rows.Close()

	var out []models.VectorColumn
	for rows.Next() {
		var col models.VectorColumn
		var dims *int
		if err := rows.Scan(
			&col.Schema, &col.Table, &col.Column, &col.TypeSchema, &col.TypeName,
			&dims, &col.Nullable, &col.EstimatedRows, &col.TableBytes, &col.TablePretty,
		); err != nil {
			return out, lims, err
		}
		col.Dimensions = dims
		out = append(out, col)
	}
	_ = caps
	return out, lims, rows.Err()
}

func discoverIndexes(ctx context.Context, pool *pgxpool.Pool, opts DiscoverOptions, caps models.VectorCapabilities) ([]models.VectorIndex, []models.Limitation, error) {
	var lims []models.Limitation
	rows, err := pool.Query(ctx, `
		SELECT
			ni.nspname AS schema,
			i.relname AS name,
			nt.nspname AS table_schema,
			t.relname AS table_name,
			am.amname AS access_method,
			COALESCE(opc.opcname, '') AS operator_class,
			COALESCE(pg_get_expr(ix.indpred, ix.indrelid), '') AS predicate,
			COALESCE(pg_get_indexdef(ix.indexrelid), '') AS definition,
			COALESCE(pg_relation_size(i.oid), 0) AS size_bytes,
			pg_size_pretty(COALESCE(pg_relation_size(i.oid), 0)) AS size_pretty,
			COALESCE(pg_relation_size(t.oid), 0) AS table_bytes,
			ix.indisvalid,
			ix.indisready,
			ix.indisunique,
			COALESCE(s.idx_scan, 0),
			COALESCE(s.idx_tup_read, 0),
			COALESCE(s.idx_tup_fetch, 0),
			COALESCE(t.reltuples::bigint, 0),
			COALESCE(array_to_string(i.reloptions, ','), ''),
			COALESCE( (SELECT array_agg(pg_get_indexdef(ix.indexrelid, k.i, true) ORDER BY k.i)
			           FROM generate_series(1, ix.indnkeyatts) AS k(i)), '{}') AS cols
		FROM pg_catalog.pg_index ix
		JOIN pg_catalog.pg_class i ON i.oid = ix.indexrelid
		JOIN pg_catalog.pg_class t ON t.oid = ix.indrelid
		JOIN pg_catalog.pg_namespace ni ON ni.oid = i.relnamespace
		JOIN pg_catalog.pg_namespace nt ON nt.oid = t.relnamespace
		JOIN pg_catalog.pg_am am ON am.oid = i.relam
		LEFT JOIN pg_catalog.pg_opclass opc ON opc.oid = ix.indclass[0]
		LEFT JOIN pg_catalog.pg_stat_user_indexes s ON s.indexrelid = ix.indexrelid
		WHERE am.amname IN ('hnsw', 'ivfflat')
		  AND ni.nspname NOT IN ('pg_catalog', 'information_schema')
		  AND ($1 = '' OR nt.nspname = $1)
		  AND ($2 = '' OR t.relname = $2)
		ORDER BY ni.nspname, i.relname
	`, opts.Schema, opts.Table)
	if err != nil {
		lims = append(lims, models.Limitation{Area: "vector_indexes", Reason: err.Error(), Impact: "Vector index inventory unavailable"})
		return nil, lims, nil
	}
	defer rows.Close()

	var out []models.VectorIndex
	for rows.Next() {
		var idx models.VectorIndex
		var predicate, relopts string
		var cols []string
		if err := rows.Scan(
			&idx.Schema, &idx.Name, &idx.TableSchema, &idx.TableName, &idx.AccessMethod,
			&idx.OperatorClass, &predicate, &idx.Definition,
			&idx.SizeBytes, &idx.SizePretty, &idx.TableBytes,
			&idx.IsValid, &idx.IsReady, &idx.IsUnique,
			&idx.IndexScans, &idx.TuplesRead, &idx.TuplesFetched, &idx.EstimatedRows,
			&relopts, &cols,
		); err != nil {
			return out, lims, err
		}
		idx.IndexedColumns = cols
		if len(cols) == 1 && strings.Contains(cols[0], "(") {
			idx.IndexedExpr = cols[0]
		}
		idx.Options = parseRelOptions(relopts)
		idx.Options = mergeOptionsFromDef(idx.Options, idx.Definition)
		idx.DistanceMetric = metricFromOpClass(idx.OperatorClass)
		idx.DistanceOperator = operatorFromMetric(idx.DistanceMetric)
		if dims := dimsFromDefinition(idx.Definition); dims != nil {
			idx.Dimensions = dims
		}
		_ = predicate
		_ = caps
		out = append(out, idx)
	}
	return out, lims, rows.Err()
}

func parseRelOptions(s string) map[string]string {
	out := map[string]string{}
	if s == "" {
		return out
	}
	for _, part := range strings.Split(s, ",") {
		kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(kv) == 2 {
			out[strings.TrimSpace(kv[0])] = strings.Trim(strings.TrimSpace(kv[1]), "'\"")
		}
	}
	return out
}

func mergeOptionsFromDef(opts map[string]string, def string) map[string]string {
	if opts == nil {
		opts = map[string]string{}
	}
	// WITH (m='16', ef_construction='64') or WITH (lists='20')
	idx := strings.Index(strings.ToUpper(def), "WITH (")
	if idx < 0 {
		return opts
	}
	rest := def[idx+6:]
	end := strings.Index(rest, ")")
	if end < 0 {
		return opts
	}
	inner := rest[:end]
	for _, part := range strings.Split(inner, ",") {
		kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(kv) == 2 {
			key := strings.TrimSpace(kv[0])
			val := strings.Trim(strings.TrimSpace(kv[1]), "'\"")
			if _, ok := opts[key]; !ok {
				opts[key] = val
			}
		}
	}
	return opts
}

func dimsFromDefinition(def string) *int {
	// Look for vector(N) in definition.
	lower := strings.ToLower(def)
	for _, typ := range []string{"vector(", "halfvec("} {
		i := strings.Index(lower, typ)
		if i < 0 {
			continue
		}
		rest := lower[i+len(typ):]
		j := strings.Index(rest, ")")
		if j < 0 {
			continue
		}
		n, err := strconv.Atoi(strings.TrimSpace(rest[:j]))
		if err == nil && n > 0 {
			return &n
		}
	}
	return nil
}

func metricFromOpClass(opc string) string {
	o := strings.ToLower(opc)
	switch {
	case strings.Contains(o, "cosine"):
		return "cosine"
	case strings.Contains(o, "ip_ops") || strings.HasSuffix(o, "_ip_ops"):
		return "ip"
	case strings.Contains(o, "l1"):
		return "l1"
	case strings.Contains(o, "hamming"):
		return "hamming"
	case strings.Contains(o, "jaccard"):
		return "jaccard"
	case strings.Contains(o, "l2") || o == "":
		return "l2"
	default:
		return "unknown"
	}
}

func operatorFromMetric(metric string) string {
	switch metric {
	case "cosine":
		return "<=>"
	case "ip":
		return "<#>"
	case "l1":
		return "<+>"
	case "hamming":
		return "<~>"
	case "jaccard":
		return "<%>"
	default:
		return "<->"
	}
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}
