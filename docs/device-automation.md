# Device Automation: Isolated Desktops, Android, Screen Marks, Nodes, Live App Loop, Phone Control

Status: design + implementation plan. The "Status" table at the end is the
source of truth for what is implemented and what was verified live.

Research checked on 2026-10-08:

- Windows Sandbox overview, editions and single-instance limit:
  https://learn.microsoft.com/en-us/windows/security/application-security/application-isolation/windows-sandbox/windows-sandbox-overview
- `.wsb` configuration (MappedFolders, ReadOnly, LogonCommand, Networking,
  ClipboardRedirection, MemoryInMB):
  https://learn.microsoft.com/en-us/windows/security/application-security/application-isolation/windows-sandbox/windows-sandbox-configure-using-wsb-file
- Android Debug Bridge (`devices -l` states, `-s` serial targeting,
  `exec-out`, `install`, `push`/`pull`): https://developer.android.com/tools/adb
- Android `uiautomator dump`, `input`, `wm size`/`wm density`, `dumpsys`:
  https://developer.android.com/studio/test/other-testing-tools/ui-automator
  and https://developer.android.com/tools/adb#shellcommands
- Win32 per-monitor DPI (`GetDpiForMonitor`, `EnumDisplayMonitors`,
  per-monitor-v2 awareness):
  https://learn.microsoft.com/en-us/windows/win32/hidpi/high-dpi-desktop-application-development-on-windows
- Web App Manifest / service worker installability and Notification API
  permission model: https://developer.mozilla.org/en-US/docs/Web/Progressive_web_apps
  and https://developer.mozilla.org/en-US/docs/Web/API/Notification/requestPermission_static

## 0. Existing system (what is reused)

| Area | Existing parts | How the new work uses them |
| --- | --- | --- |
| Agent loop | `internal/agent/coordinator.go` `buildTools`, `filterTools`, `config.allToolNames` | New tools are registered next to `computer`, gated by `tools.devices.enabled`, and listed in the allowed tool names. |
| Deferred schemas | `tool_deferral.go`, `tools/tool_catalog.go` (`CoreToolNames`, categories, `IntentTools`), `tool_search` | New tools join a new `devices` category; intent words preload the smallest group; greeting turns load nothing new. |
| Desktop control | `internal/computer` (`Backend`, contract errors, UIA), `tools/computer*.go`, `activity` overlay | The isolated desktop's in-guest agent runs the same `computer.Backend` *inside* the guest session. The host overlay is not used for guest input. |
| Acceptance checks | `internal/browser`, `browser`, `isolated_desktop`, `android`, `scenario` | The live app loop uses `ExecuteScenario` for web, terminal, isolated desktop and Android checks, with source-bound observation evidence. |
| Permissions | `permission.Service.Request`, modes, `Safe`, plan-mode denial | Every side-effecting action requests permission with the real tool name and action; discovery and status calls are `Safe`. |
| Questions | `question.Service` | Phone answers go through `Workspace.QuestionAnswer`. |
| Workflow control | `WorkflowControl` (`pause`, `resume`, `stop`, revision checked) | Phone pause/resume/stop use it with `expected_revision`, so a stale tap is rejected. |
| Events | `app.Events(ctx)` pubsub (`permission_request`, `message`, `session`, `question_batch_request`, notifications) | The phone gateway subscribes like the TUI does; nothing is duplicated. |
| Server/client | `internal/server` (local socket / named pipe, optional bearer) | Local IPC only and requires a full workspace + LLM app. Remote nodes use the same HTTP+JSON `/v1/` conventions but a capability-scoped handler with TLS and pairing (see §4). |
| Persistence | `engineering.Store`, `agentstate`, config data dir | Node registry, mark records, isolation leases, op journals and loop reports are JSON files under the Atlas data directory, written with temp-file + rename and fsync before success is reported. |

## 1. Shared lifecycle (`internal/taskstate`)

States: `queued → preparing → running → waiting_for_user |
waiting_for_permission | paused | reconnecting → completed | failed |
canceled`.

- `completed`, `failed`, `canceled` are terminal. `Transition` rejects any
  move out of a terminal state, so a late event never revives a canceled run.
