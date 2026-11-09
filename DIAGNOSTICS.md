# Diagnostics

## Rule catalog

### Phase 1

| Rule ID | Severity | Trigger |
|---------|----------|---------|
| `CONN.SATURATION` | warning/critical | Connection utilization ≥ 75% / 90% |
| `TXN.IDLE_IN_TRANSACTION` | warning/critical | Idle-in-transaction sessions |
| `TXN.LONG_RUNNING` | warning | Transactions older than threshold |
| `TXN.XID_AGE` | warning/critical | `age(datfrozenxid)` elevated |
| `LOCK.CONTENTION` | warning/critical | Ungranted locks / blocking chains |
| `LOCK.DEADLOCKS` | info/warning | Cumulative deadlock counter > 0 |
| `IDX.INVALID` | critical | `indisvalid = false` |
| `INST.VERSION_SUPPORT` | warning | Major version outside 14–18 |

### Phase 2 — Queries

| Rule ID | Severity | Trigger |
|---------|----------|---------|
| `QUERY.HIGH_TOTAL_TIME` | info/warning | ≥25% of collected total time **and** call/latency context |
| `QUERY.HIGH_TEMP_IO` | warning | High temp blocks **and** meaningful time/calls |
| `QUERY.HIGH_SHARED_READS` | info | High shared reads with non-trivial miss ratio |
| `QUERY.HIGH_WAL` | info | High `wal_bytes` when column exists **and** time/calls |
| `QUERY.HIGH_FREQUENCY` | info | Very high calls with non-trivial cost |
| `QUERY.POOR_ROW_EFFICIENCY` | info | Low rows vs buffer access **and** time |

### Phase 2 — EXPLAIN (emitted by explain engine)

| Rule ID | Severity | Notes |
|---------|----------|-------|
| `EXPLAIN.CARDINALITY` | info/warning | Requires ANALYZE; ratio ≥10× with meaningful rows |
| `EXPLAIN.LARGE_SEQ_SCAN` | info/warning | Seq Scan alone is **not** enough |
| `EXPLAIN.NESTED_LOOP_HEAVY` | warning | Nested Loop alone is **not** enough |
| `EXPLAIN.SORT_SPILL` | warning | Disk sort from ANALYZE |
| `EXPLAIN.HASH_SPILL` | warning | Hash batches > 1 |
| `EXPLAIN.FILTER_INEFFICIENT` | info | Large Rows Removed by Filter |
| `EXPLAIN.INDEX_CANDIDATE` | info | Conservative filter→column heuristic |

### Phase 2 — Indexes / vacuum / WAL / replication

| Rule ID | Severity | Trigger |
|---------|----------|---------|
| `IDX.ZERO_SCANS` | info | Non-constraint zero-scan candidates (never auto-drop) |
| `IDX.DUPLICATE` | warning | Structurally identical indexes |
| `IDX.REDUNDANT_BTREE` | info | Possible B-tree prefix redundancy |
| `IDX.LARGE_LOW_USAGE` | info | Large size + low scans |
| `VACUUM.DEAD_TUPLES` | info/warning | Past effective vacuum threshold |
| `VACUUM.FREEZE_AGE` | warning/critical | Near `autovacuum_freeze_max_age` |
| `VACUUM.STALE_STATS` | info | Past effective analyze threshold |
| `VACUUM.LONG_XACT_BLOCK` | warning | Long xacts + vacuum risk context |
| `WAL.CHECKPOINT_REQ` | info/warning | High requested checkpoint ratio |
| `WAL.BUFFERS_FULL` | info/warning | `wal_buffers_full` > 0 |
| `REPL.LAG` | info/warning | Byte/time lag; NULL ≠ 0 |
| `REPL.INACTIVE_SLOT` | info–critical | Inactive slot retention |
| `REPL.STATE` | warning | Non-streaming connection state |
| `REPL.CONFLICTS` | info | Standby conflict counters |

## Finding fields

Each finding includes rule ID, severity, object context, description, evidence (`fact` / `observation` / `hypothesis`), probable cause, confidence rationale, remediation, risk class, and validation steps.

Facts come from catalogs/views. Probable causes are labeled as inference. Overall health scores are not invented; `overall_status` is derived from the highest finding severity only.

## Limitations

When a collector cannot run (permissions, missing extension, cloud restrictions), PGScope records a `Limitation` with area, reason, impact, and optional hint — and continues. Unavailable diagnostics are never reported as healthy.

### Phase 3 — Vector

| Rule ID | Severity | Trigger |
|---------|----------|---------|
| `VEC.INDEX_INVALID` | critical | Invalid HNSW/IVFFlat index |
| `VEC.MISSING_INDEX` | info | Larger vector column without ANN index |
| `VEC.IVFFLAT_LISTS` | info | Extremely low lists vs table size |
| `VEC.HNSW_EF_CONSTRUCTION` | info | Unusual ef_construction < m |
| `VEC.HNSW_MEMORY_FIT` | warning | HNSW index larger than shared_buffers and/or effective_cache_size |
| `VEC.INDEX_ZERO_SCANS` | info | Large ANN index with zero scans |
| `VEC.EXPLAIN_*` / `VEC.OP_MISMATCH` | info/warning | Vector plan diagnostics |
| `VEC.RECALL_BELOW_TARGET` | warning | Measured Recall@K below objective |
| `VEC.LATENCY_ABOVE_TARGET` | warning | Approx p95 above objective |
| `VEC.INCOMPLETE_RESULTS` | info | Fewer than K results returned |
| `VEC.BASELINE_UNVERIFIED` | warning | Exact path used an index |
| `VEC.FILTER_*` | info/warning | Filter cohort gaps/incompleteness |
| `VEC.TUNE_RECOMMENDATION` | info | Measured search-setting recommendation |
| `VEC.REGRESSION_*` | info–critical | Compatible recall history deltas |

### Phase 4 — Historical investigation (hypothesis types)

| Hypothesis type | Positive evidence (examples) | Notes |
|-----------------|------------------------------|-------|
| `lock_contention` | `locks.*` regressions + lock/block findings | Ranked with query latency co-occurrence |
| `long_transaction` | `transactions.*` / idle-in-xact findings | |
| `connection_saturation` | `connections.*` utilization regressions | |
| `planner_regression` | `query.*` latency/reads; plan findings | Weakened if locks also explain latency |
| `autovacuum_interference` | `vacuum.*` + optional txn co-occurrence | |
| `wal_retention` | `wal.*` / slot retained bytes | |
| `replication_lag` | `replication.*` lag metrics | |
| `vector_config_regression` | `vector.recall_at_k` / latency benchmarks | Requires compatible fingerprints |
| `unknown` | none | Emitted when no rule has support |

Regressions require sustained breaches vs rolling median/MAD baselines. Confidence is categorical (`strong_evidence` … `insufficient_evidence`), never a fabricated percentage.

## Diagnostic limitations (known)

- `n_dead_tup` is an estimate, not precise bloat.
- Single-snapshot WAL/replication metrics cannot prove rates or “increasing lag”.
- Zero `idx_scan` is not sufficient to drop an index.
- Hypothetical planner cost improvements (HypoPG) are not measured runtime gains.
- EXPLAIN without ANALYZE does not execute the query; ANALYZE can still invoke functions with external side effects even in read-only transactions.
- Vector storage payload fields are estimates; measured sizes come from `pg_relation_size`.
- Recall quality depends on representative query vectors and hardware.
- Incident timelines only include observed sample times or explicitly window-derived intervals—never fabricated sub-interval event times.
- Root-cause hypotheses are competing explanations from deterministic rules, not proven causality.
