# Roadmap

## Phase 1 — Production-ready foundation ✅

- [x] Go module + CLI (`pgscope`)
- [x] Config (YAML, env, flags)
- [x] Safe pgx connectivity + version/extension detection
- [x] Snapshot models + diagnostic rule engine
- [x] Connections, transactions, locks, storage, indexes
- [x] Text/JSON/YAML reports
- [x] Unit + integration tests, Docker Compose, GitHub Actions
- [x] Core documentation

## Phase 2 — Deep PostgreSQL diagnostics ✅

- [x] Query Intelligence (`pg_stat_statements`, version-aware columns, ranking, multi-metric rules, redaction)
- [x] EXPLAIN JSON plan analysis (safe validation; ANALYZE opt-in with authorization)
- [x] Index intelligence (duplicates, redundant B-tree prefixes, unused/large-low-usage, recommend)
- [x] Autovacuum / maintenance analyzer (effective thresholds, freeze age, progress)
- [x] WAL + checkpoint monitoring (version-aware views, rate helper for paired snapshots)
- [x] Replication diagnostics (primary/standby, lag with NULL preservation, slots, conflicts)
- [x] CLI: `queries`, `indexes`, `vacuum`, `wal`, `replication`
- [x] Unified reporting + Phase-2 rule set
- [x] Unit + integration tests; race/vet

### Phase 2 known follow-ups (not blockers)

- [ ] Multi-version container matrix (14–18) in CI
- [ ] HypoPG evaluation path when `enable_hypopg: true`
- [ ] Paired-snapshot WAL rate CLI (`wal inspect --previous ...`)
- [ ] Filter `pg_stat_statements` noise from PGScope’s own catalog queries

## Phase 3 — pgvector ✅

- [x] pgvector discovery (types, operators, HNSW/IVFFlat, settings, columns, indexes)
- [x] Vector index intelligence + evidence-backed findings
- [x] Vector EXPLAIN diagnostics (integrated with Phase 2 plan analyzer)
- [x] Opt-in Recall@K with verified exact baselines
- [x] Filtered ANN cohort analysis
- [x] Bounded HNSW/IVFFlat search-parameter tuning
- [x] Labeled storage / capacity estimates
- [x] Compatible recall-history regression comparison
- [x] CLI: `vector inspect|explain|recall|filters|tune|storage|regressions`
- [x] Unit + pgvector Docker integration tests

### Phase 3 known follow-ups (not blockers)

- [ ] Broader filter predicate DSL beyond `=` / `IN`
- [ ] Expression-index exact baseline strategies beyond disable_*scan
- [ ] CI job with `pgvector/pgvector` service by default
- [ ] Sparsevec-specific storage models

## Phase 4 — Historical telemetry & intelligent investigation ✅

- [x] SQLite history store with migrations, WAL, retention, secure permissions
- [x] Metric extraction from existing collectors + safe counter delta/rate handling
- [x] Regression detection (rolling median/MAD, sustained breach, sensitivity)
- [x] Incident lifecycle with fingerprint deduplication and local resolve
- [x] Bounded correlation graph (typed edges; no unproven CAUSES)
- [x] Deterministic root-cause hypothesis rules with support/contradict/missing evidence
- [x] Timeline reconstruction from stored events; investigation reports (text/JSON)
- [x] Continuous monitor (`monitor start` / `serve`) with overlap skip, jitter, graceful shutdown
- [x] pgvector benchmark summary persistence + recall/latency regression path
- [x] CLI: `history`, `regressions`, `incidents`, `investigate`, `monitor`
- [x] Unit tests; race/vet

### Phase 4 known follow-ups (not blockers)

- [ ] Optional PostgreSQL history backend (same Store interface)
- [ ] Aggregated metric rollups for long retention
- [ ] Prometheus exporter for PGScope internal stats
- [ ] Richer planner-regression evidence via stored EXPLAIN fingerprints
- [ ] Integration tests exercising monitor against live Postgres over multiple intervals

## Phase 5 — Optimization Lab

- [ ] Baseline capture + controlled replay
- [ ] HypoPG integration (deep)
- [ ] Config experiment reports (isolated DBs only)

## Phase 6 — Platform

- [ ] Server mode + REST API
- [ ] Dashboard (React/TS)
- [ ] Read-only MCP tools
- [ ] Multi-instance fleet views
