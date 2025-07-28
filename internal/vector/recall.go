package vector

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/hghukasyan/pgscope/internal/explain"
	"github.com/hghukasyan/pgscope/pkg/models"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// RecallOptions controls opt-in recall benchmarking.
type RecallOptions struct {
	Authorize bool
	SearchSettings map[string]string // session settings for approx path
	OmitSampleDetails bool
}

// RunRecall executes exact vs approximate ANN comparison under strict safety limits.
func RunRecall(ctx context.Context, pool *pgxpool.Pool, w Workload, opts RecallOptions) (models.RecallReport, error) {
	start := time.Now()
	report := models.RecallReport{
		CollectedAt:         start.UTC(),
		Target:              w.Target.Schema + "." + w.Target.Table + "." + w.Target.VectorColumn,
		Metric:              w.Search.Metric,
		WorkloadFingerprint: w.Fingerprint(),
		Environment:         w.Benchmark.Environment,
		ConfigFingerprint:   w.Fingerprint(),
		SampleSize:          w.Benchmark.Samples,
		SearchSettings:      opts.SearchSettings,
	}
	if !opts.Authorize {
		return report, fmt.Errorf("recall benchmark requires explicit --authorize-benchmark (isolated/lab only)")
	}
	if err := w.Validate(); err != nil {
		return report, err
	}

	ctx, cancel := context.WithTimeout(ctx, w.Benchmark.MaxDuration)
	defer cancel()

	var dbname, version, vecVersion string
	_ = pool.QueryRow(ctx, `SELECT current_database(), version()`).Scan(&dbname, &version)
	_ = pool.QueryRow(ctx, `
		SELECT COALESCE((SELECT extversion FROM pg_extension WHERE extname='vector'), '')
	`).Scan(&vecVersion)
	report.Database = dbname
	report.PGVersion = version
	report.PgvectorVersion = vecVersion

	qualTable, err := QuoteQualified(w.Target.Schema, w.Target.Table)
	if err != nil {
		return report, err
	}
	qualID, err := QuoteIdent(w.Target.IDColumn)
	if err != nil {
		return report, err
	}
	qualVec, err := QuoteIdent(w.Target.VectorColumn)
	if err != nil {
		return report, err
	}

	inv, err := Discover(ctx, pool, DiscoverOptions{Schema: w.Target.Schema, Table: w.Target.Table})
	if err != nil {
		return report, err
	}
	if !inv.Available {
		return report, fmt.Errorf("pgvector not available")
	}
	for _, idx := range inv.Indexes {
		if w.Target.IndexName != "" && idx.Name != w.Target.IndexName {
			continue
		}
		if idx.TableSchema == w.Target.Schema && idx.TableName == w.Target.Table {
			report.IndexName = idx.Schema + "." + idx.Name
			report.AccessMethod = idx.AccessMethod
			if w.Target.IndexName == "" {
				break
			}
		}
	}

	queries, err := loadQueryVectors(ctx, pool, w, qualTable, qualVec)
	if err != nil {
		return report, err
	}
	if len(queries) == 0 {
		return report, fmt.Errorf("no query vectors available")
	}
	if len(queries) > w.Benchmark.Samples {
		queries = queries[:w.Benchmark.Samples]
	}

	op := w.Operator()
	maxK := 0
	for _, k := range w.Search.K {
		if k > maxK {
			maxK = k
		}
	}

	// Warmup (approx path only).
	for i := 0; i < w.Benchmark.Warmup && i < len(queries); i++ {
		_, _, _ = runKNN(ctx, pool, knnRequest{
			Table: qualTable, ID: qualID, Vec: qualVec, Operator: op,
			Query: queries[i], K: maxK, Exact: false, Timeout: w.Benchmark.Timeout,
			Filter: w.Search.Filter, Settings: opts.SearchSettings,
		})
	}

	var samples []models.RecallSample
	baselineOK := true
	for qi, qvec := range queries {
		select {
		case <-ctx.Done():
			report.Limitations = append(report.Limitations, models.Limitation{
				Area: "recall", Reason: "benchmark cancelled or max_duration reached",
				Impact: fmt.Sprintf("completed %d/%d queries", len(samples), len(queries)),
			})
			goto summarize
		default:
		}

		for _, k := range w.Search.K {
			exact, exactMeta, errExact := runKNN(ctx, pool, knnRequest{
				Table: qualTable, ID: qualID, Vec: qualVec, Operator: op,
				Query: qvec, K: k, Exact: true, Timeout: w.Benchmark.Timeout,
				Filter: w.Search.Filter,
			})
			approx, approxMeta, errApprox := runKNN(ctx, pool, knnRequest{
				Table: qualTable, ID: qualID, Vec: qualVec, Operator: op,
				Query: qvec, K: k, Exact: false, Timeout: w.Benchmark.Timeout,
				Filter: w.Search.Filter, Settings: opts.SearchSettings,
			})

			sample := models.RecallSample{
				QueryIndex:      qi,
				K:               k,
				ExactLatencyMS:  exactMeta.LatencyMS,
				ApproxLatencyMS: approxMeta.LatencyMS,
				ExactUsedIndex:  exactMeta.UsedIndex,
				ApproxUsedIndex: approxMeta.UsedIndex,
				ExactPlanNode:   exactMeta.TopNode,
				ApproxPlanNode:  approxMeta.TopNode,
			}
			if w.Search.Filter != nil && w.Search.Filter.Operator == "=" && len(w.Search.Filter.Values) > 0 {
				sample.FilterLabel = w.Search.Filter.Values[0]
			}
			if errExact != nil {
				sample.Error = "exact: " + errExact.Error()
				baselineOK = false
			} else if exactMeta.UsedIndex {
				sample.Error = "exact baseline used an index scan; refusing unverifiable baseline"
				baselineOK = false
				report.Limitations = append(report.Limitations, models.Limitation{
					Area: "exact_baseline", Reason: sample.Error,
					Impact: "Recall@K not trusted for this sample",
					Hint:   "Ensure enable_indexscan/bitmapscan off forces a seq scan for the ORDER BY distance query",
				})
			} else {
				sample.ExactIDs = exact
				sample.ExactCount = len(exact)
			}
			if errApprox != nil {
				if sample.Error != "" {
					sample.Error += "; "
				}
				sample.Error += "approx: " + errApprox.Error()
			} else {
				sample.ApproxIDs = approx
				sample.ApproxCount = len(approx)
			}
			if sample.Error == "" {
				inter, rec := RecallAtK(sample.ExactIDs, sample.ApproxIDs)
				sample.Intersection = inter
				sample.Recall = rec
				if sample.ExactCount < k || sample.ApproxCount < k {
					sample.IncompleteResult = true
				}
			}
			if opts.OmitSampleDetails || !w.Benchmark.IncludeSamples {
				sample.ExactIDs = nil
				sample.ApproxIDs = nil
			}
			samples = append(samples, sample)
		}
	}

summarize:
	report.Samples = samples
	report.ExactBaselineVerified = baselineOK
	for _, k := range w.Search.K {
		report.Summaries = append(report.Summaries, SummarizeRecall(samples, k))
	}
	report.FilterSummaries = summarizeByFilter(samples, w.Search.K)
	report.Findings = recallFindings(report, w)
	report.DurationMS = time.Since(start).Milliseconds()
	return report, nil
}

