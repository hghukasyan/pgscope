package vector

import (
	"context"
	"fmt"

	"github.com/hghukasyan/pgscope/pkg/models"
	"github.com/jackc/pgx/v5/pgxpool"
)

// AnalyzeFilters runs per-filter-value recall cohorts when a filter is configured.
func AnalyzeFilters(ctx context.Context, pool *pgxpool.Pool, w Workload, authorize bool) (models.RecallReport, error) {
	if w.Search.Filter == nil {
		return models.RecallReport{}, fmt.Errorf("workload.search.filter is required for filters analyze")
	}
	if !authorize {
		return models.RecallReport{}, fmt.Errorf("filters analyze requires --authorize-benchmark")
	}

	baseValues := append([]string(nil), w.Search.Filter.Values...)
	var mergedSamples []models.RecallSample
	var last models.RecallReport
	for _, v := range baseValues {
		wc := w
		filterCopy := *w.Search.Filter
		filterCopy.Values = []string{v}
		filterCopy.Operator = "="
		wc.Search.Filter = &filterCopy
		// Keep samples modest per cohort.
		if wc.Benchmark.Samples > 50 {
			wc.Benchmark.Samples = 50
		}
		rep, err := RunRecall(ctx, pool, wc, RecallOptions{Authorize: true, OmitSampleDetails: true})
		if err != nil {
			return last, err
		}
		for i := range rep.Samples {
			rep.Samples[i].FilterLabel = v
		}
		mergedSamples = append(mergedSamples, rep.Samples...)
		last = rep
	}
	last.Samples = mergedSamples
	last.FilterSummaries = summarizeByFilter(mergedSamples, w.Search.K)
	last.Summaries = nil
	for _, k := range w.Search.K {
		last.Summaries = append(last.Summaries, SummarizeRecall(mergedSamples, k))
	}
	last.Findings = append(last.Findings, filterFindings(last)...)
	return last, nil
}

func filterFindings(report models.RecallReport) []models.Finding {
	var out []models.Finding
	if len(report.FilterSummaries) < 2 {
		return out
	}
	// Flag large recall gaps across tenants/categories.
	targetK := report.FilterSummaries[0].K
	for _, g := range report.FilterSummaries {
		if g.K == 10 {
			targetK = 10
			break
		}
	}
	var minG, maxG models.FilterRecallGroup
	first := true
	for _, g := range report.FilterSummaries {
		if g.K != targetK || g.Samples == 0 {
			continue
		}
		if first {
			minG, maxG, first = g, g, false
			continue
		}
		if g.MeanRecall < minG.MeanRecall {
			minG = g
		}
		if g.MeanRecall > maxG.MeanRecall {
			maxG = g
		}
	}
	if maxG.Samples > 0 && minG.Samples > 0 && maxG.MeanRecall-minG.MeanRecall >= 0.15 {
		out = append(out, models.Finding{
			RuleID:     "VEC.FILTER_RECALL_GAP",
			Severity:   models.SeverityWarning,
			Database:   report.Database,
			Object:     report.Target,
			ObservedAt: report.CollectedAt,
			Title:      "Recall varies substantially across filter groups",
			Description: fmt.Sprintf(
				"Filter group %q mean recall=%.3f vs %q mean recall=%.3f (Δ=%.3f).",
				minG.Label, minG.MeanRecall, maxG.Label, maxG.MeanRecall, maxG.MeanRecall-minG.MeanRecall,
			),
			Evidence: []models.Evidence{
				{Kind: models.EvidenceFact, Source: "benchmark", Description: "min_group", Value: fmt.Sprintf("%s:%.4f", minG.Label, minG.MeanRecall)},
				{Kind: models.EvidenceFact, Source: "benchmark", Description: "max_group", Value: fmt.Sprintf("%s:%.4f", maxG.Label, maxG.MeanRecall)},
			},
			ProbableCause:       "Uneven tenant/category cardinality, post-filtering of ANN candidates, or insufficient search breadth for selective filters.",
			ConfidenceRationale: "Measured per-cohort recall under the same metric/K; still depends on sample representativeness.",
			Remediation:         "Consider iterative_scan, higher ef_search/probes, or selective partial indexes — weighing maintenance cost. Do not auto-create per-tenant indexes.",
			Risk:                models.RiskMedium,
			Validation:          "Re-run filters analyze after search setting changes.",
		})
	}
	for _, g := range report.FilterSummaries {
		if g.Incomplete > 0 && g.Samples > 0 && float64(g.Incomplete)/float64(g.Samples) > 0.2 {
			out = append(out, models.Finding{
				RuleID:     "VEC.FILTER_INCOMPLETE",
				Severity:   models.SeverityInfo,
				Database:   report.Database,
				Object:     report.Target,
				ObservedAt: report.CollectedAt,
				Title:      "Filtered ANN often returns fewer than K rows",
				Description: fmt.Sprintf(
					"Filter %q: %d/%d samples incomplete for K=%d. Confirm eligible rows ≥ K before treating as ANN failure.",
					g.Label, g.Incomplete, g.Samples, g.K,
				),
				Evidence: []models.Evidence{
					{Kind: models.EvidenceFact, Source: "benchmark", Description: "incomplete_ratio", Value: fmt.Sprintf("%d/%d", g.Incomplete, g.Samples)},
				},
				ProbableCause:       "High filter selectivity relative to ANN candidate set.",
				ConfidenceRationale: "Incomplete counts measured; eligible population not always known.",
				Remediation:         "Widen search breadth or enable iterative scanning where available; validate eligible counts.",
				Risk:                models.RiskMedium,
				Validation:          "Check result counts after iterative_scan / higher probes.",
			})
		}
	}
	return out
}
