package rules

import (
	"context"
	"fmt"

	"github.com/hghukasyan/pgscope/pkg/models"
)

// LockContentionRule flags waiting locks and blocking chains.
type LockContentionRule struct{}

func (LockContentionRule) ID() string { return "LOCK.CONTENTION" }

func (LockContentionRule) Evaluate(_ context.Context, snap models.Snapshot) ([]models.Finding, error) {
	if !snap.Locks.Collected {
		return nil, nil
	}
	if snap.Locks.WaitingCount == 0 && len(snap.Locks.BlockingChains) == 0 {
		return nil, nil
	}

	severity := models.SeverityWarning
	if snap.Locks.WaitingCount >= 5 || len(snap.Locks.BlockingChains) >= 3 {
		severity = models.SeverityCritical
	}

	ev := []models.Evidence{
		{Kind: models.EvidenceFact, Source: "pg_locks", Description: "Ungranted locks", Value: fmt.Sprintf("%d", snap.Locks.WaitingCount)},
		{Kind: models.EvidenceFact, Source: "pg_locks/pg_stat_activity", Description: "Blocking chains", Value: fmt.Sprintf("%d", len(snap.Locks.BlockingChains))},
	}
	for i, chain := range snap.Locks.BlockingChains {
		if i >= 5 {
			break
		}
		ev = append(ev, models.Evidence{
			Kind: models.EvidenceFact,
			Source: "lock_chain",
			Description: fmt.Sprintf(
				"blocked PID %d (%s) waiting on blocking PID %d (%s) relation=%s mode=%s duration=%s",
				chain.BlockedPID, chain.BlockedUser, chain.BlockingPID, chain.BlockingUser,
				chain.Relation, chain.Mode, chain.BlockedDuration,
			),
			Value: chain.BlockingQueryPreview,
		})
	}

	f := models.Finding{
		RuleID:              "LOCK.CONTENTION",
		Severity:            severity,
		Database:            snap.Instance.Database,
		ObservedAt:          snap.CollectedAt,
		Title:               "Lock contention detected",
		Description:         fmt.Sprintf("%d ungranted lock(s); %d blocking chain(s) observed.", snap.Locks.WaitingCount, len(snap.Locks.BlockingChains)),
		Evidence:            ev,
		ProbableCause:       "One or more sessions hold conflicting locks (often a long transaction or DDL) while others wait.",
		ConfidenceRationale: "Blocking relationships are measured from pg_locks. Causation of the blocking session requires inspecting its query/transaction context.",
		Remediation:         "Inspect the blocking PID query preview. Prefer fixing the holder (commit/rollback, shorter transactions). PGScope never terminates sessions automatically.",
		Risk:                models.RiskHigh,
		Validation:          "Confirm waiting_count returns to 0 with `pgscope scan` after remediation. Do not kill sessions without change-control approval.",
	}
	return []models.Finding{f}, nil
}

// HighDeadlockRule flags elevated deadlock counters.
type HighDeadlockRule struct{}

func (HighDeadlockRule) ID() string { return "LOCK.DEADLOCKS" }

func (HighDeadlockRule) Evaluate(_ context.Context, snap models.Snapshot) ([]models.Finding, error) {
	if !snap.Locks.Collected || snap.Locks.Deadlocks == 0 {
		return nil, nil
	}
	// Counter is cumulative since stats reset; treat any non-zero as informational
	// and warn when elevated. Without historical baselines we avoid fabricating rates.
	severity := models.SeverityInfo
	if snap.Locks.Deadlocks >= 10 {
		severity = models.SeverityWarning
	}

	f := models.Finding{
		RuleID:     "LOCK.DEADLOCKS",
		Severity:   severity,
		Database:   snap.Instance.Database,
		ObservedAt: snap.CollectedAt,
		Title:      "Deadlocks have been recorded",
		Description: fmt.Sprintf(
			"pg_stat_database.deadlocks = %d since stats reset (counter is cumulative, not a current rate).",
			snap.Locks.Deadlocks,
		),
		Evidence: []models.Evidence{
			{Kind: models.EvidenceFact, Source: "pg_stat_database", Description: "deadlocks counter", Value: fmt.Sprintf("%d", snap.Locks.Deadlocks)},
		},
		ProbableCause:       "Concurrent transactions lock the same objects in different orders, or application retries amplify lock conflicts.",
		ConfidenceRationale: "Only the cumulative counter is available in Phase 1. A non-zero value proves deadlocks occurred since reset, not that they are ongoing.",
		Remediation:         "Review application lock ordering, keep transactions short, and capture deadlock details from the PostgreSQL log when they recur.",
		Risk:                models.RiskMedium,
		Validation:          "After code/schema changes, reset or note the deadlock counter baseline and re-check growth over a known window.",
	}
	return []models.Finding{f}, nil
}