type knnRequest struct {
	Table, ID, Vec, Operator string
	Query                    []float64
	K                        int
	Exact                    bool
	Timeout                  time.Duration
	Filter                   *WorkloadFilter
	Settings                 map[string]string
}

type knnMeta struct {
	LatencyMS float64
	UsedIndex bool
	TopNode   string
}

func runKNN(ctx context.Context, pool *pgxpool.Pool, req knnRequest) ([]string, knnMeta, error) {
	var meta knnMeta
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, meta, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	_, _ = tx.Exec(ctx, fmt.Sprintf("SET LOCAL statement_timeout = %d", req.Timeout.Milliseconds()))
	if req.Exact {
		_, _ = tx.Exec(ctx, `SET LOCAL enable_indexscan = off`)
		_, _ = tx.Exec(ctx, `SET LOCAL enable_bitmapscan = off`)
		_, _ = tx.Exec(ctx, `SET LOCAL enable_indexonlyscan = off`)
	} else {
		for k, v := range req.Settings {
			if !allowedSearchSetting(k) {
				continue
			}
			if _, err := tx.Exec(ctx, fmt.Sprintf("SET LOCAL %s = %s", k, quoteSettingValue(v))); err != nil {
				return nil, meta, fmt.Errorf("set %s: %w", k, err)
			}
		}
	}

	filterSQL, filterArgs, err := buildFilterClause(req.Filter, 2) // $1 is vector
	if err != nil {
		return nil, meta, err
	}
	vecLiteral := vectorLiteral(req.Query)
	sql := fmt.Sprintf(
		`SELECT %s::text FROM %s WHERE true %s ORDER BY %s %s $1::vector LIMIT %d`,
		req.ID, req.Table, filterSQL, req.Vec, req.Operator, req.K,
	)
	args := []any{vecLiteral}
	args = append(args, filterArgs...)

	// Verify plan for exact path.
	explainSQL := "EXPLAIN (FORMAT JSON) " + sql
	var planJSON string
	if err := tx.QueryRow(ctx, explainSQL, args...).Scan(&planJSON); err != nil {
		return nil, meta, fmt.Errorf("explain: %w", err)
	}
	parsed, err := explain.ParseJSON([]byte(planJSON))
	if err != nil {
		return nil, meta, fmt.Errorf("parse plan: %w", err)
	}
	meta.TopNode = parsed.Root.NodeType
	meta.UsedIndex = planUsesVectorIndex(parsed.Root)

	start := time.Now()
	rows, err := tx.Query(ctx, sql, args...)
	if err != nil {
		return nil, meta, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, meta, err
		}
		ids = append(ids, id)
	}
	meta.LatencyMS = float64(time.Since(start).Microseconds()) / 1000.0
	return ids, meta, rows.Err()
}

