package rules

import (
	"context"
	"fmt"

	"github.com/hghukasyan/pgscope/pkg/models"
)

// WALCheckpointReqRule flags a high proportion of requested checkpoints.
type WALCheckpointReqRule struct{}

func (WALCheckpointReqRule) ID() string { return "WAL.CHECKPOINT_REQ" }

func (WALCheckpointReqRule) Evaluate(_ context.Context, snap models.Snapshot) ([]models.Finding, error) {
	if !snap.WAL.Collected || snap.WAL.CheckpointsReqRatio == nil {
		return nil, nil
	}
	ratio := *snap.WAL.CheckpointsReqRatio
	if ratio < 0.3 {
		return nil, nil
	}
	if snap.WAL.CheckpointsReq == nil || snap.WAL.CheckpointsTimed == nil {
		return nil, nil
	}
	total := *snap.WAL.CheckpointsReq + *snap.WAL.CheckpointsTimed
	if total < 10 {
		return nil, nil
	}
	sev := models.SeverityInfo
	if ratio >= 0.5 {
		sev = models.SeverityWarning
	}
	return []models.Finding{{
		RuleID:     "WAL.CHECKPOINT_REQ",
		Severity:   sev,
		Database:   snap.Instance.Database,
		ObservedAt: snap.CollectedAt,
		Title:      "High proportion of requested checkpoints",
		Description: fmt.Sprintf(
			"checkpoints_req=%d checkpoints_timed=%d (req ratio %.0f%%). Requested checkpoints often indicate WAL volume outpacing checkpoint_timeout / max_wal_size tuning.",
			*snap.WAL.CheckpointsReq, *snap.WAL.CheckpointsTimed, ratio*100,
		),
		Evidence: []models.Evidence{
			{Kind: models.EvidenceFact, Source: "pg_stat_bgwriter/checkpointer", Description: "checkpoints_req", Value: fmt.Sprintf("%d", *snap.WAL.CheckpointsReq)},
			{Kind: models.EvidenceFact, Source: "pg_stat_bgwriter/checkpointer", Description: "checkpoints_timed", Value: fmt.Sprintf("%d", *snap.WAL.CheckpointsTimed)},
			{Kind: models.EvidenceFact, Source: "derived", Description: "req_ratio", Value: fmt.Sprintf("%.3f", ratio)},
			{Kind: models.EvidenceFact, Source: "pg_settings", Description: "max_wal_size", Value: snap.WAL.Settings["max_wal_size"]},
			{Kind: models.EvidenceFact, Source: "pg_settings", Description: "checkpoint_timeout", Value: snap.WAL.Settings["checkpoint_timeout"]},
		},
		ProbableCause:       "Burst WAL generation or max_wal_size too small relative to write volume.",
		ConfidenceRationale: "Based on cumulative counters since stats_reset; rate conclusions require two snapshots.",
		Remediation:         "Review write workload and max_wal_size/checkpoint_timeout carefully. Avoid inferring disk saturation from this signal alone.",
		Risk:                models.RiskMedium,
		Validation:          "After tuning, track checkpoints_req ratio over a fresh stats window or paired snapshots.",
	}}, nil
}

// WALBuffersFullRule notes WAL buffer full events when exposed.
type WALBuffersFullRule struct{}

func (WALBuffersFullRule) ID() string { return "WAL.BUFFERS_FULL" }

func (WALBuffersFullRule) Evaluate(_ context.Context, snap models.Snapshot) ([]models.Finding, error) {
	if !snap.WAL.Collected || snap.WAL.WALBuffersFull == nil || *snap.WAL.WALBuffersFull == 0 {
		return nil, nil
	}
	sev := models.SeverityInfo
	if *snap.WAL.WALBuffersFull >= 100 {
		sev = models.SeverityWarning
	}
	return []models.Finding{{
		RuleID:     "WAL.BUFFERS_FULL",
		Severity:   sev,
		Database:   snap.Instance.Database,
		ObservedAt: snap.CollectedAt,
		Title:      "WAL buffers filled since statistics reset",
		Description: fmt.Sprintf(
			"wal_buffers_full=%d (cumulative). This can indicate WAL buffer pressure under write bursts, not proof of sustained I/O saturation.",
			*snap.WAL.WALBuffersFull,
		),
		Evidence: []models.Evidence{
			{Kind: models.EvidenceFact, Source: "pg_stat_wal", Description: "wal_buffers_full", Value: fmt.Sprintf("%d", *snap.WAL.WALBuffersFull)},
			{Kind: models.EvidenceFact, Source: "pg_settings", Description: "wal_buffers", Value: snap.WAL.Settings["wal_buffers"]},
		},
		ProbableCause:       "wal_buffers undersized for peak WAL generation, or extreme write bursts.",
		ConfidenceRationale: "Counter is cumulative since reset; interpret with workload context.",
		Remediation:         "Measure under load before raising wal_buffers; correlate with WAL write/sync times.",
		Risk:                models.RiskMedium,
		Validation:          "Compare wal_buffers_full growth rate across paired snapshots after changes.",
	}}, nil
}
