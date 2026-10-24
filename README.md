<p align="center">
  <img src="assets/logo.svg" alt="PGScope logo" width="112" height="112"/>
</p>

<h1 align="center">PGScope</h1>

<p align="center"><strong>See deeper. Diagnose smarter.</strong></p>

<p align="center">
  Open-source PostgreSQL performance intelligence,<br/>
  root-cause analysis, and pgvector optimization.
</p>

<p align="center">
  <a href="https://github.com/hghukasyan/pgscope/actions/workflows/ci.yml"><img src="https://github.com/hghukasyan/pgscope/actions/workflows/ci.yml/badge.svg" alt="CI"/></a>
  <img src="https://img.shields.io/badge/Go-1.26-00ADD8?logo=go&logoColor=white" alt="Go 1.26"/>
  <img src="https://img.shields.io/badge/PostgreSQL-14%E2%80%9318-4169E1?logo=postgresql&logoColor=white" alt="PostgreSQL 14-18"/>
  <img src="https://img.shields.io/badge/license-pending-lightgrey" alt="License pending"/>
</p>

<p align="center">
  <a href="docs/README.md">Documentation</a> ·
  <a href="#quick-start">Quick Start</a> ·
  <a href="#features">Features</a> ·
  <a href="docs/cli.md">CLI</a> ·
  <a href="docs/architecture.md">Architecture</a> ·
  <a href="CONTRIBUTING.md">Contributing</a>
</p>

---

<p align="center">
  <img src="assets/screenshots/overview.svg" alt="Demo: pgscope scan output" width="920"/>
</p>

<p align="center"><em>Demo environment output from <code>pgscope scan</code> against local Docker Postgres.</em></p>

## Why PGScope?

Most monitoring stacks show PostgreSQL metrics. When latency spikes or WAL grows, you still have to assemble the story yourself.

PGScope is a **production-safe, read-only Go CLI** that helps you:

1. **Observe** catalog and statistics views into a structured snapshot  
2. **Detect** evidence-based findings and sustained regressions  
3. **Correlate** locks, queries, vacuum, WAL, replication, and vector signals  
4. **Explain** competing root-cause hypotheses — with supporting and missing evidence  
5. **Recommend** investigation steps you can validate  

It is **not** a Prometheus exporter, hosted APM, or dashboard. There is no auto-remediation: PGScope never kills sessions, never changes configuration, and never runs DDL.

## Features

### PostgreSQL health

Connections, long transactions, lock waits, storage sizes, and a unified `scan` report.

```bash
pgscope health
pgscope scan
```

### Query intelligence

Ranked `pg_stat_statements` workload and safe EXPLAIN analysis.

```bash
pgscope queries top
pgscope queries explain --file query.sql
```

→ [docs/cli.md](docs/cli.md) · [docs/diagnostics.md](docs/diagnostics.md)

### Index intelligence

Inventory, zero-scan candidates, duplicates/redundancy, conservative recommendations.

```bash
pgscope indexes inspect
pgscope indexes duplicates
pgscope indexes recommend
```

### Locks and transactions

Blocking chains, wait counts, idle-in-transaction, and XID age — surfaced in `scan` / `health` (no separate `locks` command).

### Autovacuum, storage, and WAL

Dead-tuple risk, vacuum progress, WAL counters, checkpoints, and slot retention.

```bash
pgscope vacuum inspect
pgscope wal checkpoints
pgscope wal slots
```

### Replication diagnostics

Primary/standby role, connection lag (NULLs preserved), and replication slots.

```bash
pgscope replication inspect
pgscope replication lag
```

### pgvector intelligence

Discovery, vector EXPLAIN, opt-in Recall@K, filter cohorts, bounded tuning, storage estimates.

```bash
pgscope vector inspect
pgscope vector recall --config deployments/vector-workload.example.yaml --authorize-benchmark
```

→ [docs/pgvector.md](docs/pgvector.md)

### Historical regression analysis

Local SQLite history, counter-safe rates, sustained regression detection.

```bash
pgscope monitor once
pgscope regressions detect --last 1h
```

→ [docs/monitoring.md](docs/monitoring.md)

### Root-cause investigation

Incident grouping, typed evidence edges, competing hypotheses, timelines.

```bash
pgscope investigate --last 1h
pgscope incidents list
```

