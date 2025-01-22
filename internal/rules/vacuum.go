package rules

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/hghukasyan/pgscope/internal/collector"
	"github.com/hghukasyan/pgscope/pkg/models"
)

// VacuumDeadTuplesRule flags tables past effective autovacuum thresholds with
// meaningful dead tuple accumulation (estimates, not precise bloat).
type VacuumDeadTuplesRule struct{}

func (VacuumDeadTuplesRule) ID() string { return "VACUUM.DEAD_TUPLES" }

func (VacuumDeadTuplesRule) Evaluate(_ context.Context, snap models.Snapshot) ([]models.Finding, error) {
	if !snap.Vacuum.Collected {
		return nil, nil
	}
	var findings []models.Finding
	for _, t := range snap.Vacuum.Tables {
		if !t.WouldAutovacuum || t.DeadTuplePct < 5 {
			continue
		}
		sev := models.SeverityInfo
		if t.DeadTuplePct >= 20 || t.DeadTuples >= t.VacuumThreshold*2 {
			sev = models.SeverityWarning
		}
		obj := t.Schema + "." + t.Name
		findings = append(findings, models.Finding{
			RuleID:     "VACUUM.DEAD_TUPLES",
			Severity:   sev,
			Database:   snap.Instance.Database,
			Object:      obj,
			ObservedAt: snap.CollectedAt,
			Title:      "Table past effective autovacuum vacuum threshold",
			Description: fmt.Sprintf(
				"%s has n_dead_tup=%d (≈%.1f%%) exceeding effective threshold %d. Counts are planner estimates, not precise bloat.",
				obj, t.DeadTuples, t.DeadTuplePct, t.VacuumThreshold,
			),
			Evidence: []models.Evidence{
				{Kind: models.EvidenceFact, Source: "pg_stat_user_tables", Description: "n_dead_tup", Value: fmt.Sprintf("%d", t.DeadTuples)},
				{Kind: models.EvidenceFact, Source: "pg_stat_user_tables", Description: "n_live_tup", Value: fmt.Sprintf("%d", t.LiveTuples)},
				{Kind: models.EvidenceFact, Source: "derived", Description: "effective_vacuum_threshold", Value: collector.FormatVacuumThresholdExplain(t)},
				{Kind: models.EvidenceFact, Source: "pg_stat_user_tables", Description: "last_autovacuum", Value: fmtTime(t.LastAutovacuum)},
			},
			ProbableCause:       "Autovacuum lagging (workers busy, long transactions, aggressive scale factor) or bursty updates/deletes.",
			ConfidenceRationale: "Threshold math uses effective per-table settings when present. Dead tuples are estimates.",
			Remediation:         "Investigate long transactions; review autovacuum_vacuum_scale_factor for this table only if evidence supports it. Do not disable autovacuum.",
			Risk:                models.RiskMedium,
			Validation:          "Confirm n_dead_tup declines after autovacuum/vacuum and threshold math with `pgscope vacuum inspect`.",
		})
	}
	return findings, nil
}

// VacuumFreezeAgeRule flags tables approaching freeze max age.
type VacuumFreezeAgeRule struct{}

func (VacuumFreezeAgeRule) ID() string { return "VACUUM.FREEZE_AGE" }

func (VacuumFreezeAgeRule) Evaluate(_ context.Context, snap models.Snapshot) ([]models.Finding, error) {
	if !snap.Vacuum.Collected {
		return nil, nil
	}
	freezeMax := int64(200_000_000)
	if v, ok := snap.Vacuum.Settings["autovacuum_freeze_max_age"]; ok {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			freezeMax = n
		}
	}
	var findings []models.Finding
	for _, t := range snap.Vacuum.Tables {
		if t.RelFrozenXIDAge <= 0 {
			continue
		}
		ratio := float64(t.RelFrozenXIDAge) / float64(freezeMax)
		if ratio < 0.8 {
			continue
		}
		sev := models.SeverityWarning
		if ratio >= 0.95 {
			sev = models.SeverityCritical
		}
		obj := t.Schema + "." + t.Name
		findings = append(findings, models.Finding{
			RuleID:     "VACUUM.FREEZE_AGE",
			Severity:   sev,
			Database:   snap.Instance.Database,
			Object:      obj,
			ObservedAt: snap.CollectedAt,
			Title:      "Table approaching transaction ID freeze threshold",
			Description: fmt.Sprintf(
				"%s relfrozenxid age=%d is %.0f%% of autovacuum_freeze_max_age=%d.",
				obj, t.RelFrozenXIDAge, ratio*100, freezeMax,
			),
			Evidence: []models.Evidence{
				{Kind: models.EvidenceFact, Source: "pg_class", Description: "age(relfrozenxid)", Value: fmt.Sprintf("%d", t.RelFrozenXIDAge)},
				{Kind: models.EvidenceFact, Source: "pg_settings", Description: "autovacuum_freeze_max_age", Value: fmt.Sprintf("%d", freezeMax)},
			},
			ProbableCause:       "Autovacuum freeze falling behind due to large tables, long transactions, or insufficient vacuum throughput.",
			ConfidenceRationale: "XID age is a catalog fact; wraparound imminence also depends on XID consumption rate.",
			Remediation:         "Clear long-running transactions; allow aggressive freeze vacuum during a maintenance window if needed.",
			Risk:                models.RiskHigh,
			Validation:          "Re-check age(relfrozenxid) after freeze vacuum completes.",
		})
	}
	return findings, nil
}

