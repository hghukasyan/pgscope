package history

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/hghukasyan/pgscope/pkg/models"
	_ "modernc.org/sqlite"
)

// SQLiteStore is a crash-safe local history backend.
type SQLiteStore struct {
	db   *sql.DB
	path string
	mu   sync.Mutex // serializes writers; readers may proceed via SQLite WAL
}

// OpenSQLite opens or creates a history database at path with secure permissions.
func OpenSQLite(ctx context.Context, path string) (*SQLiteStore, error) {
	if path == "" {
		return nil, fmt.Errorf("history path is empty")
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create history dir %s: %w", dir, err)
	}
	// Ensure directory stays private if it already existed with looser perms.
	_ = os.Chmod(dir, 0o700)

	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=synchronous(NORMAL)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(2)
	db.SetConnMaxLifetime(time.Hour)

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	// Restrict file permissions after creation.
	_ = os.Chmod(path, 0o600)

	if err := migrate(ctx, db); err != nil {
		_ = db.Close()
		return nil, err
	}

	return &SQLiteStore{db: db, path: path}, nil
}

func (s *SQLiteStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *SQLiteStore) Status(ctx context.Context) (models.HistoryStatus, error) {
	st := models.HistoryStatus{
		Enabled:       true,
		Backend:       "sqlite",
		Path:          s.path,
		SchemaVersion: schemaVersion,
	}
	if fi, err := os.Stat(s.path); err == nil {
		st.DBSizeBytes = fi.Size()
	}
	_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM snapshots`).Scan(&st.SnapshotCount)
	_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM metrics`).Scan(&st.MetricCount)
	_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM incidents`).Scan(&st.IncidentCount)
	_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM regressions`).Scan(&st.RegressionCount)

	var oldest, newest sql.NullString
	_ = s.db.QueryRowContext(ctx, `SELECT MIN(collected_at), MAX(collected_at) FROM snapshots`).Scan(&oldest, &newest)
	if oldest.Valid {
		if t, err := time.Parse(time.RFC3339Nano, oldest.String); err == nil {
			st.OldestSnapshot = &t
		}
	}
	if newest.Valid {
		if t, err := time.Parse(time.RFC3339Nano, newest.String); err == nil {
			st.NewestSnapshot = &t
		}
	}
	return st, nil
}

