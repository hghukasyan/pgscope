package vector

import (
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/hghukasyan/pgscope/pkg/models"
)

// AnalyzeInventory produces evidence-backed vector index findings.
func AnalyzeInventory(inv models.VectorInventory, database string) []models.Finding {
	if !inv.Collected || !inv.Available {
		return nil
	}
	now := time.Now().UTC()
	var findings []models.Finding
	findings = append(findings, invalidIndexFindings(inv, database, now)...)
	findings = append(findings, missingIndexFindings(inv, database, now)...)
	findings = append(findings, paramFindings(inv, database, now)...)
	findings = append(findings, unusedIndexFindings(inv, database, now)...)
	findings = append(findings, hnswMemoryFitFindings(inv, database, now)...)
	return findings
}

// HNSWMemoryContext compares measured HNSW index sizes to cache-related GUCs.
type HNSWMemoryContext struct {
	SharedBuffersBytes      int64
	SharedBuffersPretty     string
	EffectiveCacheBytes     int64
	EffectiveCachePretty    string
	SharedBuffersKnown      bool
	EffectiveCacheKnown     bool
}

// MemoryContextFromSettings parses shared_buffers / effective_cache_size from settings maps.
func MemoryContextFromSettings(settings map[string]string) HNSWMemoryContext {
	var ctx HNSWMemoryContext
	if settings == nil {
		return ctx
	}
	if v := settings["shared_buffers"]; v != "" {
		ctx.SharedBuffersPretty = v
		if n, err := ParsePostgresSize(v); err == nil {
			ctx.SharedBuffersBytes = n
			ctx.SharedBuffersKnown = true
		}
	}
	if v := settings["effective_cache_size"]; v != "" {
		ctx.EffectiveCachePretty = v
		if n, err := ParsePostgresSize(v); err == nil {
			ctx.EffectiveCacheBytes = n
			ctx.EffectiveCacheKnown = true
		}
	}
	return ctx
}

// ParsePostgresSize parses PostgreSQL pretty sizes (e.g. "128MB", "4GB", "8192").
// Bare integers are treated as bytes.
func ParsePostgresSize(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty size")
	}
	i := 0
	for i < len(s) && (unicode.IsDigit(rune(s[i])) || s[i] == '.') {
		i++
	}
	if i == 0 {
		return 0, fmt.Errorf("invalid size %q", s)
	}
	num, err := strconv.ParseFloat(s[:i], 64)
	if err != nil {
		return 0, fmt.Errorf("invalid size %q: %w", s, err)
	}
	unit := strings.ToLower(strings.TrimSpace(s[i:]))
	unit = strings.ReplaceAll(unit, " ", "")
	var mul float64
	switch unit {
	case "", "b", "byte", "bytes":
		mul = 1
	case "k", "kb", "kib":
		mul = 1024
	case "m", "mb", "mib":
		mul = 1024 * 1024
	case "g", "gb", "gib":
		mul = 1024 * 1024 * 1024
	case "t", "tb", "tib":
		mul = 1024 * 1024 * 1024 * 1024
	default:
		return 0, fmt.Errorf("unknown size unit %q in %q", unit, s)
	}
	return int64(num * mul), nil
}

