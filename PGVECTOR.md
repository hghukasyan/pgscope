# pgvector

Native pgvector diagnostics are a **first-class Phase 3** capability.

## What PGScope does today

- Discovers the `vector` extension (any schema), types (`vector`, `halfvec`, `sparsevec`), operators, HNSW/IVFFlat access methods, operator classes, and settable search GUCs.
- Inventories vector columns (including unconstrained dimensions) and ANN indexes with options (`m`, `ef_construction`, `lists`, …).
- Emits evidence-backed index findings (invalid indexes, missing ANN indexes on larger tables, unusual parameters, zero-scan large indexes).
- Extends EXPLAIN with vector-specific diagnostics (seq scan + distance ops, post-filter on index scans, sort nodes, operator/opclass mismatch hints).
- Runs **opt-in** Recall@K benchmarks with verified exact baselines (index scans disabled + plan inspection).
- Supports filtered cohort analysis, bounded HNSW/IVFFlat search-parameter tuning, labeled storage estimates, and compatible recall-history regression comparison.

Embeddings are never written to diagnostic reports.

## Commands

```bash
export DATABASE_URL='postgres://pgscope:pgscope@localhost:5433/pgscope?sslmode=disable'

pgscope vector inspect
pgscope vector inspect --schema public --table documents --format json
pgscope vector storage --table documents

# Reuses Phase 2 SQL safety (single read-only SELECT)
pgscope vector explain --file query.sql

# Opt-in benchmarks — isolated/lab only
pgscope vector recall --config deployments/vector-workload.example.yaml --authorize-benchmark
pgscope vector filters analyze --config deployments/vector-workload.example.yaml --authorize-benchmark
pgscope vector tune --config deployments/vector-workload.example.yaml --authorize-benchmark --objective recall

# Compare saved history JSON files
pgscope vector regressions --baseline hist/a.json --current hist/b.json
```

## Local vector lab

```bash
docker compose -f deployments/docker-compose.yml --profile vector up -d --wait
export DATABASE_URL='postgres://pgscope:pgscope@localhost:5433/pgscope?sslmode=disable'
# or: export PGSCOPE_VECTOR_DSN=...
./bin/pgscope vector inspect
```

## Distance metrics and operators

| Metric | Operator | Typical opclass |
|--------|----------|-----------------|
| L2 | `<->` | `vector_l2_ops` |
| Cosine | `<=>` | `vector_cosine_ops` |
| Inner product (negated) | `<#>` | `vector_ip_ops` |
| L1 | `<+>` | `vector_l1_ops` |
| Hamming (bit) | `<~>` | `bit_hamming_ops` |
| Jaccard (bit) | `<%>` | `bit_jaccard_ops` |

Operator availability is discovered from catalogs; do not assume every type supports every operator.

## HNSW vs IVFFlat

- **HNSW**: graph index; build params `m`, `ef_construction`; search primarily via `hnsw.ef_search` (and iterative scan settings when available).
- **IVFFlat**: inverted lists; build param `lists`; search via `ivfflat.probes` (and iterative scan / max_probes when available).

`pgscope vector inspect` and scan reports show each HNSW index size next to `shared_buffers` and `effective_cache_size`. When an index exceeds those cache estimates, rule `VEC.HNSW_MEMORY_FIT` warns: ANN latency often jumps once the graph spills to disk. Host RAM / OS page cache are not visible from catalogs, so this is a residency risk signal, not proof of I/O.

PGScope never rebuilds indexes automatically. Build-parameter experiments belong in an isolated lab.

## Recall@K methodology

```text
Recall@K = |ApproximateTopK ∩ ExactTopK| / |ExactTopK|
```

- Exact path uses `enable_indexscan/bitmapscan/indexonlyscan=off` inside a read-only `REPEATABLE READ` transaction and **verifies** the plan does not use an index.
- Approximate path uses normal planner choices plus optional session search settings.
- When fewer than K eligible rows exist, the denominator is `|ExactTopK|` (documented in the report).
- p99 is marked unstable for small sample counts.

## Production safety

- Metadata inspect/EXPLAIN: allowed under normal read-only defaults.
- Recall / filters / tune: require `--authorize-benchmark` and `benchmark.environment: isolated|lab`.
- Hard limits: samples ≤ 5000, concurrency ≤ 8, max_duration ≤ 30m, candidate configs ≤ 32.
- Filter operators whitelisted to `=` and `IN` only; identifiers are validated/quoted; no string-interpolated predicates from arbitrary YAML.
- Statement timeouts are mandatory for benchmark queries.

## Storage estimates

`pgscope vector storage` labels projections clearly. Measured table/index bytes come from `pg_relation_size`. Payload projections assume dense widths and are **not** physical TOAST truths. Halfvec recommendations are never made without recall validation.

## Known limitations

- First release filter support is equality / `IN` only.
- Exact baseline verification may fail for unusual expression indexes; those samples are marked untrusted.
- Tuning sweeps search GUCs only — never index build params in-place.
- Regression comparison requires matching workload fingerprints.