- Waiting for a question or permission is not cancellation.
- Each record carries `revision`; writers compare-and-swap so concurrent
  updates (TUI, phone, agent) cannot silently overwrite each other.
- Success is reported only after the record is durably written
  (`Persist` returns the error otherwise and the run is reported `failed`
  with `persist_error`).

Mapped equivalents: isolation environment (`preparing/running/paused/
reconnecting/completed`), node plan steps, Android sequences, app loop
iterations, phone-visible session status.

## 2. Isolated desktop (`internal/isolation`)

### User flow

1. `atlas isolation probe` (or tool `isolated_desktop` action `capabilities`)
   lists backends: `current_desktop` (always; **not isolated**),
   `windows_sandbox` (available/unavailable with reason), `hyperv_vm`
   (detected; requires a user-prepared VM, reported as "not provisioned").
2. Agent calls `isolated_desktop` `start` → Atlas writes a `.wsb` into its
   own lease directory, maps (a) the Atlas executable directory read-only and
   (b) a per-lease exchange folder read-write, and sets `LogonCommand` to run
   `atlas node serve --isolated` inside the guest.
3. The guest agent creates a TLS key, writes its certificate fingerprint and
   listening addresses to the exchange folder, and accepts only the
   pre-provisioned token the host wrote there. The host verifies the pinned
   fingerprint and connects. State `preparing → running`.
4. Observation/input (`observe`, `click`, `type`, `key`, `scroll`,
   `windows`, `launch`) run inside the guest session through the guest's own
   `computer.Backend`. Host `SendInput` is never called for guest actions.
5. `handoff` brings the sandbox window to the foreground for the user and
   pauses automation (`paused`); `resume` continues.
6. `import`/`export` move files only through the exchange folder with size
   limits and SHA-256 verification.
7. `stop` terminates only the processes Atlas launched (PID + creation time
   recorded in the lease) and deletes only the lease directory.

### Constraints (truthful)

- Windows virtual desktops (Win+Ctrl+D) share one input queue and one
  session: they do **not** isolate mouse/keyboard and are not offered.
- Windows Sandbox: Windows 10/11 Pro, Enterprise, Education (build 18342+
  for `.wsb`), hardware virtualization, the optional feature enabled (needs
  admin once; Atlas never enables it). Only one sandbox can run at a time; if
  a sandbox not owned by Atlas is running, `start` fails with `busy_foreign`.
- The sandbox window cannot be resized through `.wsb`.
- Capture inside the guest may stop when the sandbox client window is
  minimized (RDP behavior); `observe` reports `capture_blank` instead of
  pretending the screen is empty.
- No silent fallback: when the isolated backend fails, the tool returns
  `isolation_unavailable` and the agent must ask before using `computer` on
  the host desktop.

### Trust boundaries

Exchange folder is the only writable host path visible to the guest. The
guest cannot reach host files except the read-only Atlas binary directory.
Networking is required for the control channel (Hyper-V default switch);
the guest listener requires the token and is bound to the guest NIC.

### Acceptance

Run Notepad in the sandbox, type text, read it back from the guest UI, while
the host foreground window and cursor are sampled before/after and must be
unchanged.

## 3. Android (`internal/android`)

- ADB resolution order: `tools.devices.adb_path` → `ANDROID_SDK_ROOT` /
  `ANDROID_HOME` `platform-tools/adb(.exe)` → `PATH`. Each candidate must
  exist and `adb version` must succeed; a dangling `ANDROID_HOME` is
  reported (`adb_not_found` with the checked paths and install guidance).
  Atlas never downloads the SDK.
- `devices`: parses `adb devices -l` → `{serial, state(ready|unauthorized|
  offline|disconnected|unknown), model, product, transport_id, emulator}`.
- Every device action requires `serial`; with more than one device there is
  no default. Each command is run as `adb -s <serial> ...`.
- `observe`: `wm size`, `wm density`, `dumpsys input` (SurfaceOrientation) /
  `dumpsys window` (focused package/activity), `exec-out screencap -p`, and
  `exec-out uiautomator dump /dev/tty` when requested. Returns an
  `observation_id` binding serial, size, rotation, focused activity and a UI
  hash.
