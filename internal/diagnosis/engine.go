// Package diagnosis runs diagnostic rules against a snapshot.
package diagnosis

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/hghukasyan/pgscope/internal/rules"
	"github.com/hghukasyan/pgscope/pkg/models"
)

// Engine evaluates a set of rules against snapshots.
type Engine struct {
	rules []rules.Rule
}

// New creates an Engine with the given rules (defaults to Phase-2 rules).
func New(r ...rules.Rule) *Engine {
	if len(r) == 0 {
		r = rules.DefaultRules()
	}
	return &Engine{rules: r}
}

// Analyze evaluates all rules and builds a ScanResult.
func (e *Engine) Analyze(ctx context.Context, snap models.Snapshot) (models.ScanResult, error) {
	var findings []models.Finding
	for _, rule := range e.rules {
		select {
		case <-ctx.Done():
			return models.ScanResult{}, ctx.Err()
		default:
		}
		fs, err := rule.Evaluate(ctx, snap)
		if err != nil {
			return models.ScanResult{}, fmt.Errorf("rule %s: %w", rule.ID(), err)
		}
		findings = append(findings, fs...)
	}

	sort.SliceStable(findings, func(i, j int) bool {
		if severityRank(findings[i].Severity) != severityRank(findings[j].Severity) {
			return severityRank(findings[i].Severity) > severityRank(findings[j].Severity)
		}
		return findings[i].RuleID < findings[j].RuleID
	})

	recs := recommendationsFrom(findings)
	status, summary := summarize(snap, findings)

	return models.ScanResult{
		Snapshot:        snap,
		Findings:        findings,
		Recommendations: recs,
		OverallStatus:   status,
		Summary:         summary,
	}, nil
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

func recommendationsFrom(findings []models.Finding) []models.Recommendation {
	var out []models.Recommendation
	for _, f := range findings {
		if f.Remediation == "" {
			continue
		}
		out = append(out, models.Recommendation{
			ID:          "REC." + f.RuleID,
			FindingRefs: []string{f.RuleID},
			Title:       f.Title,
			Summary:     f.Remediation,
			Risk:        f.Risk,
			Validation:  f.Validation,
		})
	}
	return out
}

func summarize(snap models.Snapshot, findings []models.Finding) (string, string) {
	var crit, warn, info int
	for _, f := range findings {
		switch f.Severity {
		case models.SeverityCritical:
			crit++
		case models.SeverityWarning:
			warn++
		default:
			info++
		}
	}
	status := "ok"
	switch {
	case crit > 0:
		status = "critical"
	case warn > 0:
		status = "warning"
	case info > 0:
		status = "info"
	}

	parts := []string{
		fmt.Sprintf("PostgreSQL %d database %q", snap.Instance.MajorVersion, snap.Instance.Database),
		fmt.Sprintf("%d finding(s) (%d critical, %d warning, %d info)", len(findings), crit, warn, info),
	}
	if n := len(snap.Limitations); n > 0 {
		parts = append(parts, fmt.Sprintf("%d collection limitation(s)", n))
	}
	if snap.Connections.Collected {
		parts = append(parts, fmt.Sprintf("connections %d/%d (%.0f%%)",
			snap.Connections.Total, snap.Connections.MaxConnections, snap.Connections.UtilizationPct))
	}
	return status, strings.Join(parts, "; ")
}
