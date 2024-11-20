package explain

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/hghukasyan/pgscope/pkg/models"
)

// AnalyzePlan walks the plan tree and emits evidence-backed findings.
// Seq scans and nested loops are not automatically problems.
func AnalyzePlan(result *models.ExplainResult, database string) {
	if result == nil {
		return
	}
	now := time.Now().UTC()
	var findings []models.Finding
	var walk func(n models.PlanNode, parent *models.PlanNode)
	walk = func(n models.PlanNode, parent *models.PlanNode) {
		findings = append(findings, cardinalityFindings(n, database, now)...)
		findings = append(findings, seqScanFindings(n, result.Summary, database, now)...)
		findings = append(findings, nestedLoopFindings(n, parent, database, now)...)
		findings = append(findings, sortSpillFindings(n, database, now)...)
		findings = append(findings, hashSpillFindings(n, database, now)...)
		findings = append(findings, filterFindings(n, database, now)...)
		findings = append(findings, indexOpportunityFindings(n, database, now)...)
		for i := range n.Plans {
			walk(n.Plans[i], &n)
		}
	}
	walk(result.Root, nil)

	// Deterministic ordering by rule id then path (encoded in Object).
	sortFindings(findings)
	result.Findings = findings
}

func cardinalityFindings(n models.PlanNode, database string, now time.Time) []models.Finding {
	if n.ActualRows == nil || n.ActualLoops == nil {
		return nil
	}
	actualTotal := *n.ActualRows * *n.ActualLoops
	if n.PlanRows <= 0 || actualTotal <= 0 {
		return nil
	}
	ratio := actualTotal / n.PlanRows
	if ratio < 1 {
		ratio = n.PlanRows / actualTotal
	}
	// Require a clear mismatch with meaningful row volume.
	if ratio < 10 || math.Max(actualTotal, n.PlanRows) < 1000 {
		return nil
	}
	sev := models.SeverityInfo
	if ratio >= 100 {
		sev = models.SeverityWarning
	}
	obj := nodeObject(n)
	return []models.Finding{{
		RuleID:     "EXPLAIN.CARDINALITY",
		Severity:   sev,
		Database:   database,
		Object:     obj,
		ObservedAt: now,
		Title:      "Cardinality estimate diverges from actual rows",
		Description: fmt.Sprintf(
			"Node %q estimated %.0f rows but observed %.0f rows across %.0f loop(s) (ratio ≈ %.1fx).",
			n.NodeType, n.PlanRows, actualTotal, *n.ActualLoops, ratio,
		),
		Evidence: []models.Evidence{
			{Kind: models.EvidenceFact, Source: "EXPLAIN ANALYZE", Description: "node path", Value: n.Path},
			{Kind: models.EvidenceFact, Source: "EXPLAIN ANALYZE", Description: "plan_rows", Value: fmt.Sprintf("%.0f", n.PlanRows)},
			{Kind: models.EvidenceFact, Source: "EXPLAIN ANALYZE", Description: "actual_rows*loops", Value: fmt.Sprintf("%.0f", actualTotal)},
			{Kind: models.EvidenceFact, Source: "EXPLAIN ANALYZE", Description: "ratio", Value: fmt.Sprintf("%.1f", ratio)},
		},
		ProbableCause:       "Stale or missing statistics, correlated predicates, or non-uniform data the planner under/over-estimated.",
		ConfidenceRationale: "Row counts are measured facts from EXPLAIN ANALYZE. The ratio threshold (≥10x with ≥1000 rows) reduces noise from tiny plans.",
		Remediation:         "ANALYZE the involved relations; consider extended statistics for correlated columns. Re-run EXPLAIN ANALYZE to validate.",
		Risk:                models.RiskLow,
		Validation:          "Confirm plan_rows moves closer to actual_rows after ANALYZE / statistics changes.",
	}}
}

