# Architecture

PGScope is a modular Go monolith. Capabilities are packages under `internal/`; shared domain types live in `pkg/models`.

```text
cmd/pgscope          CLI entrypoint
internal/cli         Cobra commands (scan, history, regressions, incidents, investigate, monitor, …)
internal/config      YAML + env configuration
internal/database    Safe pgx pool, timeouts, redaction
internal/collector   Snapshot collection from catalogs/views (version-aware)
internal/sqlsafe     PostgreSQL-aware statement validation for EXPLAIN
internal/explain     EXPLAIN JSON parse + evidence-backed plan diagnostics
internal/vector      pgvector discovery, recall, tune, storage, regression
internal/telemetry   Snapshot → MetricSample extraction; safe counter deltas
internal/history     SQLite persistence, migrations, retention
internal/regression  Rolling median/MAD regression detection
internal/incident    Lifecycle, correlation graph, hypotheses, timelines
internal/monitor     Bounded continuous collection scheduler
internal/diagnosis   Rule engine orchestration
internal/rules       Diagnostic rules (Phase 1–3)
internal/report      Text / JSON / YAML rendering
pkg/models           Snapshot, Finding, Evidence, history/incident types
```

## Data flow

```text
DATABASE_URL → database.Connect (read-only GUCs)
            → collector.Collect → models.Snapshot
            → diagnosis.Engine.Analyze → models.ScanResult
            → report.Write (text|json|yaml)
```

Focused commands (`queries top`, `vacuum inspect`, …) call individual collectors and render without requiring a full scan.

Historical / monitoring path:

```text
collector.Collect → telemetry.ExtractMetrics → history.Store (SQLite)
                  → regression.DetectGroups → incident.ProcessRegressions
                  → hypotheses + timeline + investigation report
```

One-shot CLI diagnostics do not require the monitor or history backend.

EXPLAIN path:

```text
SQL file → sqlsafe.Validate* → EXPLAIN (FORMAT JSON) in read-only tx
        → explain.ParseJSON → explain.AnalyzePlan → findings
```

## Design rules

1. Collectors emit **facts** (or limitations). They never invent metrics.
2. Rules emit **findings** with evidence, probable cause, and confidence rationale.
3. Hypotheses are labeled; confidence scores are not manufactured.
4. Packages depend inward: `cli` → `collector`/`explain`/`diagnosis`/`report` → `models`/`database`/`config`/`sqlsafe`.
5. Circular imports are forbidden.
6. Future API / dashboard / MCP consume the same `ScanResult` types — do not fork domain models.
7. Multi-metric rules only: never flag inefficiency from a single counter in isolation.
8. Rate conclusions require two valid snapshots (see `DeriveWALRates`).

## Collection efficiency

- Bounded `pgx` pool (`max_conns` default 4)
- Context cancellation + per-statement timeouts
- Configurable `top_queries` / `top_indexes` / `top_vacuum_tables` limits
- SQL-side `ORDER BY … LIMIT` for statement ranking
- Collector enable flags and optional `deep_scan`
- Catalog/statistics views only in default scans

## Phase boundaries

| Phase | Focus |
|-------|--------|
| 1 | Connect, snapshot, connections/locks/txns/storage/indexes, CLI scan |
| 2 | Query intelligence, EXPLAIN, index intelligence, vacuum, WAL, replication |
| 3 | pgvector discovery, ANN intelligence, Recall@K, tune, storage, regressions |
| 4 | Historical telemetry, regressions, incidents, RCA, continuous monitor |
| 5 | Optimization lab |
| 6 | API, dashboard, MCP |
