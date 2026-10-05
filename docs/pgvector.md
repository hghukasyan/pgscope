# pgvector intelligence

Detailed methodology: [PGVECTOR.md](../PGVECTOR.md).

## What is implemented

- Discovery of vector types, operators, HNSW/IVFFlat indexes, and settings
- Vector-aware EXPLAIN diagnostics
- Opt-in Recall@K with exact baselines (`--authorize-benchmark`, `environment: isolated|lab`)
- Filtered ANN cohort analysis
- Bounded search-parameter tuning
- Labeled storage estimates
- Compatible recall-history regression comparison
- Persistence of compact benchmark summaries into local history (`--persist-history`)

## What is not claimed

- Index metadata looking “healthy” does **not** prove recall quality.
- Benchmarks are not run on every monitor interval.
- Raw embeddings are never stored in history or reports.

## Quick commands

```bash
export DATABASE_URL='postgres://pgscope:pgscope@localhost:5433/pgscope?sslmode=disable'
pgscope vector inspect
pgscope vector recall --config deployments/vector-workload.example.yaml --authorize-benchmark
```