func hnswMemoryFitFindings(inv models.VectorInventory, database string, now time.Time) []models.Finding {
	mem := MemoryContextFromSettings(inv.Settings)
	if !mem.SharedBuffersKnown && !mem.EffectiveCacheKnown {
		return nil
	}
	var out []models.Finding
	for _, idx := range inv.Indexes {
		if !idx.IsValid || idx.AccessMethod != "hnsw" || idx.SizeBytes <= 0 {
			continue
		}
		exceedsShared := mem.SharedBuffersKnown && idx.SizeBytes > mem.SharedBuffersBytes
		exceedsCache := mem.EffectiveCacheKnown && idx.SizeBytes > mem.EffectiveCacheBytes
		if !exceedsShared && !exceedsCache {
			continue
		}
		sev := models.SeverityInfo
		if exceedsShared {
			sev = models.SeverityWarning
		}
		if exceedsCache {
			sev = models.SeverityWarning
		}
		desc := fmt.Sprintf(
			"HNSW index %s.%s is %s.",
			idx.Schema, idx.Name, prettyOrBytes(idx.SizePretty, idx.SizeBytes),
		)
		if mem.SharedBuffersKnown {
			desc += fmt.Sprintf(" shared_buffers=%s.", mem.SharedBuffersPretty)
		}
		if mem.EffectiveCacheKnown {
			desc += fmt.Sprintf(" effective_cache_size=%s.", mem.EffectiveCachePretty)
		}
		desc += " Vector search latency often jumps sharply once the graph no longer fits in cache and starts reading from disk."

		ev := []models.Evidence{
			{Kind: models.EvidenceFact, Source: "pg_relation_size", Description: "index_bytes", Value: fmt.Sprintf("%d", idx.SizeBytes)},
			{Kind: models.EvidenceFact, Source: "pg_relation_size", Description: "index_pretty", Value: prettyOrBytes(idx.SizePretty, idx.SizeBytes)},
		}
		if mem.SharedBuffersKnown {
			ev = append(ev,
				models.Evidence{Kind: models.EvidenceFact, Source: "pg_settings", Description: "shared_buffers", Value: mem.SharedBuffersPretty},
				models.Evidence{Kind: models.EvidenceFact, Source: "pg_settings", Description: "shared_buffers_bytes", Value: fmt.Sprintf("%d", mem.SharedBuffersBytes)},
				models.Evidence{Kind: models.EvidenceFact, Source: "comparison", Description: "fits_shared_buffers", Value: strconv.FormatBool(!exceedsShared)},
			)
		}
		if mem.EffectiveCacheKnown {
			ev = append(ev,
				models.Evidence{Kind: models.EvidenceFact, Source: "pg_settings", Description: "effective_cache_size", Value: mem.EffectiveCachePretty},
				models.Evidence{Kind: models.EvidenceFact, Source: "pg_settings", Description: "effective_cache_size_bytes", Value: fmt.Sprintf("%d", mem.EffectiveCacheBytes)},
				models.Evidence{Kind: models.EvidenceFact, Source: "comparison", Description: "fits_effective_cache_size", Value: strconv.FormatBool(!exceedsCache)},
			)
		}

		out = append(out, models.Finding{
			RuleID:              "VEC.HNSW_MEMORY_FIT",
			Severity:            sev,
			Database:            database,
			Object:              idx.Schema + "." + idx.Name,
			ObservedAt:          now,
			Title:               "HNSW index may not fit in available cache memory",
			Description:         desc,
			Evidence:            ev,
			ProbableCause:       "HNSW graph larger than PostgreSQL buffer cache and/or the planner's effective cache estimate.",
			ConfidenceRationale: "Compares measured pg_relation_size to shared_buffers/effective_cache_size. Host RAM and OS page cache are not visible from catalogs, so this is a residency risk signal — not proof of disk I/O.",
			Remediation:         "Ensure hot HNSW indexes fit in memory (raise host RAM / shared_buffers carefully, or shrink/split indexes). Validate latency with `pgscope vector recall` under representative concurrency.",
			Risk:                models.RiskMedium,
			Validation:          "Compare approx p95 with a warm cache vs after a cache drop in an isolated lab; watch buffer hit ratios during ANN queries.",
		})
	}
	return out
}

func prettyOrBytes(pretty string, bytes int64) string {
	if pretty != "" {
		return pretty
	}
	return FormatStorageBytes(bytes)
}

