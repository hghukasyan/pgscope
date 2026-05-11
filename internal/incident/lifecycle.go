// Package incident implements incident lifecycle, correlation, and root-cause hypotheses.
package incident

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/hghukasyan/pgscope/internal/config"
	"github.com/hghukasyan/pgscope/internal/history"
	"github.com/hghukasyan/pgscope/pkg/models"
)

// Engine manages incident creation, deduplication, and resolution against a Store.
type Engine struct {
	store history.Store
	cfg   config.IncidentsConfig
	corr  config.CorrelationConfig
}

// New creates an incident Engine.
func New(store history.Store, incCfg config.IncidentsConfig, corrCfg config.CorrelationConfig) *Engine {
	if incCfg.DeduplicationWindow <= 0 {
		incCfg.DeduplicationWindow = 10 * time.Minute
	}
	if incCfg.AutoResolveAfter <= 0 {
		incCfg.AutoResolveAfter = 15 * time.Minute
	}
	if corrCfg.MaxEvidenceNodes <= 0 {
		corrCfg.MaxEvidenceNodes = 1000
	}
	if corrCfg.MaxHypothesesPerIncident <= 0 {
		corrCfg.MaxHypothesesPerIncident = 10
	}
	return &Engine{store: store, cfg: incCfg, corr: corrCfg}
}

// ProcessRegressions upserts incidents for regressions and returns affected incidents.
func (e *Engine) ProcessRegressions(ctx context.Context, regs []models.Regression, findings []models.Finding, now time.Time) ([]models.Incident, error) {
	if !e.cfg.Enabled || e.store == nil {
		return nil, nil
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}

	// Group regressions into candidate fingerprints.
	groups := groupRegressions(regs)
	var out []models.Incident
	for fp, group := range groups {
		existing, found, err := e.store.FindOpenByFingerprint(ctx, fp)
		if err != nil {
			return out, err
		}
		primary := group[0]
		if found {
			existing.LastObservedAt = now
			if severityRank(primary.Severity) > severityRank(existing.Severity) {
				existing.Severity = primary.Severity
			}
			for _, r := range group {
				existing.RegressionIDs = appendUnique(existing.RegressionIDs, r.ID)
			}
			existing.Status = models.IncidentInvestigating
			if err := e.store.SaveIncident(ctx, existing); err != nil {
				return out, err
			}
			out = append(out, existing)
			continue
		}

		inc := models.Incident{
			ID:             newIncidentID(now),
			InstanceKey:    primary.InstanceKey,
			Database:       primary.Database,
			Title:          titleFor(group),
			Status:         models.IncidentOpen,
			Severity:       maxSeverity(group),
			StartedAt:      now,
			LastObservedAt: now,
			Fingerprint:    fp,
			TriggerMetric:  primary.MetricName,
			Summary:        fmt.Sprintf("Detected %d related regression(s) around %s", len(group), primary.MetricName),
		}
		for _, r := range group {
			inc.RegressionIDs = append(inc.RegressionIDs, r.ID)
			if qid := r.Dimensions["query_id"]; qid != "" {
				inc.AffectedQueries = appendUnique(inc.AffectedQueries, qid)
			}
			if rel := r.Dimensions["relation"]; rel != "" {
				inc.AffectedObjects = appendUnique(inc.AffectedObjects, rel)
			}
			if idx := r.Dimensions["index"]; idx != "" {
				inc.AffectedObjects = appendUnique(inc.AffectedObjects, idx)
			}
		}
		for _, f := range findings {
			if f.Severity == models.SeverityWarning || f.Severity == models.SeverityCritical {
				inc.FindingRefs = appendUnique(inc.FindingRefs, f.RuleID)
			}
		}
		if err := e.store.SaveIncident(ctx, inc); err != nil {
			return out, err
		}
		// Attach incident id onto regressions when saving later.
		for i := range group {
			group[i].IncidentID = inc.ID
		}
		_ = e.store.SaveRegressions(ctx, group)

		hyps := EvaluateHypotheses(inc, group, findings, e.corr.MaxHypothesesPerIncident)
		if err := e.store.SaveHypotheses(ctx, hyps); err != nil {
			return out, err
		}
		edges := Correlate(inc, group, findings, e.corr.MaxEvidenceNodes)
		if err := e.store.SaveEdges(ctx, inc.ID, edges); err != nil {
			return out, err
		}
		tl := BuildTimeline(inc, group, findings, nil)
		if err := e.store.SaveTimeline(ctx, inc.ID, tl); err != nil {
			return out, err
		}
		out = append(out, inc)
	}

	if err := e.AutoResolve(ctx, now); err != nil {
		return out, err
	}
	return out, nil
}

