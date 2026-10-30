# Deterministic web regression with Atlas browser-use

## Scope and execution

`web/e2e/browser-use-regress.ts` starts the same isolated real Atlas server,
web gateway and scripted fake model as the Playwright suite. It builds
`internal/webui/webtest/webregress`, then runs each JSON scenario in a new
headless Chromium process with a new temporary profile, interaction-state
directory and artifact directory. No paid model, real user profile, saved
browser session or user credential is used.

The runner is a thin adapter over the production `compileBrowserWorkflow` and
browser tool pipeline. It reuses semantic actions, `assert`, `wait_for`,
`Verify`, `OnError`, bounded safe retry and the existing protected screenshot
implementation. It does not contain a second browser engine. Mutation retry
remains governed by the production pipeline: only a pre-input semantic target
failure can receive one retry. The overall process and pipeline have separate
time bounds, and the independently owned manager is closed on every exit.

The fixed scenarios cover project opening, session creation and reopening,
prompt/streaming response, tool-result rendering, cancellation, a scripted
provider error and workspace/session isolation. Assertions are DOM conditions
with explicit expected values. The runner independently requires every
compiled action and generated verification step to succeed. A recovery route,
extra step or later successful assertion cannot erase a required failure.

## Reports and failure behavior

Each scenario writes `report.json` and JUnit `junit.xml`; failure adds a clean
protected `failure.png` where the driver supports it. The report includes the
step, expected and observed status/outcome, error code and retry count. Text
artifacts are redacted for supplied secrets, registered vault values and
common credential fields. Screenshot capture failure is explicitly reported.
Any scenario, report-write, browser-preflight or infrastructure failure yields
a nonzero process exit. An unsupported browser is not a skip or a pass.

## CI and Playwright migration boundary

The Ubuntu `browser-regression` job installs supported Chromium dependencies,
runs the browser/workflow contract tests, executes the Atlas scenarios and
uploads evidence even on failure. The Playwright suite runs afterward and is
unchanged. Both suites remain mandatory because Atlas browser-use does not yet
prove equivalent coverage for attachments, terminal/browser cross-client
behavior, permission mode changes, long-answer scrolling, multi-tab behavior
or anti-CSRF checks. Playwright can only be reduced after that coverage has an
explicit scenario-by-scenario equivalence record.

Local commands, when Go, Node and a supported Chromium are installed:

```sh
task web:e2e:browser-use
npm --prefix web run e2e:browser-use
npm --prefix web run e2e
```

Set `ATLAS_E2E_CHROMIUM` to select a Chromium executable explicitly. On Windows,
macOS and Linux, an installed normal Chrome is also auto-detected. A missing
executable or Go toolchain is an error, not a skipped regression.

The scripted model error asserts the user-visible HTTP 400 `Bad Request` state.
The Playwright screenshot fixture uses `[[permission]]` to request a harmless but
risk-classified removal of a nonexistent probe file, so safe-command auto-approve
cannot bypass the permission UI it is intended to capture.