func invalidIndexFindings(inv models.VectorInventory, database string, now time.Time) []models.Finding {
	var out []models.Finding
	for _, idx := range inv.Indexes {
		if idx.IsValid {
			continue
		}
		out = append(out, models.Finding{
			RuleID:     "VEC.INDEX_INVALID",
			Severity:   models.SeverityCritical,
			Database:   database,
			Object:     idx.Schema + "." + idx.Name,
			ObservedAt: now,
			Title:      "Invalid vector index",
			Description: fmt.Sprintf(
				"Vector index %s.%s (%s) has indisvalid=false and will not be used by the planner.",
				idx.Schema, idx.Name, idx.AccessMethod,
			),
			Evidence: []models.Evidence{
				{Kind: models.EvidenceFact, Source: "pg_index", Description: "access_method", Value: idx.AccessMethod},
				{Kind: models.EvidenceFact, Source: "pg_index", Description: "indisvalid", Value: "false"},
				{Kind: models.EvidenceFact, Source: "pg_get_indexdef", Description: "definition", Value: idx.Definition},
			},
			ProbableCause:       "CREATE INDEX CONCURRENTLY failed or a prior rebuild left the index invalid.",
			ConfidenceRationale: "indisvalid is a catalog fact.",
			Remediation:         "REINDEX (CONCURRENTLY when possible) or drop/recreate during a maintenance window. PGScope will not execute DDL.",
			Risk:                models.RiskHigh,
			Validation:          "Confirm indisvalid=true with `pgscope vector inspect`.",
		})
	}
	return out
}

func missingIndexFindings(inv models.VectorInventory, database string, now time.Time) []models.Finding {
	var out []models.Finding
	for _, col := range inv.Columns {
		if col.HasVectorIndex {
			continue
		}
		// Small tables: missing ANN index is often fine.
		if col.EstimatedRows > 0 && col.EstimatedRows < 1000 {
			continue
		}
		if col.EstimatedRows == 0 && col.TableBytes < 8*1024*1024 {
			continue
		}
		out = append(out, models.Finding{
			RuleID:     "VEC.MISSING_INDEX",
			Severity:   models.SeverityInfo,
			Database:   database,
			Object:     fmt.Sprintf("%s.%s.%s", col.Schema, col.Table, col.Column),
			ObservedAt: now,
			Title:      "Vector column without ANN index",
			Description: fmt.Sprintf(
				"%s.%s.%s (%s) has no HNSW/IVFFlat index. This is not automatically a problem — exact search may be preferred for small/critical workloads.",
				col.Schema, col.Table, col.Column, col.TypeName,
			),
			Evidence: []models.Evidence{
				{Kind: models.EvidenceFact, Source: "pg_attribute", Description: "type", Value: col.TypeName},
				{Kind: models.EvidenceFact, Source: "pg_class", Description: "estimated_rows", Value: fmt.Sprintf("%d", col.EstimatedRows)},
				{Kind: models.EvidenceFact, Source: "pg_relation_size", Description: "table_bytes", Value: fmt.Sprintf("%d", col.TableBytes)},
			},
			ProbableCause:       "No approximate index created yet, or exact search is intentional.",
			ConfidenceRationale: "Absence of hnsw/ivfflat indexes on the column is a catalog fact; necessity depends on workload latency/recall goals.",
			Remediation:         "If ANN latency is required, create an HNSW or IVFFlat index with the correct operator class for your distance metric in a lab first.",
			Risk:                models.RiskMedium,
			Validation:          "Benchmark exact vs ANN with `pgscope vector recall` before deploying an index.",
		})
	}
	return out
}