// Resolve marks an incident resolved in local history only.
func (e *Engine) Resolve(ctx context.Context, id string, now time.Time) (models.Incident, error) {
	inc, err := e.store.GetIncident(ctx, id)
	if err != nil {
		return inc, err
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	inc.Status = models.IncidentResolved
	inc.ResolvedAt = &now
	inc.LastObservedAt = now
	return inc, e.store.SaveIncident(ctx, inc)
}

// AutoResolve closes open incidents that have not been observed recently.
func (e *Engine) AutoResolve(ctx context.Context, now time.Time) error {
	incs, err := e.store.ListIncidents(ctx, "", 500)
	if err != nil {
		return err
	}
	for _, inc := range incs {
		if inc.Status == models.IncidentResolved {
			continue
		}
		if now.Sub(inc.LastObservedAt) >= e.cfg.AutoResolveAfter {
			t := now
			inc.Status = models.IncidentResolved
			inc.ResolvedAt = &t
			if err := e.store.SaveIncident(ctx, inc); err != nil {
				return err
			}
		}
	}
	return nil
}

func groupRegressions(regs []models.Regression) map[string][]models.Regression {
	out := map[string][]models.Regression{}
	for _, r := range regs {
		fp := fingerprint(r)
		out[fp] = append(out[fp], r)
	}
	return out
}

func fingerprint(r models.Regression) string {
	parts := []string{r.InstanceKey, r.Database, family(r.MetricName)}
	if q := r.Dimensions["query_id"]; q != "" {
		parts = append(parts, "q:"+q)
	}
	if rel := r.Dimensions["relation"]; rel != "" {
		parts = append(parts, "rel:"+rel)
	}
	if slot := r.Dimensions["slot"]; slot != "" {
		parts = append(parts, "slot:"+slot)
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "|")))
	return hex.EncodeToString(sum[:8])
}

func family(metric string) string {
	switch {
	case strings.HasPrefix(metric, "locks."):
		return "locks"
	case strings.HasPrefix(metric, "connections."):
		return "connections"
	case strings.HasPrefix(metric, "transactions."):
		return "transactions"
	case strings.HasPrefix(metric, "query."):
		return "query"
	case strings.HasPrefix(metric, "vacuum."):
		return "vacuum"
	case strings.HasPrefix(metric, "wal."):
		return "wal"
	case strings.HasPrefix(metric, "replication."):
		return "replication"
	case strings.HasPrefix(metric, "vector."):
		return "vector"
	default:
		return metric
	}
}

func titleFor(regs []models.Regression) string {
	if len(regs) == 0 {
		return "Performance anomaly"
	}
	r := regs[0]
	pct := r.RelativeChange * 100
	return fmt.Sprintf("%s changed %+.0f%%", r.MetricName, pct)
}

func maxSeverity(regs []models.Regression) models.Severity {
	sev := models.SeverityInfo
	for _, r := range regs {
		if severityRank(r.Severity) > severityRank(sev) {
			sev = r.Severity
		}
	}
	return sev
}

func severityRank(s models.Severity) int {
	switch s {
	case models.SeverityCritical:
		return 3
	case models.SeverityWarning:
		return 2
	case models.SeverityInfo:
		return 1
	default:
		return 0
	}
}

func appendUnique(list []string, v string) []string {
	for _, x := range list {
		if x == v {
			return list
		}
	}
	return append(list, v)
}

func newIncidentID(now time.Time) string {
	var b [3]byte
	_, _ = rand.Read(b[:])
	return fmt.Sprintf("PG-%d-%s", now.Year(), strings.ToUpper(hex.EncodeToString(b[:])))
}

// Investigate builds a full report for an incident.
func (e *Engine) Investigate(ctx context.Context, incidentID string) (models.InvestigationReport, error) {
	inc, err := e.store.GetIncident(ctx, incidentID)
	if err != nil {
		return models.InvestigationReport{}, err
	}
	hyps, _ := e.store.ListHypotheses(ctx, incidentID)
	tl, _ := e.store.ListTimeline(ctx, incidentID)
	edges, _ := e.store.ListEdges(ctx, incidentID)

	var regs []models.Regression
	allRegs, _ := e.store.ListRegressions(ctx, inc.StartedAt.Add(-time.Hour), inc.LastObservedAt.Add(time.Hour), 500)
	idset := map[string]struct{}{}
	for _, id := range inc.RegressionIDs {
		idset[id] = struct{}{}
	}
	for _, r := range allRegs {
		if _, ok := idset[r.ID]; ok || r.IncidentID == incidentID {
			regs = append(regs, r)
		}
	}
	sort.Slice(hyps, func(i, j int) bool { return hyps[i].Rank < hyps[j].Rank })

	rep := models.InvestigationReport{
		Incident:    inc,
		Regressions: regs,
		Hypotheses:  hyps,
		Timeline:    tl,
		Edges:       edges,
		GeneratedAt: time.Now().UTC(),
	}
	if len(hyps) == 0 {
		rep.Limitations = append(rep.Limitations, models.Limitation{
			Area: "root_cause", Reason: "No hypotheses persisted", Impact: "Manual investigation required",
		})
	}
	return rep, nil
}
