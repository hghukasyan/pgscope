package rules

import (
	"context"
	"fmt"

	"github.com/hghukasyan/pgscope/pkg/models"
)

// QueryHighTotalTimeRule flags statements with high cumulative execution cost
// relative to the rest of the collected workload (never on a single metric alone).
type QueryHighTotalTimeRule struct{}

func (QueryHighTotalTimeRule) ID() string { return "QUERY.HIGH_TOTAL_TIME" }

func (QueryHighTotalTimeRule) Evaluate(_ context.Context, snap models.Snapshot) ([]models.Finding, error) {
	if !snap.Queries.Collected || len(snap.Queries.Statements) == 0 {
		return nil, nil
	}
	var total float64
	for _, s := range snap.Queries.Statements {
		total += s.TotalExecTimeMS
	}
	if total <= 0 {
		return nil, nil
	}

	var findings []models.Finding
	for _, s := range snap.Queries.Statements {
		share := s.TotalExecTimeMS / total
		// Require both high share and meaningful call volume / mean latency context.
		if share < 0.25 || s.Calls < 5 {
			continue
		}
		if s.MeanExecTimeMS < 1 && s.Calls < 1000 {
			continue
		}
		sev := models.SeverityInfo
		if share >= 0.5 {
			sev = models.SeverityWarning
		}
		obj := fmt.Sprintf("queryid=%d", s.QueryID)
		ev := []models.Evidence{
			{Kind: models.EvidenceFact, Source: "pg_stat_statements", Description: "queryid", Value: fmt.Sprintf("%d", s.QueryID)},
			{Kind: models.EvidenceFact, Source: "pg_stat_statements", Description: "total_exec_time_ms", Value: fmt.Sprintf("%.2f", s.TotalExecTimeMS)},
			{Kind: models.EvidenceFact, Source: "pg_stat_statements", Description: "mean_exec_time_ms", Value: fmt.Sprintf("%.3f", s.MeanExecTimeMS)},
			{Kind: models.EvidenceFact, Source: "pg_stat_statements", Description: "calls", Value: fmt.Sprintf("%d", s.Calls)},
			{Kind: models.EvidenceFact, Source: "derived", Description: "share_of_collected_total", Value: fmt.Sprintf("%.1f%%", share*100)},
		}
		if snap.Queries.StatsReset != nil {
			ev = append(ev, models.Evidence{Kind: models.EvidenceFact, Source: "pg_stat_database", Description: "stats_reset", Value: snap.Queries.StatsReset.UTC().Format("2006-01-02T15:04:05Z")})
		}
		if s.QueryText != "" {
			ev = append(ev, models.Evidence{Kind: models.EvidenceObservation, Source: "pg_stat_statements", Description: "query_preview", Value: s.QueryText})
		}
		findings = append(findings, models.Finding{
			RuleID:              "QUERY.HIGH_TOTAL_TIME",
			Severity:            sev,
			Database:            snap.Instance.Database,
			Object:              obj,
			ObservedAt:          snap.CollectedAt,
			Title:               "Statement dominates cumulative execution time",
			Description:         fmt.Sprintf("Statement queryid=%d accounts for %.1f%% of collected total_exec_time with %d calls (mean %.3f ms). High total time alone is not proof of inefficiency.", s.QueryID, share*100, s.Calls, s.MeanExecTimeMS),
			Evidence:            ev,
			ProbableCause:       "Hot path query, missing index, repeated client calls, or simply a large necessary workload share.",
			ConfidenceRationale: "Requires ≥25% of collected total time and call-volume context; compares within the bounded top-N sample, not the entire instance history.",
			Remediation:         "Inspect with `pgscope queries explain`, review call frequency from the application, and validate plans before changing indexes or SQL.",
			Risk:                models.RiskMedium,
			Validation:          "After changes, compare total_exec_time share and mean_exec_time over a comparable stats window.",
		})
	}
	return findings, nil
}

// QueryHighTempIORule flags statements with substantial temporary block I/O
// together with non-trivial execution cost.
type QueryHighTempIORule struct{}

func (QueryHighTempIORule) ID() string { return "QUERY.HIGH_TEMP_IO" }

