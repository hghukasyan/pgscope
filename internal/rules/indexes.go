package rules

import (
	"context"
	"fmt"

	"github.com/hghukasyan/pgscope/pkg/models"
)

// InvalidIndexRule flags invalid indexes that need REINDEX.
type InvalidIndexRule struct{}

func (InvalidIndexRule) ID() string { return "IDX.INVALID" }

func (InvalidIndexRule) Evaluate(_ context.Context, snap models.Snapshot) ([]models.Finding, error) {
	if !snap.Indexes.Collected || len(snap.Indexes.Invalid) == 0 {
		return nil, nil
	}

	ev := []models.Evidence{
		{Kind: models.EvidenceFact, Source: "pg_index", Description: "Invalid indexes", Value: fmt.Sprintf("%d", len(snap.Indexes.Invalid))},
	}
	for i, idx := range snap.Indexes.Invalid {
		if i >= 10 {
			break
		}
		obj := fmt.Sprintf("%s.%s", idx.Schema, idx.Name)
		ev = append(ev, models.Evidence{
			Kind:        models.EvidenceFact,
			Source:      "pg_index",
			Description: fmt.Sprintf("Invalid index on %s.%s", idx.TableSchema, idx.TableName),
			Value:       obj,
		})
	}

	f := models.Finding{
		RuleID:              "IDX.INVALID",
		Severity:            models.SeverityCritical,
		Database:            snap.Instance.Database,
		ObservedAt:          snap.CollectedAt,
		Title:               "Invalid indexes present",
		Description:         fmt.Sprintf("%d index(es) have indisvalid = false and are not used by the planner.", len(snap.Indexes.Invalid)),
		Evidence:            ev,
		ProbableCause:       "CREATE INDEX CONCURRENTLY failed, or a prior REINDEX left the index invalid.",
		ConfidenceRationale: "indisvalid is a catalog fact. The failure cause requires checking logs/history.",
		Remediation:         "REINDEX (CONCURRENTLY when possible) or drop and recreate the invalid indexes during a maintenance window. PGScope will not execute DDL.",
		Risk:                models.RiskHigh,
		Validation:          "Confirm invalid index list is empty after maintenance with `pgscope scan`.",
	}
	return []models.Finding{f}, nil
}

// UnusedIndexObservationRule notes zero-scan non-unique indexes without recommending drops.
type UnusedIndexObservationRule struct{}

func (UnusedIndexObservationRule) ID() string { return "IDX.ZERO_SCANS" }

func (UnusedIndexObservationRule) Evaluate(_ context.Context, snap models.Snapshot) ([]models.Finding, error) {
	if !snap.Indexes.Collected || len(snap.Indexes.NeverUsed) == 0 {
		return nil, nil
	}

	ev := []models.Evidence{
		{Kind: models.EvidenceObservation, Source: "pg_stat_user_indexes", Description: "Non-unique indexes with zero scans", Value: fmt.Sprintf("%d", len(snap.Indexes.NeverUsed))},
	}
	if snap.Indexes.StatsReset != nil {
		ev = append(ev, models.Evidence{
			Kind: models.EvidenceFact, Source: "pg_stat_database",
			Description: "stats_reset", Value: snap.Indexes.StatsReset.UTC().Format("2006-01-02T15:04:05Z"),
		})
	}
	for i, idx := range snap.Indexes.NeverUsed {
		if i >= 5 {
			break
		}
		ev = append(ev, models.Evidence{
			Kind:        models.EvidenceFact,
			Source:      "pg_stat_user_indexes",
			Description: fmt.Sprintf("%s.%s on %s.%s size=%s am=%s", idx.Schema, idx.Name, idx.TableSchema, idx.TableName, idx.SizePretty, idx.AccessMethod),
			Value:       "idx_scan=0",
		})
	}

	f := models.Finding{
		RuleID:              "IDX.ZERO_SCANS",
		Severity:            models.SeverityInfo,
		Database:            snap.Instance.Database,
		ObservedAt:          snap.CollectedAt,
		Title:               "Indexes with zero scans observed",
		Description:         "Some non-primary, non-unique, non-constraint indexes have zero recorded scans. This alone is not sufficient evidence to drop them.",
		Evidence:            ev,
		ProbableCause:       "Indexes may be unused, used only rarely (stats recently reset), required for uncommon admin queries, or used via constraint paths not reflected in idx_scan.",
		ConfidenceRationale: "Zero scans is a fact for the current stats window. Drop safety requires workload history, FK patterns, and uniqueness requirements — PGScope never auto-recommends DROP INDEX.",
		Remediation:         "Track these indexes across a full business cycle before considering removal. Never drop based solely on idx_scan = 0.",
		Risk:                models.RiskHigh,
		Validation:          "Compare idx_scan over a longer observation window after a known stats_reset baseline.",
	}
	return []models.Finding{f}, nil
}

// DuplicateIndexRule flags structurally identical indexes.
type DuplicateIndexRule struct{}

func (DuplicateIndexRule) ID() string { return "IDX.DUPLICATE" }

