# CLI reference

Global flags:

```text
--config string      YAML config path
--dsn string         PostgreSQL URL (or DATABASE_URL / PGSCOPE_DSN)
-f, --format         text | json | yaml
--timeout duration   overall command timeout (default 30s)
--no-color
-v, --verbose
```

## Core

| Command | Description |
|---------|-------------|
| `pgscope version` | Print version |
| `pgscope connect` | Verify connectivity and identity |
| `pgscope health` | Compact health summary |
| `pgscope scan` | Full collection + rule findings |

## Queries

| Command | Description |
|---------|-------------|
| `pgscope queries top` | Ranked `pg_stat_statements` |
| `pgscope queries explain --file q.sql` | Safe EXPLAIN (FORMAT JSON) |

Sort keys for `top`: `total-time`, `mean-time`, `calls`, `shared-reads`, `temp-io`, `wal`.

`EXPLAIN ANALYZE` requires `--analyze --authorize-analyze` (prefer an isolated database).

## Indexes

| Command | Description |
|---------|-------------|
| `pgscope indexes inspect` | Inventory and usage |
| `pgscope indexes unused` | Zero-scan candidates (**not** drop advice) |
| `pgscope indexes duplicates` | Identical / redundant groups |
| `pgscope indexes recommend` | Conservative missing-index candidates |

## Vacuum / WAL / replication

| Command | Description |
|---------|-------------|
| `pgscope vacuum inspect` | Autovacuum / analyze stats |
| `pgscope vacuum risks` | Maintenance risk tables |
| `pgscope vacuum progress` | Active vacuum progress |
| `pgscope wal inspect` | WAL counters and settings |
| `pgscope wal slots` | Slot WAL retention |
| `pgscope wal checkpoints` | Checkpoint counters |
| `pgscope replication inspect` | Role, connections, slots |
| `pgscope replication slots` | Slot listing |
| `pgscope replication lag` | Byte/time lag (NULLs preserved) |

There is **no** separate `pgscope locks` command; lock waits and blocking chains appear in `scan` / `health`.

## pgvector

| Command | Description |
|---------|-------------|
| `pgscope vector inspect` | Discover columns / indexes |
| `pgscope vector explain` | Vector-aware EXPLAIN |
| `pgscope vector recall` | Opt-in Recall@K (`--authorize-benchmark`) |
| `pgscope vector filters analyze` | Filtered ANN cohorts |
| `pgscope vector tune` | Bounded search-parameter experiments |
| `pgscope vector storage` | Labeled capacity estimates |
| `pgscope vector regressions` | Compatible recall-history comparison |

## History & investigation

| Command | Description |
|---------|-------------|
| `pgscope history status\|snapshots\|metrics\|cleanup` | Local SQLite history |
| `pgscope regressions list\|detect\|queries\|vector` | Regression detection |
| `pgscope incidents list\|show\|timeline\|resolve` | Incident lifecycle (local only) |
| `pgscope investigate` | Correlate evidence + hypotheses |
| `pgscope monitor start\|once` | Continuous monitoring |
| `pgscope serve` | Alias of `monitor start` |

Examples:

```bash
pgscope monitor once
pgscope regressions detect --last 1h
pgscope investigate --last 1h
pgscope incidents list
pgscope incidents show PG-2026-ABC123
```
