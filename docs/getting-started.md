# Getting started

## Requirements

- Go **1.26+** (to build from source)
- Network access to a PostgreSQL **14–18** database
- A role with monitoring privileges (see [Security](security.md))

## Install (build from source)

```bash
git clone https://github.com/hghukasyan/pgscope.git
cd pgscope
go build -o bin/pgscope ./cmd/pgscope
./bin/pgscope version
```

Optional `go install` (once the module is published on the default branch):

```bash
go install github.com/hghukasyan/pgscope/cmd/pgscope@latest
```

Prebuilt release binaries and Homebrew formulas are **not published yet**.

## Configure the database URL

```bash
export DATABASE_URL='postgres://user:pass@localhost:5432/mydb?sslmode=require'
# or
export PGSCOPE_DSN='postgres://...'
# or
./bin/pgscope --dsn 'postgres://...' scan
```

Passwords are redacted in logs and CLI output.

## Local demo Postgres

```bash
make docker-up
export DATABASE_URL='postgres://pgscope:pgscope@localhost:5432/pgscope?sslmode=disable'
make build
./bin/pgscope connect
./bin/pgscope scan
```

With pgvector:

```bash
docker compose -f deployments/docker-compose.yml --profile vector up -d --wait
export DATABASE_URL='postgres://pgscope:pgscope@localhost:5433/pgscope?sslmode=disable'
./bin/pgscope vector inspect
```

## First scan

```bash
./bin/pgscope scan
./bin/pgscope scan -f json
./bin/pgscope health
```

Exit codes: `0` ok/info, `1` warning findings, `2` critical findings.

## Optional: pg_stat_statements

Query ranking requires the extension:

```sql
-- shared_preload_libraries = 'pg_stat_statements'  (restart if newly added)
CREATE EXTENSION IF NOT EXISTS pg_stat_statements;
GRANT pg_read_all_stats TO your_pgscope_role;
```

If it is missing, PGScope records a **limitation** and continues.

## Next steps

- [CLI reference](cli.md)
- [Configuration](configuration.md)
- [Historical monitoring](monitoring.md)
- [pgvector](pgvector.md)
- [Troubleshooting](troubleshooting.md)