func planUsesVectorIndex(n models.PlanNode) bool {
	am := strings.ToLower(n.NodeType)
	if strings.Contains(am, "index") && (strings.Contains(strings.ToLower(n.IndexName), "hnsw") ||
		strings.Contains(strings.ToLower(n.IndexName), "ivf") ||
		strings.Contains(am, "hnsw") || strings.Contains(am, "ivfflat")) {
		return true
	}
	// Any Index Scan / Bitmap while indexscan disabled is still "used index".
	if strings.Contains(am, "index") {
		return true
	}
	for _, c := range n.Plans {
		if planUsesVectorIndex(c) {
			return true
		}
	}
	return false
}

func buildFilterClause(f *WorkloadFilter, startArg int) (string, []any, error) {
	if f == nil {
		return "", nil, nil
	}
	col, err := QuoteIdent(f.Column)
	if err != nil {
		return "", nil, err
	}
	switch f.Operator {
	case "=":
		if len(f.Values) == 0 {
			return "", nil, fmt.Errorf("filter values empty")
		}
		// Round-robin single value applied by caller via Values[0] typically;
		// for recall we bind the first value unless FilterLabel selects another.
		return fmt.Sprintf(" AND %s = $%d", col, startArg), []any{f.Values[0]}, nil
	case "IN":
		parts := make([]string, len(f.Values))
		args := make([]any, len(f.Values))
		for i, v := range f.Values {
			parts[i] = fmt.Sprintf("$%d", startArg+i)
			args[i] = v
		}
		return fmt.Sprintf(" AND %s IN (%s)", col, strings.Join(parts, ",")), args, nil
	default:
		return "", nil, fmt.Errorf("unsupported filter operator")
	}
}

func vectorLiteral(v []float64) string {
	parts := make([]string, len(v))
	for i, x := range v {
		parts[i] = fmt.Sprintf("%g", x)
	}
	return "[" + strings.Join(parts, ",") + "]"
}

func allowedSearchSetting(name string) bool {
	switch name {
	case "hnsw.ef_search", "hnsw.iterative_scan", "hnsw.max_scan_tuples", "hnsw.scan_mem_multiplier",
		"ivfflat.probes", "ivfflat.iterative_scan", "ivfflat.max_probes":
		return true
	default:
		return false
	}
}

