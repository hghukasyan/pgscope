# Historical telemetry & monitoring

Detailed notes: [HISTORY.md](../HISTORY.md).

## What is implemented

- Local SQLite history (snapshots, metrics, findings, regressions, incidents, hypotheses, timelines)
- Safe counter delta / rate derivation (resets → unavailable, never fake positive rates)
- Regression detection (rolling median + MAD, sustained breach)
- Incident deduplication and local resolve
- Competing root-cause hypotheses with support / contradict / missing evidence
- Continuous monitor with overlap skip, jitter, graceful shutdown

## Commands

```bash
pgscope history status
pgscope monitor once
pgscope monitor start
pgscope regressions detect --last 1h
pgscope investigate --last 1h
pgscope incidents list
```

Enable history in YAML (`history.enabled: true`) or use `monitor` which opens the configured SQLite path.

## Planned (not implemented)

- PostgreSQL as a history backend (interface exists; SQLite only today)
- Prometheus exporter for PGScope internal metrics
- Aggregated long-retention rollups