- Coordinate or element actions require `observation_id`. Before input the
  device is re-probed (rotation, size, focused activity); a mismatch returns
  `stale_observation` with `input_sent:false`.
- Element targeting: `element` index from the UI dump → center of bounds,
  validated against the screen size.
- Text: `input text` supports only a safe ASCII subset; non-ASCII (e.g.
  Turkish) text is sent only through the ADB Keyboard IME broadcast when that
  IME is installed **and** active; otherwise `unicode_unsupported` (nothing
  typed). After typing, callers can `verify_text` against a fresh UI dump.
- `install`: separate action, requires explicit permission, a local `.apk`
  inside the workspace, SHA-256 recorded and reported; no network URL.
- `push`/`pull`: size limit, SHA-256 verified against `sha256sum` on device
  (when available) or by pulling back.
- `sequence`: up to 24 steps, each re-validating the observation, cancellable
  via context, stopping at the first failure.
- Screenshots are stored as session artifacts only when requested; the
  report never includes typed `text` marked `sensitive`.

Acceptance (requires device/emulator): form fill → navigate → verify via UI
dump. Without a device: protocol tests against a fake `adb` executable; they
are reported as protocol tests, not device runs.

## 4. Multi-computer nodes (`internal/nodes`)

- `atlas node serve --root <dir> [--listen 127.0.0.1:7443] [--allow
  exec,files,desktop]` runs a node. Default bind is loopback; any
  non-loopback bind requires `--lan` and prints a warning. There is no
  unauthenticated endpoint besides `/v1/node/pair` (one-time code) and
  `/v1/node/hello` (version + fingerprint only).
- TLS: self-signed ECDSA P-256 certificate created on first start; the
  controller pins its SHA-256 fingerprint (given out of band with the
  pairing command). Plain HTTP is never used.
- Pairing: node prints `atlas node pair https://host:port --code XXXX-XXXX
  --fingerprint sha256:...`. Code: 10 minutes, single use, 5 failed attempts
  then locked. The node issues a 256-bit token; only its SHA-256 is stored
  on the node. The controller stores the token in its credential store.
- Revocation: `atlas node revoke <name>` revokes on the node (when reachable)
  and deletes locally; `atlas node serve --revoke <device-id>` on the node.
- Capabilities: node reports OS, arch, hostname, root, allowed capabilities.
  The controller can never use a capability the node did not grant.
- Operations carry `operation_id`. The node journals each op (persisted) and
  a duplicate request returns the recorded state/result instead of running
  again. After reconnect the controller reconciles by `GET /v1/node/ops/{id}`.
- Cancel: `POST /v1/node/ops/{id}/cancel` kills the owned process tree.
- File transfer: max size (default 64 MiB), SHA-256 header, relative path
  under root only, no `..`, no absolute paths, parent resolved with
  `EvalSymlinks` and must stay under root, existing symlink targets refused,
  temp file + fsync + rename.
- Tool `remote_node`: `list`, `capabilities`, `exec`, `push`, `pull`,
  `status`, `cancel`, `plan`. `plan` executes up to 16 steps across nodes as
  a DAG (independent steps in parallel, dependent ones in order); every
  result carries `node`, `node_id`, `os`, so paths and coordinates are never
  mixed between machines. Partial success is reported per step.
- The Windows Sandbox guest is a node with a pre-provisioned token, so the
  isolated desktop and remote computers share one protocol.

Acceptance: run `go test` on the WSL Linux node and an app check on the
Windows host/node in one `plan`, merged with node identities.

## 5. Screen marks (`internal/marking`)

- `/mark` in the TUI (or `atlas mark`) captures the virtual screen *before*
  showing the overlay, then shows a full-virtual-screen layered window that
  displays the frozen capture. The user drags a region (Esc cancels). The
  overlay swallows all input, so nothing reaches the target app.
- Record: id, created_at, monitor (device name, bounds, DPI, scale), virtual
  screen rect (may have negative origin), crop PNG + SHA-256, top-level
  window (HWND, title, process name/pid, window rect) found under the
  selection center excluding the overlay, UIA element (name, role,
  automation id, bounds) when available, `source: "user_mark"`, validity.