func (DuplicateIndexRule) Evaluate(_ context.Context, snap models.Snapshot) ([]models.Finding, error) {
	if !snap.Indexes.Collected || len(snap.Indexes.Duplicates) == 0 {
		return nil, nil
	}
	var findings []models.Finding
	for _, g := range snap.Indexes.Duplicates {
		ev := []models.Evidence{
			{Kind: models.EvidenceFact, Source: "pg_index", Description: "table", Value: g.TableSchema + "." + g.TableName},
			{Kind: models.EvidenceFact, Source: "derived", Description: "reason", Value: g.Reason},
		}
		names := make([]string, 0, len(g.Indexes))
		for _, idx := range g.Indexes {
			names = append(names, idx.Schema+"."+idx.Name)
			ev = append(ev, models.Evidence{
				Kind: models.EvidenceFact, Source: "pg_index",
				Description: idx.Schema + "." + idx.Name,
				Value:       idx.Definition,
			})
		}
		findings = append(findings, models.Finding{
			RuleID:              "IDX.DUPLICATE",
			Severity:            models.SeverityWarning,
			Database:            snap.Instance.Database,
			Object:              g.TableSchema + "." + g.TableName,
			ObservedAt:          snap.CollectedAt,
			Title:               "Structurally identical indexes detected",
			Description:         fmt.Sprintf("Indexes %v appear structurally identical on %s.%s.", names, g.TableSchema, g.TableName),
			Evidence:            ev,
			ProbableCause:       "Repeated migrations or overlapping CREATE INDEX statements.",
			ConfidenceRationale: "Structural identity includes AM, columns/expressions, includes, predicate, opclasses, collations, sort options, and uniqueness.",
			Remediation:         "Manually review and consider dropping one duplicate after confirming no dependency. PGScope will not drop indexes.",
			Risk:                models.RiskHigh,
			Validation:          "Confirm only one remains and query plans are unchanged.",
		})
	}
	return findings, nil
}

// RedundantBTreeIndexRule notes possible B-tree prefix redundancy without DROP advice.
type RedundantBTreeIndexRule struct{}

func (RedundantBTreeIndexRule) ID() string { return "IDX.REDUNDANT_BTREE" }

func (RedundantBTreeIndexRule) Evaluate(_ context.Context, snap models.Snapshot) ([]models.Finding, error) {
	if !snap.Indexes.Collected || len(snap.Indexes.Redundant) == 0 {
		return nil, nil
	}
	var findings []models.Finding
	for _, g := range snap.Indexes.Redundant {
		ev := []models.Evidence{
			{Kind: models.EvidenceObservation, Source: "derived", Description: "reason", Value: g.Reason},
		}
		for _, idx := range g.Indexes {
			ev = append(ev, models.Evidence{
				Kind: models.EvidenceFact, Source: "pg_index",
				Description: idx.Schema + "." + idx.Name,
				Value:       fmt.Sprintf("cols=%v unique=%t predicate=%q", idx.IndexColumns, idx.IsUnique, idx.Predicate),
			})
		}
		findings = append(findings, models.Finding{
			RuleID:              "IDX.REDUNDANT_BTREE",
			Severity:            models.SeverityInfo,
			Database:            snap.Instance.Database,
			Object:              g.TableSchema + "." + g.TableName,
			ObservedAt:          snap.CollectedAt,
			Title:               "Possible redundant B-tree index prefix",
			Description:         g.Reason + " A leading-column B-tree index is not always interchangeable — uniqueness, NULL ordering, collations, and partial predicates were checked.",
			Evidence:            ev,
			ProbableCause:       "Overlapping index designs where a wider index can serve queries that use the narrower leading columns.",
			ConfidenceRationale: "Heuristic prefix analysis; expression indexes and differing predicates are excluded. Never auto-drop.",
			Remediation:         "Validate with workload history and EXPLAIN before considering removal of the narrower index.",
			Risk:                models.RiskHigh,
			Validation:          "Confirm plans and latency after any change in a lab; keep the unique/constraint-backed index.",
		})
	}
	return findings, nil
}

// LargeLowUsageIndexRule flags large indexes with very low scan counts plus size evidence.
type LargeLowUsageIndexRule struct{}

func (LargeLowUsageIndexRule) ID() string { return "IDX.LARGE_LOW_USAGE" }

func (LargeLowUsageIndexRule) Evaluate(_ context.Context, snap models.Snapshot) ([]models.Finding, error) {
	if !snap.Indexes.Collected {
		return nil, nil
	}
	var findings []models.Finding
	for _, idx := range snap.Indexes.Indexes {
		if idx.IsPrimary || idx.IsUnique || idx.ConstraintName != "" || !idx.IsValid {
			continue
		}
		if idx.SizeBytes < 64*1024*1024 { // 64MB
			continue
		}
		if idx.IndexScans > 10 {
			continue
		}
		findings = append(findings, models.Finding{
			RuleID:     "IDX.LARGE_LOW_USAGE",
			Severity:   models.SeverityInfo,
			Database:   snap.Instance.Database,
			Object:     idx.Schema + "." + idx.Name,
			ObservedAt: snap.CollectedAt,
			Title:      "Large index with low scan count",
			Description: fmt.Sprintf(
				"%s.%s is %s with idx_scan=%d. Size plus low usage is a review signal, not a drop recommendation.",
				idx.Schema, idx.Name, idx.SizePretty, idx.IndexScans,
			),
			Evidence: []models.Evidence{
				{Kind: models.EvidenceFact, Source: "pg_relation_size", Description: "size_bytes", Value: fmt.Sprintf("%d", idx.SizeBytes)},
				{Kind: models.EvidenceFact, Source: "pg_stat_user_indexes", Description: "idx_scan", Value: fmt.Sprintf("%d", idx.IndexScans)},
				{Kind: models.EvidenceFact, Source: "pg_am", Description: "access_method", Value: idx.AccessMethod},
			},
			ProbableCause:       "Index created for a rare query, leftover from schema evolution, or stats window too short.",
			ConfidenceRationale: "Requires both large size and low scans; constraint/unique indexes excluded.",
			Remediation:         "Monitor across a full business cycle before any drop consideration.",
			Risk:                models.RiskHigh,
			Validation:          "Track idx_scan and write overhead over time.",
		})
	}
	return findings, nil
}