// VacuumStaleStatsRule flags tables needing analyze based on effective thresholds.
type VacuumStaleStatsRule struct{}

func (VacuumStaleStatsRule) ID() string { return "VACUUM.STALE_STATS" }

func (VacuumStaleStatsRule) Evaluate(_ context.Context, snap models.Snapshot) ([]models.Finding, error) {
	if !snap.Vacuum.Collected {
		return nil, nil
	}
	var findings []models.Finding
	for _, t := range snap.Vacuum.Tables {
		if !t.WouldAutoanalyze || t.ModSinceAnalyze < t.AnalyzeThreshold {
			continue
		}
		obj := t.Schema + "." + t.Name
		findings = append(findings, models.Finding{
			RuleID:     "VACUUM.STALE_STATS",
			Severity:   models.SeverityInfo,
			Database:   snap.Instance.Database,
			Object:      obj,
			ObservedAt: snap.CollectedAt,
			Title:      "Table past effective autoanalyze threshold",
			Description: fmt.Sprintf(
				"%s n_mod_since_analyze=%d exceeds analyze threshold %d (last_autoanalyze=%s).",
				obj, t.ModSinceAnalyze, t.AnalyzeThreshold, fmtTime(t.LastAutoanalyze),
			),
			Evidence: []models.Evidence{
				{Kind: models.EvidenceFact, Source: "pg_stat_user_tables", Description: "n_mod_since_analyze", Value: fmt.Sprintf("%d", t.ModSinceAnalyze)},
				{Kind: models.EvidenceFact, Source: "derived", Description: "analyze_threshold", Value: fmt.Sprintf("%d", t.AnalyzeThreshold)},
				{Kind: models.EvidenceFact, Source: "pg_stat_user_tables", Description: "last_autoanalyze", Value: fmtTime(t.LastAutoanalyze)},
			},
			ProbableCause:       "High modification rate relative to autoanalyze settings, or analyze delayed by vacuum workload.",
			ConfidenceRationale: "Uses effective per-table analyze threshold math when overrides exist.",
			Remediation:         "Allow autoanalyze to catch up; consider manual ANALYZE during maintenance if plans are clearly wrong.",
			Risk:                models.RiskLow,
			Validation:          "Confirm n_mod_since_analyze drops and plans stabilize after analyze.",
		})
	}
	return findings, nil
}

// VacuumLongXactRule notes long transactions that can block cleanup.
type VacuumLongXactRule struct{}

func (VacuumLongXactRule) ID() string { return "VACUUM.LONG_XACT_BLOCK" }

func (VacuumLongXactRule) Evaluate(_ context.Context, snap models.Snapshot) ([]models.Finding, error) {
	if !snap.Vacuum.Collected || snap.Vacuum.LongXactBlockingAge == "" {
		return nil, nil
	}
	if len(snap.Transactions.LongRunning) == 0 && snap.Connections.IdleInTransaction == 0 {
		return nil, nil
	}
	return []models.Finding{{
		RuleID:     "VACUUM.LONG_XACT_BLOCK",
		Severity:   models.SeverityWarning,
		Database:   snap.Instance.Database,
		ObservedAt: snap.CollectedAt,
		Title:      "Long transactions may delay vacuum cleanup",
		Description: fmt.Sprintf(
			"Oldest observed transaction age is %s while vacuum risk tables=%d. Long transactions prevent removal of dead row versions still visible to them.",
			snap.Vacuum.LongXactBlockingAge, len(snap.Vacuum.Risks),
		),
		Evidence: []models.Evidence{
			{Kind: models.EvidenceFact, Source: "pg_stat_activity", Description: "oldest_xact_age", Value: snap.Vacuum.LongXactBlockingAge},
			{Kind: models.EvidenceFact, Source: "vacuum", Description: "risk_tables", Value: fmt.Sprintf("%d", len(snap.Vacuum.Risks))},
			{Kind: models.EvidenceFact, Source: "pg_stat_activity", Description: "long_running_txns", Value: fmt.Sprintf("%d", len(snap.Transactions.LongRunning))},
		},
		ProbableCause:       "Idle-in-transaction or long analytical transactions holding an old snapshot.",
		ConfidenceRationale: "Combines transaction age with vacuum risk evidence; does not claim vacuum is currently blocked without xmin inspection.",
		Remediation:         "Identify and end orphan idle-in-transaction sessions; move long analytics to replicas.",
		Risk:                models.RiskMedium,
		Validation:          "Confirm oldest xact age drops and dead tuples decline afterward.",
	}}, nil
}

func fmtTime(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}
