Built-in Atlas tool: work on the other computer the user connected to with `/support` → "Connect to another computer". The computer owner accepted the connection and chose what is allowed; every call runs there, never on this computer.

<when_to_use>
- The user asks for IT support, diagnosis, a file, an installation or a fix on the connected (remote) computer.
- Call `status` first: it names the computer (`remote_device_id`, name, OS), the granted capabilities, the allowed folders, the session expiry and whether the remote desktop is usable.
- Work on this computer keeps using `bash`, `view`, `computer` and `browser`. Never use those for the remote computer, and never use this tool for this computer.
</when_to_use>

<actions>
- `status` → state (connected, paused, reconnecting, revoked, expired…), remote device, capabilities, folders, owner, expires_at, desktop availability.
- `system_info` {device_id} → OS, version, uptime, CPU count, memory, disks of the allowed folders (used %), network interfaces. Disk "used %" is capacity, not drive health.
- `list` {device_id, remote_path?} → entries of an allowed folder; empty path lists the folder labels. Links are reported, not followed.
- `exec` {device_id, argv, cwd?, timeout_ms?} → literal argv on the remote computer (no shell unless argv starts one). Needs `command`; installers also need `install`, service/registry/network changes `services`, shutdown/restart `restart`.
- `verify_install` {device_id, operation_id, verify_argv, expected_output} → independent command probe of a completed installation from this session. Require the installed application's identity/version in stdout; a zero exit code alone fails verification. Installer commands are not verification probes. This proves the requested probe criterion, not every application feature.
- `push` {device_id, local_path, remote_path, conflict?} → send a workspace file in verified chunks; installed only after the remote SHA-256 check. Never executed automatically.
- `pull` {device_id, remote_path, local_path, conflict?} → fetch a file into this workspace with size and SHA-256 checks.
- `observe` {device_id, image?} → remote screen geometry (virtual-screen physical pixels), monitors, windows, cursor and an `observation_id`; `image:true` adds the PNG.
- `automation` {device_id, query, window_id?, name?, role?} → read-only accessibility data of the remote desktop. Prefer it over screenshots.
- `input` {device_id, observation_id, input_action, x, y, …} → mouse/keyboard on the remote desktop, aimed with the latest observation of the same connection. Observe again after each pointer action.
- `launch` {device_id, argv} → open a program (or a browser with a URL) on the remote desktop. Browsers open there, never here.
- `tool` {device_id, session_id, generation, tool, arguments} → run the host's native `browser`, `computer` or `tool_pipeline`. Use the current session and generation from `status`. Ordinary browser/computer argument objects work here; never send a JSON string. Pipelines may contain only host browser/computer steps, semantic browser workflows or durable desktop flows. Each child checks the owner's current permissions and hooks. All requests on one host session are serialized so focus and observations cannot race.
- `operation_status` / `cancel` {device_id, operation_id} → reconcile or stop an earlier operation.
- `request_permission` {device_id, capabilities, reason} → ask the owner; then poll `permission_status` {device_id, permission_id}. A request is not a grant.
</actions>

<results>
Every result carries `device` (remote_device_id, name, OS, session_id, generation), `operation_id`, `input_sent`, `effect` (none/applied/unknown), `verification` and, for changes, a `checkpoint`. Errors carry `code`, `input_sent`, `effect`, `retryable` and `next_step`.
</results>

<rules>
- `/support` discovers filesystem roots automatically; no manual folder selection is required. Use the root labels from `status` for file paths. File capabilities are still required; never ask the owner to fill a folder field that no longer exists in this menu.
- Pass `device_id` from `status` on every call. A different id is refused; never "try" another computer.
- Preserve `session_id` in saved plans. Native tool calls also require `generation`; after reconnect, refresh status and re-observe before reusing targets. A new session does not inherit an old plan.
- `outcome_unknown`: the connection dropped after sending. Do not send it again. The tool already read the remote journal if it could (`reconciled`); otherwise call `operation_status` once connected, or observe the remote state.
- `capability_denied`, `session_paused`, `control_paused`, `secure_desktop`: the owner decides. Ask with `request_permission` or tell the user; do not work around them. Atlas never bypasses UAC: an elevation prompt must be answered by the person at the remote computer.
- `session_revoked`, `session_expired`, `remote_offline`: stop remote work and report. Never continue on this computer instead.
- `remote_desktop_unavailable`: the remote screen is locked, signed out or not interactive; ask the owner. Never use this computer's desktop instead.
- Exit code 0 or an accepted install is not success. Verify the user's original problem (version, file, service state, app opens) before calling it done, and record the state before a change so it can be undone.
</rules>

<remote_workflow>
Use `tool_pipeline` help on the host for its detailed, current limits. For example:
```json
{"action":"tool","device_id":"DEVICE_FROM_STATUS","session_id":"SESSION_FROM_STATUS","generation":1,"tool":"tool_pipeline","arguments":{"help":"desktop_flow"}}
```
For a remote browser workflow:
```json
{"action":"tool","device_id":"DEVICE_FROM_STATUS","session_id":"SESSION_FROM_STATUS","generation":1,"tool":"tool_pipeline","arguments":{"browser":{"steps":[{"id":"open","action":"navigate","url":"https://example.com"},{"id":"read","action":"observe"}]}}}
```
The generation above is illustrative; always use the live value. Screen results include an actual image part alongside the device/operation envelope, not a base64 blob to interpret as text. Native question, permission, hook and stop metadata survives the transport. A stopped or unknown operation is not success. Resume a durable desktop flow by its host `run_id` and a new outer call after reconciling the old operation; never replay uncertain input. Raw eval/CDP, credential access and local shell steps are excluded from native remote workflows; authorized system commands use remote `exec` instead.
</remote_workflow>