func seqScanFindings(n models.PlanNode, summary models.PlanSummary, database string, now time.Time) []models.Finding {
	if !strings.EqualFold(n.NodeType, "Seq Scan") {
		return nil
	}
	// A sequential scan alone is not a problem. Require supporting evidence:
	// large estimated rows AND a selective-looking filter, or large actual cost share.
	large := n.PlanRows >= 100_000 || n.TotalCost >= 10_000
	selectiveFilter := n.Filter != "" && n.PlanRows >= 10_000
	actualHeavy := false
	if n.ActualTotalTime != nil && n.ActualRows != nil {
		actualHeavy = *n.ActualTotalTime >= 50 && *n.ActualRows >= 50_000
	}
	if !large && !selectiveFilter && !actualHeavy {
		return nil
	}
	// Skip tiny tables incorrectly flagged via high cost alone without rows.
	if n.PlanRows < 1000 && (n.ActualRows == nil || *n.ActualRows < 1000) {
		return nil
	}

	sev := models.SeverityInfo
	if (selectiveFilter && n.PlanRows >= 100_000) || actualHeavy {
		sev = models.SeverityWarning
	}

	ev := []models.Evidence{
		{Kind: models.EvidenceFact, Source: "EXPLAIN", Description: "node path", Value: n.Path},
		{Kind: models.EvidenceFact, Source: "EXPLAIN", Description: "relation", Value: relationOf(n)},
		{Kind: models.EvidenceFact, Source: "EXPLAIN", Description: "plan_rows", Value: fmt.Sprintf("%.0f", n.PlanRows)},
		{Kind: models.EvidenceFact, Source: "EXPLAIN", Description: "total_cost", Value: fmt.Sprintf("%.2f", n.TotalCost)},
	}
	if n.Filter != "" {
		ev = append(ev, models.Evidence{Kind: models.EvidenceFact, Source: "EXPLAIN", Description: "filter", Value: n.Filter})
	}
	if n.ActualTotalTime != nil {
		ev = append(ev, models.Evidence{Kind: models.EvidenceFact, Source: "EXPLAIN ANALYZE", Description: "actual_total_time_ms", Value: fmt.Sprintf("%.3f", *n.ActualTotalTime)})
	}

	return []models.Finding{{
		RuleID:              "EXPLAIN.LARGE_SEQ_SCAN",
		Severity:            sev,
		Database:            database,
		Object:              nodeObject(n),
		ObservedAt:          now,
		Title:               "Large sequential scan with supporting evidence",
		Description:         fmt.Sprintf("Seq Scan on %s has plan_rows=%.0f cost=%.2f. This is not automatically inefficient, but size/filter evidence suggests reviewing indexes.", relationOf(n), n.PlanRows, n.TotalCost),
		Evidence:            ev,
		ProbableCause:       "Missing or unused index for the filter/join condition, or a deliberate full scan that is cheaper than random I/O.",
		ConfidenceRationale: "Finding requires large row estimates and/or a filter on a sizeable relation; small sequential scans are ignored.",
		Remediation:         "Compare with an index-backed plan after creating a candidate index in a lab (or HypoPG). Validate with EXPLAIN (ANALYZE) on a realistic data set.",
		Risk:                models.RiskMedium,
		Validation:          "Confirm cost/time improves under representative data; do not add indexes for one-off scans.",
	}}
}