- Coordinates: stored in physical virtual-screen pixels; the crop has its own
  origin; `CropToScreen` converts crop pixels to screen pixels; window-
  relative offsets are stored so a moved window can be re-resolved.
- Validation (`screen_mark` `validate`): window still exists with the same
  pid; window rect unchanged or offset re-applied; crop pixel hash similarity
  within threshold; otherwise `stale` with reasons. Stale marks can be read
  but never used as an input target.
- Prompt attachment: crop image + a manifest that says the marked content is
  data to inspect, not instructions. Only the crop is sent, never the full
  screen.
- TUI shows the mark as an attachment chip; removing the chip removes it
  from the prompt.

Acceptance: two monitors with different DPI. On this development machine
only one monitor exists, so geometry is verified by unit tests with
synthetic monitor layouts (negative origins, 100%/150% scale) and a live
single-monitor run.

## 6. Live app loop (`app_loop` tool)

Actions: `plan` (criteria → steps), `start`, `ready`, `check`, `logs`,
`stop`, `report`.

- `start` launches the project's run command as an owned process (argv, cwd
  inside the workspace), records pid + creation time + command hash. If the
  port is already listening and not owned, it **attaches** (never kills) and
  reports `attached_existing`.
- `ready`: bounded probes (HTTP status/body substring, TCP, log regex) with
  a deadline; no fixed sleeps.
- `check`: runs a web, terminal, isolated desktop or Android `scenario`
  through `ExecuteScenario` and correlates captured app log lines from the owned process
  window. Page load alone never passes: at least one non-navigation
  assertion is required.
- Iteration ledger per loop id: `max_iterations` (default 5, hard cap 10)
  and `deadline`; exceeding either ends the loop `failed` with the last
  evidence.
- `stop` only kills processes whose pid **and** creation time match the
  ownership record.
- Replanning is rejected after an app starts or attaches, after an iteration,
  for another session, or when an existing record is corrupt. The owned
  process remains recorded and stoppable.
- Desktop checks require an already opened foreground isolated guest app and its
  `window_id`; optional `node` selects another paired isolated environment.
  Android checks require an already opened app, exact `serial`, and expected
  foreground `package`. The check never starts a host app instead.
- Native selectors are `id:AutomationId` / `name:ExactName` on desktop and
  exact resource IDs on Android. Steps are `click`, `type`, `key`; assertions
  are `text`, `value`, `visible`, `focused`. Input resolves a fresh unique
  target; type/key re-check focus after clicking. Observation polling never
  repeats input. Password fields and truncated trees cannot pass.
- Desktop input carries the expected window identity to the guest, which
  re-checks foreground focus before sending input. Android text/key input
  carries the latest observation and re-checks foreground app and geometry
  under the device input lock after permission approval.
- Native screenshots and accessibility trees use the same source-bound
  artifact store and criterion ledger as web checks. Tests use controlled
  device adapters; real guest/device acceptance remains environment dependent.
- `report`: criterion → action → observation → result rows, plus
  unverifiable criteria with reasons (for example external services).

Acceptance: controlled sample app with a deliberate UI bug: observe failure,
fix, re-run, passing report.

Example native checks (open the app and resolve its identity first):

```json
{"action":"check","loop_id":"note-save","scenario":{"id":"note-save","version":1,"target":"desktop","node":"sandbox","window_id":"123","width":0,"height":0,"steps":[{"action":"type","selector":"id:Editor","value":"hello"}],"assertions":[{"kind":"value","selector":"id:Editor","expected":"hello"}]}}
```

```json
{"action":"check","loop_id":"android-form","scenario":{"id":"form","version":1,"target":"android","serial":"emulator-5554","package":"com.example.app","width":0,"height":0,"steps":[{"action":"type","selector":"com.example.app:id/name","value":"Ada"},{"action":"click","selector":"com.example.app:id/save"}],"assertions":[{"kind":"text","selector":"com.example.app:id/result","expected":"Ada"}]}}
```

Replace example identities with fresh real observations. Typing follows the
application's input behavior (it does not silently clear an existing value).
If a control lacks a usable accessibility selector, report the missing
capability instead of treating screenshot appearance as a passed assertion.