func loadQueryVectors(ctx context.Context, pool *pgxpool.Pool, w Workload, qualTable, qualVec string) ([][]float64, error) {
	if len(w.Queries.InlineVectors) > 0 {
		out := w.Queries.InlineVectors
		if len(out) > w.Benchmark.Samples {
			out = out[:w.Benchmark.Samples]
		}
		return out, nil
	}
	if w.Queries.FixtureFile != "" {
		return loadFixtureVectors(w.Queries.FixtureFile, w.Benchmark.Samples)
	}
	// Sample vectors from table; values stay in memory for queries only — never reported.
	sql := fmt.Sprintf(`SELECT %s::text FROM %s TABLESAMPLE SYSTEM (50) LIMIT $1`, qualVec, qualTable)
	rows, err := pool.Query(ctx, sql, w.Benchmark.Samples)
	if err != nil {
		// TABLESAMPLE may fail on empty; fallback ORDER BY random()
		sql = fmt.Sprintf(`SELECT %s::text FROM %s ORDER BY random() LIMIT $1`, qualVec, qualTable)
		rows, err = pool.Query(ctx, sql, w.Benchmark.Samples)
		if err != nil {
			return nil, err
		}
	}
	defer rows.Close()
	var out [][]float64
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		v, err := parseVectorText(s)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func parseVectorText(s string) ([]float64, error) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "[")
	s = strings.TrimSuffix(s, "]")
	if s == "" {
		return nil, fmt.Errorf("empty vector")
	}
	parts := strings.Split(s, ",")
	out := make([]float64, 0, len(parts))
	for _, p := range parts {
		var f float64
		if _, err := fmt.Sscanf(strings.TrimSpace(p), "%f", &f); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, nil
}

