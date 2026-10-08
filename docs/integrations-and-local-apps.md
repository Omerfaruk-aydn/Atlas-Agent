# Integrations hub (`/integrations`) and local apps (`/app`)

## `/integrations`

One screen and one agent tool (`integrations`) read the same service
(`internal/integrations`). It lists:

| Kind | Source | Free check |
| --- | --- | --- |
| Model providers | `providers` in atlas.json / atlasrc / environment, plus the built-in catalog | The provider's own connection test (lists models). MiniMax, Alibaba Singapore, Bedrock and Vercel offer no free verification: only the key format is checked and the item is shown as `limited`. |
| Accounts | Providers with an OAuth token | Local expiry check; an expired token is refreshed through the existing refresh flow. |
| MCP servers | `mcp` in config, live client state | Connect, MCP handshake, list tools/prompts/resources. No tool is called. |
| Local tools | git, gh, node, npm, python, go, docker, dlv, adb, ffmpeg, rg, Chrome | Find on PATH and run `--version`. |
| App runtimes | Runtimes `/app` can build on | Shares the probe of the matching tool and applies the version constraint. |
| Local apps | The `/app` registry | Manifest and runtime; `/healthz` when running. |

Statuses: `not configured`, `not checked`, `checking`, `ready`,
`needs sign-in`, `missing dependency`, `connection error`, `limited`,
`disabled`. Configured is never shown as usable: only a live check or a
running subsystem sets `ready`. Results from before a restart are kept in
`<data dir>/integrations/checks.json` and shown as *before restart*, never as
the current state. A check whose configuration changed while it ran is
discarded.

For local apps, a running process alone does not establish readiness. The
local health check must pass, and failures remain visible until a new check
succeeds. Source edits, process changes and restarts invalidate cached checks.
Lists and details both mark outdated verification as needing re-verification.

Keys: `/` search, `f` kind, `s` status, `a` show catalog providers, `t` test,
`shift+t` test everything shown (asks first), `x` cancel running checks,
`enter` details and actions. Each check has a 15 s timeout.

Actions are offered only when the item supports them: set up / sign in
(opens `/model` or the MCP sign-in dialog), enable, disable (keeps settings),
forget saved credentials, remove an MCP entry. Atlas only writes the global
data `atlas.json` or the workspace `.atlas/atlas.json`, and not when a shell
config (atlasrc) loaded later would override the change; the detail then
names the file to edit. Destructive actions ask for confirmation, and a
failed write is reported as a failure.

Secrets are registered with the service and redacted (plus common credential
shapes) from every error, detail, saved history and tool result.

## `/app`

Atlas builds and manages small web apps on this computer. Supported today:
**Python 3.9+, standard library only, SQLite**, served on `127.0.0.1`.

- **New app…** sends the request to the assistant, which uses the
  `local_app` tool: `runtimes` → `plan` → `scaffold` → adapt → `verify` →
  `start` → `record_acceptance` for each acceptance criterion. An app is
  `ready` only after verification passed and every criterion has passing
  evidence for the current files.
- **Verify** runs: runtime check, compile, the app's tests, a start against a
  copy of the data, a real create-and-list flow over HTTP, a restart and the
  record read back.
- **Start / Stop / Restart / Open in browser / Show log.** Atlas only stops
  processes it started (PID + creation time identity); another program on
  the port is reported, never stopped. Apps started by Atlas stop when Atlas
  exits.
- **Back up now / Restore data** (consistent SQLite snapshots with
  `VACUUM INTO`, checksums, schema versions; the current data is backed up
  before a restore).
- **Change the app with the assistant…** runs `update_begin` (backup of files
  and data), the change, then `update_finish` (verification against a copy of
  the real data). **Roll back the last update** restores both.
- **Remove from the list** keeps files, data and backups.
  **Delete the app's data** stops the app, backs up and empties its data
  folders. **Delete the app's files** stops the app, keeps a data backup and
  deletes the folder. Each asks for confirmation separately.

Locations: apps in `~/atlas-apps` (`ATLAS_APPS_HOME`), registry, logs and
backups in `<data dir>/apps` (`ATLAS_APPS_DIR`).

Source restores preserve directories excluded from backups, including `.git`,
virtual environments and nested excluded folders. They replace source files
without deleting these resources or following existing links. Conflicting
preserved paths fail the source swap and restore the files already moved.

Verification fingerprints read every source file in full; they do not truncate
large files or omit files after a count limit. A read error cannot produce a
valid partial fingerprint. Legacy fingerprints require one new verification
before the app can be shown as verified again.
