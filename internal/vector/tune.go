package vector

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/hghukasyan/pgscope/pkg/models"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TuneOptions controls bounded parameter sweeps.
type TuneOptions struct {
	Authorize bool
	Objective string // recall | latency | balanced
}

// RunTune evaluates candidate search settings against a recall workload.
func RunTune(ctx context.Context, pool *pgxpool.Pool, w Workload, opts TuneOptions) (models.TuneReport, error) {
	start := time.Now()
	report := models.TuneReport{
		CollectedAt:         start.UTC(),
		Target:              w.Target.Schema + "." + w.Target.Table,
		Objective:           opts.Objective,
		WorkloadFingerprint: w.Fingerprint(),
	}
	if !opts.Authorize {
		return report, fmt.Errorf("tuning requires --authorize-benchmark on an isolated/lab environment")
	}
	if err := w.Validate(); err != nil {
		return report, err
	}
	if opts.Objective == "" {
		opts.Objective = "balanced"
		report.Objective = opts.Objective
	}

	inv, err := Discover(ctx, pool, DiscoverOptions{Schema: w.Target.Schema, Table: w.Target.Table})
	if err != nil {
		return report, err
	}
	am := "hnsw"
	for _, idx := range inv.Indexes {
		if idx.TableSchema == w.Target.Schema && idx.TableName == w.Target.Table {
			am = idx.AccessMethod
			if w.Target.IndexName == "" || idx.Name == w.Target.IndexName {
				report.AccessMethod = am
				break
			}
		}
	}
	if report.AccessMethod == "" {
		report.AccessMethod = am
	}

	candidates := buildCandidates(w, report.AccessMethod, inv.Capabilities)
	if len(candidates) == 0 {
		report.Limitations = append(report.Limitations, models.Limitation{
			Area: "tuning", Reason: "No tunable settings detected for this pgvector build",
			Impact: "Cannot sweep search parameters",
		})
		report.DurationMS = time.Since(start).Milliseconds()
		return report, nil
	}

	// Baseline (empty settings = server defaults).
	baseRep, err := RunRecall(ctx, pool, w, RecallOptions{Authorize: true, OmitSampleDetails: true})
	if err != nil {
		return report, fmt.Errorf("baseline recall: %w", err)
	}
	baseCand := candidateFromRecall(map[string]string{"(defaults)": "true"}, baseRep, w)
	report.Baseline = &baseCand

	// Alternate order: reverse half to reduce time-order bias.
	for i, j := 0, len(candidates)-1; i < j; i, j = i+1, j-1 {
		if i%2 == 0 {
			candidates[i], candidates[j] = candidates[j], candidates[i]
		}
	}

	for _, settings := range candidates {
		select {
		case <-ctx.Done():
			report.Limitations = append(report.Limitations, models.Limitation{
				Area: "tuning", Reason: "cancelled", Impact: "partial candidate set evaluated",
			})
			goto finish
		default:
		}
		rep, err := RunRecall(ctx, pool, w, RecallOptions{
			Authorize: true, SearchSettings: settings, OmitSampleDetails: true,
		})
		cand := candidateFromRecall(settings, rep, w)
		if err != nil {
			cand.Error = err.Error()
		}
		report.Candidates = append(report.Candidates, cand)
	}

finish:
	report.Recommendation, report.ParetoNotes = recommend(report, opts.Objective, w)
	report.Findings = tuneFindings(report, w)
	report.DurationMS = time.Since(start).Milliseconds()
	return report, nil
}

func buildCandidates(w Workload, am string, caps models.VectorCapabilities) []map[string]string {
	var out []map[string]string
	max := w.Tuning.MaxCandidates
	switch am {
	case "hnsw":
		efs := w.Tuning.HNSWEfSearch
		if len(efs) == 0 {
			efs = []int{20, 40, 80, 160}
		}
		for _, ef := range efs {
			if !contains(caps.SettingsAvailable, "hnsw.ef_search") && len(caps.SettingsAvailable) == 0 {
				// Still try — settingSettable may work at runtime.
			}
			cfg := map[string]string{"hnsw.ef_search": strconv.Itoa(ef)}
			if w.Tuning.EnableIterativeScan != nil && *w.Tuning.EnableIterativeScan && caps.HasIterativeScanHNSW {
				cfg["hnsw.iterative_scan"] = "relaxed_order"
			}
			out = append(out, cfg)
			if len(out) >= max {
				return out
			}
		}
	case "ivfflat":
		probes := w.Tuning.IVFFlatProbes
		if len(probes) == 0 {
			probes = []int{1, 5, 10, 20}
		}
		for _, p := range probes {
			cfg := map[string]string{"ivfflat.probes": strconv.Itoa(p)}
			if w.Tuning.EnableIterativeScan != nil && *w.Tuning.EnableIterativeScan && caps.HasIterativeScanIVF {
				cfg["ivfflat.iterative_scan"] = "relaxed_order"
			}
			out = append(out, cfg)
			if len(out) >= max {
				return out
			}
		}
	}
	return out
}