func loadFixtureVectors(path string, limit int) ([][]float64, error) {
	w, err := LoadWorkload(path)
	if err == nil && len(w.Queries.InlineVectors) > 0 {
		out := w.Queries.InlineVectors
		if len(out) > limit {
			out = out[:limit]
		}
		return out, nil
	}
	// Simple YAML list file: vectors: [[...],[...]]
	type fixture struct {
		Vectors [][]float64 `yaml:"vectors"`
	}
	data, err2 := readFile(path)
	if err2 != nil {
		if err != nil {
			return nil, err
		}
		return nil, err2
	}
	var f fixture
	if err := unmarshalYAML(data, &f); err != nil {
		return nil, err
	}
	out := f.Vectors
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func summarizeByFilter(samples []models.RecallSample, ks []int) []models.FilterRecallGroup {
	type key struct {
		label string
		k     int
	}
	groups := map[key]*models.FilterRecallGroup{}
	mins := map[key]float64{}
	for _, s := range samples {
		if s.FilterLabel == "" || s.Error != "" {
			continue
		}
		k := key{s.FilterLabel, s.K}
		g, ok := groups[k]
		if !ok {
			g = &models.FilterRecallGroup{Label: s.FilterLabel, K: s.K}
			groups[k] = g
			mins[k] = 1
		}
		g.Samples++
		g.MeanRecall += s.Recall
		if s.Recall < mins[k] {
			mins[k] = s.Recall
		}
		if s.IncompleteResult {
			g.Incomplete++
		}
	}
	var out []models.FilterRecallGroup
	for k, g := range groups {
		if g.Samples > 0 {
			g.MeanRecall /= float64(g.Samples)
		}
		g.MinRecall = mins[k]
		out = append(out, *g)
	}
	_ = ks
	return out
}

func recallFindings(report models.RecallReport, w Workload) []models.Finding {
	var out []models.Finding
	now := report.CollectedAt
	for _, sum := range report.Summaries {
		if sum.Samples == 0 {
			continue
		}
		if w.Objectives.MinRecallAt10 != nil && sum.K == 10 && sum.MeanRecall < *w.Objectives.MinRecallAt10 {
			out = append(out, models.Finding{
				RuleID:     "VEC.RECALL_BELOW_TARGET",
				Severity:   models.SeverityWarning,
				Database:   report.Database,
				Object:     report.Target,
				ObservedAt: now,
				Title:      "Recall@10 below configured objective",
				Description: fmt.Sprintf(
					"Mean Recall@10=%.3f over %d samples is below objective %.3f.",
					sum.MeanRecall, sum.Samples, *w.Objectives.MinRecallAt10,
				),
				Evidence: []models.Evidence{
					{Kind: models.EvidenceFact, Source: "benchmark", Description: "mean_recall_at_10", Value: fmt.Sprintf("%.4f", sum.MeanRecall)},
					{Kind: models.EvidenceFact, Source: "benchmark", Description: "min_recall_at_10", Value: fmt.Sprintf("%.4f", sum.MinRecall)},
					{Kind: models.EvidenceFact, Source: "workload", Description: "objective", Value: fmt.Sprintf("%.4f", *w.Objectives.MinRecallAt10)},
					{Kind: models.EvidenceFact, Source: "benchmark", Description: "samples", Value: fmt.Sprintf("%d", sum.Samples)},
				},
				ProbableCause:       "Search breadth too low (ef_search/probes), filter interaction, or index build parameters.",
				ConfidenceRationale: "Measured against verified exact baseline when ExactBaselineVerified=true.",
				Remediation:         "Evaluate higher hnsw.ef_search / ivfflat.probes with `pgscope vector tune` in an isolated environment.",
				Risk:                models.RiskMedium,
				Validation:          "Re-run recall with the candidate settings and confirm mean/min Recall@10.",
			})
		}
		if w.Objectives.MaxP95LatencyMS != nil && sum.ApproxLatency.Count > 0 && sum.ApproxLatency.P95MS > *w.Objectives.MaxP95LatencyMS {
			out = append(out, models.Finding{
				RuleID:     "VEC.LATENCY_ABOVE_TARGET",
				Severity:   models.SeverityWarning,
				Database:   report.Database,
				Object:     report.Target,
				ObservedAt: now,
				Title:      "Approximate search p95 latency above objective",
				Description: fmt.Sprintf(
					"Approx p95=%.2fms over %d samples exceeds objective %.2fms.",
					sum.ApproxLatency.P95MS, sum.ApproxLatency.Count, *w.Objectives.MaxP95LatencyMS,
				),
				Evidence: []models.Evidence{
					{Kind: models.EvidenceFact, Source: "benchmark", Description: "p95_ms", Value: fmt.Sprintf("%.3f", sum.ApproxLatency.P95MS)},
					{Kind: models.EvidenceFact, Source: "workload", Description: "max_p95_latency_ms", Value: fmt.Sprintf("%.3f", *w.Objectives.MaxP95LatencyMS)},
				},
				ProbableCause:       "High search breadth, cold cache, or undersized hardware for the index.",
				ConfidenceRationale: "Client-observed latency from monotonic clock; server EXPLAIN ANALYZE time not required.",
				Remediation:         "Trade recall vs latency via tuning; ensure benchmark hardware matches production class.",
				Risk:                models.RiskMedium,
				Validation:          "Re-measure p95 under the same workload fingerprint.",
			})
		}
		if sum.IncompleteResults > 0 {
			out = append(out, models.Finding{
				RuleID:     "VEC.INCOMPLETE_RESULTS",
				Severity:   models.SeverityInfo,
				Database:   report.Database,
				Object:     report.Target,
				ObservedAt: now,
				Title:      "Some queries returned fewer than K results",
				Description: fmt.Sprintf(
					"%d/%d samples for K=%d returned fewer than K rows. This is not automatically an ANN failure when fewer than K eligible rows exist.",
					sum.IncompleteResults, sum.Samples, sum.K,
				),
				Evidence: []models.Evidence{
					{Kind: models.EvidenceFact, Source: "benchmark", Description: "incomplete_results", Value: fmt.Sprintf("%d", sum.IncompleteResults)},
				},
				ProbableCause:       "Filter selectivity, small partitions, or search breadth limiting candidates.",
				ConfidenceRationale: "Counts are measured; root cause needs eligible-row context.",
				Remediation:         "Use `pgscope vector filters analyze` and consider iterative_scan where supported.",
				Risk:                models.RiskLow,
				Validation:          "Confirm eligible row counts per filter group exceed K.",
			})
		}
	}
	if !report.ExactBaselineVerified {
		out = append(out, models.Finding{
			RuleID:     "VEC.BASELINE_UNVERIFIED",
			Severity:   models.SeverityWarning,
			Database:   report.Database,
			Object:     report.Target,
			ObservedAt: now,
			Title:      "Exact baseline not fully verified",
			Description: "One or more exact searches could not be verified as index-free. Recall numbers for those samples are not trusted.",
			Evidence: []models.Evidence{
				{Kind: models.EvidenceFact, Source: "benchmark", Description: "exact_baseline_verified", Value: "false"},
			},
			ProbableCause:       "Planner still selected an index, or exact queries failed.",
			ConfidenceRationale: "Plan inspection flagged index usage under disable_*scan settings.",
			Remediation:         "Inspect EXPLAIN output and adjust exact-path strategy; do not trust recall until verified.",
			Risk:                models.RiskHigh,
			Validation:          "Re-run with plans showing Seq Scan for exact path.",
		})
	}
	return out
}

// helpers to avoid circular imports with yaml in fixture loader
func readFile(path string) ([]byte, error) { return loadBytes(path) }
