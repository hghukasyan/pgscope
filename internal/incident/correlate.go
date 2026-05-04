package incident

import (
	"fmt"
	"strings"

	"github.com/hghukasyan/pgscope/pkg/models"
)

// Correlate builds a bounded evidence graph for an incident.
// Edges are typed relationships; CAUSES is never asserted as proven fact.
func Correlate(inc models.Incident, regs []models.Regression, findings []models.Finding, maxNodes int) []models.EvidenceEdge {
	if maxNodes <= 0 {
		maxNodes = 1000
	}
	nodes := map[string]struct{}{}
	add := func(id string) bool {
		if _, ok := nodes[id]; ok {
			return true
		}
		if len(nodes) >= maxNodes {
			return false
		}
		nodes[id] = struct{}{}
		return true
	}

	incNode := "incident:" + inc.ID
	add(incNode)

	var edges []models.EvidenceEdge
	link := func(from, to string, t models.EdgeType, note string) {
		if !add(from) || !add(to) {
			return
		}
		edges = append(edges, models.EvidenceEdge{From: from, To: to, Type: t, Note: note})
	}

	for _, r := range regs {
		rn := "regression:" + r.ID
		link(incNode, rn, models.EdgeAffects, r.MetricName)
		fam := family(r.MetricName)
		fn := "family:" + fam
		link(rn, fn, models.EdgeCorrelatesWith, "metric family")

		if q := r.Dimensions["query_id"]; q != "" {
			link(rn, "query:"+q, models.EdgeAffects, "query fingerprint")
		}
		if rel := r.Dimensions["relation"]; rel != "" {
			link(rn, "relation:"+rel, models.EdgeAffects, "relation")
		}
	}

	// Cross-signal hints from co-occurring metric families (not causal claims).
	families := map[string]bool{}
	for _, r := range regs {
		families[family(r.MetricName)] = true
	}
	if families["locks"] && families["query"] {
		link("family:locks", "family:query", models.EdgeCorrelatesWith, "lock waits co-occur with query latency")
	}
	if families["transactions"] && families["vacuum"] {
		link("family:transactions", "family:vacuum", models.EdgeCorrelatesWith, "long transactions co-occur with dead tuple growth")
	}
	if families["wal"] && families["replication"] {
		link("family:wal", "family:replication", models.EdgeCorrelatesWith, "WAL growth co-occurs with replication lag/slots")
	}
	if families["connections"] && families["query"] {
		link("family:connections", "family:query", models.EdgeCorrelatesWith, "connection saturation co-occurs with query latency")
	}

	for _, f := range findings {
		fn := "finding:" + f.RuleID
		if !add(fn) {
			break
		}
		note := f.Title
		if len(note) > 80 {
			note = note[:80]
		}
		link(incNode, fn, models.EdgeSupports, note)
		if strings.Contains(strings.ToLower(f.RuleID), "lock") || strings.Contains(strings.ToLower(f.Title), "lock") {
			link(fn, "family:locks", models.EdgeSupports, "lock-related finding")
		}
		if strings.Contains(strings.ToLower(f.RuleID), "vacuum") || strings.Contains(strings.ToLower(f.Title), "vacuum") {
			link(fn, "family:vacuum", models.EdgeSupports, "vacuum-related finding")
		}
	}

	// Bound edge list roughly with nodes.
	maxEdges := maxNodes * 2
	if len(edges) > maxEdges {
		edges = edges[:maxEdges]
	}
	return edges
}

// BuildTimeline reconstructs observed events only (no fabricated precise timing).
func BuildTimeline(inc models.Incident, regs []models.Regression, findings []models.Finding, extras []models.TimelineEvent) []models.TimelineEvent {
	var events []models.TimelineEvent
	events = append(events, models.TimelineEvent{
		At: inc.StartedAt, Kind: "status", Summary: fmt.Sprintf("Incident opened: %s", inc.Title),
		SourceID: inc.ID, Observed: true,
	})
	for _, r := range regs {
		events = append(events, models.TimelineEvent{
			At: r.DetectedAt, Kind: "regression",
			Summary: fmt.Sprintf("%s: baseline=%.4g current=%.4g (%+.0f%%)",
				r.MetricName, r.BaselineValue, r.CurrentValue, r.RelativeChange*100),
			SourceID: r.ID, Observed: true,
			Details: fmt.Sprintf("window %s→%s samples=%d reliability=%s",
				r.WindowStart.Format("15:04:05"), r.WindowEnd.Format("15:04:05"), r.SampleCount, r.Reliability),
		})
		if !r.BaselineStart.IsZero() {
			events = append(events, models.TimelineEvent{
				At: r.BaselineEnd, Kind: "note",
				Summary:  fmt.Sprintf("Baseline window for %s ended (median=%.4g)", r.MetricName, r.BaselineValue),
				SourceID: r.ID, Observed: false,
				Details: "Inferred from baseline sample window, not an instantaneous event",
			})
		}
	}
	for _, f := range findings {
		at := f.ObservedAt
		if at.IsZero() {
			at = inc.StartedAt
		}
		events = append(events, models.TimelineEvent{
			At: at, Kind: "finding", Summary: f.Title, SourceID: f.RuleID, Observed: !f.ObservedAt.IsZero(),
		})
	}
	events = append(events, extras...)
	if inc.ResolvedAt != nil {
		events = append(events, models.TimelineEvent{
			At: *inc.ResolvedAt, Kind: "status", Summary: "Incident resolved (local history)",
			SourceID: inc.ID, Observed: true,
		})
	}
	// Sort by time
	for i := 0; i < len(events); i++ {
		for j := i + 1; j < len(events); j++ {
			if events[j].At.Before(events[i].At) {
				events[i], events[j] = events[j], events[i]
			}
		}
	}
	return events
}
