package vector

import (
	"fmt"
	"strings"
	"time"

	"github.com/hghukasyan/pgscope/pkg/models"
)

// AnalyzeVectorPlan adds vector-specific findings to an EXPLAIN result.
func AnalyzeVectorPlan(result *models.ExplainResult, inv models.VectorInventory, database string) {
	if result == nil {
		return
	}
	now := time.Now().UTC()
	var findings []models.Finding
	ops := detectDistanceOps(result.SQLPreview)
	hasANNIndex := false
	for _, idx := range inv.Indexes {
		if idx.IsValid {
			hasANNIndex = true
			break
		}
	}

	var walk func(n models.PlanNode)
	walk = func(n models.PlanNode) {
		nt := strings.ToLower(n.NodeType)
		if strings.Contains(nt, "seq scan") && len(ops) > 0 {
			findings = append(findings, models.Finding{
				RuleID:     "VEC.EXPLAIN_SEQ_SCAN",
				Severity:   models.SeverityInfo,
				Database:   database,
				Object:     nodeObj(n),
				ObservedAt: now,
				Title:      "Vector distance query uses sequential scan",
				Description: fmt.Sprintf(
					"Plan node %q with distance operator(s) %v uses a sequential scan (cost=%.2f rows=%.0f). This is not automatically a regression — for small tables exact scan can be cheaper.",
					n.NodeType, ops, n.TotalCost, n.PlanRows,
				),
				Evidence: []models.Evidence{
					{Kind: models.EvidenceFact, Source: "EXPLAIN", Description: "node", Value: n.NodeType},
					{Kind: models.EvidenceFact, Source: "EXPLAIN", Description: "relation", Value: n.RelationName},
					{Kind: models.EvidenceFact, Source: "EXPLAIN", Description: "total_cost", Value: fmt.Sprintf("%.2f", n.TotalCost)},
					{Kind: models.EvidenceFact, Source: "derived", Description: "distance_ops", Value: strings.Join(ops, ",")},
					{Kind: models.EvidenceFact, Source: "inventory", Description: "has_ann_index", Value: fmt.Sprintf("%v", hasANNIndex)},
				},
				ProbableCause:       "No matching ANN index, operator/opclass mismatch, expression mismatch, or planner cost model prefers seq scan.",
				ConfidenceRationale: "Plan node type is a fact; whether an index would help requires recall/latency measurement.",
				Remediation:         "Confirm operator class matches the distance operator; compare exact vs ANN with `pgscope vector recall` before creating indexes.",
				Risk:                models.RiskMedium,
				Validation:          "Re-EXPLAIN after index/metric alignment and confirm Index Scan with acceptable cost.",
			})
		}
		if strings.Contains(nt, "index") {
			if isVectorIndexName(n.IndexName, inv) || strings.Contains(nt, "hnsw") || strings.Contains(nt, "ivfflat") {
				// Check filter above index: parent Filter with child index is OK; Filter on index node may mean post-filtering.
				if n.Filter != "" {
					findings = append(findings, models.Finding{
						RuleID:     "VEC.EXPLAIN_POST_FILTER",
						Severity:   models.SeverityInfo,
						Database:   database,
						Object:     nodeObj(n),
						ObservedAt: now,
						Title:      "Filter applied with vector index scan",
						Description: fmt.Sprintf(
							"Index scan on %s applies Filter %q. Post-index filtering can reduce result completeness for ANN LIMIT queries.",
							n.IndexName, n.Filter,
						),
						Evidence: []models.Evidence{
							{Kind: models.EvidenceFact, Source: "EXPLAIN", Description: "index", Value: n.IndexName},
							{Kind: models.EvidenceFact, Source: "EXPLAIN", Description: "filter", Value: n.Filter},
						},
						ProbableCause:       "Predicate not covered by the index; ANN may return fewer than LIMIT rows after filtering.",
						ConfidenceRationale: "Filter present on index node is a plan fact; impact needs eligible-row and recall evidence.",
						Remediation:         "Evaluate iterative_scan where supported, partial indexes, or filter-aware benchmarks via `pgscope vector filters analyze`.",
						Risk:                models.RiskMedium,
						Validation:          "Measure result completeness and Recall@K under the filter.",
					})
				}
			}
		}
		if strings.EqualFold(n.NodeType, "Sort") && len(ops) > 0 {
			findings = append(findings, models.Finding{
				RuleID:     "VEC.EXPLAIN_SORT",
				Severity:   models.SeverityInfo,
				Database:   database,
				Object:     nodeObj(n),
				ObservedAt: now,
				Title:      "Explicit sort in vector distance plan",
				Description: "A Sort node appears in a distance-ordered query. ANN indexes typically avoid a separate sort when used for ORDER BY distance LIMIT.",
				Evidence: []models.Evidence{
					{Kind: models.EvidenceFact, Source: "EXPLAIN", Description: "node", Value: n.NodeType},
					{Kind: models.EvidenceFact, Source: "EXPLAIN", Description: "total_cost", Value: fmt.Sprintf("%.2f", n.TotalCost)},
				},
				ProbableCause:       "Index not used for ordering, or ORDER BY expression does not match the indexed distance operator.",
				ConfidenceRationale: "Sort presence is factual; necessity depends on plan shape.",
				Remediation:         "Ensure ORDER BY uses the indexed operator without wrapping expressions that block index order.",
				Risk:                models.RiskLow,
				Validation:          "Re-EXPLAIN and confirm Index Scan without Sort for ANN path.",
			})
		}
		for i := range n.Plans {
			walk(n.Plans[i])
		}
	}
	walk(result.Root)

	// Operator / opclass mismatch hint when ANN indexes exist but seq scan chosen.
	if hasANNIndex && len(ops) > 0 {
		findings = append(findings, mismatchHints(ops, inv, database, now)...)
	}

	result.Findings = append(result.Findings, findings...)
}