func paramFindings(inv models.VectorInventory, database string, now time.Time) []models.Finding {
	var out []models.Finding
	for _, idx := range inv.Indexes {
		if !idx.IsValid {
			continue
		}
		switch idx.AccessMethod {
		case "ivfflat":
			lists := atoiDefault(idx.Options["lists"], 0)
			rows := idx.EstimatedRows
			if lists > 0 && rows > 0 {
				// Common heuristic: lists ≈ rows/1000, but do not declare suboptimal from params alone.
				// Only flag extreme mismatches with clear evidence wording.
				if lists == 1 && rows >= 10000 {
					out = append(out, models.Finding{
						RuleID:     "VEC.IVFFLAT_LISTS",
						Severity:   models.SeverityInfo,
						Database:   database,
						Object:     idx.Schema + "." + idx.Name,
						ObservedAt: now,
						Title:      "IVFFlat lists parameter is very low relative to table size",
						Description: fmt.Sprintf(
							"Index %s has lists=%d with estimated_rows=%d. Low lists can reduce ANN quality; validate with recall benchmarks — do not rebuild from this signal alone.",
							idx.Name, lists, rows,
						),
						Evidence: []models.Evidence{
							{Kind: models.EvidenceFact, Source: "pg_class.reloptions", Description: "lists", Value: strconv.Itoa(lists)},
							{Kind: models.EvidenceFact, Source: "pg_class", Description: "estimated_rows", Value: fmt.Sprintf("%d", rows)},
						},
						ProbableCause:       "Index created with default/low lists for a larger table.",
						ConfidenceRationale: "Parameter vs row estimate only; effectiveness requires measured Recall@K.",
						Remediation:         "Evaluate probes and, in an isolated lab, alternative list counts. Never rebuild production indexes automatically.",
						Risk:                models.RiskHigh,
						Validation:          "Compare Recall@K and p95 under `pgscope vector recall` / `vector tune`.",
					})
				}
			}
		case "hnsw":
			m := atoiDefault(idx.Options["m"], 0)
			efc := atoiDefault(idx.Options["ef_construction"], 0)
			if m > 0 && efc > 0 && efc < m {
				out = append(out, models.Finding{
					RuleID:     "VEC.HNSW_EF_CONSTRUCTION",
					Severity:   models.SeverityInfo,
					Database:   database,
					Object:     idx.Schema + "." + idx.Name,
					ObservedAt: now,
					Title:      "HNSW ef_construction lower than m",
					Description: fmt.Sprintf(
						"Index %s has m=%d and ef_construction=%d. Unusual configurations deserve measured validation, not automatic rebuilds.",
						idx.Name, m, efc,
					),
					Evidence: []models.Evidence{
						{Kind: models.EvidenceFact, Source: "index options", Description: "m", Value: strconv.Itoa(m)},
						{Kind: models.EvidenceFact, Source: "index options", Description: "ef_construction", Value: strconv.Itoa(efc)},
					},
					ProbableCause:       "Non-default build parameters.",
					ConfidenceRationale: "Catalog options only; quality depends on data and search settings.",
					Remediation:         "Benchmark recall/latency before changing build parameters in a lab.",
					Risk:                models.RiskHigh,
					Validation:          "Use `pgscope vector recall` against a representative workload.",
				})
			}
		}
	}
	return out
}

func unusedIndexFindings(inv models.VectorInventory, database string, now time.Time) []models.Finding {
	var out []models.Finding
	for _, idx := range inv.Indexes {
		if idx.IndexScans > 0 || !idx.IsValid {
			continue
		}
		if idx.SizeBytes < 16*1024*1024 {
			continue
		}
		out = append(out, models.Finding{
			RuleID:     "VEC.INDEX_ZERO_SCANS",
			Severity:   models.SeverityInfo,
			Database:   database,
			Object:     idx.Schema + "." + idx.Name,
			ObservedAt: now,
			Title:      "Large vector index with zero scans",
			Description: fmt.Sprintf(
				"%s.%s (%s, %s) has idx_scan=0. This may indicate unused ANN capacity or a short stats window — not automatic drop advice.",
				idx.Schema, idx.Name, idx.AccessMethod, idx.SizePretty,
			),
			Evidence: []models.Evidence{
				{Kind: models.EvidenceFact, Source: "pg_stat_user_indexes", Description: "idx_scan", Value: "0"},
				{Kind: models.EvidenceFact, Source: "pg_relation_size", Description: "size_bytes", Value: fmt.Sprintf("%d", idx.SizeBytes)},
			},
			ProbableCause:       "Index unused by current workload, stats recently reset, or queries use a different metric/expression.",
			ConfidenceRationale: "Zero scans plus size; never sufficient alone to drop.",
			Remediation:         "Confirm metric/operator alignment and workload history before any drop consideration.",
			Risk:                models.RiskHigh,
			Validation:          "Track idx_scan across a full business cycle.",
		})
	}
	return out
}

func atoiDefault(s string, def int) int {
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return n
}
