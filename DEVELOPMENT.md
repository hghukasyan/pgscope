# Development

## Prerequisites

- Go 1.26+ (module declares `go 1.26.0`; CI uses `1.26.x`)
- Docker (for local Postgres / integration tests)
- `make` (optional)

## Setup

```bash
git clone https://github.com/hghukasyan/pgscope.git
cd pgscope
go mod tidy
make build
```

## Local Postgres

```bash
make docker-up
export DATABASE_URL='postgres://pgscope:pgscope@localhost:5432/pgscope?sslmode=disable'
./bin/pgscope scan
```

pgvector profile:

```bash
docker compose -f deployments/docker-compose.yml --profile vector up -d --wait
export DATABASE_URL='postgres://pgscope:pgscope@localhost:5433/pgscope?sslmode=disable'
```

## Tests

```bash
go test ./...
go test -race ./...
go vet ./...

# Integration (requires Docker)
DATABASE_URL='postgres://pgscope:pgscope@localhost:5432/pgscope?sslmode=disable' \
  go test ./test/integration/ -v -count=1
```

If Docker is unavailable, unit tests still run; integration tests skip when no DSN is set.

## Project conventions

- Prefer the standard library.
- Dependencies: `pgx/v5`, Cobra, `yaml.v3` (Phase 1–2).
- Logging via `log/slog`.
- No placeholder CLI commands — only expose implemented functionality.
- Collectors must be version-aware; missing columns/extensions → `Limitation`.
- Diagnostic rules require multi-metric evidence where applicable.
- Do not commit secrets or local `pgscope.yaml` with credentials.

## Useful Phase 2 commands

```bash
./bin/pgscope queries top --sort mean-time --limit 10
./bin/pgscope queries explain --file /tmp/q.sql
./bin/pgscope indexes duplicates
./bin/pgscope vacuum risks
./bin/pgscope wal checkpoints
./bin/pgscope replication lag
```