## 7. Phone control (`internal/mobile`)

For application-free access from **outside the home network**, see
[the self-hosted mobile relay guide](mobile-remote.md). The same HTTPS panel
works in iPhone Safari and Android Chrome; Atlas opens outbound connections
to your server, without a home router port or phone VPN application.

- Off by default. `/mobile` in the TUI starts the
  gateway inside the running Atlas process; default bind is loopback; LAN
  bind requires explicit `--lan`/toggle and uses HTTPS with a self-signed
  certificate whose fingerprint is shown in the TUI.
- Pairing: TUI shows a QR with `https://host:port/pair#<nonce>`. The nonce
  is single-use, 2-minute, and is not a credential: the phone submits it with
  a device name, the TUI shows a pending device with a 6-digit verification
  code shown on both screens, and the desktop user must accept. Only then is
  an HttpOnly, Secure, SameSite=Strict session cookie issued (12 h idle,
  7 d absolute).
- Every state-changing request needs the CSRF token (double submit bound to
  the session) and an `Origin` matching the gateway origin; per-device rate
  limit; request body limit.
- API: sessions (title, status, duration, device info), messages (redacted
  text), pending permission/question with ids, answer permission (session
  id + tool call id + request id must match the *current* pending request; a
  stale or second answer returns `409 stale_request`), answer question,
  pause/resume/stop via `WorkflowControl` with revision, send prompt with
  idempotency key, upload attachment (size/type checked).
- Events: SSE with monotonically increasing ids; reconnect with
  `Last-Event-ID` replays from a bounded ring, otherwise the client is told
  to resync.
- Notifications: in-page notifications only when the page is open and the
  user opted in; no push service is used (would require a public endpoint).
- Previews: screenshots are not streamed by default; a per-device toggle
  allows low-resolution previews.

### Away from home with Tailscale

Atlas never opens a port to the internet. To reach the phone gateway away
from home, put the computer and the phone on the same Tailscale tailnet:

```json
{
  "tools": {
    "devices": {
      "mobile_lan": true,
      "mobile_listen": "100.101.102.103:7777",
      "mobile_hosts": ["home-pc.tail1234.ts.net"]
    }
  }
}
```

- `mobile_listen`: prefer this computer's Tailscale address over
  `0.0.0.0`, so the gateway is not bound on other networks. Some ISPs also
  use `100.64.0.0/10` (carrier-grade NAT); binding the Tailscale address
  keeps those networks out.
- The computer's own `100.64.0.0/10` interface addresses are added to the
  certificate and the allowed `Host` list automatically. This only names an
  address of this computer; it is not treated as proof that the caller is a
  Tailscale device. Pairing, the session cookie, CSRF, `Origin` and `Host`
  checks apply exactly as on the LAN.
- `mobile_hosts`: exact hostnames such as the MagicDNS name. Each one is
  matched exactly; wildcards, ports, URLs and IP literals are rejected and
  stop the gateway from starting. It needs `mobile_lan`.
- The pairing QR uses the first configured name, then the Tailscale
  address, then a LAN address. The session cookie belongs to the address the
  phone paired on, so pair on the Tailscale name to use one pairing at home
  and away.
- Certificate renewal: when any advertised name or address (including a new
  LAN address after enabling LAN or a DHCP change) is missing from the
  certificate, `/mobile` renews it with a new fingerprint. Paired phones stay paired, since
  their token does not depend on the certificate, but their browser warns
  again. The `/mobile` dialog then shows the old and new fingerprints; on the
  phone, accept the warning only if the certificate's SHA-256 fingerprint
  matches the new one, then check Settings in the phone app shows the same
  value. If it does not match, do not continue and unpair the phone.
- The computer must be awake with Atlas and `/mobile` running; the first
  pairing still has to be accepted at the computer.

Not verified: a real Tailscale connection from a phone. Tests cover
address classification, hostname validation, `Host` checks and
certificate renewal.

UI: responsive PWA with manifest, light/dark via `prefers-color-scheme`,
language from Atlas setting, 44px targets, distinct designs for
disconnected/reconnecting/waiting/completed/partial failure states.