func detectDistanceOps(sql string) []string {
	var ops []string
	for _, op := range []string{"<=>", "<#>", "<+>", "<->", "<~>", "<%>"} {
		if strings.Contains(sql, op) {
			ops = append(ops, op)
		}
	}
	return ops
}

func isVectorIndexName(name string, inv models.VectorInventory) bool {
	for _, idx := range inv.Indexes {
		if idx.Name == name || idx.Schema+"."+idx.Name == name {
			return true
		}
	}
	return false
}

func mismatchHints(ops []string, inv models.VectorInventory, database string, now time.Time) []models.Finding {
	wanted := map[string]string{
		"<=>": "cosine", "<#>": "ip", "<+>": "l1", "<->": "l2", "<~>": "hamming", "<%>": "jaccard",
	}
	var out []models.Finding
	for _, op := range ops {
		metric := wanted[op]
		matched := false
		for _, idx := range inv.Indexes {
			if idx.DistanceMetric == metric || idx.DistanceOperator == op {
				matched = true
				break
			}
		}
		if matched {
			continue
		}
		out = append(out, models.Finding{
			RuleID:     "VEC.OP_MISMATCH",
			Severity:   models.SeverityWarning,
			Database:   database,
			ObservedAt: now,
			Title:      "Distance operator may not match available ANN operator classes",
			Description: fmt.Sprintf(
				"Query uses operator %s (metric≈%s) but no discovered index advertises that metric/operator. Planner may ignore ANN indexes.",
				op, metric,
			),
			Evidence: []models.Evidence{
				{Kind: models.EvidenceFact, Source: "SQL", Description: "operator", Value: op},
				{Kind: models.EvidenceObservation, Source: "inventory", Description: "index_metrics", Value: indexMetrics(inv)},
			},
			ProbableCause:       "Index built with a different distance opclass than the query ORDER BY operator.",
			ConfidenceRationale: "Operator/opclass inventory comparison; expression indexes need manual review.",
			Remediation:         "Create or use an index with the matching operator class, or change the query operator to match the index.",
			Risk:                models.RiskMedium,
			Validation:          "EXPLAIN should show the ANN index when aligned.",
		})
	}
	return out
}

func indexMetrics(inv models.VectorInventory) string {
	var parts []string
	for _, idx := range inv.Indexes {
		parts = append(parts, fmt.Sprintf("%s:%s", idx.Name, idx.DistanceMetric))
	}
	return strings.Join(parts, ",")
}

func nodeObj(n models.PlanNode) string {
	if n.RelationName != "" {
		return fmt.Sprintf("%s:%s@%s", n.NodeType, n.RelationName, n.Path)
	}
	return fmt.Sprintf("%s@%s", n.NodeType, n.Path)
}