### Planned (not implemented yet)

- Prebuilt release binaries / package managers  
- PostgreSQL-backed history store  
- Prometheus exporter for PGScope internals  
- Web dashboard / API / MCP (see [ROADMAP.md](ROADMAP.md))

## Quick Start

**Requirements:** Go 1.26+, PostgreSQL 14–18.

```bash
git clone https://github.com/hghukasyan/pgscope.git
cd pgscope
go build -o bin/pgscope ./cmd/pgscope

# Local demo (pg_stat_statements preloaded)
make docker-up
export DATABASE_URL='postgres://pgscope:pgscope@localhost:5432/pgscope?sslmode=disable'

./bin/pgscope connect
./bin/pgscope scan
```

Optional once the module is published:

```bash
go install github.com/hghukasyan/pgscope/cmd/pgscope@latest
```

Privileges: `GRANT pg_monitor TO your_role;` (superuser never required).  
Troubleshooting: [docs/troubleshooting.md](docs/troubleshooting.md)

## Screenshots

All screenshots are SVG renders of **real** CLI output from the Docker demo environment (credentials redacted). Regenerate with `scripts/render_terminal_svg.py`.

| | |
|:--:|:--:|
| ![queries](assets/screenshots/queries.svg) | ![indexes](assets/screenshots/indexes.svg) |
| *queries top* | *indexes inspect / duplicates* |
| ![health](assets/screenshots/health.svg) | ![pgvector](assets/screenshots/pgvector.svg) |
| *health / vacuum* | *vector inspect* |

## Architecture

<p align="center">
  <img src="assets/diagrams/architecture.svg" alt="PGScope architecture diagram" width="720"/>
</p>

```text
PGScope CLI → Diagnostic Core → Collectors / Analysis / Reporting
                                      ↓
                                PostgreSQL
                     (optional: pg_stat_statements, pgvector)
                                      ↓
                         Local SQLite history (optional)
```

Details: [docs/architecture.md](docs/architecture.md)

## CLI reference

| Command | Purpose |
|---------|---------|
| `pgscope scan` | Full diagnostics + findings |
| `pgscope health` | Compact health summary |
| `pgscope queries top` | Ranked statements |
| `pgscope queries explain` | Safe EXPLAIN |
| `pgscope indexes …` | Index intelligence |
| `pgscope vacuum …` | Autovacuum / risks / progress |
| `pgscope wal …` | WAL / checkpoints / slots |
| `pgscope replication …` | Role, lag, slots |
| `pgscope vector …` | pgvector intelligence |
| `pgscope history …` | Local telemetry history |
| `pgscope regressions …` | Regression detection |
| `pgscope incidents …` | Incident lifecycle |
| `pgscope investigate` | RCA report |
| `pgscope monitor` / `serve` | Continuous monitoring |

Full table: [docs/cli.md](docs/cli.md)

## Security and production safety

- Read-only by default (`default_transaction_read_only=on`)
- Statement / lock timeouts and a bounded connection pool
- DSN passwords redacted; query text truncated or omitted
- `EXPLAIN ANALYZE` and vector benchmarks are **opt-in** with explicit authorization flags
- History stores no credentials or embeddings

See [SECURITY.md](SECURITY.md) and [docs/security.md](docs/security.md).

## Documentation

| Doc | |
|-----|--|
| [Getting started](docs/getting-started.md) | Install & first scan |
| [Configuration](docs/configuration.md) | YAML & history settings |
| [CLI](docs/cli.md) | Command reference |
| [Monitoring](docs/monitoring.md) | Regressions & incidents |
| [pgvector](docs/pgvector.md) | Vector workflows |
| [Roadmap](ROADMAP.md) | What’s next |

## Contributing

Issues and PRs are welcome. Please read [CONTRIBUTING.md](CONTRIBUTING.md) and keep changes evidence-driven and production-safe.

```bash
make vet test race build
```

## License

This repository currently has **no LICENSE file**. Apache-2.0 is the intended recommendation; a license will only be added with maintainer approval.

## Banner

<p align="center">
  <img src="assets/banner.svg" alt="PGScope banner" width="920"/>
</p>

---

<p align="center">
  If PGScope helps you understand PostgreSQL under pressure, consider starring the repository and contributing a finding rule or collector improvement.
</p>