### iPhone and iPad: control Atlas from Safari

The phone panel supports iPhone/iPad Safari and Home Screen web app mode:
task messages, questions, permission decisions, pause/resume/stop and allowed
attachments all use the same authenticated API as the desktop workspace.
This is the phone controller; the Android automation tool remains a separate
device-control backend.

1. Configure `mobile_lan` and a reachable private `mobile_listen` address as
   above (or a Tailscale address for use away from home). Start the new Atlas
   build and open `/mobile` in its TUI.
2. Scan the QR with the iPhone camera and open it in Safari. Compare the
   six-digit pairing code with the computer, then accept on the computer.
3. In Safari, choose Share → Add to Home Screen. Leave **Open as Web App**
   enabled when offered. Open Atlas from the new icon. If iOS uses a separate
   cookie store and asks to pair again, use a fresh QR; never transfer a
   session token manually. The manifest's install URL contains no nonce.
4. Returning after locking the phone, backgrounding Safari or navigating
   back refreshes authentication and task snapshots before reopening the
   event stream. An expired session returns to pairing. Returning never
   replays an action; unsent prompt text and answers to an unchanged question
   remain drafts. Changed/resolved question identities do not inherit drafts.

Apple's Home Screen behavior is documented in
[WebKit's Safari 26 notes](https://webkit.org/blog/17333/webkit-features-in-safari-26-0/).
The panel supplies an opaque 180px Apple touch icon, 192/512px manifest icons,
safe-area spacing in portrait/landscape and a dynamic viewport height.

**Trusted HTTPS for iPhone:** an exception for a self-signed Safari page is
not a substitute for a trusted HTTPS origin in installed web apps. Atlas can
load a PEM certificate chain and private key that your phone already trusts:

```json
{
  "tools": {
    "devices": {
      "mobile_lan": true,
      "mobile_listen": "100.101.102.103:7777",
      "mobile_hosts": ["home-pc.tail1234.ts.net"],
      "mobile_tls_cert": "C:/Users/YourName/.atlas-certs/home-pc.tail1234.ts.net.crt",
      "mobile_tls_key": "C:/Users/YourName/.atlas-certs/home-pc.tail1234.ts.net.key"
    }
  }
}
```

Use your actual address, hostname and absolute file paths. For Tailscale,
follow its [HTTPS certificate setup](https://tailscale.com/docs/how-to/set-up-https-certificates)
and run `tailscale cert home-pc.tail1234.ts.net` in a private folder outside
the workspace. Enabling certificate issuance publishes the device's full
DNS name in certificate transparency logs, and certificates obtained as
files require renewal. Atlas does not enable issuance or install a root
certificate on your iPhone. After replacing renewed files, stop and reopen
`/mobile` to reload them.

Both files are required. Startup rejects expired, not-yet-valid, mismatched,
wrong-purpose or wrong-host certificates without falling back. Only allowed
hostnames covered by the certificate are advertised. The certificate's
private key is never served by the panel. Host, Origin, CSRF, pairing and
session-expiry checks remain in force; a trusted certificate alone grants
no access. With neither TLS file configured, the existing generated
self-signed certificate behavior is retained.

There is no background push service: iOS may suspend the page while locked.
Questions and permissions refresh on return. The notification toggle is
disabled on iPhone/iPad rather than promising notifications that Atlas does
not deliver there. The computer must remain awake and connected.

Verification: five WebKit tests with iPhone emulation cover layout, install
guidance, standalone mode, foreground resynchronization with fresh CSRF,
question/prompt drafts, session expiry and unavailable browser storage.
Go tests verify raster icon routes, trusted TLS with a test CA, rejected
certificate configurations and preserved pairing checks. A physical iPhone,
real Home Screen installation, device keyboard and real tailnet connection
have **not** been live-tested.

## 8. Tool contracts

- New category `devices` with terms (android, emulator, sandbox, isolated,
  node, remote, phone, mobile, mark, işaret, cihaz, telefon, yalıtılmış).
  Tool descriptions start with "Built-in Atlas tool" so "mobile use" or
  "remote device" is not mistaken for an external package.
- Flat schemas: `action` enum + flat scalar fields; at most one level of
  objects (plan steps array of flat objects).
- Unknown fields rejected via strict decoding with a structured error
  (`code`, `field`, `message`, `input_sent`, `effect`, `next_step`).
- Discovery (`tool_search`) loads schemas only; no action, no permission.

## 9. Implementation plan (trackable)

1. `taskstate` + `transfer` (path/size/hash) packages with tests.
2. `nodes`: TLS identity, pairing, token store, journal, exec/files, client,
   CLI `atlas node`, tool `remote_node` + plan DAG.
3. `isolation`: probe, Windows Sandbox lease (wsb, owned processes),
   guest-agent provisioning via `node serve --isolated`, desktop endpoints in
   the node server, tool `isolated_desktop`, CLI `atlas isolation`.
4. `android`: adb discovery, device parsing, observation, actions, tool
   `android`, CLI `atlas android devices`.
5. `marking`: model, geometry, store, validation, Windows overlay, tool
   `screen_mark`, TUI `/mark`.
6. `app_loop`: process ownership, readiness, scenario integration, report.
7. `mobile`: gateway, pairing, SSE, PWA, TUI `/mobile`.
8. Catalog/deferral/config/i18n wiring; tests; lint; build dev exe.

## 10. Status

Checked 2026-10-08 on Windows 10 Pro 19045, a single 1920x1080 monitor,
BIOS virtualization off (no hypervisor), no Android SDK installed.

| Capability | Code | Automated tests | Live on this machine |
| --- | --- | --- | --- |
| Isolated desktop | done | `internal/isolation` (capability evaluation, .wsb generation, lease start/stop with a fake guest, bystander process survives Stop) | `atlas isolation probe` reports the real blockers; `start` refuses with `isolation_unavailable` and never falls back to the host. Sandbox start/observe/input **not verified**: needs virtualization enabled in firmware plus the Windows Sandbox feature. |
| Android | done | `internal/android` against a fake adb (device states, `-s` on every call, uiautomator parsing, password redaction, stale observation on rotation/app change, Unicode refusal without ADB Keyboard) | `atlas android devices` reports adb missing and the paths it checked. Real device/emulator **not verified**: no adb or device here. |
| Screen marks | done | `internal/marking` (multi-monitor/negative-origin/DPI geometry with synthetic layouts, hash validation, store) | `ATLAS_LIVE_MARK=1` drove the real overlay: 300x200 region mapped back to virtual coordinates, overlay excluded from capture, Escape cancels, UI Automation element resolved. Multiple monitors and mixed DPI **not verified** (one monitor). TUI `/mark` builds but was not exercised in a live terminal session. |
| Multi-computer | done | `internal/nodes` (pairing, pinning, revocation, journal replay, cancel, transfer checks, DAG plan) | Two Atlas processes over HTTPS on loopback: wrong fingerprint refused, pairing, revocation, plan with real `go test` on the node, push/pull with hash check, and a rerun with the same `--id` replaying every node step. WSL/other-machine node **not verified** (WSL2 needs the hypervisor). |
| Live app loop | done | `internal/devloop`, tool tests; desktop/Android adapters exercise failed check → fix → passed check, foreground/focus rejection, missing/truncated evidence and source invalidation | `ATLAS_LIVE_APP_LOOP=1`: real Node app started as owned process, HTTP readiness, headless Chrome scenario failed on the real bug, edit, re-check passed, report kept the unverifiable criterion as `unverified`, app stopped. Native guest/device runs **not verified**. |
| Phone control | done | `internal/mobile` (fragment-only nonce, desktop acceptance, cookie flags, Origin/CSRF/Host checks, idempotent replay, stale permission/question answers, revocation, idle expiry, upload sniffing, SSE replay/gap) | PWA screens checked in a browser at phone size against canned data (pairing, list, permission, question, controls, light/dark). Against a running Atlas TUI and a real phone **not verified**. |

Known limits are listed with each section above; nothing silently degrades
to host input.

Helping another person's computer, with their acceptance and their own
permission choices, is `/support`; see [remote-support.md](remote-support.md).
Its host reuses the node API, journal and transfer code described here under
session-scoped access instead of paired-device tokens.