func nestedLoopFindings(n models.PlanNode, parent *models.PlanNode, database string, now time.Time) []models.Finding {
	if !strings.EqualFold(n.NodeType, "Nested Loop") {
		return nil
	}
	if len(n.Plans) < 2 {
		return nil
	}
	inner := n.Plans[1]
	// Expensive nested loop: high outer rows driving expensive inner scans repeatedly.
	outerRows := n.Plans[0].PlanRows
	if n.Plans[0].ActualRows != nil && n.Plans[0].ActualLoops != nil {
		outerRows = *n.Plans[0].ActualRows * *n.Plans[0].ActualLoops
	}
	innerLoops := 1.0
	if inner.ActualLoops != nil {
		innerLoops = *inner.ActualLoops
	} else if outerRows > 0 {
		innerLoops = outerRows
	}
	innerCostly := inner.TotalCost >= 100 || strings.EqualFold(inner.NodeType, "Seq Scan")
	repeated := innerLoops >= 1000 || (n.ActualLoops != nil && *n.ActualLoops >= 1 && outerRows >= 1000)
	if !(innerCostly && repeated && outerRows >= 1000) {
		return nil
	}
	_ = parent

	return []models.Finding{{
		RuleID:     "EXPLAIN.NESTED_LOOP_HEAVY",
		Severity:   models.SeverityWarning,
		Database:   database,
		Object:     nodeObject(n),
		ObservedAt: now,
		Title:      "Nested loop with repeated expensive inner scan",
		Description: fmt.Sprintf(
			"Nested Loop outer rows≈%.0f with inner node %q cost=%.2f loops≈%.0f. Nested loops are often optimal for small outers; this pattern suggests inner work is amplified.",
			outerRows, inner.NodeType, inner.TotalCost, innerLoops,
		),
		Evidence: []models.Evidence{
			{Kind: models.EvidenceFact, Source: "EXPLAIN", Description: "node path", Value: n.Path},
			{Kind: models.EvidenceFact, Source: "EXPLAIN", Description: "outer_rows", Value: fmt.Sprintf("%.0f", outerRows)},
			{Kind: models.EvidenceFact, Source: "EXPLAIN", Description: "inner_node", Value: inner.NodeType},
			{Kind: models.EvidenceFact, Source: "EXPLAIN", Description: "inner_cost", Value: fmt.Sprintf("%.2f", inner.TotalCost)},
			{Kind: models.EvidenceFact, Source: "EXPLAIN", Description: "inner_loops", Value: fmt.Sprintf("%.0f", innerLoops)},
		},
		ProbableCause:       "Missing index on the inner relation's join key, or a cardinality misestimate that chose Nested Loop over Hash/Merge Join.",
		ConfidenceRationale: "Requires both a large outer and a costly/repeated inner — Nested Loop alone is not flagged.",
		Remediation:         "Ensure an index supports the inner join condition; ANALYZE; compare alternative join plans after statistics refresh.",
		Risk:                models.RiskMedium,
		Validation:          "Re-EXPLAIN and confirm join type/cost with realistic parameters.",
	}}
}

func sortSpillFindings(n models.PlanNode, database string, now time.Time) []models.Finding {
	if !strings.EqualFold(n.NodeType, "Sort") {
		return nil
	}
	if !strings.EqualFold(n.SortSpaceType, "Disk") {
		return nil
	}
	used := 0.0
	if n.SortSpaceUsedKB != nil {
		used = *n.SortSpaceUsedKB
	}
	return []models.Finding{{
		RuleID:     "EXPLAIN.SORT_SPILL",
		Severity:   models.SeverityWarning,
		Database:   database,
		Object:     nodeObject(n),
		ObservedAt: now,
		Title:      "Sort spilled to disk",
		Description: fmt.Sprintf(
			"Sort used on-disk storage (%s, %.0f KB). This is measured from EXPLAIN ANALYZE and often indicates work_mem pressure for this node.",
			n.SortMethod, used,
		),
		Evidence: []models.Evidence{
			{Kind: models.EvidenceFact, Source: "EXPLAIN ANALYZE", Description: "node path", Value: n.Path},
			{Kind: models.EvidenceFact, Source: "EXPLAIN ANALYZE", Description: "sort_method", Value: n.SortMethod},
			{Kind: models.EvidenceFact, Source: "EXPLAIN ANALYZE", Description: "sort_space_type", Value: n.SortSpaceType},
			{Kind: models.EvidenceFact, Source: "EXPLAIN ANALYZE", Description: "sort_space_used_kb", Value: fmt.Sprintf("%.0f", used)},
		},
		ProbableCause:       "work_mem too small for this sort, or an unexpectedly large sort set due to cardinality errors.",
		ConfidenceRationale: "Disk sort space type is reported by the executor; root cause (GUC vs cardinality) needs correlation.",
		Remediation:         "Review work_mem for the session/workload carefully (global increases have memory risk). Prefer reducing sorted rows via indexes or earlier filters.",
		Risk:                models.RiskMedium,
		Validation:          "Re-run EXPLAIN ANALYZE and confirm Sort Space Type = Memory under representative load.",
	}}
}