func (QueryHighTempIORule) Evaluate(_ context.Context, snap models.Snapshot) ([]models.Finding, error) {
	if !snap.Queries.Collected {
		return nil, nil
	}
	var findings []models.Finding
	for _, s := range snap.Queries.Statements {
		temp := s.TempBlksRead + s.TempBlksWritten
		if temp < 10_000 || s.Calls < 3 || s.TotalExecTimeMS < 100 {
			continue
		}
		perCall := float64(temp) / float64(s.Calls)
		if perCall < 100 && temp < 100_000 {
			continue
		}
		findings = append(findings, models.Finding{
			RuleID:     "QUERY.HIGH_TEMP_IO",
			Severity:   models.SeverityWarning,
			Database:   snap.Instance.Database,
			Object:     fmt.Sprintf("queryid=%d", s.QueryID),
			ObservedAt: snap.CollectedAt,
			Title:      "Statement generates substantial temporary block I/O",
			Description: fmt.Sprintf(
				"queryid=%d has temp_blks_read+written=%d across %d calls (≈%.0f blocks/call) with total_exec_time=%.1f ms.",
				s.QueryID, temp, s.Calls, perCall, s.TotalExecTimeMS,
			),
			Evidence: []models.Evidence{
				{Kind: models.EvidenceFact, Source: "pg_stat_statements", Description: "temp_blks_read", Value: fmt.Sprintf("%d", s.TempBlksRead)},
				{Kind: models.EvidenceFact, Source: "pg_stat_statements", Description: "temp_blks_written", Value: fmt.Sprintf("%d", s.TempBlksWritten)},
				{Kind: models.EvidenceFact, Source: "pg_stat_statements", Description: "calls", Value: fmt.Sprintf("%d", s.Calls)},
				{Kind: models.EvidenceFact, Source: "pg_stat_statements", Description: "total_exec_time_ms", Value: fmt.Sprintf("%.2f", s.TotalExecTimeMS)},
			},
			ProbableCause:       "Sorts/hashes spilling to disk (work_mem), large materializations, or queries reading/writing temp files.",
			ConfidenceRationale: "Requires both elevated temp blocks and meaningful execution time/calls — temp I/O alone on tiny statements is ignored.",
			Remediation:         "EXPLAIN (ANALYZE, BUFFERS) the statement in a lab; consider reducing sorted/hashed rows or carefully adjusting work_mem for the workload class.",
			Risk:                models.RiskMedium,
			Validation:          "Confirm temp_blks counters grow more slowly over a comparable window after changes.",
		})
	}
	return findings, nil
}

// QueryHighSharedReadsRule flags statements with high physical reads plus cost.
type QueryHighSharedReadsRule struct{}

func (QueryHighSharedReadsRule) ID() string { return "QUERY.HIGH_SHARED_READS" }

func (QueryHighSharedReadsRule) Evaluate(_ context.Context, snap models.Snapshot) ([]models.Finding, error) {
	if !snap.Queries.Collected {
		return nil, nil
	}
	var findings []models.Finding
	for _, s := range snap.Queries.Statements {
		if s.SharedBlksRead < 50_000 || s.Calls < 5 {
			continue
		}
		hitRatio := 0.0
		denom := s.SharedBlksHit + s.SharedBlksRead
		if denom > 0 {
			hitRatio = float64(s.SharedBlksHit) / float64(denom)
		}
		if hitRatio > 0.95 && s.SharedBlksRead < 500_000 {
			continue
		}
		findings = append(findings, models.Finding{
			RuleID:     "QUERY.HIGH_SHARED_READS",
			Severity:   models.SeverityInfo,
			Database:   snap.Instance.Database,
			Object:     fmt.Sprintf("queryid=%d", s.QueryID),
			ObservedAt: snap.CollectedAt,
			Title:      "Statement performs substantial shared buffer reads",
			Description: fmt.Sprintf(
				"queryid=%d shared_blks_read=%d hit_ratio=%.1f%% calls=%d mean=%.3f ms. Physical reads may be expected for cold/large scans.",
				s.QueryID, s.SharedBlksRead, hitRatio*100, s.Calls, s.MeanExecTimeMS,
			),
			Evidence: []models.Evidence{
				{Kind: models.EvidenceFact, Source: "pg_stat_statements", Description: "shared_blks_read", Value: fmt.Sprintf("%d", s.SharedBlksRead)},
				{Kind: models.EvidenceFact, Source: "pg_stat_statements", Description: "shared_blks_hit", Value: fmt.Sprintf("%d", s.SharedBlksHit)},
				{Kind: models.EvidenceFact, Source: "derived", Description: "hit_ratio", Value: fmt.Sprintf("%.3f", hitRatio)},
				{Kind: models.EvidenceFact, Source: "pg_stat_statements", Description: "calls", Value: fmt.Sprintf("%d", s.Calls)},
			},
			ProbableCause:       "Large sequential scans, insufficient shared_buffers for the working set, or cold cache after restart.",
			ConfidenceRationale: "Pairs read volume with hit ratio; high-hit workloads are not flagged. Does not prove disk saturation.",
			Remediation:         "Review the plan for unnecessary reads; ensure hot indexes/tables fit the cache; avoid global shared_buffers changes without measurement.",
			Risk:                models.RiskMedium,
			Validation:          "Track shared_blks_read growth and mean latency after plan/cache changes.",
		})
	}
	return findings, nil
}

