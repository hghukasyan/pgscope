# Release readiness checklist

## Product honesty

- [x] Documented commands match `pgscope --help`
- [x] Screenshots generated from real CLI output (SVG + raw text under `assets/screenshots/`)
- [x] Planned features labeled as planned (binaries, Homebrew, dashboard, PG history backend)
- [x] No LICENSE claimed as applied until a LICENSE file exists

## Build & quality

- [ ] `gofmt` clean
- [ ] `go vet ./...`
- [ ] `go test ./...`
- [ ] `go test -race ./...`
- [ ] Cross-compile smoke (optional): `GOOS=linux GOARCH=amd64 go build ./cmd/pgscope`
- [ ] No credentials in tree (`rg -n 'postgres://.*:.*@' --glob '!.git/**'`)

## Docs & GitHub

- [x] Polished README with logo, banner, architecture diagram
- [x] `docs/` navigation
- [x] CONTRIBUTING, CODE_OF_CONDUCT, SECURITY, issue/PR templates
- [ ] Apply [GITHUB_METADATA.md](GITHUB_METADATA.md) in GitHub settings
- [ ] Add LICENSE with maintainer approval
- [ ] Tag first release only after CI is green on `main`

## Screenshot regen

```bash
make docker-up
export DATABASE_URL='postgres://pgscope:pgscope@localhost:5432/pgscope?sslmode=disable'
# capture + render via scripts/render_terminal_svg.py
```

PNG exports are optional; GitHub renders the committed SVGs directly.
