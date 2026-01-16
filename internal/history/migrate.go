package history

import (
	"context"
	"database/sql"
	"fmt"
)

const schemaVersion = 1

var migrations = []string{
	`-- schema_migrations table created separately
CREATE TABLE IF NOT EXISTS snapshots (
  id TEXT PRIMARY KEY,
  collected_at TEXT NOT NULL,
  instance_key TEXT NOT NULL,
  database_name TEXT NOT NULL DEFAULT '',
  pg_version TEXT NOT NULL DEFAULT '',
  duration_ms INTEGER NOT NULL DEFAULT 0,
  finding_count INTEGER NOT NULL DEFAULT 0,
  metric_count INTEGER NOT NULL DEFAULT 0,
  status TEXT NOT NULL DEFAULT 'ok'
);
CREATE INDEX IF NOT EXISTS idx_snapshots_collected ON snapshots(collected_at);
CREATE INDEX IF NOT EXISTS idx_snapshots_instance ON snapshots(instance_key, collected_at);

CREATE TABLE IF NOT EXISTS metrics (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  snapshot_id TEXT NOT NULL,
  collected_at TEXT NOT NULL,
  instance_key TEXT NOT NULL,
  database_name TEXT NOT NULL DEFAULT '',
  name TEXT NOT NULL,
  value REAL NOT NULL,
  kind TEXT NOT NULL,
  unit TEXT NOT NULL DEFAULT '',
  dimensions_json TEXT NOT NULL DEFAULT '{}',
  source TEXT NOT NULL DEFAULT '',
  reset_token TEXT NOT NULL DEFAULT '',
  quality TEXT NOT NULL DEFAULT 'ok',
  duration_ms INTEGER NOT NULL DEFAULT 0,
  FOREIGN KEY(snapshot_id) REFERENCES snapshots(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_metrics_name_time ON metrics(name, collected_at);
CREATE INDEX IF NOT EXISTS idx_metrics_snap ON metrics(snapshot_id);
CREATE INDEX IF NOT EXISTS idx_metrics_inst_name ON metrics(instance_key, name, collected_at);

CREATE TABLE IF NOT EXISTS findings (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  snapshot_id TEXT NOT NULL,
  collected_at TEXT NOT NULL,
  rule_id TEXT NOT NULL,
  severity TEXT NOT NULL,
  payload_json TEXT NOT NULL,
  FOREIGN KEY(snapshot_id) REFERENCES snapshots(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_findings_snap ON findings(snapshot_id);

CREATE TABLE IF NOT EXISTS regressions (
  id TEXT PRIMARY KEY,
  detected_at TEXT NOT NULL,
  metric_name TEXT NOT NULL,
  instance_key TEXT NOT NULL,
  database_name TEXT NOT NULL DEFAULT '',
  payload_json TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_regressions_time ON regressions(detected_at);
CREATE INDEX IF NOT EXISTS idx_regressions_metric ON regressions(metric_name, detected_at);

CREATE TABLE IF NOT EXISTS incidents (
  id TEXT PRIMARY KEY,
  fingerprint TEXT NOT NULL,
  instance_key TEXT NOT NULL,
  database_name TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL,
  severity TEXT NOT NULL,
  started_at TEXT NOT NULL,
  last_observed_at TEXT NOT NULL,
  resolved_at TEXT,
  payload_json TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_incidents_status ON incidents(status, last_observed_at);
CREATE INDEX IF NOT EXISTS idx_incidents_fp ON incidents(fingerprint, status);

CREATE TABLE IF NOT EXISTS hypotheses (
  id TEXT PRIMARY KEY,
  incident_id TEXT NOT NULL,
  rank INTEGER NOT NULL DEFAULT 0,
  payload_json TEXT NOT NULL,
  FOREIGN KEY(incident_id) REFERENCES incidents(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_hypotheses_incident ON hypotheses(incident_id);

CREATE TABLE IF NOT EXISTS timeline_events (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  incident_id TEXT NOT NULL,
  at TEXT NOT NULL,
  kind TEXT NOT NULL,
  payload_json TEXT NOT NULL,
  FOREIGN KEY(incident_id) REFERENCES incidents(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_timeline_incident ON timeline_events(incident_id, at);

CREATE TABLE IF NOT EXISTS evidence_edges (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  incident_id TEXT NOT NULL,
  from_node TEXT NOT NULL,
  to_node TEXT NOT NULL,
  edge_type TEXT NOT NULL,
  note TEXT NOT NULL DEFAULT '',
  FOREIGN KEY(incident_id) REFERENCES incidents(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_edges_incident ON evidence_edges(incident_id);

CREATE TABLE IF NOT EXISTS vector_benchmarks (
  id TEXT PRIMARY KEY,
  collected_at TEXT NOT NULL,
  instance_key TEXT NOT NULL,
  database_name TEXT NOT NULL DEFAULT '',
  workload_fingerprint TEXT NOT NULL,
  config_fingerprint TEXT NOT NULL,
  recall_at_k REAL NOT NULL,
  k INTEGER NOT NULL,
  p50_latency_ms REAL NOT NULL,
  p95_latency_ms REAL NOT NULL,
  sample_count INTEGER NOT NULL,
  index_name TEXT NOT NULL DEFAULT '',
  notes TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_vecbench_fp ON vector_benchmarks(workload_fingerprint, collected_at);
`,
}

func migrate(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS schema_migrations (
  version INTEGER PRIMARY KEY,
  applied_at TEXT NOT NULL
)`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	var current int
	err := db.QueryRowContext(ctx, `SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&current)
	if err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}

	for v := current; v < schemaVersion; v++ {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, migrations[v]); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("migration %d: %w", v+1, err)
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO schema_migrations(version, applied_at) VALUES (?, datetime('now'))`, v+1); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("record migration %d: %w", v+1, err)
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}