// QueryHighWALRule flags statements with high WAL generation when columns exist.
type QueryHighWALRule struct{}

func (QueryHighWALRule) ID() string { return "QUERY.HIGH_WAL" }

func (QueryHighWALRule) Evaluate(_ context.Context, snap models.Snapshot) ([]models.Finding, error) {
	if !snap.Queries.Collected {
		return nil, nil
	}
	var findings []models.Finding
	for _, s := range snap.Queries.Statements {
		if s.WALBytes == nil || s.Calls < 5 {
			continue
		}
		if *s.WALBytes < 10_000_000 { // 10MB cumulative
			continue
		}
		if s.TotalExecTimeMS < 50 {
			continue
		}
		findings = append(findings, models.Finding{
			RuleID:     "QUERY.HIGH_WAL",
			Severity:   models.SeverityInfo,
			Database:   snap.Instance.Database,
			Object:     fmt.Sprintf("queryid=%d", s.QueryID),
			ObservedAt: snap.CollectedAt,
			Title:      "Statement generates substantial WAL",
			Description: fmt.Sprintf(
				"queryid=%d wal_bytes=%d across %d calls. WAL volume alone does not prove a problem; correlate with write patterns.",
				s.QueryID, *s.WALBytes, s.Calls,
			),
			Evidence: []models.Evidence{
				{Kind: models.EvidenceFact, Source: "pg_stat_statements", Description: "wal_bytes", Value: fmt.Sprintf("%d", *s.WALBytes)},
				{Kind: models.EvidenceFact, Source: "pg_stat_statements", Description: "calls", Value: fmt.Sprintf("%d", s.Calls)},
				{Kind: models.EvidenceFact, Source: "pg_stat_statements", Description: "total_exec_time_ms", Value: fmt.Sprintf("%.2f", s.TotalExecTimeMS)},
			},
			ProbableCause:       "Bulk writes, updates touching many indexes, or full-row updates of wide tables.",
			ConfidenceRationale: "Requires WAL bytes column (PG13+) plus call/time context.",
			Remediation:         "Review write shape (HOT updates, index count, fillfactor) and batch sizes; validate in a lab.",
			Risk:                models.RiskMedium,
			Validation:          "Compare wal_bytes growth for this queryid after write-path changes.",
		})
	}
	return findings, nil
}

// QueryHighFrequencyRule flags extremely chatty statements with meaningful cost.
type QueryHighFrequencyRule struct{}

func (QueryHighFrequencyRule) ID() string { return "QUERY.HIGH_FREQUENCY" }