func (s *SQLiteStore) SaveSnapshot(ctx context.Context, meta models.HistorySnapshotMeta, findings []models.Finding, metrics []models.MetricSample) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	meta.FindingCount = len(findings)
	meta.MetricCount = len(metrics)
	if meta.Status == "" {
		meta.Status = "ok"
	}

	_, err = tx.ExecContext(ctx, `
INSERT INTO snapshots(id, collected_at, instance_key, database_name, pg_version, duration_ms, finding_count, metric_count, status)
VALUES(?,?,?,?,?,?,?,?,?)
ON CONFLICT(id) DO UPDATE SET
  finding_count=excluded.finding_count,
  metric_count=excluded.metric_count,
  status=excluded.status`,
		meta.ID, meta.CollectedAt.UTC().Format(time.RFC3339Nano), meta.InstanceKey, meta.Database,
		meta.PGVersion, meta.DurationMS, meta.FindingCount, meta.MetricCount, meta.Status)
	if err != nil {
		return fmt.Errorf("insert snapshot: %w", err)
	}

	for _, f := range findings {
		payload, err := json.Marshal(f)
		if err != nil {
			return err
		}
		at := f.ObservedAt
		if at.IsZero() {
			at = meta.CollectedAt
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO findings(snapshot_id, collected_at, rule_id, severity, payload_json) VALUES(?,?,?,?,?)`,
			meta.ID, at.UTC().Format(time.RFC3339Nano), f.RuleID, string(f.Severity), string(payload)); err != nil {
			return fmt.Errorf("insert finding: %w", err)
		}
	}

	for _, m := range metrics {
		dims, err := json.Marshal(m.Dimensions)
		if err != nil {
			return err
		}
		if m.Quality == "" {
			m.Quality = models.QualityOK
		}
		at := m.CollectedAt
		if at.IsZero() {
			at = meta.CollectedAt
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO metrics(snapshot_id, collected_at, instance_key, database_name, name, value, kind, unit,
  dimensions_json, source, reset_token, quality, duration_ms)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			meta.ID, at.UTC().Format(time.RFC3339Nano), nullStr(m.InstanceKey, meta.InstanceKey),
			nullStr(m.Database, meta.Database), m.Name, m.Value, string(m.Kind), m.Unit,
			string(dims), m.Source, m.ResetToken, string(m.Quality), m.DurationMS); err != nil {
			return fmt.Errorf("insert metric: %w", err)
		}
	}

	return tx.Commit()
}

func (s *SQLiteStore) ListSnapshots(ctx context.Context, since, until time.Time, limit int) ([]models.HistorySnapshotMeta, error) {
	if limit <= 0 {
		limit = 50
	}
	q := `SELECT id, collected_at, instance_key, database_name, pg_version, duration_ms, finding_count, metric_count, status
FROM snapshots WHERE 1=1`
	args := []any{}
	if !since.IsZero() {
		q += ` AND collected_at >= ?`
		args = append(args, since.UTC().Format(time.RFC3339Nano))
	}
	if !until.IsZero() {
		q += ` AND collected_at <= ?`
		args = append(args, until.UTC().Format(time.RFC3339Nano))
	}
	q += ` ORDER BY collected_at DESC LIMIT ?`
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []models.HistorySnapshotMeta
	for rows.Next() {
		var m models.HistorySnapshotMeta
		var at string
		if err := rows.Scan(&m.ID, &at, &m.InstanceKey, &m.Database, &m.PGVersion, &m.DurationMS, &m.FindingCount, &m.MetricCount, &m.Status); err != nil {
			return nil, err
		}
		m.CollectedAt, _ = time.Parse(time.RFC3339Nano, at)
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) ListMetrics(ctx context.Context, name string, since, until time.Time, limit int) ([]models.MetricSample, error) {
	if limit <= 0 {
		limit = 200
	}
	q := `SELECT snapshot_id, collected_at, instance_key, database_name, name, value, kind, unit,
 dimensions_json, source, reset_token, quality, duration_ms FROM metrics WHERE 1=1`
	args := []any{}
	if name != "" {
		q += ` AND name = ?`
		args = append(args, name)
	}
	if !since.IsZero() {
		q += ` AND collected_at >= ?`
		args = append(args, since.UTC().Format(time.RFC3339Nano))
	}
	if !until.IsZero() {
		q += ` AND collected_at <= ?`
		args = append(args, until.UTC().Format(time.RFC3339Nano))
	}
	q += ` ORDER BY collected_at DESC LIMIT ?`
	args = append(args, limit)
	return s.queryMetrics(ctx, q, args...)
}

func (s *SQLiteStore) ListMetricSeries(ctx context.Context, name string, dims map[string]string, since, until time.Time) ([]models.MetricSample, error) {
	samples, err := s.ListMetrics(ctx, name, since, until, 5000)
	if err != nil {
		return nil, err
	}
	if len(dims) == 0 {
		// reverse to ascending for series analysis
		rev := make([]models.MetricSample, len(samples))
		for i, s := range samples {
			rev[len(samples)-1-i] = s
		}
		return rev, nil
	}
	var filtered []models.MetricSample
	for i := len(samples) - 1; i >= 0; i-- {
		m := samples[i]
		ok := true
		for k, v := range dims {
			if m.Dimensions[k] != v {
				ok = false
				break
			}
		}
		if ok {
			filtered = append(filtered, m)
		}
	}
	return filtered, nil
}

func (s *SQLiteStore) queryMetrics(ctx context.Context, q string, args ...any) ([]models.MetricSample, error) {
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.MetricSample
	for rows.Next() {
		var m models.MetricSample
		var at, kind, quality, dimsJSON string
		if err := rows.Scan(&m.SnapshotID, &at, &m.InstanceKey, &m.Database, &m.Name, &m.Value, &kind, &m.Unit,
			&dimsJSON, &m.Source, &m.ResetToken, &quality, &m.DurationMS); err != nil {
			return nil, err
		}
		m.CollectedAt, _ = time.Parse(time.RFC3339Nano, at)
		m.Kind = models.MetricKind(kind)
		m.Quality = models.MetricQuality(quality)
		_ = json.Unmarshal([]byte(dimsJSON), &m.Dimensions)
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) SaveRegressions(ctx context.Context, regs []models.Regression) error {
	if len(regs) == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, r := range regs {
		payload, err := json.Marshal(r)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO regressions(id, detected_at, metric_name, instance_key, database_name, payload_json)
VALUES(?,?,?,?,?,?)
ON CONFLICT(id) DO UPDATE SET payload_json=excluded.payload_json`,
			r.ID, r.DetectedAt.UTC().Format(time.RFC3339Nano), r.MetricName, r.InstanceKey, r.Database, string(payload)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *SQLiteStore) ListRegressions(ctx context.Context, since, until time.Time, limit int) ([]models.Regression, error) {
	if limit <= 0 {
		limit = 100
	}
	q := `SELECT payload_json FROM regressions WHERE 1=1`
	args := []any{}
	if !since.IsZero() {
		q += ` AND detected_at >= ?`
		args = append(args, since.UTC().Format(time.RFC3339Nano))
	}
	if !until.IsZero() {
		q += ` AND detected_at <= ?`
		args = append(args, until.UTC().Format(time.RFC3339Nano))
	}
	q += ` ORDER BY detected_at DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.Regression
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			return nil, err
		}
		var r models.Regression
		if err := json.Unmarshal([]byte(payload), &r); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) GetRegression(ctx context.Context, id string) (models.Regression, error) {
	var payload string
	err := s.db.QueryRowContext(ctx, `SELECT payload_json FROM regressions WHERE id=?`, id).Scan(&payload)
	if err != nil {
		return models.Regression{}, err
	}
	var r models.Regression
	return r, json.Unmarshal([]byte(payload), &r)
}

func (s *SQLiteStore) SaveIncident(ctx context.Context, inc models.Incident) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	payload, err := json.Marshal(inc)
	if err != nil {
		return err
	}
	var resolved any
	if inc.ResolvedAt != nil {
		resolved = inc.ResolvedAt.UTC().Format(time.RFC3339Nano)
	}
	_, err = s.db.ExecContext(ctx, `
INSERT INTO incidents(id, fingerprint, instance_key, database_name, status, severity, started_at, last_observed_at, resolved_at, payload_json)
VALUES(?,?,?,?,?,?,?,?,?,?)
ON CONFLICT(id) DO UPDATE SET
  status=excluded.status,
  severity=excluded.severity,
  last_observed_at=excluded.last_observed_at,
  resolved_at=excluded.resolved_at,
  payload_json=excluded.payload_json`,
		inc.ID, inc.Fingerprint, inc.InstanceKey, inc.Database, string(inc.Status), string(inc.Severity),
		inc.StartedAt.UTC().Format(time.RFC3339Nano), inc.LastObservedAt.UTC().Format(time.RFC3339Nano),
		resolved, string(payload))
	return err
}

func (s *SQLiteStore) ListIncidents(ctx context.Context, status models.IncidentStatus, limit int) ([]models.Incident, error) {
	if limit <= 0 {
		limit = 50
	}
	q := `SELECT payload_json FROM incidents WHERE 1=1`
	args := []any{}
	if status != "" {
		q += ` AND status = ?`
		args = append(args, string(status))
	}
	q += ` ORDER BY last_observed_at DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.Incident
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			return nil, err
		}
		var inc models.Incident
		if err := json.Unmarshal([]byte(payload), &inc); err != nil {
			return nil, err
		}
		out = append(out, inc)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) GetIncident(ctx context.Context, id string) (models.Incident, error) {
	var payload string
	err := s.db.QueryRowContext(ctx, `SELECT payload_json FROM incidents WHERE id=?`, id).Scan(&payload)
	if err != nil {
		return models.Incident{}, err
	}
	var inc models.Incident
	return inc, json.Unmarshal([]byte(payload), &inc)
}

