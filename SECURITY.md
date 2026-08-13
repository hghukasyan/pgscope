# Security

PGScope is designed for production use with a **read-only, least-privilege** posture.

## Guarantees

- Sets `default_transaction_read_only = on` on every session when `read_only: true` (default).
- Applies `statement_timeout` and `lock_timeout`.
- Uses a bounded `pgx` pool (`max_conns` default 4).
- Redacts passwords in DSN display/logging (`database.RedactDSN`).
- Truncates query text for previews; does not bind or expand parameters.
- With `sensitive_data_safe: true` (default), statement text in reports is truncated/redacted and marked.
- `collect_query_text: false` omits statement text entirely.
- Does not terminate backends.
- Does not create/drop/rebuild indexes.
- Does not change PostgreSQL configuration.
- Does not drop replication slots.
- Default EXPLAIN runs `ANALYZE false` only after PostgreSQL-aware validation of a single read-only SELECT/WITH/VALUES/TABLE statement.
- Rejects multi-statement SQL and data-modifying CTEs before EXPLAIN.
- `EXPLAIN ANALYZE` requires `--analyze` **and** `--authorize-analyze`.
- Potentially side-effecting function calls under ANALYZE require `--trusted-isolated`.
- Vector recall/filters/tune require `--authorize-benchmark` and `environment: isolated|lab`.
- Benchmark samples, concurrency, duration, and candidate counts are hard-capped.
- Filter YAML supports only `=` / `IN` with validated identifiers and bound parameters.
- Raw embeddings are never written to reports or history files.
- Local history (SQLite) stores no DSNs/passwords; query text off by default; file mode `0600`, dir `0700`.
- Incident `resolve` affects PGScope local history only — never PostgreSQL sessions or configuration.
- Monitoring never auto-kills sessions, never runs DDL, never executes correlation-derived SQL.
- Metric cardinality is bounded at extraction time; correlation graphs are node-capped.

## Important honesty limits

A read-only transaction **cannot** prevent every external side effect (for example some user-defined functions, dblink, or filesystem helpers). Prefer an isolated lab database for ANALYZE.

Keyword matching alone is not the security boundary; validation uses a PostgreSQL-aware lexer (comments, dollar-quoting, statement splitting) plus structural checks.

## Secrets

- Prefer `DATABASE_URL` / `PGSCOPE_DSN` environment variables over committing DSNs.
- Never log vector embeddings (Phase 3).
- CI must use disposable credentials only.
- Treat JSON/YAML reports as sensitive when query text is included.

## Threat notes

- A role with `pg_monitor` can read query text from `pg_stat_activity` / `pg_stat_statements`.
- Cloud-managed Postgres may hide replication catalogs; missing access becomes a limitation, not an error crash.

## Reporting vulnerabilities

Prefer a **private GitHub security advisory** on this repository. Contact the maintainers only if advisories are unavailable. Do not file public issues with exploit details until a fix is available.

Community expectations: [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md).
