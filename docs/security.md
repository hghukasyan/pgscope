# Security

Canonical policy: [SECURITY.md](../SECURITY.md).

## Defaults

- Read-only sessions (`default_transaction_read_only=on`)
- Statement and lock timeouts
- Bounded connection pool
- DSN password redaction
- Query text truncation / optional omission
- No session kills, no DDL, no config changes

## Expensive / invasive operations (opt-in)

| Operation | Required flags / constraints |
|-----------|------------------------------|
| `EXPLAIN ANALYZE` | `--analyze --authorize-analyze` |
| Vector recall / tune / filters | `--authorize-benchmark` + `environment: isolated\|lab` |

Prefer an isolated lab database for ANALYZE and vector benchmarks.

## Reporting vulnerabilities

Use a private GitHub security advisory. Do not open public issues with exploit details before a fix is available.
