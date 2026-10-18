# Remote IT support between two computers (`/support`)

`/support` lets one Atlas installation help another. The person who needs
help creates a short-lived, single-use code; the supporter enters it; the
owner sees who is asking, compares a verification code and accepts or
rejects. Only then can the supporter's agent work on that computer, inside
the permissions the owner chose. Pairing alone does not start computer use:
the owner's normal cursor remains available while the connection is idle.
The banner and agent cursor appear during remote operations and disappear
after the last active operation ends. The connection stays available for
the next task. A pipeline keeps its indicators for the whole operation.

This is separate from `/rc` and `/mobile` (phone control of your own Atlas).
Those keep their own protocol, pairing and limits.

## Menu

`/support` opens:

1. **Get support for this computer** (Bu bilgisayara destek al)
2. **Connect to another computer** (Başka bilgisayara bağlan)
3. **Active support sessions**
4. **File transfers**
5. **Support history**
6. **Connection and privacy settings**

### Receiving support

1. Choose permissions (space toggles; `1` diagnose, `2` assist, `3` full),
   the session length (`t`) and
   the connection path (`w`: local network, internet, or both).
   The menu does not ask for folder paths. When file permissions are selected,
   accessible local drives are discovered automatically (the filesystem root
   on non-Windows systems). The setup screen explains this scope before the
   owner creates a code. File read/send/receive/modify permissions remain
   independent, and operating-system access permissions still apply.
2. `enter` creates the code (`XXXX-XXXX-XXXX`). `c` copies it, `l` copies an
   `atlas-support://` link, `q` shows the link as a QR code. The code is
   valid for 10 minutes and works once.
3. When someone uses the code, `/support` comes to the front even if it was
   closed. It shows the name the device sent (unverified), its verified
   certificate fingerprint, and a six-digit verification code. Compare that
   code with the supporter's screen, then `a` accepts or `r` rejects. Knowing
   the code alone never connects anyone.
4. During the session the screen shows the supporter, the time left, the
   permissions, the folders, what the agent is doing now and the last
   operations, plus:
   - `p` pause/resume the support task (no change is accepted; reading
     status stays possible),
   - `s` pause/resume screen control (mouse, keyboard and launching only),
   - `f` turn off file access,
   - `x` end the session (asks once). Running commands are stopped and
     recorded as canceled with an unknown effect.
   The on-screen banner's **Stop** button also ends the session.
5. If the supporter asks for more permissions, the request (with its reason)
   appears here: `y` grants it for this session, `n` denies it.

### Supporting another computer

Enter the code (or `CODE@address`, or the link) and compare the verification
code with the owner. After acceptance the screen shows the remote device name
and id, its OS, the connection state, the granted permissions, whether the
remote screen can be controlled, the signed-in user and the allowed folders.
Then give tasks in the normal chat, for example:

- "Karşı bilgisayardaki disk doluluğunu incele."
- "Find out why this application does not open on the connected computer."
- "Send me the report in the allowed folder."
- "Install this program there and verify it is installed."

The agent uses the `remote_support` tool and, for IT tasks, the
`it-support` role (BT Uzmanı / Sistem Analisti). Every result names the
remote device id, the operation id, whether anything was sent, its effect and
whether it was verified.

## Permissions

| Permission | Allows |
| --- | --- |
| `system_info` | OS, uptime, CPU, memory, disk capacity of allowed folders, network interfaces |
| `file_read` | Listing the exposed filesystem roots |
| `file_send` | Sending files from this computer to the supporter |
| `file_receive` | Receiving new files from the supporter |
| `file_modify` | Replacing existing files |
| `command` | Running commands (literal argv) |
| `screen_view` | Screen geometry, windows, accessibility data and screenshots |
| `input` | Mouse and keyboard; launching programs |
| `browser` | Opening a browser on this computer |
| `install` | Installers and package managers (on top of `command`) |
| `services` | Service, registry, network and boot settings (on top of `command`) |
| `restart` | Shutdown and restart commands (on top of `command`) |

Permissions are enforced on the computer receiving support, on every request.
The supporter's own approval mode (including auto-approve/YOLO) cannot widen
them. Installers, service changes and restarts are recognized by program name
and by common shell phrases as defence in depth; `command` remains a
high-impact grant because a shell can hide intent.

UAC and other secure-desktop prompts are never bypassed: they appear on the
supported computer and only the person there can answer them.

