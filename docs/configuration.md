# Configuration

PGScope merges settings from:

1. Built-in defaults
2. Optional YAML file (`--config`)
3. Flags / environment (`DATABASE_URL`, `PGSCOPE_DSN`)

See [`pgscope.example.yaml`](../pgscope.example.yaml) for a complete annotated example.

## Common keys

| Key | Default | Purpose |
|-----|---------|---------|
| `timeout` | `30s` | Overall command timeout |
| `statement_timeout` | `15s` | Per-statement timeout |
| `lock_timeout` | `2s` | Lock wait timeout |
| `max_conns` | `4` | Bounded pgx pool |
| `read_only` | `true` | `default_transaction_read_only=on` |
| `sensitive_data_safe` | `true` | Truncate/redact statement text |
| `collect_query_text` | `true` | Include truncated query text when safe |
| `top_queries` / `top_indexes` / `top_vacuum_tables` | bounded | Cardinality limits |
| `enable_queries` / `enable_vacuum` / `enable_wal` / `enable_replication` / `enable_indexes` | `true` | Collector toggles |

## Phase 4 (history & monitoring)

```yaml
history:
  enabled: true
  backend: sqlite
  path: ~/.pgscope/history.db
  store_query_text: false
  retention:
    raw_metrics: 168h
    incidents: 8760h

monitoring:
  enabled: true
  default_interval: 30s
  collection_timeout: 10s
  max_concurrent_collectors: 3

regression:
  enabled: true
  baseline_window: 1h
  evaluation_window: 10m
  min_samples: 5
  sensitivity: medium   # low | medium | high

incidents:
  enabled: true
  deduplication_window: 10m
  auto_resolve_after: 15m
```

History never stores PostgreSQL credentials. Directory permissions are `0700`; DB file `0600`.

## Formats

`--format text|json|yaml` (default `text`).
