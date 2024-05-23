// Package rules defines the diagnostic rule interface and Phase-1 rule set.
package rules

import (
	"context"

	"github.com/hghukasyan/pgscope/pkg/models"
)

// Rule evaluates a snapshot and returns zero or more findings.
type Rule interface {
	ID() string
	Evaluate(ctx context.Context, snapshot models.Snapshot) ([]models.Finding, error)
}

// AllPhase1 returns the Phase-1 diagnostic rule set.
func AllPhase1() []Rule {
	return []Rule{
		ConnectionSaturationRule{},
		IdleInTransactionRule{},
		LongRunningTransactionRule{},
		LockContentionRule{},
		InvalidIndexRule{},
		XIDAgeRule{},
		UnsupportedVersionRule{},
		HighDeadlockRule{},
	}
}

// AllPhase2 returns Phase-1 rules plus Phase-2 intelligence rules.
func AllPhase2() []Rule {
	return append(AllPhase1(),
		UnusedIndexObservationRule{},
		DuplicateIndexRule{},
		RedundantBTreeIndexRule{},
		LargeLowUsageIndexRule{},
		QueryHighTotalTimeRule{},
		QueryHighTempIORule{},
		QueryHighSharedReadsRule{},
		QueryHighWALRule{},
		QueryHighFrequencyRule{},
		QueryPoorRowEfficiencyRule{},
		VacuumDeadTuplesRule{},
		VacuumFreezeAgeRule{},
		VacuumStaleStatsRule{},
		VacuumLongXactRule{},
		WALCheckpointReqRule{},
		WALBuffersFullRule{},
		ReplicationLagRule{},
		ReplicationInactiveSlotRule{},
		ReplicationDisconnectedRule{},
		ReplicaConflictRule{},
	)
}

// AllPhase3 returns Phase-2 rules plus vector inventory diagnostics.
func AllPhase3() []Rule {
	return append(AllPhase2(), VectorInventoryRule{})
}

// DefaultRules returns the current production rule set (Phase 3).
func DefaultRules() []Rule { return AllPhase3() }
