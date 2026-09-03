# Contributing

Thanks for helping improve PGScope.

Please also read the [Code of Conduct](CODE_OF_CONDUCT.md).

## Principles

1. Prefer evidence over speculation.
2. Production safety beats cleverness.
3. No placeholder commands or fabricated metrics.
4. Keep phase scope honest — label unfinished work as planned.
5. Preserve read-only defaults.

## Workflow

1. Fork / branch from `main`.
2. Add tests for behavior changes.
3. Run:

   ```bash
   go vet ./...
   go test ./...
   go test -race ./...
   ```

4. Update `docs/` (and root references) when user-visible behavior changes.
5. Open a PR using the template (summary + test plan).
6. Redact DSNs, passwords, and embeddings from logs/screenshots.

## Go style

- Standard formatting (`gofmt`).
- Table-driven unit tests.
- Context cancellation honored on I/O paths.
- Errors wrapped with `%w`; DSN always redacted.

## Documentation & assets

- Prefer editing files under `docs/` for user guides.
- Regenerate demo screenshots with `scripts/render_terminal_svg.py` from real CLI output.
- Do not invent terminal metrics by hand.

## License

Do not add or change a license file without maintainer approval. Apache-2.0 is the intended recommendation when licensing is finalized.
