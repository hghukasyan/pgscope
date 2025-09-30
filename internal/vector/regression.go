package vector

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/hghukasyan/pgscope/pkg/models"
)

// SaveRecallHistory stores a compact recall report (no embeddings) under dir.
func SaveRecallHistory(dir string, report models.RecallReport) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	// Strip sample ID lists for history.
	cp := report
	for i := range cp.Samples {
		cp.Samples[i].ExactIDs = nil
		cp.Samples[i].ApproxIDs = nil
	}
	name := fmt.Sprintf("recall_%s_%d.json", report.WorkloadFingerprint, report.CollectedAt.Unix())
	path := filepath.Join(dir, name)
	data, err := json.MarshalIndent(cp, "", "  ")
	if err != nil {
		return "", err
	}
	return path, os.WriteFile(path, data, 0o644)
}

// LoadRecallReport loads a previously saved report.
func LoadRecallReport(path string) (models.RecallReport, error) {
	var r models.RecallReport
	data, err := os.ReadFile(path)
	if err != nil {
		return r, err
	}
	err = json.Unmarshal(data, &r)
	return r, err
}

// CompareRecallReports detects regressions only for compatible workloads.
func CompareRecallReports(baseline, current models.RecallReport) models.VectorRegressionCompare {
	out := models.VectorRegressionCompare{
		Baseline: baseline.WorkloadFingerprint,
		Current:  current.WorkloadFingerprint,
	}
	if baseline.WorkloadFingerprint == "" || current.WorkloadFingerprint == "" {
		out.Compatible = false
		out.Reason = "missing workload fingerprint"
		return out
	}
	if baseline.WorkloadFingerprint != current.WorkloadFingerprint {
		out.Compatible = false
		out.Reason = "incompatible workloads (fingerprint mismatch)"
		return out
	}
	if baseline.Metric != current.Metric || baseline.Target != current.Target {
		out.Compatible = false
		out.Reason = "target/metric mismatch despite fingerprint"
		return out
	}
	out.Compatible = true
	now := time.Now().UTC()

	b10 := summaryAt(baseline.Summaries, 10)
	c10 := summaryAt(current.Summaries, 10)
	if b10.Samples > 0 && c10.Samples > 0 {
		if b10.Samples < 10 || c10.Samples < 10 {
			out.Findings = append(out.Findings, models.Finding{
				RuleID:     "VEC.REGRESSION_INSUFFICIENT_SAMPLES",
				Severity:   models.SeverityInfo,
				ObservedAt: now,
				Title:      "Regression comparison has small sample sizes",
				Description: fmt.Sprintf("baseline samples=%d current samples=%d; treat deltas cautiously.", b10.Samples, c10.Samples),
				Evidence: []models.Evidence{
					{Kind: models.EvidenceFact, Source: "baseline", Description: "samples", Value: fmt.Sprintf("%d", b10.Samples)},
					{Kind: models.EvidenceFact, Source: "current", Description: "samples", Value: fmt.Sprintf("%d", c10.Samples)},
				},
				Risk: models.RiskLow,
			})
		}
		delta := c10.MeanRecall - b10.MeanRecall
		if delta <= -0.05 {
			sev := models.SeverityWarning
			if delta <= -0.10 {
				sev = models.SeverityCritical
			}
			out.Findings = append(out.Findings, models.Finding{
				RuleID:     "VEC.REGRESSION_RECALL",
				Severity:   sev,
				ObservedAt: now,
				Object:     current.Target,
				Title:      "Recall@10 declined versus compatible baseline",
				Description: fmt.Sprintf(
					"Recall@10 mean %.3f → %.3f (Δ=%.3f) for fingerprint %s.",
					b10.MeanRecall, c10.MeanRecall, delta, current.WorkloadFingerprint,
				),
				Evidence: []models.Evidence{
					{Kind: models.EvidenceFact, Source: "baseline", Description: "mean_recall_at_10", Value: fmt.Sprintf("%.4f", b10.MeanRecall)},
					{Kind: models.EvidenceFact, Source: "current", Description: "mean_recall_at_10", Value: fmt.Sprintf("%.4f", c10.MeanRecall)},
					{Kind: models.EvidenceFact, Source: "benchmark", Description: "fingerprint", Value: current.WorkloadFingerprint},
				},
				ProbableCause:       "Index rebuild parameters, search settings, data distribution shift, or pgvector upgrade.",
				ConfidenceRationale: "Compatible fingerprint ensures same target/metric/K/filter shape; sample bias still possible.",
				Remediation:         "Diff search settings and index definitions; re-tune in lab; confirm with additional samples.",
				Risk:                models.RiskMedium,
				Validation:          "Re-run identical workload YAML and confirm Recall@10 recovers.",
			})
		}
		latDelta := c10.ApproxLatency.P95MS - b10.ApproxLatency.P95MS
		if b10.ApproxLatency.Count > 0 && c10.ApproxLatency.Count > 0 && latDelta >= 20 && latDelta/b10.ApproxLatency.P95MS >= 0.5 {
			out.Findings = append(out.Findings, models.Finding{
				RuleID:     "VEC.REGRESSION_LATENCY",
				Severity:   models.SeverityWarning,
				ObservedAt: now,
				Object:     current.Target,
				Title:      "Approximate p95 latency increased significantly",
				Description: fmt.Sprintf(
					"p95 %.1fms → %.1fms (Δ=%.1fms) under compatible workload.",
					b10.ApproxLatency.P95MS, c10.ApproxLatency.P95MS, latDelta,
				),
				Evidence: []models.Evidence{
					{Kind: models.EvidenceFact, Source: "baseline", Description: "p95_ms", Value: fmt.Sprintf("%.3f", b10.ApproxLatency.P95MS)},
					{Kind: models.EvidenceFact, Source: "current", Description: "p95_ms", Value: fmt.Sprintf("%.3f", c10.ApproxLatency.P95MS)},
				},
				ProbableCause:       "Higher search breadth, larger index, cache effects, or host contention.",
				ConfidenceRationale: "Compatible workload; absolute latency also depends on machine load.",
				Remediation:         "Compare settings fingerprints and host conditions; re-benchmark on quiet lab hardware.",
				Risk:                models.RiskMedium,
				Validation:          "Confirm p95 under controlled isolated replay.",
			})
		}
	}
	if len(out.Findings) == 0 {
		out.Findings = append(out.Findings, models.Finding{
			RuleID:     "VEC.REGRESSION_NONE",
			Severity:   models.SeverityInfo,
			ObservedAt: now,
			Title:      "No significant recall/latency regression detected",
			Description: "Compatible baseline comparison found no recall drop ≥0.05 or large p95 increase.",
			Evidence: []models.Evidence{
				{Kind: models.EvidenceFact, Source: "benchmark", Description: "fingerprint", Value: current.WorkloadFingerprint},
			},
			Risk: models.RiskNone,
		})
	}
	return out
}

func summaryAt(sums []models.RecallSummary, k int) models.RecallSummary {
	for _, s := range sums {
		if s.K == k {
			return s
		}
	}
	return models.RecallSummary{}
}
