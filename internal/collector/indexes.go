package collector

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/hghukasyan/pgscope/pkg/models"
)

// CollectIndexes gathers index inventory; tableFilter is schema.table or empty.
func (c *Collector) CollectIndexes(ctx context.Context, tableFilter string) (models.IndexStats, []models.Limitation) {
	return c.collectIndexesFiltered(ctx, tableFilter)
}

func (c *Collector) collectIndexes(ctx context.Context) (models.IndexStats, []models.Limitation) {
	return c.collectIndexesFiltered(ctx, "")
}

func (c *Collector) collectIndexesFiltered(ctx context.Context, tableFilter string) (models.IndexStats, []models.Limitation) {
	var lims []models.Limitation
	stats := models.IndexStats{DeepScan: c.cfg.DeepScan}

	if !c.cfg.EnableIndexes {
		lims = append(lims, models.Limitation{
			Area: "indexes", Reason: "Index collector disabled by configuration",
			Impact: "Index intelligence skipped",
		})
		return stats, lims
	}

	limit := c.cfg.TopIndexes
	if limit <= 0 {
		limit = 15
	}
	fetchLimit := limit * 5
	if c.cfg.DeepScan || tableFilter != "" {
		fetchLimit = 5000
	}

	rows, err := c.pool.Query(ctx, `
		SELECT
			ni.nspname AS schema,
			i.relname AS name,
			nt.nspname AS table_schema,
			t.relname AS table_name,
			COALESCE(pg_relation_size(i.oid), 0) AS size_bytes,
			pg_size_pretty(COALESCE(pg_relation_size(i.oid), 0)) AS size_pretty,
			COALESCE(pg_relation_size(t.oid), 0) AS table_bytes,
			COALESCE(am.amname, '') AS access_method,
			ix.indisunique,
			ix.indisprimary,
			ix.indisexclusion,
			ix.indisvalid,
			ix.indisready,
			ix.indislive,
			(ix.indpred IS NOT NULL) AS is_partial,
			COALESCE(pg_get_expr(ix.indpred, ix.indrelid), '') AS predicate,
			COALESCE(pg_get_indexdef(ix.indexrelid), '') AS definition,
			COALESCE(s.idx_scan, 0) AS index_scans,
			COALESCE(s.idx_tup_read, 0) AS tuples_read,
			COALESCE(s.idx_tup_fetch, 0) AS tuples_fetched,
			COALESCE(con.conname, '') AS constraint_name,
			ix.indnkeyatts,
			ix.indnatts,
			COALESCE( (SELECT array_agg(x::smallint ORDER BY ord)
			           FROM unnest(string_to_array(btrim(ix.indoption::text), ' ')::smallint[]) WITH ORDINALITY AS u(x, ord)), '{}'::smallint[]) AS indoption,
			COALESCE( (SELECT array_agg(pg_get_indexdef(ix.indexrelid, k.i, true) ORDER BY k.i)
			           FROM generate_series(1, ix.indnatts) AS k(i)), '{}') AS coldefs,
			COALESCE( (SELECT array_agg(COALESCE(opc.opcname, '') ORDER BY k.i)
			           FROM generate_series(1, ix.indnkeyatts) AS k(i)
			           LEFT JOIN pg_opclass opc ON opc.oid = ix.indclass[k.i]), '{}') AS opclasses,
			COALESCE( (SELECT array_agg(COALESCE(coll.collname, '') ORDER BY k.i)
			           FROM generate_series(1, ix.indnkeyatts) AS k(i)
			           LEFT JOIN pg_collation coll ON coll.oid = ix.indcollation[k.i]), '{}') AS collations
		FROM pg_catalog.pg_index ix
		JOIN pg_catalog.pg_class i ON i.oid = ix.indexrelid
		JOIN pg_catalog.pg_class t ON t.oid = ix.indrelid
		JOIN pg_catalog.pg_namespace ni ON ni.oid = i.relnamespace
		JOIN pg_catalog.pg_namespace nt ON nt.oid = t.relnamespace
		LEFT JOIN pg_catalog.pg_am am ON am.oid = i.relam
		LEFT JOIN pg_catalog.pg_stat_user_indexes s ON s.indexrelid = ix.indexrelid
		LEFT JOIN pg_catalog.pg_constraint con ON con.conindid = ix.indexrelid AND con.contype IN ('p','u','x')
		WHERE ni.nspname NOT IN ('pg_catalog', 'information_schema', 'pg_toast')
		  AND ni.nspname NOT LIKE 'pg_temp_%'
		  AND (
		    $1 = ''
		    OR (nt.nspname || '.' || t.relname) = $1
		    OR t.relname = $1
		  )
		ORDER BY size_bytes DESC
		LIMIT $2
	`, tableFilter, fetchLimit)
	if err != nil {
		reason := err.Error()
		hint := ""
		if isPermissionError(err) {
			reason = "Permission denied reading index metadata"
			hint = "Grant pg_monitor and SELECT on relevant catalogs"
		}
		lims = append(lims, models.Limitation{
			Area: "indexes", Reason: reason, Impact: "Index inventory unavailable", Hint: hint,
		})
		return stats, lims
	}
	defer rows.Close()

	var all []models.IndexInfo
	for rows.Next() {
		var idx models.IndexInfo
		var nKeyAtts, nAtts int
		var indoption []int16
		var coldefs, opclasses, collations []string
		if err := rows.Scan(
			&idx.Schema, &idx.Name, &idx.TableSchema, &idx.TableName,
			&idx.SizeBytes, &idx.SizePretty, &idx.TableBytes, &idx.AccessMethod,
			&idx.IsUnique, &idx.IsPrimary, &idx.IsExclusion, &idx.IsValid, &idx.IsReady, &idx.IsLive,
			&idx.IsPartial, &idx.Predicate, &idx.Definition,
			&idx.IndexScans, &idx.TuplesRead, &idx.TuplesFetched, &idx.ConstraintName,
			&nKeyAtts, &nAtts, &indoption, &coldefs, &opclasses, &collations,
		); err != nil {
			lims = append(lims, models.Limitation{Area: "indexes", Reason: err.Error(), Impact: "Partial index inventory"})
			return stats, lims
		}
		idx.IndOption = indoption
		idx.OpClasses = opclasses
		idx.Collations = collations
		for i, def := range coldefs {
			if i < nKeyAtts {
				if strings.Contains(def, "(") && !isSimpleIdent(def) {
					idx.IndexExprs = append(idx.IndexExprs, def)
				} else {
					idx.IndexColumns = append(idx.IndexColumns, def)
				}
			} else {
				idx.IncludeColumns = append(idx.IncludeColumns, def)
			}
		}
		_ = nAtts
		idx.StructuralKey = structuralKey(idx)
		all = append(all, idx)
	}
	if err := rows.Err(); err != nil {
		lims = append(lims, models.Limitation{Area: "indexes", Reason: err.Error(), Impact: "Partial index inventory"})
		return stats, lims
	}

	stats.Indexes = all
	for _, idx := range all {
		if !idx.IsValid {
			stats.Invalid = append(stats.Invalid, idx)
		}
		if idx.IndexScans == 0 && !idx.IsPrimary && !idx.IsUnique && !idx.IsExclusion && idx.IsValid && idx.ConstraintName == "" {
			stats.NeverUsed = append(stats.NeverUsed, idx)
		}
	}
	if len(all) > limit && tableFilter == "" && !c.cfg.DeepScan {
		stats.Largest = append([]models.IndexInfo(nil), all[:limit]...)
	} else {
		stats.Largest = append([]models.IndexInfo(nil), all...)
	}
	if len(stats.NeverUsed) > 50 {
		stats.NeverUsed = stats.NeverUsed[:50]
	}

	stats.Duplicates = FindDuplicateIndexes(all)
	stats.Redundant = FindRedundantBTreeIndexes(all)

	var reset *time.Time
	err = c.pool.QueryRow(ctx, `
		SELECT stats_reset FROM pg_stat_database WHERE datname = current_database()
	`).Scan(&reset)
	if err == nil {
		stats.StatsReset = reset
	}

	stats.Collected = true
	return stats, lims
}

