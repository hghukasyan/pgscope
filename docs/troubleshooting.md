# Troubleshooting

## `database connection string required`

Set one of:

```bash
export DATABASE_URL='postgres://...'
# or
export PGSCOPE_DSN='postgres://...'
# or
pgscope --dsn 'postgres://...' scan
```

## Permission / missing view limitations

Findings may be empty while **limitations** list missing privileges or extensions. Grant `pg_monitor` or `pg_read_all_stats`, and install `pg_stat_statements` / `pgvector` when needed.

## `queries top` shows setup hints

`pg_stat_statements` is not installed or not in `shared_preload_libraries`. See [Getting started](getting-started.md).

## History path permission errors

Default path is `~/.pgscope/history.db`. Ensure the home directory is writable, or set:

```yaml
history:
  path: /path/you/can/write/history.db
```

## Monitor skipped cycles

If a collection run exceeds `collection_timeout`, the next tick may log that the previous cycle is still running and skip an overlap. Increase timeout or reduce collector scope.

## Vector recall refused

Benchmarks require `--authorize-benchmark` and a workload YAML with `environment: isolated` or `lab`.

## CI / Go version

The module requires **Go 1.26.x** (see `go.mod` and `.github/workflows/ci.yml`).
