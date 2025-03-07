package rules

import (
	"context"
	"fmt"

	"github.com/hghukasyan/pgscope/pkg/models"
)

// ReplicationLagRule flags replicas with measurable byte or time lag.
// NULL lag values are never treated as zero or as automatic failures.
type ReplicationLagRule struct{}

func (ReplicationLagRule) ID() string { return "REPL.LAG" }

func (ReplicationLagRule) Evaluate(_ context.Context, snap models.Snapshot) ([]models.Finding, error) {
	if !snap.Replication.Collected || snap.Replication.Role != "primary" {
		return nil, nil
	}
	var findings []models.Finding
	for _, c := range snap.Replication.Connections {
		hasByteLag := c.ByteLagReplay != nil && *c.ByteLagReplay > 16*1024*1024 // 16MB
		hasTimeLag := c.ReplayLag != nil && *c.ReplayLag != ""
		if !hasByteLag && !hasTimeLag {
			continue
		}
		// Without a prior snapshot we cannot claim lag is "increasing".
		sev := models.SeverityInfo
		if hasByteLag && *c.ByteLagReplay > 256*1024*1024 {
			sev = models.SeverityWarning
		}
		ev := []models.Evidence{
			{Kind: models.EvidenceFact, Source: "pg_stat_replication", Description: "application_name", Value: c.ApplicationName},
			{Kind: models.EvidenceFact, Source: "pg_stat_replication", Description: "state", Value: c.State},
			{Kind: models.EvidenceFact, Source: "pg_stat_replication", Description: "pid", Value: fmt.Sprintf("%d", c.PID)},
		}
		if c.ByteLagReplay != nil {
			ev = append(ev, models.Evidence{Kind: models.EvidenceFact, Source: "pg_stat_replication", Description: "byte_lag_replay", Value: fmt.Sprintf("%d", *c.ByteLagReplay)})
		}
		if c.ByteLagSent != nil {
			ev = append(ev, models.Evidence{Kind: models.EvidenceFact, Source: "pg_stat_replication", Description: "byte_lag_sent", Value: fmt.Sprintf("%d", *c.ByteLagSent)})
		}
		if c.ReplayLag != nil {
			ev = append(ev, models.Evidence{Kind: models.EvidenceFact, Source: "pg_stat_replication", Description: "replay_lag", Value: *c.ReplayLag})
		} else {
			ev = append(ev, models.Evidence{Kind: models.EvidenceObservation, Source: "pg_stat_replication", Description: "replay_lag", Value: "NULL (not interpreted as zero)"})
		}
		findings = append(findings, models.Finding{
			RuleID:     "REPL.LAG",
			Severity:   sev,
			Database:   snap.Instance.Database,
			Object:     fmt.Sprintf("pid=%d", c.PID),
			ObservedAt: snap.CollectedAt,
			Title:      "Replica lag observed",
			Description: fmt.Sprintf(
				"Standby %q state=%s shows lag evidence. Byte lag and time lag are reported separately; NULL time lag is not treated as zero.",
				c.ApplicationName, c.State,
			),
			Evidence:            ev,
			ProbableCause:       "Replica apply/network slower than primary WAL generation, or replica busy with queries/conflicts.",
			ConfidenceRationale: "Single snapshot cannot prove lag is increasing; reports current measured byte/time lag only.",
			Remediation:         "Inspect replica load, conflicts, and network. Use paired snapshots to see whether lag grows.",
			Risk:                models.RiskMedium,
			Validation:          "Re-run `pgscope replication lag` and confirm byte/time lag declines.",
		})
	}
	return findings, nil
}

// ReplicationInactiveSlotRule flags inactive slots retaining WAL.
type ReplicationInactiveSlotRule struct{}

func (ReplicationInactiveSlotRule) ID() string { return "REPL.INACTIVE_SLOT" }

func (ReplicationInactiveSlotRule) Evaluate(_ context.Context, snap models.Snapshot) ([]models.Finding, error) {
	if !snap.Replication.Collected {
		return nil, nil
	}
	var findings []models.Finding
	for _, s := range snap.Replication.Slots {
		if s.Active {
			continue
		}
		retained := int64(0)
		if s.RetainedBytes != nil {
			retained = *s.RetainedBytes
		}
		sev := models.SeverityInfo
		if retained >= 64*1024*1024 || s.WalStatus == "unreserved" || s.WalStatus == "lost" {
			sev = models.SeverityWarning
		}
		if retained >= 1024*1024*1024 {
			sev = models.SeverityCritical
		}
		ev := []models.Evidence{
			{Kind: models.EvidenceFact, Source: "pg_replication_slots", Description: "slot_name", Value: s.SlotName},
			{Kind: models.EvidenceFact, Source: "pg_replication_slots", Description: "slot_type", Value: s.SlotType},
			{Kind: models.EvidenceFact, Source: "pg_replication_slots", Description: "active", Value: "false"},
			{Kind: models.EvidenceFact, Source: "pg_replication_slots", Description: "restart_lsn", Value: s.RestartLSN},
			{Kind: models.EvidenceFact, Source: "pg_replication_slots", Description: "wal_status", Value: s.WalStatus},
		}
		if s.RetainedBytes != nil {
			ev = append(ev, models.Evidence{Kind: models.EvidenceFact, Source: "derived", Description: "retained_bytes", Value: fmt.Sprintf("%d", *s.RetainedBytes)})
		}
		findings = append(findings, models.Finding{
			RuleID:     "REPL.INACTIVE_SLOT",
			Severity:   sev,
			Database:   snap.Instance.Database,
			Object:     s.SlotName,
			ObservedAt: snap.CollectedAt,
			Title:      "Inactive replication slot may retain WAL",
			Description: fmt.Sprintf(
				"Slot %q is inactive (type=%s, wal_status=%s). Inactive slots can retain WAL and grow disk usage until consumed or dropped.",
				s.SlotName, s.SlotType, s.WalStatus,
			),
			Evidence:            ev,
			ProbableCause:       "Subscriber/replica disconnected, abandoned slot, or delayed logical consumer.",
			ConfidenceRationale: "active=false is a catalog fact; retained_bytes is derived from LSN difference when available.",
			Remediation:         "Restore the consumer or drop the slot after confirming it is obsolete. PGScope never drops slots.",
			Risk:                models.RiskHigh,
			Validation:          "Confirm slot becomes active or is removed and WAL disk usage stabilizes.",
		})
	}
	return findings, nil
}