func isSimpleIdent(s string) bool {
	s = strings.Trim(s, `"`)
	for _, r := range s {
		if !(r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')) {
			return false
		}
	}
	return s != ""
}

func structuralKey(idx models.IndexInfo) string {
	return strings.Join([]string{
		idx.TableSchema, idx.TableName, idx.AccessMethod,
		strings.Join(idx.IndexColumns, ","),
		strings.Join(idx.IndexExprs, ","),
		strings.Join(idx.IncludeColumns, ","),
		idx.Predicate,
		strings.Join(idx.OpClasses, ","),
		fmt.Sprintf("%v", idx.IndOption),
		strings.Join(idx.Collations, ","),
		fmt.Sprintf("u=%t", idx.IsUnique),
	}, "|")
}

// FindDuplicateIndexes groups indexes with identical structural keys.
func FindDuplicateIndexes(indexes []models.IndexInfo) []models.DuplicateIndexGroup {
	byKey := map[string][]models.IndexInfo{}
	for _, idx := range indexes {
		if idx.StructuralKey == "" {
			continue
		}
		byKey[idx.StructuralKey] = append(byKey[idx.StructuralKey], idx)
	}
	var out []models.DuplicateIndexGroup
	for _, group := range byKey {
		if len(group) < 2 {
			continue
		}
		out = append(out, models.DuplicateIndexGroup{
			TableSchema: group[0].TableSchema,
			TableName:   group[0].TableName,
			Indexes:     group,
			Reason:      "Identical access method, columns/expressions, includes, predicate, opclasses, collations, and uniqueness",
		})
	}
	// Stable-ish order by table name.
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			ai := out[i].TableSchema + "." + out[i].TableName
			aj := out[j].TableSchema + "." + out[j].TableName
			if aj < ai {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

// FindRedundantBTreeIndexes finds B-tree indexes whose leading columns are a
// prefix of another B-tree index on the same table, carefully excluding
// uniqueness/constraint/partial/expression mismatches.
func FindRedundantBTreeIndexes(indexes []models.IndexInfo) []models.DuplicateIndexGroup {
	byTable := map[string][]models.IndexInfo{}
	for _, idx := range indexes {
		if !strings.EqualFold(idx.AccessMethod, "btree") {
			continue
		}
		if len(idx.IndexExprs) > 0 {
			continue // expression indexes need manual review
		}
		if !idx.IsValid {
			continue
		}
		key := idx.TableSchema + "." + idx.TableName
		byTable[key] = append(byTable[key], idx)
	}

	var out []models.DuplicateIndexGroup
	for _, group := range byTable {
		for i, a := range group {
			for j, b := range group {
				if i == j {
					continue
				}
				if isBTreePrefixRedundant(a, b) {
					out = append(out, models.DuplicateIndexGroup{
						TableSchema: a.TableSchema,
						TableName:   a.TableName,
						Indexes:     []models.IndexInfo{a, b},
						Reason: fmt.Sprintf(
							"%s.%s columns %v may be redundant with %s.%s columns %v (same predicate/uniqueness constraints considered)",
							a.Schema, a.Name, a.IndexColumns, b.Schema, b.Name, b.IndexColumns,
						),
					})
				}
			}
		}
	}
	return out
}

func isBTreePrefixRedundant(shorter, longer models.IndexInfo) bool {
	if len(shorter.IndexColumns) == 0 || len(shorter.IndexColumns) >= len(longer.IndexColumns) {
		return false
	}
	// Partial predicates must match exactly (including both empty).
	if shorter.Predicate != longer.Predicate {
		return false
	}
	// Unique/PK shorter index is NOT redundant with a non-unique longer index.
	if (shorter.IsUnique || shorter.IsPrimary) && !(longer.IsUnique || longer.IsPrimary) {
		return false
	}
	// Constraint-backed indexes are never flagged as drop-redundant here.
	if shorter.ConstraintName != "" || shorter.IsPrimary || shorter.IsExclusion {
		return false
	}
	for i := range shorter.IndexColumns {
		if !strings.EqualFold(shorter.IndexColumns[i], longer.IndexColumns[i]) {
			return false
		}
		if i < len(shorter.OpClasses) && i < len(longer.OpClasses) {
			if shorter.OpClasses[i] != longer.OpClasses[i] {
				return false
			}
		}
		if i < len(shorter.IndOption) && i < len(longer.IndOption) {
			if shorter.IndOption[i] != longer.IndOption[i] {
				return false
			}
		}
		if i < len(shorter.Collations) && i < len(longer.Collations) {
			if shorter.Collations[i] != longer.Collations[i] {
				return false
			}
		}
	}
	return true
}

// RecommendIndexesFromPlans builds conservative index candidates from EXPLAIN findings.
func RecommendIndexesFromPlans(findings []models.Finding, existing []models.IndexInfo) []models.IndexCandidate {
	var out []models.IndexCandidate
	existingKeys := map[string]bool{}
	for _, idx := range existing {
		key := strings.ToLower(idx.TableSchema + "." + idx.TableName + "|" + strings.Join(idx.IndexColumns, ","))
		existingKeys[key] = true
	}
	for _, f := range findings {
		if f.RuleID != "EXPLAIN.INDEX_CANDIDATE" && f.RuleID != "EXPLAIN.LARGE_SEQ_SCAN" {
			continue
		}
		var rel, colsCSV string
		for _, e := range f.Evidence {
			switch e.Description {
			case "relation":
				rel = e.Value
			case "candidate_columns":
				colsCSV = e.Value
			}
		}
		if rel == "" || colsCSV == "" {
			continue
		}
		schema, table := "public", rel
		if parts := strings.SplitN(rel, ".", 2); len(parts) == 2 {
			schema, table = parts[0], parts[1]
		}
		cols := strings.Split(colsCSV, ",")
		for i := range cols {
			cols[i] = strings.TrimSpace(cols[i])
		}
		key := strings.ToLower(schema + "." + table + "|" + strings.Join(cols, ","))
		if existingKeys[key] {
			continue
		}
		create := fmt.Sprintf("CREATE INDEX CONCURRENTLY ON %s.%s (%s);", schema, table, strings.Join(cols, ", "))
		out = append(out, models.IndexCandidate{
			Schema:      schema,
			Table:       table,
			Columns:     cols,
			CreateSQL:   create,
			Reason:      f.Description,
			WriteOverhead: "Additional index maintenance on INSERT/UPDATE/DELETE; measure write impact before deploying.",
			StorageNotes:  "Index storage grows with table cardinality and column width; estimate with pg_relation_size after creation in a lab.",
			Confidence:    f.ConfidenceRationale,
			Validation:    "Use EXPLAIN (ANALYZE) on representative queries, or HypoPG if explicitly enabled. Do not treat planner cost alone as runtime proof.",
			AffectedQueries: []string{f.Object},
		})
	}
	return out
}