func hashSpillFindings(n models.PlanNode, database string, now time.Time) []models.Finding {
	if !strings.Contains(strings.ToLower(n.NodeType), "hash") {
		return nil
	}
	if n.HashBatches == nil || *n.HashBatches <= 1 {
		return nil
	}
	batches := *n.HashBatches
	buckets := int64(0)
	if n.HashBuckets != nil {
		buckets = *n.HashBuckets
	}
	return []models.Finding{{
		RuleID:     "EXPLAIN.HASH_SPILL",
		Severity:   models.SeverityWarning,
		Database:   database,
		Object:     nodeObject(n),
		ObservedAt: now,
		Title:      "Hash operation used multiple batches (possible spill)",
		Description: fmt.Sprintf(
			"Hash node reports batches=%d buckets=%d. Multiple batches often indicate the hash table did not fit in work_mem.",
			batches, buckets,
		),
		Evidence: []models.Evidence{
			{Kind: models.EvidenceFact, Source: "EXPLAIN ANALYZE", Description: "node path", Value: n.Path},
			{Kind: models.EvidenceFact, Source: "EXPLAIN ANALYZE", Description: "hash_batches", Value: fmt.Sprintf("%d", batches)},
			{Kind: models.EvidenceFact, Source: "EXPLAIN ANALYZE", Description: "hash_buckets", Value: fmt.Sprintf("%d", buckets)},
		},
		ProbableCause:       "Hash table larger than work_mem, or underestimated build-side rows.",
		ConfidenceRationale: "Batch count > 1 is a measured executor signal; it is not proof of disk saturation.",
		Remediation:         "Reduce build-side cardinality, ensure statistics are current, and tune work_mem cautiously for the specific workload.",
		Risk:                models.RiskMedium,
		Validation:          "Confirm Hash Batches returns to 1 after changes under the same data volume.",
	}}
}

func filterFindings(n models.PlanNode, database string, now time.Time) []models.Finding {
	if n.Filter == "" {
		return nil
	}
	// Inefficient filtering: rows removed after a large scan (Rows Removed by Filter).
	removed := 0.0
	if v, ok := n.Extra["Rows Removed by Filter"]; ok {
		if f, ok := asFloat(v); ok {
			removed = f
		}
	}
	if removed < 10_000 {
		return nil
	}
	baseRows := n.PlanRows
	if n.ActualRows != nil {
		baseRows = *n.ActualRows
	}
	if baseRows <= 0 {
		baseRows = 1
	}
	if removed/baseRows < 5 && removed < 100_000 {
		return nil
	}
	return []models.Finding{{
		RuleID:     "EXPLAIN.FILTER_INEFFICIENT",
		Severity:   models.SeverityInfo,
		Database:   database,
		Object:     nodeObject(n),
		ObservedAt: now,
		Title:      "Large number of rows removed by filter",
		Description: fmt.Sprintf(
			"Node %q removed ≈%.0f rows via Filter after scanning. An index matching the filter may reduce work if selectivity is stable.",
			n.NodeType, removed,
		),
		Evidence: []models.Evidence{
			{Kind: models.EvidenceFact, Source: "EXPLAIN", Description: "node path", Value: n.Path},
			{Kind: models.EvidenceFact, Source: "EXPLAIN", Description: "filter", Value: n.Filter},
			{Kind: models.EvidenceFact, Source: "EXPLAIN", Description: "rows_removed_by_filter", Value: fmt.Sprintf("%.0f", removed)},
		},
		ProbableCause:       "Predicate applied after a broad scan because no suitable index exists or the planner preferred a seq scan.",
		ConfidenceRationale: "Rows Removed by Filter is measured; index benefit still depends on write overhead and selectivity.",
		Remediation:         "Evaluate a candidate index on the filtered columns in a lab; weigh write amplification.",
		Risk:                models.RiskMedium,
		Validation:          "EXPLAIN after index creation should show Index/Bitmap scan with lower cost for the same predicate.",
	}}
}