## Connection paths

**Local network.** The host listens on a random TCP port. The supporter finds
it by UDP discovery (port 47653, only answered for the current code's slot)
or connects to `CODE@address:port` shown on the owner's screen. Nothing
leaves the network.

**Internet.** A short code cannot find a computer on another network by
itself. Internet mode needs a relay you operate:

```bash
atlas support relay --listen 0.0.0.0:8443 --tls-cert cert.pem --tls-key key.pem --token-file /etc/atlas/support-token
```

Both computers set the same relay in **Connection and privacy settings** (or
`tools.devices.support_relay_url` and `support_relay_token_file`). Both
connect outbound over HTTPS/WebSocket; no port forwarding is needed. Without a
configured relay, internet mode fails with `relay_not_configured`; Atlas never
pretends to be connected and never contacts a relay that is not configured
(a link naming another relay is refused).

The relay pairs two connections that name the same slot (a hash of the code's
non-secret part) and copies bytes. It sees only end-to-end TLS. It does not
log slots, codes or payloads, and it rate-limits attempts per client.

## Security model

- Pairing codes come from `crypto/rand`, are single use, expire after at most
  30 minutes (10 by default) and are disabled after 5 wrong attempts. The
  secret part never leaves the computers: it is stretched with PBKDF2 and
  proven with an HMAC bound to the TLS session's exporter, so a relay or a
  man in the middle cannot replay or forward it.
- Both computers have their own certificate identity; the owner sees the
  supporter's fingerprint, and a reconnect is pinned to the host's.
- The code is not an access token. Acceptance creates a session token that
  exists only in memory on the supporter and as a hash on the host.
- Device id, session id and connection generation are separate. Every request
  carries all three; a request prepared for an earlier connection is refused
  with `stale_connection`, and late events from an old connection cannot
  change the current one.
- Commands are literal argv, journaled with an operation id and a request
  hash: the same id with the same request returns the recorded result, with a
  different request it is refused. A request whose connection dropped is
  reported `outcome_unknown` and reconciled from the journal, never resent.
- Files move in verified 4 MiB chunks; the whole-file SHA-256 is checked
  before an atomic rename. Conflicts are `error`, `rename` or `overwrite`
  (overwrite needs `file_modify`). Traversal, absolute paths, symlinks and
  Windows junctions that leave the allowed folders are refused. Received files
  are never executed.
- Screen input is bound to the latest observation of the same session and
  connection; a changed geometry or an older observation is refused.
- No hidden or permanent access exists: sessions are visible, time-limited,
  started by people at both ends and end on disconnect, expiry, Atlas exit or
  the banner's Stop button. A session that ended stays ended after a restart.

## Native browser and desktop workflows

The `remote_support` tool can dispatch `browser`, `computer` and
`tool_pipeline` through its `tool` action. These tools execute on the supported
computer, with that session's browser, desktop backend and durable workflow
store. Native calls require the device, session and connection generation
reported by `status`. Refresh and re-observe after reconnection. A closed
session cannot be resurrected by an old request.

The owner permissions are checked before every child action, including inside
a long workflow. The TUI host also applies its configured tool hooks. Requests
on one session are serialized to prevent focus races. Raw browser scripts,
CDP, credential operations and local shell steps are unavailable in this
channel; system commands use the separately authorized `exec` action. Remote
browser and desktop screenshots are image parts accompanied by target data.
Questions, permissions and stop metadata are preserved across transport.

Completed operation IDs return the recorded response without executing again.
Unknown results require reconciliation; they are never silently retried.
Native desktop flows retain their host-side `run_id` and completed nodes for
explicit continuation. Owner pauses or revoked permissions stop subsequent
actions. Read-only observations remain available when input is paused.

Installation requests remain unverified until an independent check succeeds.
`verify_install` references the recorded installation and runs a literal argv
probe with a required stdout identity/version match. Failure, truncation or
an unrelated session does not produce a verified result. No verification
command can bypass the owner's command permissions.

## History

Each side keeps a JSONL history per session under the Atlas devices directory
(`devices/support/host/history` and `devices/support/controller/history`):
who connected from which device, granted permissions and their changes,
operations with ids, states, effects and transfer hashes, pauses, connection
losses and how the session ended. It never stores codes, tokens, passwords,
command arguments or file contents. Screens and files are not uploaded
anywhere.

## Error codes

`pairing_invalid`, `pairing_expired`, `pairing_used`, `pairing_rejected`,
`pairing_rate_limited`, `session_revoked`, `session_expired`,
`session_paused`, `control_paused`, `capability_denied`, `remote_offline`,
`connection_lost`, `outcome_unknown`, `stale_connection`, `stale_target`,
`remote_desktop_unavailable`, `secure_desktop`, `transfer_interrupted`,
`hash_mismatch`, `path_denied`, `unsupported_platform`,
`relay_not_configured`, `no_support_target`, `wrong_target`,
`protocol_error`, `local_error`.

Errors carry `input_sent`, `effect` (`none`, `applied`, `unknown`),
`operation_id`, `remote_device_id`, `retryable` and `next_step`.

## Command line

```bash
atlas support host --allow diagnose --folder Work=D:\support
```

```bash
atlas support join ABCD-EFGH-JKMN@192.168.1.20:51515 --sysinfo --pull Work/log.txt:log.txt
```

`host` prints the code, asks `Accept? [y/N]` for a verified request and runs
until Ctrl+C. `join` prints the verification code, waits for acceptance and
prints one JSON result per step (`--sysinfo`, `--push`, `--pull`, `--exec`,
`--launch`, `--observe`).

## Platform notes

- Commands, files, system information and history work on Windows, macOS and
  Linux. Disk capacity uses `GetDiskFreeSpaceEx` / `statfs`; other platforms
  report the fields as unavailable.
- Screen observation and input need Atlas's desktop backend (Windows today)
  and an unlocked interactive session; otherwise requests fail with
  `remote_desktop_unavailable`, never with this computer's desktop.
- The `it-support` role's command recipes cover Windows, macOS and Linux
  (`atlas://skills/remote-it-support/reference.md`).

## Live acceptance on two computers

See [remote-support-acceptance.md](remote-support-acceptance.md).
## Live screen and operation timeline

With a connected support session, press `o` in `/support` to open a private
loopback viewer. It can open before the first agent screenshot. Press Start
in the viewer to capture a read-only frame about once per second; Stop ends
the capture worker. A hidden or abandoned panel stops capture after its idle
grace period. Closing Atlas closes the viewer server as well.

This viewing channel does not acquire the agent cursor, replace its observed
input target or feed frames into the model conversation. One frame is kept in
memory (8 MiB bound), with session, connection generation and capture time.
No temporary screenshot files are created. Disconnected/old frames are marked
cached; frames from another session or connection generation are rejected.
Viewing still requires the remote owner's screen-view permission.

The latest 50 operation events are shown with real dispatch, outcome and
verification states. Task/session/call/workflow/step identities and start/end
times are recorded when available. The durable history is restored when the
viewer opens, and sensitive summaries are redacted in both memory and history.
Workflow steps are currently reported from their final adapter result, not
as a claim that every intermediate execution event is streamed live.

The private viewer URL is local to the supporter's computer. It is not a
public sharing link or a replacement for the authenticated `/rc` phone panel.
# Internet support through Tailscale Funnel

In `/support`, choose to receive support and press `w` until the connection
mode is **Tailscale Funnel — internet, no VPS**. Select the permissions and
create the offer. Tailscale must already be installed, running and signed in
on the receiving computer; `/rc` provides its setup helpers. If Tailscale
asks for Funnel approval, open the URL shown by Atlas and retry.

Copy the **complete support link** with `l`. On the supporting computer,
open `/support`, choose to connect to another computer and paste this link.
A bare pairing code cannot discover a remote Funnel address. The supporting
computer needs neither Tailscale nor the owner's relay settings.

Compare the verification codes on both computers and accept on the receiving
computer. The existing permissions, end-to-end TLS identities, one-time
pairing, session expiry and resume protocol also apply to this transport.
Atlas exposes only a bounded WebSocket tunnel on a loopback HTTP server,
not the support API directly. Public HTTPS is verified normally; the inner
support connection remains end-to-end authenticated.

The support route uses its own ownership file and an available Funnel port,
so `/rc` and `/support` can run together without resetting other routes.
Withdrawal, expiry, session termination or Atlas exit closes the support
route. Keep the receiving computer awake. Funnel bandwidth limits apply.

Transport tests use a local HTTPS server; real two-computer internet
acceptance still requires a live Tailscale run.
