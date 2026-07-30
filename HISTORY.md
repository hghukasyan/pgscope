# Historical Telemetry & Investigation (Phase 4)

PGScope persists local observations so it can explain **why** PostgreSQL behavior changed—not only that it changed.

Workflow: **Observe → Detect → Correlate → Investigate → Explain → Recommend → Verify**

## Storage

- Backend: SQLite (`modernc.org/sqlite`, no CGO) at `~/.pgscope/history.db` by default
- Stores snapshot metadata, metric samples, findings, regressions, incidents, hypotheses, timeline events, evidence edges, and compact vector benchmark summaries
- Credentials are never stored; query text is off by default (`history.store_query_text: false`)
- Embeddings are never stored
- Directory `0700`, database file `0600`
- Schema migrations + WAL mode; retention via `pgscope history cleanup`

Enable in config:

```yaml
history:
  enabled: true
  backend: sqlite
  path: ~/.pgscope/history.db
```

## Commands

```bash
pgscope history status
pgscope history snapshots --last 24h
pgscope history metrics --last 1h
pgscope history cleanup

pgscope regressions list
pgscope regressions detect --last 1h
pgscope regressions queries
pgscope regressions vector

pgscope incidents list
pgscope incidents show <id>
pgscope incidents timeline <id>
pgscope incidents resolve <id>   # local history only

pgscope investigate --last 1h
pgscope investigate --query-id <id>
pgscope investigate --incident <id>

pgscope monitor start            # continuous collection
pgscope monitor once
pgscope serve --config pgscope.yaml
```

Vector recall can persist comparable summaries:

```bash
pgscope vector recall --config workload.yaml --authorize-benchmark --persist-history
```

## Counter correctness

Rates are derived only when:

- Timestamps are strictly increasing and within sane bounds
- Reset tokens match (postmaster start / stats_reset identity)
- Counters do not decrease

Negative deltas are reported as `reset` / unavailable—never converted into fake positive rates.

## Regression detection

Uses rolling median + MAD with relative thresholds and **sustained breach** requirements. Transient spikes do not open incidents. Reliability is labeled `strong|moderate|weak|insufficient` (no fake p-values).

## Incidents & RCA

Incidents group related regressions by fingerprint (instance, database, metric family, query/relation identity). Correlation edges use typed relationships (`BLOCKS`, `AFFECTS`, `CORRELATES_WITH`, `SUPPORTS`, …) and never assert unproven `CAUSES`.

Hypothesis rules (lock contention, long transactions, connection saturation, planner regression, autovacuum interference, WAL retention, replication lag, vector config regression) emit supporting, contradicting, and missing evidence with explainable confidence categories.

## Continuous monitoring

`pgscope monitor start` runs bounded cycles with collection timeout, overlap skipping, jitter, graceful SIGINT/SIGTERM shutdown, snapshot persistence, regression evaluation, incident processing, and periodic retention cleanup. Existing one-shot CLI commands remain usable without the monitor.
