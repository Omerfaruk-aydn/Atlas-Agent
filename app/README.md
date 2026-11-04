# Atlas desktop app

The Electron app behind `atlas-agent app`. It is a separate product from the
web client: it has its own renderer here and shares only the Go backend.

```
main/       Electron main process: windows, gateway child, desktop IPC
preload/    window.atlas, the renderer's narrow view of the desktop
renderer/   React renderer, built by Vite into renderer/dist
test/       Unit tests and Electron end-to-end tests with a fake gateway
scripts/    Packaging
```

## Commands

Run these from this folder.

| Command | What it does |
|---|---|
| `npm ci` | Install dependencies |
| `npm run typecheck` | Typecheck the renderer |
| `npm test` | Unit tests |
| `npm run build` | Build the renderer |
| `npm run test:e2e` | End-to-end tests; run `npm run build` first |
| `npm run package` | Build `dist/atlas-app-windows-x64.zip` |

To run from source, build the renderer and start it through the CLI from the
repository root, so the gateway matches your checkout:

```
go run . app path/to/project
```

## Windows and projects

A window shows one project. Picking a folder in the app moves that window to
it; `atlas-agent app <path>` opens a window of its own. A project that already
has a window is focused instead of opened twice.

## Connecting models

Subscriptions (ChatGPT, Claude, Copilot, Antigravity, Muse, Kimi, MiniMax) sign
in through the browser with the same flows as `atlas-agent login`, run by the
gateway in `internal/webui/app_login.go`. Other providers take a key. Logos come
from `@lobehub/icons-static-svg`, mapped in `renderer/src/lib/logos.ts`; a
provider without an official mark shows none.

`node test/shots.mjs <folder>` saves screenshots of the main screens.

## How it ships

Windows only for now, unsigned, and inside the CLI: there is no separate
installer. The release adds `atlas-app-windows-x64.zip`, and `install.ps1` and
the npm package's postinstall unpack it into an `app` folder next to the
binary, where `atlas-agent app` finds `Atlas.exe`. Set `ATLAS_AGENT_SKIP_APP=1`
to skip it. `.github/workflows/app.yml` builds and attaches the zip on `v*` tags.

Windows SmartScreen may warn about the unsigned `Atlas.exe` the first time.

## Environment

| Variable | Used for |
|---|---|
| `ATLAS_APP_DIR` | Another folder holding the app |
| `ATLAS_APP_ELECTRON` | Another Electron binary, for a source checkout |
| `ATLAS_APP_DEVTOOLS=1` | Open DevTools with each window |
| `ATLAS_APP_DEV_GATEWAY` | Development only: use a running gateway (the tests' fake one) |
| `ATLAS_APP_USER_DATA` | Development only: another user data folder |
