package rules

import (
	"context"
	"fmt"

	"github.com/hghukasyan/pgscope/pkg/models"
)

// XIDAgeRule warns when datfrozenxid age approaches wraparound risk thresholds.
type XIDAgeRule struct{}

func (XIDAgeRule) ID() string { return "TXN.XID_AGE" }

func (XIDAgeRule) Evaluate(_ context.Context, snap models.Snapshot) ([]models.Finding, error) {
	if !snap.Transactions.Collected || snap.Transactions.DatfrozenxidAge == 0 {
		return nil, nil
	}
	age := snap.Transactions.DatfrozenxidAge
	// Conservative thresholds well below wraparound (2^31).
	const warnAge int64 = 500_000_000
	const critAge int64 = 1_000_000_000

	var severity models.Severity
	switch {
	case age >= critAge:
		severity = models.SeverityCritical
	case age >= warnAge:
		severity = models.SeverityWarning
	default:
		return nil, nil
	}

	f := models.Finding{
		RuleID:     "TXN.XID_AGE",
		Severity:   severity,
		Database:   snap.Instance.Database,
		ObservedAt: snap.CollectedAt,
		Title:      "Transaction ID age is elevated",
		Description: fmt.Sprintf(
			"age(datfrozenxid) = %d. Autovacuum freeze may be falling behind.",
			age,
		),
		Evidence: []models.Evidence{
			{Kind: models.EvidenceFact, Source: "pg_database", Description: "age(datfrozenxid)", Value: fmt.Sprintf("%d", age)},
		},
		ProbableCause:       "Long-running transactions, insufficient autovacuum workers/throughput, or very large tables delaying freeze.",
		ConfidenceRationale: "XID age is measured from the catalog. Whether wraparound is imminent depends on consumption rate, which Phase 1 does not yet trend.",
		Remediation:         "Investigate long transactions and autovacuum lag. Consider vacuum freeze strategies on large tables during maintenance. Do not disable autovacuum.",
		Risk:                models.RiskHigh,
		Validation:          "Re-check age(datfrozenxid) after vacuum/freeze work and ensure it trends down.",
	}
	return []models.Finding{f}, nil
}

// UnsupportedVersionRule notes versions outside the supported window.
type UnsupportedVersionRule struct{}

func (UnsupportedVersionRule) ID() string { return "INST.VERSION_SUPPORT" }

func (UnsupportedVersionRule) Evaluate(_ context.Context, snap models.Snapshot) ([]models.Finding, error) {
	caps := snap.Instance.Capabilities
	if caps.Supported {
		return nil, nil
	}
	f := models.Finding{
		RuleID:     "INST.VERSION_SUPPORT",
		Severity:   models.SeverityWarning,
		Database:   snap.Instance.Database,
		ObservedAt: snap.CollectedAt,
		Title:      "PostgreSQL version outside supported window",
		Description: fmt.Sprintf(
			"Detected major version %d; PGScope targets %d–%d.",
			snap.Instance.MajorVersion, caps.MinSupportedMajor, caps.MaxSupportedMajor,
		),
		Evidence: []models.Evidence{
			{Kind: models.EvidenceFact, Source: "server_version_num", Description: "version_num", Value: fmt.Sprintf("%d", snap.Instance.VersionNum)},
			{Kind: models.EvidenceFact, Source: "version()", Description: "version_string", Value: snap.Instance.VersionString},
		},
		ProbableCause:       "Instance is newer or older than the currently validated PGScope support matrix.",
		ConfidenceRationale: "Version numbers are authoritative; support window is a product policy, not a database fault.",
		Remediation:         "Proceed with caution. Prefer validating PGScope against a matching major version in a lab before relying on findings in production.",
		Risk:                models.RiskMedium,
		Validation:          "Confirm collectors complete without limitations unique to this major version.",
	}
	return []models.Finding{f}, nil
}
