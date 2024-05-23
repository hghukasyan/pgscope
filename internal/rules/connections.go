package rules

import (
	"context"
	"fmt"

	"github.com/hghukasyan/pgscope/pkg/models"
)

// ConnectionSaturationRule flags high connection utilization.
type ConnectionSaturationRule struct{}

func (ConnectionSaturationRule) ID() string { return "CONN.SATURATION" }

func (ConnectionSaturationRule) Evaluate(_ context.Context, snap models.Snapshot) ([]models.Finding, error) {
	if !snap.Connections.Collected || snap.Connections.MaxConnections == 0 {
		return nil, nil
	}
	util := snap.Connections.UtilizationPct
	var severity models.Severity
	switch {
	case util >= 90:
		severity = models.SeverityCritical
	case util >= 75:
		severity = models.SeverityWarning
	default:
		return nil, nil
	}

	f := models.Finding{
		RuleID:     "CONN.SATURATION",
		Severity:   severity,
		Database:   snap.Instance.Database,
		ObservedAt: snap.CollectedAt,
		Title:      "Connection utilization is high",
		Description: fmt.Sprintf(
			"Client backends use %.1f%% of max_connections (%d / %d).",
			util, snap.Connections.Total, snap.Connections.MaxConnections,
		),
		Evidence: []models.Evidence{
			{Kind: models.EvidenceFact, Source: "pg_stat_activity", Description: "Total client backends", Value: fmt.Sprintf("%d", snap.Connections.Total)},
			{Kind: models.EvidenceFact, Source: "pg_settings", Description: "max_connections", Value: fmt.Sprintf("%d", snap.Connections.MaxConnections)},
			{Kind: models.EvidenceFact, Source: "derived", Description: "Utilization percent", Value: fmt.Sprintf("%.1f", util)},
			{Kind: models.EvidenceFact, Source: "pg_stat_activity", Description: "Active backends", Value: fmt.Sprintf("%d", snap.Connections.Active)},
			{Kind: models.EvidenceFact, Source: "pg_stat_activity", Description: "Idle backends", Value: fmt.Sprintf("%d", snap.Connections.Idle)},
		},
		ProbableCause:       "Application connection pools may be oversized, leaky, or lacking idle timeouts; alternatively max_connections is too low for the workload.",
		ConfidenceRationale: "Finding is based on measured backend counts versus max_connections. It does not prove application-side pool misconfiguration without pool metrics.",
		Remediation:         "Review application pool sizes (prefer fewer, longer-lived pooled connections), ensure idle timeouts, and avoid raising max_connections without measuring memory impact.",
		Risk:                models.RiskMedium,
		Validation:          "Re-run `pgscope scan` after pool changes and confirm utilization drops below 75% under peak load.",
	}
	return []models.Finding{f}, nil
}

// IdleInTransactionRule flags idle-in-transaction sessions.
type IdleInTransactionRule struct{}

func (IdleInTransactionRule) ID() string { return "TXN.IDLE_IN_TRANSACTION" }

func (IdleInTransactionRule) Evaluate(_ context.Context, snap models.Snapshot) ([]models.Finding, error) {
	if !snap.Connections.Collected {
		return nil, nil
	}
	count := snap.Connections.IdleInTransaction + snap.Connections.IdleInTransactionAborted
	if count == 0 && len(snap.Transactions.IdleInTransaction) == 0 {
		return nil, nil
	}

	severity := models.SeverityWarning
	if count >= 5 || len(snap.Transactions.IdleInTransaction) > 0 {
		severity = models.SeverityCritical
	}
	if count == 0 {
		count = len(snap.Transactions.IdleInTransaction)
	}

	ev := []models.Evidence{
		{Kind: models.EvidenceFact, Source: "pg_stat_activity", Description: "Idle-in-transaction backends", Value: fmt.Sprintf("%d", count)},
	}
	for i, s := range snap.Transactions.IdleInTransaction {
		if i >= 5 {
			break
		}
		ev = append(ev, models.Evidence{
			Kind:        models.EvidenceFact,
			Source:      "pg_stat_activity",
			Description: fmt.Sprintf("PID %d state=%s duration=%s", s.PID, s.State, s.Duration),
			Value:       s.QueryPreview,
		})
	}

	f := models.Finding{
		RuleID:              "TXN.IDLE_IN_TRANSACTION",
		Severity:            severity,
		Database:            snap.Instance.Database,
		ObservedAt:          snap.CollectedAt,
		Title:               "Idle-in-transaction sessions detected",
		Description:         fmt.Sprintf("%d session(s) are idle inside an open transaction, holding locks and delaying vacuum.", count),
		Evidence:            ev,
		ProbableCause:       "Application code opened a transaction and stopped issuing statements (slow client, missing commit/rollback, or connection returned to a pool mid-transaction).",
		ConfidenceRationale: "pg_stat_activity reports idle-in-transaction state directly; root cause in application code is inferred, not proven.",
		Remediation:         "Find and fix the client that leaves transactions open. Set idle_in_transaction_session_timeout. Ensure pools do not check out connections with open transactions.",
		Risk:                models.RiskLow,
		Validation:          "Confirm idle-in-transaction count returns to zero with `pgscope health` after the fix.",
	}
	return []models.Finding{f}, nil
}

// LongRunningTransactionRule flags long open transactions.
type LongRunningTransactionRule struct{}

func (LongRunningTransactionRule) ID() string { return "TXN.LONG_RUNNING" }

func (LongRunningTransactionRule) Evaluate(_ context.Context, snap models.Snapshot) ([]models.Finding, error) {
	if !snap.Transactions.Collected || len(snap.Transactions.LongRunning) == 0 {
		return nil, nil
	}

	ev := []models.Evidence{
		{Kind: models.EvidenceFact, Source: "pg_stat_activity", Description: "Long-running transactions", Value: fmt.Sprintf("%d", len(snap.Transactions.LongRunning))},
	}
	if snap.Transactions.OldestXactAge != "" {
		ev = append(ev, models.Evidence{
			Kind: models.EvidenceFact, Source: "pg_stat_activity",
			Description: "Oldest transaction age", Value: snap.Transactions.OldestXactAge,
		})
	}
	for i, s := range snap.Transactions.LongRunning {
		if i >= 5 {
			break
		}
		ev = append(ev, models.Evidence{
			Kind:        models.EvidenceFact,
			Source:      "pg_stat_activity",
			Description: fmt.Sprintf("PID %d duration=%s state=%s", s.PID, s.Duration, s.State),
			Value:       s.QueryPreview,
		})
	}

	f := models.Finding{
		RuleID:              "TXN.LONG_RUNNING",
		Severity:            models.SeverityWarning,
		Database:            snap.Instance.Database,
		ObservedAt:          snap.CollectedAt,
		Title:               "Long-running transactions present",
		Description:         fmt.Sprintf("%d transaction(s) exceed the configured age threshold.", len(snap.Transactions.LongRunning)),
		Evidence:            ev,
		ProbableCause:       "Long analytical queries, missing commit boundaries, or interactive sessions holding open transactions.",
		ConfidenceRationale: "Transaction age is measured from xact_start. Whether the transaction is necessary for business logic requires operator judgment.",
		Remediation:         "Identify the workload (PID + query preview). Split long units of work, use shorter transactions, or move heavy reports to replicas.",
		Risk:                models.RiskMedium,
		Validation:          "Re-scan and confirm long-running transaction list is empty or expected.",
	}
	return []models.Finding{f}, nil
}