func (s *SQLiteStore) FindOpenByFingerprint(ctx context.Context, fingerprint string) (models.Incident, bool, error) {
	var payload string
	err := s.db.QueryRowContext(ctx,
		`SELECT payload_json FROM incidents WHERE fingerprint=? AND status IN ('open','investigating','mitigated')
ORDER BY last_observed_at DESC LIMIT 1`, fingerprint).Scan(&payload)
	if err == sql.ErrNoRows {
		return models.Incident{}, false, nil
	}
	if err != nil {
		return models.Incident{}, false, err
	}
	var inc models.Incident
	if err := json.Unmarshal([]byte(payload), &inc); err != nil {
		return models.Incident{}, false, err
	}
	return inc, true, nil
}

func (s *SQLiteStore) SaveHypotheses(ctx context.Context, hyps []models.Hypothesis) error {
	if len(hyps) == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, h := range hyps {
		payload, err := json.Marshal(h)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO hypotheses(id, incident_id, rank, payload_json) VALUES(?,?,?,?)
ON CONFLICT(id) DO UPDATE SET rank=excluded.rank, payload_json=excluded.payload_json`,
			h.ID, h.IncidentID, h.Rank, string(payload)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *SQLiteStore) ListHypotheses(ctx context.Context, incidentID string) ([]models.Hypothesis, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT payload_json FROM hypotheses WHERE incident_id=? ORDER BY rank ASC`, incidentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.Hypothesis
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			return nil, err
		}
		var h models.Hypothesis
		if err := json.Unmarshal([]byte(payload), &h); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) SaveTimeline(ctx context.Context, incidentID string, events []models.TimelineEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DELETE FROM timeline_events WHERE incident_id=?`, incidentID); err != nil {
		return err
	}
	for _, e := range events {
		payload, err := json.Marshal(e)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO timeline_events(incident_id, at, kind, payload_json) VALUES(?,?,?,?)`,
			incidentID, e.At.UTC().Format(time.RFC3339Nano), e.Kind, string(payload)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *SQLiteStore) ListTimeline(ctx context.Context, incidentID string) ([]models.TimelineEvent, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT payload_json FROM timeline_events WHERE incident_id=? ORDER BY at ASC`, incidentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.TimelineEvent
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			return nil, err
		}
		var e models.TimelineEvent
		if err := json.Unmarshal([]byte(payload), &e); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) SaveEdges(ctx context.Context, incidentID string, edges []models.EvidenceEdge) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DELETE FROM evidence_edges WHERE incident_id=?`, incidentID); err != nil {
		return err
	}
	for _, e := range edges {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO evidence_edges(incident_id, from_node, to_node, edge_type, note) VALUES(?,?,?,?,?)`,
			incidentID, e.From, e.To, string(e.Type), e.Note); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *SQLiteStore) ListEdges(ctx context.Context, incidentID string) ([]models.EvidenceEdge, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT from_node, to_node, edge_type, note FROM evidence_edges WHERE incident_id=?`, incidentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.EvidenceEdge
	for rows.Next() {
		var e models.EvidenceEdge
		var t string
		if err := rows.Scan(&e.From, &e.To, &t, &e.Note); err != nil {
			return nil, err
		}
		e.Type = models.EdgeType(t)
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) SaveVectorBenchmark(ctx context.Context, summary VectorBenchmarkSummary) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.ExecContext(ctx, `
INSERT INTO vector_benchmarks(id, collected_at, instance_key, database_name, workload_fingerprint, config_fingerprint,
  recall_at_k, k, p50_latency_ms, p95_latency_ms, sample_count, index_name, notes)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)
ON CONFLICT(id) DO UPDATE SET recall_at_k=excluded.recall_at_k, p50_latency_ms=excluded.p50_latency_ms, p95_latency_ms=excluded.p95_latency_ms`,
		summary.ID, summary.CollectedAt.UTC().Format(time.RFC3339Nano), summary.InstanceKey, summary.Database,
		summary.WorkloadFingerprint, summary.ConfigFingerprint, summary.RecallAtK, summary.K,
		summary.P50LatencyMS, summary.P95LatencyMS, summary.SampleCount, summary.IndexName, summary.Notes)
	return err
}