func candidateFromRecall(settings map[string]string, rep models.RecallReport, w Workload) models.TuneCandidate {
	c := models.TuneCandidate{
		Settings:  settings,
		Summaries: rep.Summaries,
	}
	c.MeetsObjectives = meetsObjectives(rep, w)
	return c
}

func meetsObjectives(rep models.RecallReport, w Workload) bool {
	for _, sum := range rep.Summaries {
		if w.Objectives.MinRecallAt10 != nil && sum.K == 10 && sum.MeanRecall < *w.Objectives.MinRecallAt10 {
			return false
		}
		if w.Objectives.MaxP95LatencyMS != nil && sum.ApproxLatency.P95MS > *w.Objectives.MaxP95LatencyMS {
			return false
		}
		if w.Objectives.MaxErrorRate != nil && sum.Samples > 0 {
			rate := float64(sum.Errors) / float64(sum.Samples+sum.Errors)
			if rate > *w.Objectives.MaxErrorRate {
				return false
			}
		}
	}
	return rep.ExactBaselineVerified
}

func recommend(report models.TuneReport, objective string, w Workload) (string, string) {
	var best *models.TuneCandidate
	var bestScore float64
	pareto := "Candidates that meet all objectives are preferred; otherwise tradeoffs are listed."
	for i := range report.Candidates {
		c := &report.Candidates[i]
		if c.Error != "" || len(c.Summaries) == 0 {
			continue
		}
		sum := pickSummary(c.Summaries, 10)
		score := sum.MeanRecall
		switch objective {
		case "latency":
			if sum.ApproxLatency.P95MS <= 0 {
				continue
			}
			score = -sum.ApproxLatency.P95MS
		case "balanced":
			latPenalty := sum.ApproxLatency.P95MS / 1000.0
			score = sum.MeanRecall - latPenalty
		}
		if c.MeetsObjectives {
			score += 10
		}
		if best == nil || score > bestScore {
			best = c
			bestScore = score
		}
	}
	if best == nil {
		return "No successful candidate measurements; inspect limitations and baseline.", pareto
	}
	rec := fmt.Sprintf("Best measured candidate under objective %q: settings=%v", objective, best.Settings)
	if sum := pickSummary(best.Summaries, 10); sum.Samples > 0 {
		rec += fmt.Sprintf(" (Recall@10≈%.3f p95≈%.1fms meets_objectives=%v)", sum.MeanRecall, sum.ApproxLatency.P95MS, best.MeetsObjectives)
	}
	rec += ". This is a measured recommendation for this workload fingerprint only — not universally optimal."
	if !best.MeetsObjectives {
		pareto = "No candidate met all objectives; reported best is a Pareto-style tradeoff for the chosen objective."
	}
	_ = w
	return rec, pareto
}

func pickSummary(sums []models.RecallSummary, k int) models.RecallSummary {
	for _, s := range sums {
		if s.K == k {
			return s
		}
	}
	if len(sums) > 0 {
		return sums[0]
	}
	return models.RecallSummary{}
}

func tuneFindings(report models.TuneReport, w Workload) []models.Finding {
	var out []models.Finding
	if report.Recommendation != "" {
		out = append(out, models.Finding{
			RuleID:     "VEC.TUNE_RECOMMENDATION",
			Severity:   models.SeverityInfo,
			ObservedAt: report.CollectedAt,
			Object:     report.Target,
			Title:      "Measured vector search parameter recommendation",
			Description: report.Recommendation,
			Evidence: []models.Evidence{
				{Kind: models.EvidenceFact, Source: "benchmark", Description: "candidates_evaluated", Value: fmt.Sprintf("%d", len(report.Candidates))},
				{Kind: models.EvidenceFact, Source: "benchmark", Description: "objective", Value: report.Objective},
				{Kind: models.EvidenceObservation, Source: "benchmark", Description: "pareto_notes", Value: report.ParetoNotes},
			},
			ProbableCause:       "Search settings change candidate graph traversal / probe count.",
			ConfidenceRationale: "Based on measured recall/latency for this workload fingerprint only.",
			Remediation:         "Apply candidate settings at session/transaction scope in application code after lab validation. PGScope never alters production GUCs persistently.",
			Risk:                models.RiskMedium,
			Validation:          "Re-run `pgscope vector recall` with the candidate settings.",
		})
	}
	_ = w
	return out
}