func (QueryHighFrequencyRule) Evaluate(_ context.Context, snap models.Snapshot) ([]models.Finding, error) {
	if !snap.Queries.Collected || len(snap.Queries.Statements) == 0 {
		return nil, nil
	}
	var maxCalls int64
	for _, s := range snap.Queries.Statements {
		if s.Calls > maxCalls {
			maxCalls = s.Calls
		}
	}
	var findings []models.Finding
	for _, s := range snap.Queries.Statements {
		if s.Calls < 10_000 {
			continue
		}
		if s.TotalExecTimeMS < 100 && s.MeanExecTimeMS < 0.5 {
			continue
		}
		findings = append(findings, models.Finding{
			RuleID:     "QUERY.HIGH_FREQUENCY",
			Severity:   models.SeverityInfo,
			Database:   snap.Instance.Database,
			Object:     fmt.Sprintf("queryid=%d", s.QueryID),
			ObservedAt: snap.CollectedAt,
			Title:      "Statement is called very frequently",
			Description: fmt.Sprintf(
				"queryid=%d has %d calls (mean %.3f ms, total %.1f ms). Frequency plus non-trivial time warrants client-side batching review.",
				s.QueryID, s.Calls, s.MeanExecTimeMS, s.TotalExecTimeMS,
			),
			Evidence: []models.Evidence{
				{Kind: models.EvidenceFact, Source: "pg_stat_statements", Description: "calls", Value: fmt.Sprintf("%d", s.Calls)},
				{Kind: models.EvidenceFact, Source: "pg_stat_statements", Description: "mean_exec_time_ms", Value: fmt.Sprintf("%.3f", s.MeanExecTimeMS)},
				{Kind: models.EvidenceFact, Source: "pg_stat_statements", Description: "total_exec_time_ms", Value: fmt.Sprintf("%.2f", s.TotalExecTimeMS)},
			},
			ProbableCause:       "ORM chatty patterns, missing caching, or per-row lookups in application loops.",
			ConfidenceRationale: "Requires high calls and non-trivial cumulative/mean time so cheap health checks are not flagged.",
			Remediation:         "Consider batching, prepared statements, or caching at the application layer after confirming necessity.",
			Risk:                models.RiskLow,
			Validation:          "Confirm call rate decreases after application changes over a comparable window.",
		})
	}
	return findings, nil
}

// QueryPoorRowEfficiencyRule uses rows vs time when both are meaningful.
type QueryPoorRowEfficiencyRule struct{}

func (QueryPoorRowEfficiencyRule) ID() string { return "QUERY.POOR_ROW_EFFICIENCY" }

func (QueryPoorRowEfficiencyRule) Evaluate(_ context.Context, snap models.Snapshot) ([]models.Finding, error) {
	if !snap.Queries.Collected {
		return nil, nil
	}
	var findings []models.Finding
	for _, s := range snap.Queries.Statements {
		if s.Calls < 10 || s.Rows < 1 || s.TotalExecTimeMS < 500 {
			continue
		}
		rowsPerMS := float64(s.Rows) / s.TotalExecTimeMS
		// Very low row throughput with substantial shared reads suggests heavy scanning per result row.
		if rowsPerMS > 10 {
			continue
		}
		if s.SharedBlksRead+s.SharedBlksHit < 10_000 {
			continue
		}
		blocksPerRow := float64(s.SharedBlksRead+s.SharedBlksHit) / float64(s.Rows)
		if blocksPerRow < 5 {
			continue
		}
		findings = append(findings, models.Finding{
			RuleID:     "QUERY.POOR_ROW_EFFICIENCY",
			Severity:   models.SeverityInfo,
			Database:   snap.Instance.Database,
			Object:     fmt.Sprintf("queryid=%d", s.QueryID),
			ObservedAt: snap.CollectedAt,
			Title:      "Low rows processed relative to buffer access",
			Description: fmt.Sprintf(
				"queryid=%d processed %d rows using %d shared blocks (≈%.1f blocks/row) over %.1f ms total. Multi-metric signal only.",
				s.QueryID, s.Rows, s.SharedBlksRead+s.SharedBlksHit, blocksPerRow, s.TotalExecTimeMS,
			),
			Evidence: []models.Evidence{
				{Kind: models.EvidenceFact, Source: "pg_stat_statements", Description: "rows", Value: fmt.Sprintf("%d", s.Rows)},
				{Kind: models.EvidenceFact, Source: "pg_stat_statements", Description: "shared_blks_hit+read", Value: fmt.Sprintf("%d", s.SharedBlksHit+s.SharedBlksRead)},
				{Kind: models.EvidenceFact, Source: "derived", Description: "blocks_per_row", Value: fmt.Sprintf("%.2f", blocksPerRow)},
				{Kind: models.EvidenceFact, Source: "pg_stat_statements", Description: "total_exec_time_ms", Value: fmt.Sprintf("%.2f", s.TotalExecTimeMS)},
			},
			ProbableCause:       "Broad scans returning few rows, poor selectivity, or missing indexes for filters.",
			ConfidenceRationale: "Combines rows, buffer access, and time; never flags on a single metric.",
			Remediation:         "EXPLAIN the statement and evaluate selective indexes or query rewrite in a lab.",
			Risk:                models.RiskMedium,
			Validation:          "Confirm blocks/row and latency improve after plan changes.",
		})
	}
	return findings, nil
}
