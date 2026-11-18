# Contributing to Atlas Agent

Thanks for helping. Bug reports, provider fixes, new tools, docs and tests are all welcome.

## Quick start

```bash
git clone https://github.com/Omerfaruk-aydn/Atlas-Agent.git
cd Atlas-Agent
go build ./...
go test -p 1 ./internal/...
```

- Go is built with `CGO_ENABLED=0`.
- Run tests with `-p 1`; some Windows toolchains hang when packages link in parallel.
- Format with `gofumpt -w .` and lint with `golangci-lint run`.
- The desktop app lives in `app/` and the web UI in `web/`. Each has its own `npm test`.
- [AGENTS.md](AGENTS.md) describes the architecture and code style.

## Good first contributions

- Fix something in the README or docs that no longer matches the code.
- Add or correct a model in `internal/deps/atlas-models/internal/providers/configs/`. Cite the vendor page
  in the pull request and keep the catalog tests passing.
- Add a test for a tool or provider adapter that has none.
- Look for issues labelled [`good first issue`](https://github.com/Omerfaruk-aydn/Atlas-Agent/labels/good%20first%20issue).

## Pull requests

- One concern per pull request, and a test that fails without the change.
- Use semantic commit titles: `fix:`, `feat:`, `docs:`, `test:`, `chore:`, `refactor:`.
- Comments explain why, not what.
- Contributions are covered by the [Contributor License Agreement](CLA.md); opening a pull request means you agree to it.

## Reporting bugs

Open an issue with the version (`atlas-agent --version`), your OS, what you ran and what happened.
For security problems, read [SECURITY.md](SECURITY.md) instead of opening a public issue.