// ReplicationDisconnectedRule flags non-streaming states.
type ReplicationDisconnectedRule struct{}

func (ReplicationDisconnectedRule) ID() string { return "REPL.STATE" }

func (ReplicationDisconnectedRule) Evaluate(_ context.Context, snap models.Snapshot) ([]models.Finding, error) {
	if !snap.Replication.Collected || snap.Replication.Role != "primary" {
		return nil, nil
	}
	var findings []models.Finding
	for _, c := range snap.Replication.Connections {
		if c.State == "" || c.State == "streaming" {
			continue
		}
		findings = append(findings, models.Finding{
			RuleID:     "REPL.STATE",
			Severity:   models.SeverityWarning,
			Database:   snap.Instance.Database,
			Object:     fmt.Sprintf("pid=%d", c.PID),
			ObservedAt: snap.CollectedAt,
			Title:      "Replication connection not in streaming state",
			Description: fmt.Sprintf(
				"Replica %q pid=%d reports state=%q (sync_state=%q).",
				c.ApplicationName, c.PID, c.State, c.SyncState,
			),
			Evidence: []models.Evidence{
				{Kind: models.EvidenceFact, Source: "pg_stat_replication", Description: "state", Value: c.State},
				{Kind: models.EvidenceFact, Source: "pg_stat_replication", Description: "sync_state", Value: c.SyncState},
				{Kind: models.EvidenceFact, Source: "pg_stat_replication", Description: "application_name", Value: c.ApplicationName},
			},
			ProbableCause:       "Catchup after disconnect, startup, or replication configuration issue.",
			ConfidenceRationale: "State is reported by PostgreSQL; transient catchup states can be normal.",
			Remediation:         "Monitor whether the connection returns to streaming; investigate network/auth if it persists.",
			Risk:                models.RiskMedium,
			Validation:          "Confirm state=streaming with `pgscope replication inspect`.",
		})
	}
	return findings, nil
}

// ReplicaConflictRule flags standby query conflicts when present.
type ReplicaConflictRule struct{}

func (ReplicaConflictRule) ID() string { return "REPL.CONFLICTS" }

func (ReplicaConflictRule) Evaluate(_ context.Context, snap models.Snapshot) ([]models.Finding, error) {
	if !snap.Replication.Collected || snap.Replication.Role != "standby" {
		return nil, nil
	}
	var findings []models.Finding
	for _, c := range snap.Replication.Conflicts {
		total := c.ConflTablespace + c.ConflLock + c.ConflSnapshot + c.ConflBufferpin + c.ConflDeadlock
		if total == 0 {
			continue
		}
		findings = append(findings, models.Finding{
			RuleID:     "REPL.CONFLICTS",
			Severity:   models.SeverityInfo,
			Database:   c.Database,
			ObservedAt: snap.CollectedAt,
			Title:      "Standby query conflicts recorded",
			Description: fmt.Sprintf(
				"Database %q has %d conflict counter total (snapshot=%d lock=%d deadlock=%d bufferpin=%d tablespace=%d). Counters are cumulative.",
				c.Database, total, c.ConflSnapshot, c.ConflLock, c.ConflDeadlock, c.ConflBufferpin, c.ConflTablespace,
			),
			Evidence: []models.Evidence{
				{Kind: models.EvidenceFact, Source: "pg_stat_database_conflicts", Description: "confl_snapshot", Value: fmt.Sprintf("%d", c.ConflSnapshot)},
				{Kind: models.EvidenceFact, Source: "pg_stat_database_conflicts", Description: "confl_lock", Value: fmt.Sprintf("%d", c.ConflLock)},
				{Kind: models.EvidenceFact, Source: "pg_stat_database_conflicts", Description: "confl_deadlock", Value: fmt.Sprintf("%d", c.ConflDeadlock)},
			},
			ProbableCause:       "Primary vacuum/remove conflicting with long read queries on the standby.",
			ConfidenceRationale: "Counters prove conflicts occurred since reset, not that they are ongoing.",
			Remediation:         "Consider hot_standby_feedback or shorter standby queries; max_standby_streaming_delay tuning with care.",
			Risk:                models.RiskMedium,
			Validation:          "Track conflict counter growth rate after configuration/workload changes.",
		})
	}
	return findings, nil
}
