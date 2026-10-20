# Third-party notices

ATLAS-AGENT's own code is covered by [LICENSE.md](LICENSE.md). The web client
in `web/` includes work from other projects. Their licenses require these
notices to travel with the code.

## Web client

### OpenCode (MIT)

The web client's interface packages and screens are derived from the OpenCode
web app, version 2.0.32 (source commit `86ac38ad`):

| Where                      | What                                                        |
| -------------------------- | ----------------------------------------------------------- |
| `web/vendor/atlas-ui`      | UI components, tokens and styles (OpenCode `packages/ui`)   |
| `web/vendor/atlas-session-ui` | Session and message components (OpenCode `packages/session-ui`) |
| `web/vendor/atlas-util`    | Small shared helpers                                        |
| `web/src/oc`               | Screens adapted to the ATLAS-AGENT server's data            |

Each vendored directory keeps the upstream license file unchanged
(`web/vendor/*/LICENSE`). The copyright line there is the upstream one:

> MIT License — Copyright (c) 2025 opencode

The MIT license requires that copyright notice and permission notice be
included in all copies or substantial portions of the software. Do not remove
those files.

### npm dependencies

The web client's runtime dependencies are listed in `web/package.json` and
locked in `web/package-lock.json`. Each keeps its own license, which ships in
its package under `node_modules` and is bundled with the build as the package
requires.

## Go module

Go dependencies are listed in `go.mod`; their licenses are in each module's
source. The vendored model and provider libraries under `internal/deps/` keep
their own license files.
