# Development

Canonical notes: [DEVELOPMENT.md](../DEVELOPMENT.md).

```bash
make vet test race build
make integration   # Docker Compose Postgres + integration tests
```

## Screenshot regeneration

Demo screenshots under `assets/screenshots/` are SVG renders of **real** CLI output:

```bash
make docker-up
# optional: seed demo tables, then
export DATABASE_URL='postgres://pgscope:pgscope@localhost:5432/pgscope?sslmode=disable'
./bin/pgscope scan --no-color > /tmp/out.txt
python3 scripts/render_terminal_svg.py /tmp/out.txt assets/screenshots/overview.svg --title 'pgscope scan'
```

Raw capture text is kept in `assets/screenshots/raw/` for auditability.

## Principles

1. Evidence over speculation.
2. No fabricated metrics or placeholder commands.
3. Preserve read-only defaults.
4. Update docs when user-visible behavior changes.
