package incident

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/hghukasyan/pgscope/pkg/models"
)

type hypRule struct {
	Type        string
	Title       string
	Description string
	Match       func(regs []models.Regression, findings []models.Finding) (support, contradict []models.Evidence, missing []string)
	Validation  string
	Remediation string
}

// EvaluateHypotheses produces competing root-cause explanations with explainable confidence.
func EvaluateHypotheses(inc models.Incident, regs []models.Regression, findings []models.Finding, max int) []models.Hypothesis {
	if max <= 0 {
		max = 10
	}
	rules := hypothesisRules()
	var out []models.Hypothesis
	for _, rule := range rules {
		support, contradict, missing := rule.Match(regs, findings)
		conf, rationale := scoreConfidence(support, contradict, missing)
		if conf == models.ConfidenceInsufficient && len(support) == 0 {
			continue
		}
		h := models.Hypothesis{
			ID:                    newHypID(),
			IncidentID:            inc.ID,
			Type:                  rule.Type,
			Title:                 rule.Title,
			Description:           rule.Description,
			SupportingEvidence:    support,
			ContradictingEvidence: contradict,
			MissingEvidence:       missing,
			Confidence:            conf,
			ConfidenceRationale:   rationale,
			Validation:            rule.Validation,
			Remediation:           rule.Remediation,
		}
		out = append(out, h)
	}
	// Rank: strong > moderate > weak > insufficient; then by support count.
	rankOrder := map[models.HypothesisConfidence]int{
		models.ConfidenceStrong: 0, models.ConfidenceModerate: 1,
		models.ConfidenceWeak: 2, models.ConfidenceInsufficient: 3,
	}
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			ri, rj := rankOrder[out[i].Confidence], rankOrder[out[j].Confidence]
			if rj < ri || (rj == ri && len(out[j].SupportingEvidence) > len(out[i].SupportingEvidence)) {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	if len(out) > max {
		out = out[:max]
	}
	for i := range out {
		out[i].Rank = i + 1
		if i > 0 {
			out[i].ConfidenceRationale += fmt.Sprintf(" Ranked below #%d (%s) due to weaker or fewer supporting signals.",
				out[0].Rank, out[0].Type)
		}
	}
	if len(out) == 0 {
		out = append(out, models.Hypothesis{
			ID: newHypID(), IncidentID: inc.ID, Type: "unknown", Title: "Unknown root cause",
			Description: "Insufficient correlated evidence to support a specific root-cause hypothesis.",
			Confidence: models.ConfidenceInsufficient,
			ConfidenceRationale: "No hypothesis rule matched supporting evidence.",
			Rank: 1,
			MissingEvidence: []string{"sustained metric regressions", "related diagnostic findings"},
			Validation: "Collect additional snapshots covering locks, transactions, queries, WAL, and replication during the anomaly window.",
		})
	}
	return out
}

func scoreConfidence(support, contradict []models.Evidence, missing []string) (models.HypothesisConfidence, string) {
	s, c := len(support), len(contradict)
	switch {
	case s >= 3 && c == 0:
		return models.ConfidenceStrong, fmt.Sprintf("%d supporting evidence items and no contradictions.", s)
	case s >= 2 && c == 0:
		return models.ConfidenceModerate, fmt.Sprintf("%d supporting evidence items and no contradictions.", s)
	case s >= 2 && c > 0:
		return models.ConfidenceWeak, fmt.Sprintf("%d supporting and %d contradicting evidence items.", s, c)
	case s == 1:
		return models.ConfidenceWeak, "Only one supporting evidence item."
	default:
		msg := "Insufficient supporting evidence."
		if len(missing) > 0 {
			msg += " Missing: " + strings.Join(missing, "; ") + "."
		}
		return models.ConfidenceInsufficient, msg
	}
}

func hypothesisRules() []hypRule {
	hasFamily := func(regs []models.Regression, fam string) bool {
		for _, r := range regs {
			if family(r.MetricName) == fam {
				return true
			}
		}
		return false
	}
	metricEvidence := func(regs []models.Regression, prefix string) []models.Evidence {
		var out []models.Evidence
		for _, r := range regs {
			if strings.HasPrefix(r.MetricName, prefix) {
				out = append(out, models.Evidence{
					Kind: models.EvidenceFact, Source: "regression:" + r.ID,
					Description: r.MetricName,
					Value: fmt.Sprintf("baseline=%.4g current=%.4g (%+.0f%%)", r.BaselineValue, r.CurrentValue, r.RelativeChange*100),
				})
			}
		}
		return out
	}
	findingEvidence := func(findings []models.Finding, needles ...string) []models.Evidence {
		var out []models.Evidence
		for _, f := range findings {
			blob := strings.ToLower(f.RuleID + " " + f.Title + " " + f.Description)
			for _, n := range needles {
				if strings.Contains(blob, strings.ToLower(n)) {
					out = append(out, models.Evidence{
						Kind: models.EvidenceObservation, Source: f.RuleID, Description: f.Title,
					})
					break
				}
			}
		}
		return out
	}

	return []hypRule{
		{
			Type: "lock_contention", Title: "Lock contention",
			Description: "Long-held locks or blocking chains may be elevating wait times and query latency.",
			Match: func(regs []models.Regression, findings []models.Finding) ([]models.Evidence, []models.Evidence, []string) {
				sup := append(metricEvidence(regs, "locks."), findingEvidence(findings, "lock", "block")...)
				var contra []models.Evidence
				if !hasFamily(regs, "locks") && len(findingEvidence(findings, "lock")) == 0 {
					return nil, nil, []string{"lock wait metrics", "blocking chain findings"}
				}
				if hasFamily(regs, "locks") && !hasFamily(regs, "query") && len(findingEvidence(findings, "latency", "query")) == 0 {
					contra = append(contra, models.Evidence{Kind: models.EvidenceObservation, Source: "correlation", Description: "Lock waits without concurrent query latency signal"})
				}
				missing := []string{}
				if len(findingEvidence(findings, "block")) == 0 {
					missing = append(missing, "root blocker session details")
				}
				return uniqueEvidence(sup), contra, missing
			},
			Validation:  "Inspect blocking chains; confirm query latency recovers after the root blocker ends.",
			Remediation: "Review application transaction boundaries; avoid long critical sections holding locks.",
		},
		{
			Type: "long_transaction", Title: "Long-running transaction",
			Description: "Long-lived transactions may hold snapshots/locks and prevent cleanup or progress.",
			Match: func(regs []models.Regression, findings []models.Finding) ([]models.Evidence, []models.Evidence, []string) {
				sup := append(metricEvidence(regs, "transactions."), findingEvidence(findings, "transaction", "idle in transaction", "xid")...)
				if len(sup) == 0 {
					return nil, nil, []string{"transaction age metrics"}
				}
				return uniqueEvidence(sup), nil, nil
			},
			Validation:  "Identify oldest xact from pg_stat_activity; verify duration overlaps the anomaly window.",
			Remediation: "End or redesign long transactions; keep transactions short and avoid idle-in-transaction.",
		},
		{
			Type: "connection_saturation", Title: "Connection saturation",
			Description: "Connection utilization approaching max_connections can queue work and amplify latency.",
			Match: func(regs []models.Regression, findings []models.Finding) ([]models.Evidence, []models.Evidence, []string) {
				sup := append(metricEvidence(regs, "connections."), findingEvidence(findings, "connection", "max_connections")...)
				if len(sup) == 0 {
					return nil, nil, []string{"connection utilization metrics"}
				}
				return uniqueEvidence(sup), nil, nil
			},
			Validation:  "Compare connection.utilization_pct to max_connections during the evaluation window.",
			Remediation: "Use pooling; reduce idle sessions; raise max_connections only with capacity planning.",
		},
		{
			Type: "planner_regression", Title: "Query planner regression",
			Description: "Plan changes or cardinality misestimates may increase latency and physical reads.",
			Match: func(regs []models.Regression, findings []models.Finding) ([]models.Evidence, []models.Evidence, []string) {
				sup := append(metricEvidence(regs, "query."), findingEvidence(findings, "seq scan", "rows", "plan", "estimate")...)
				var contra []models.Evidence
				if hasFamily(regs, "locks") {
					contra = append(contra, models.Evidence{Kind: models.EvidenceObservation, Source: "correlation", Description: "Lock contention also present; may explain latency without plan change"})
				}
				if len(sup) == 0 {
					return nil, nil, []string{"query latency regressions", "EXPLAIN compare"}
				}
				missing := []string{}
				if len(findingEvidence(findings, "plan", "estimate")) == 0 {
					missing = append(missing, "execution plan fingerprint change")
				}
				return uniqueEvidence(sup), contra, missing
			},
			Validation:  "EXPLAIN (ANALYZE, BUFFERS) the affected query fingerprint before/after; compare plan structure and row estimates.",
			Remediation: "Refresh statistics; review expression indexes/predicates; avoid disabling planner features casually.",
		},
		{
			Type: "autovacuum_interference", Title: "Autovacuum / snapshot interference",
			Description: "Dead tuples may accumulate when long snapshots prevent vacuum cleanup.",
			Match: func(regs []models.Regression, findings []models.Finding) ([]models.Evidence, []models.Evidence, []string) {
				sup := append(metricEvidence(regs, "vacuum."), findingEvidence(findings, "vacuum", "dead tuple", "autovacuum")...)
				if hasFamily(regs, "transactions") {
					sup = append(sup, models.Evidence{Kind: models.EvidenceObservation, Source: "correlation", Description: "Transaction anomalies co-occur with vacuum metrics"})
				}
				if len(sup) == 0 {
					return nil, nil, []string{"dead tuple metrics"}
				}
				return uniqueEvidence(sup), nil, nil
			},
			Validation:  "Check n_dead_tup trends and whether vacuum progresses while old snapshots exist.",
			Remediation: "Resolve long transactions; tune autovacuum thresholds; avoid disabling autovacuum.",
		},
		{
			Type: "wal_retention", Title: "WAL retention pressure",
			Description: "Elevated WAL generation or inactive slots may retain WAL and pressure disk.",
			Match: func(regs []models.Regression, findings []models.Finding) ([]models.Evidence, []models.Evidence, []string) {
				sup := append(metricEvidence(regs, "wal."), findingEvidence(findings, "wal", "checkpoint", "slot")...)
				sup = append(sup, metricEvidence(regs, "replication.slot_retained")...)
				if len(sup) == 0 {
					return nil, nil, []string{"WAL byte rates", "replication slot retention"}
				}
				return uniqueEvidence(sup), nil, nil
			},
			Validation:  "Inspect pg_replication_slots retained WAL and pg_stat_wal rates across the incident window.",
			Remediation: "Drop unused slots; fix stuck consumers; review burst write workloads.",
		},
		{
			Type: "replication_lag", Title: "Replication lag",
			Description: "Replica apply/send lag increased relative to baseline.",
			Match: func(regs []models.Regression, findings []models.Finding) ([]models.Evidence, []models.Evidence, []string) {
				sup := append(metricEvidence(regs, "replication."), findingEvidence(findings, "replication", "lag", "standby")...)
				if len(sup) == 0 {
					return nil, nil, []string{"replication lag metrics"}
				}
				return uniqueEvidence(sup), nil, nil
			},
			Validation:  "Compare byte_lag_replay across primary and standby during the evaluation window.",
			Remediation: "Check network, apply rate, and long transactions on standby; review sync/async configuration.",
		},
		{
			Type: "vector_config_regression", Title: "Vector search configuration regression",
			Description: "Comparable pgvector benchmark recall/latency degraded versus prior compatible runs.",
			Match: func(regs []models.Regression, findings []models.Finding) ([]models.Evidence, []models.Evidence, []string) {
				sup := append(metricEvidence(regs, "vector."), findingEvidence(findings, "vector", "recall", "hnsw", "ivfflat")...)
				if len(sup) == 0 {
					return nil, nil, []string{"comparable vector benchmark summaries"}
				}
				return uniqueEvidence(sup), nil, []string{"confirm workload fingerprint compatibility"}
			},
			Validation:  "Re-run the same workload fingerprint; compare recall@k and p95 only against compatible configs.",
			Remediation: "Review ef_search/probes, filters, and index rebuild parameters; do not assume metadata health equals recall health.",
		},
	}
}

func uniqueEvidence(in []models.Evidence) []models.Evidence {
	seen := map[string]bool{}
	var out []models.Evidence
	for _, e := range in {
		k := e.Source + "|" + e.Description + "|" + e.Value
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, e)
	}
	return out
}

func newHypID() string {
	var b [5]byte
	_, _ = rand.Read(b[:])
	return "hyp-" + hex.EncodeToString(b[:])
}
