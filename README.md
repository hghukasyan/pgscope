<p align="center">
  <img src="assets/logo.svg" alt="PGScope" width="96" height="96"/>
</p>

<h1 align="center">PGScope</h1>

<p align="center">
  Read-only PostgreSQL diagnostics from the command line.<br/>
  Findings, regressions, and pgvector analysis — without touching the database.
</p>

<p align="center">
  <a href="https://github.com/hghukasyan/pgscope/actions/workflows/ci.yml"><img src="https://github.com/hghukasyan/pgscope/actions/workflows/ci.yml/badge.svg" alt="CI"/></a>
  <img src="https://img.shields.io/badge/Go-1.26-00ADD8?logo=go&logoColor=white" alt="Go 1.26"/>
  <img src="https://img.shields.io/badge/PostgreSQL-14%E2%80%9318-4169E1?logo=postgresql&logoColor=white" alt="PostgreSQL 14–18"/>
</p>

<p align="center">
  <a href="docs/getting-started.md">Getting started</a> ·
  <a href="docs/cli.md">CLI</a> ·
  <a href="docs/architecture.md">Architecture</a> ·
  <a href="CONTRIBUTING.md">Contributing</a>
</p>

---

<p align="center">
  <img src="assets/screenshots/overview.svg" alt="pgscope scan" width="900"/>
</p>

## What it does

Monitoring tools show metrics. When something is wrong, you still stitch the story together by hand.

PGScope is a Go CLI that reads PostgreSQL catalogs and stats, then turns them into a structured report: what's unhealthy, what changed, and which explanations fit the evidence.

It never kills sessions, never changes config, and never runs DDL. `EXPLAIN ANALYZE` and vector benchmarks stay behind explicit opt-in flags.

## Commands

```bash
pgscope scan                  # full diagnostic pass
pgscope health                # compact health view
pgscope queries top           # ranked pg_stat_statements
pgscope queries explain -f q.sql
pgscope indexes inspect
pgscope vacuum inspect
pgscope wal checkpoints
pgscope replication lag
pgscope vector inspect
pgscope monitor once          # snapshot into local history
pgscope regressions detect --last 1h
pgscope investigate --last 1h
```

More detail in [docs/cli.md](docs/cli.md). pgvector workflows: [docs/pgvector.md](docs/pgvector.md). Continuous history: [docs/monitoring.md](docs/monitoring.md).

## Quick start

Requires Go 1.26+ and PostgreSQL 14–18. Prefer a role with `pg_monitor` — superuser is not required.

```bash
git clone https://github.com/hghukasyan/pgscope.git
cd pgscope
go build -o bin/pgscope ./cmd/pgscope

make docker-up   # optional local demo
export DATABASE_URL='postgres://pgscope:pgscope@localhost:5432/pgscope?sslmode=disable'

./bin/pgscope connect
./bin/pgscope scan
```

Stuck? See [docs/troubleshooting.md](docs/troubleshooting.md).

## Safety

- Connections run with `default_transaction_read_only=on`
- Statement and lock timeouts; small connection pool
- Passwords redacted in output; query text truncated or omitted by default
- Local SQLite history stores no credentials or embeddings

Policy: [SECURITY.md](SECURITY.md).

## Docs

| | |
|--|--|
| [Getting started](docs/getting-started.md) | Install and first scan |
| [Configuration](docs/configuration.md) | YAML, env, history |
| [CLI](docs/cli.md) | Command reference |
| [Architecture](docs/architecture.md) | How the pieces fit |
| [Roadmap](ROADMAP.md) | What's next |

```bash
make vet test race build
```

License is not published yet.
