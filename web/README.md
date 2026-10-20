# ATLAS-AGENT web client

The browser UI for `atlas-agent web`. It is a SolidJS app that talks only to
the Go gateway in `internal/webui`; the gateway relays to the Atlas server. The
built files are embedded in the Go binary.

## Layout

| Path                | What                                                              |
| ------------------- | ----------------------------------------------------------------- |
| `src/oc`            | Screens: home, session, composer, settings, command palette       |
| `src/api`           | Typed client for the gateway's `/api` routes                      |
| `src/state`         | App, workspace and preference stores                              |
| `src/oc/i18n`       | Strings. `atlas.ts` holds the Atlas-only ones and overrides       |
| `vendor/atlas-*`    | Interface packages (see `../THIRD_PARTY_NOTICES.md`)              |
| `e2e`               | Playwright tests against the real binary                          |

Aliases such as `@atlas/ui/*` point into `vendor/` (see `vite.config.ts`).

## Commands

Run from the repository root with [Task](https://taskfile.dev), or from `web/`
with npm.

```bash
task web:install   # npm ci
task web:build     # builds into internal/webui/build, which Go embeds
task web:test      # typecheck and unit tests
task web:e2e       # builds dist/dev/atlas-web-dev and drives it in a browser
```

Rebuild the Go binary after `web:build`, since the files are embedded at
compile time.

## Notes

- The "/" menu lists the terminal's commands (`src/oc/cli-commands.ts`). A few
  need your computer (`/mark`, `/remote-control`, `/mobile`, `/support`,
  `/app`, `/profiles`, `/resume-use`) and only show a note in the browser.
- Do not read a `createResource` directly inside a screen: while it reloads it
  suspends the whole screen. Read `.latest` instead, as `useCatalog` does.
- The sign-in link printed by `atlas-agent web` works once and expires after ten
  minutes.