func indexOpportunityFindings(n models.PlanNode, database string, now time.Time) []models.Finding {
	if !strings.EqualFold(n.NodeType, "Seq Scan") || n.Filter == "" || n.RelationName == "" {
		return nil
	}
	if n.PlanRows < 10_000 {
		return nil
	}
	cols := extractSimpleFilterColumns(n.Filter)
	if len(cols) == 0 {
		return nil
	}
	return []models.Finding{{
		RuleID:     "EXPLAIN.INDEX_CANDIDATE",
		Severity:   models.SeverityInfo,
		Database:   database,
		Object:     nodeObject(n),
		ObservedAt: now,
		Title:      "Potential index opportunity from filter on sequential scan",
		Description: fmt.Sprintf(
			"Seq Scan on %s with filter %q and plan_rows=%.0f suggests evaluating an index on %v.",
			relationOf(n), n.Filter, n.PlanRows, cols,
		),
		Evidence: []models.Evidence{
			{Kind: models.EvidenceFact, Source: "EXPLAIN", Description: "node path", Value: n.Path},
			{Kind: models.EvidenceFact, Source: "EXPLAIN", Description: "relation", Value: relationOf(n)},
			{Kind: models.EvidenceFact, Source: "EXPLAIN", Description: "filter", Value: n.Filter},
			{Kind: models.EvidenceObservation, Source: "derived", Description: "candidate_columns", Value: strings.Join(cols, ",")},
		},
		ProbableCause:       "Planner chose a sequential scan because no matching index exists or statistics favor it.",
		ConfidenceRationale: "Heuristic extraction of simple equality/range columns; expressions and functions need manual review. Not a create-index command.",
		Remediation:         fmt.Sprintf("In a lab, consider CREATE INDEX ON %s (%s); validate with EXPLAIN (ANALYZE) and HypoPG if enabled. Account for write overhead.", relationOf(n), strings.Join(cols, ", ")),
		Risk:                models.RiskMedium,
		Validation:          "Confirm the planner uses the index and latency improves on representative data; never deploy untested indexes.",
	}}
}

func extractSimpleFilterColumns(filter string) []string {
	// Conservative: identify simple col <op> const patterns. Function-wrapped
	// predicates are ignored because an index recommendation would be unsafe.
	f := strings.TrimSpace(filter)
	replacer := strings.NewReplacer("(", " ", ")", " ")
	f = replacer.Replace(f)
	var cols []string
	tokens := strings.Fields(f)
	for i := 0; i < len(tokens); i++ {
		tok := strings.Trim(tokens[i], "\"")
		up := strings.ToUpper(tok)
		if up == "AND" || up == "OR" || up == "NOT" || up == "IS" || up == "NULL" {
			continue
		}
		if i+1 >= len(tokens) {
			continue
		}
		op := tokens[i+1]
		switch op {
		case "=", ">", "<", ">=", "<=", "<>", "!=":
			if strings.Contains(tok, ".") {
				parts := strings.Split(tok, ".")
				tok = parts[len(parts)-1]
			}
			if tok != "" && !strings.ContainsAny(tok, "'$") {
				cols = appendUnique(cols, tok)
			}
		}
	}
	return cols
}

func nodeObject(n models.PlanNode) string {
	rel := relationOf(n)
	if rel == "" {
		return fmt.Sprintf("%s@%s", n.NodeType, n.Path)
	}
	return fmt.Sprintf("%s:%s@%s", n.NodeType, rel, n.Path)
}

func relationOf(n models.PlanNode) string {
	if n.RelationName == "" {
		return ""
	}
	if n.Schema != "" {
		return n.Schema + "." + n.RelationName
	}
	return n.RelationName
}

func sortFindings(fs []models.Finding) {
	for i := 0; i < len(fs); i++ {
		for j := i + 1; j < len(fs); j++ {
			if fs[j].RuleID < fs[i].RuleID || (fs[j].RuleID == fs[i].RuleID && fs[j].Object < fs[i].Object) {
				fs[i], fs[j] = fs[j], fs[i]
			}
		}
	}
}