func (s *SQLiteStore) ListVectorBenchmarks(ctx context.Context, fingerprint string, since, until time.Time, limit int) ([]VectorBenchmarkSummary, error) {
	if limit <= 0 {
		limit = 50
	}
	q := `SELECT id, collected_at, instance_key, database_name, workload_fingerprint, config_fingerprint,
 recall_at_k, k, p50_latency_ms, p95_latency_ms, sample_count, index_name, notes FROM vector_benchmarks WHERE 1=1`
	args := []any{}
	if fingerprint != "" {
		q += ` AND workload_fingerprint = ?`
		args = append(args, fingerprint)
	}
	if !since.IsZero() {
		q += ` AND collected_at >= ?`
		args = append(args, since.UTC().Format(time.RFC3339Nano))
	}
	if !until.IsZero() {
		q += ` AND collected_at <= ?`
		args = append(args, until.UTC().Format(time.RFC3339Nano))
	}
	q += ` ORDER BY collected_at DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []VectorBenchmarkSummary
	for rows.Next() {
		var b VectorBenchmarkSummary
		var at string
		if err := rows.Scan(&b.ID, &at, &b.InstanceKey, &b.Database, &b.WorkloadFingerprint, &b.ConfigFingerprint,
			&b.RecallAtK, &b.K, &b.P50LatencyMS, &b.P95LatencyMS, &b.SampleCount, &b.IndexName, &b.Notes); err != nil {
			return nil, err
		}
		b.CollectedAt, _ = time.Parse(time.RFC3339Nano, at)
		out = append(out, b)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) Cleanup(ctx context.Context, retention RetentionPolicy) (CleanupStats, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var stats CleanupStats
	now := time.Now().UTC()

	if retention.RawMetrics > 0 {
		cutoff := now.Add(-retention.RawMetrics).Format(time.RFC3339Nano)
		res, err := s.db.ExecContext(ctx, `DELETE FROM metrics WHERE collected_at < ?`, cutoff)
		if err != nil {
			return stats, err
		}
		stats.MetricsDeleted, _ = res.RowsAffected()
		res, err = s.db.ExecContext(ctx, `
DELETE FROM snapshots WHERE collected_at < ? AND id NOT IN (SELECT DISTINCT snapshot_id FROM metrics)`, cutoff)
		if err != nil {
			return stats, err
		}
		stats.SnapshotsDeleted, _ = res.RowsAffected()
		res, err = s.db.ExecContext(ctx, `DELETE FROM regressions WHERE detected_at < ?`, cutoff)
		if err != nil {
			return stats, err
		}
		stats.RegressionsDeleted, _ = res.RowsAffected()
	}
	if retention.Incidents > 0 {
		cutoff := now.Add(-retention.Incidents).Format(time.RFC3339Nano)
		res, err := s.db.ExecContext(ctx,
			`DELETE FROM incidents WHERE status='resolved' AND COALESCE(resolved_at, last_observed_at) < ?`, cutoff)
		if err != nil {
			return stats, err
		}
		stats.IncidentsDeleted, _ = res.RowsAffected()
	}
	_, _ = s.db.ExecContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`)
	return stats, nil
}

func nullStr(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}

// Ensure SQLiteStore implements Store.
var _ Store = (*SQLiteStore)(nil)
