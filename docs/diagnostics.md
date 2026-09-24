# Diagnostics

PGScope evaluates deterministic rules against a collected snapshot. Findings include evidence, probable cause, remediation guidance, and validation hints.

Full rule catalog: [DIAGNOSTICS.md](../DIAGNOSTICS.md).

## Philosophy

- Prefer multi-signal rules (never flag “inefficiency” from a single counter alone).
- Missing permissions/extensions become **limitations**, not fabricated health.
- Rates require compatible samples (counter resets are handled explicitly).

## Areas covered

- Connections & saturation
- Long / idle-in-transaction sessions
- Lock waits & blocking chains
- Indexes (invalid, duplicates, zero-scan candidates, recommendations)
- Query workload (`pg_stat_statements`) and EXPLAIN findings
- Autovacuum / freeze age / vacuum progress
- WAL / checkpoints
- Replication lag & slots
- pgvector index/recall findings
- Historical regressions & root-cause hypotheses

## Output formats

`text` (default), `json`, `yaml`.
