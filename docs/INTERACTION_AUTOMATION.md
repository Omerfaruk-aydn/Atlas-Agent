# Browser and desktop automation

Atlas extends the existing `browser` and `computer` tools with explicit targets,
bounded observation, assertions, file transfer, interaction history, and user
handoffs. Enable these tools with the existing browser/computer configuration.
Desktop accessibility and OCR require Windows and Windows PowerShell; OCR also
requires an installed Windows OCR language. Browser actions require Chromium.

## Browser actions

### Page actions

Advanced parameters are supplied under `advanced`; its action is derived from
the outer `action`. Existing `selector` and `ref` remain supported.

```json
{"action":"find","advanced":{"role":"textbox","name":"Email"}}
{"action":"semantic_type","advanced":{"role":"textbox","name":"Email","text":"fixture@example.test"}}
{"action":"semantic_click","advanced":{"role":"button","name":"Save"}}
{"action":"assert","advanced":{"selector":"#result","condition":"text","expected":"Saved","timeout_ms":10000}}
```

Semantic targets match exact role/name/label values and cross open shadow roots.
The implementation covers common native roles and explicit ARIA roles; it is
not a complete accessibility-name or ARIA conformance engine. Ambiguous targets
must be refined. Input waits for visibility, stable bounds, enabled/editable
state and an unobstructed target, within 30 seconds at most. An accepted input
does not establish success: use a separate assertion for the expected result.
Assertions support `visible`, `hidden`, `enabled`, `text`, `value`, `url`, and
`title`. Password values cannot be asserted or emitted by semantic observation.

`tabs` lists page targets and the active target; `tab_new` opens/selects a page;
`tab_select` requires `advanced.tab_id`; `tab_close` closes an inactive page.
New popups appear in `tabs`. `frames` returns the frame tree; `advanced.frame_id`
selects a frame for semantic observation and input. Closed shadow roots and
provider/CDP limitations produce errors rather than a fabricated observation.

`network` returns the most recent 200 response/failure records, correlated by
request ID. Cookies, authorization headers, bodies, URL credentials, queries and
fragments are omitted. Pair these records with the existing `console` action.

`capture_region` accepts native CSS-pixel `x`, `y`, `width`, and `height` under
`advanced`. Each dimension is limited to 4096 pixels. This reduces the image
area sent to the model without changing the coordinate space of page input.

File paths must stay within the workspace, including through symlink ancestors.
`upload` requires a file-input selector and `advanced.paths`. `download_start`
requires one download directory. Trigger the download, then `download_wait`
requires its expected file path and `advanced.newer_than` (an RFC 3339 timestamp
recorded before triggering the download). The result checks a new, non-empty,
stable regular file without a `.crdownload` companion. Filesystem stability is
not a content-validity check: inspect the downloaded artifact before using it.

## Desktop actions

Desktop accessibility parameters belong under `automation`. Start with
`windows`, then use a returned top-level `window_id` for subsequent actions.

```json
{"action":"inspect","automation":{"window_id":"123456"}}
{"action":"find","automation":{"window_id":"123456","role":"ControlType.Edit","name":"Email"}}
{"action":"set_value","automation":{"window_id":"123456","element_id":"42:123457","text":"fixture@example.test"}}
{"action":"invoke","automation":{"window_id":"123456","role":"ControlType.Button","name":"Save"}}
{"action":"assert","automation":{"window_id":"123456","name":"Saved","condition":"visible"}}
```

`inspect` caps the control tree at 500 elements and reports truncation. `find`
returns matches; input requires exactly one enabled, on-screen match. Element
runtime IDs are observations, not permanent identities: inspect again when
the application changes. `invoke` and `set_value` use UI Automation control
patterns. Applications that do not expose a pattern return a clear unsupported
result; use OCR/visual observation and existing pixel input instead.

`focus` selects the named window. Supplying `automation.window_id` on existing
pixel/keyboard actions checks the foreground window before input. `monitors`
lists displays; accessibility coordinates are desktop physical pixels. The
returned `screen_origin` translates these to screenshot coordinates, including
monitors left/above the primary display. OCR coordinates already use screenshot
pixels. `ocr` reads current screen text with Windows OCR. `capture_region` uses
outer `x`, `y`, `width`, and `height` and reports the crop origin.

`capture_window` requires `automation.window_id` and crops the window's visible
desktop rectangle. Occluding windows can appear in this capture; it does not
fabricate an unobstructed rendering of a covered application.

Assertions support `visible`, `hidden`, `enabled`, `text`, and `value`; no
password values are exposed. Native accessibility differs by application;
successful OCR does not imply that every UI control is accessible.

## Ownership, history and visual records

One desktop driver operation owns the shared desktop until it actually ends,
including when its caller times out. Calls waiting for ownership are cancellable.
Specialist browser sessions have independent tabs/processes and per-chat
persistent profile directories under `user_data_dir/atlas-sessions`. Existing
configured profile state is imported once. Explicit `remote_url` connections
retain the remote browser's shared login state and serialize control across
chats using that endpoint; they are not isolated cookie contexts.

`status` and `trace` inspect shared parent-chat control and the last 200 actions.
Metadata persists beneath `.atlas/interactions`; untracked interaction records
are excluded from source fingerprints. Typed text, TOTP seeds/codes, arbitrary
scripts and tool-output bodies are not recorded in this history.

`trace_start` explicitly enables before/after PNG recording for navigation,
clicks and other supported non-text actions; `trace_stop` disables it. Text,
OTP entry and arbitrary script execution are excluded. Images can still contain
visible personal data or a previously filled field; enable recording only for
the intended workflow. PNGs stay local under `.atlas/interactions/captures`.

The capture cache is bounded to 1024 files / 512 MiB; remove unneeded captures
when that limit is reached. Preview frames reuse identical content by hash.

Browser `export_test` requires one new workspace `.spec.ts` file in
`advanced.paths`. It exports observed navigation, stable clicks and supported
assertions to a Playwright test. It refuses to overwrite a file. Credentials,
typed values, query strings and arbitrary scripts are omitted; TODO comments
identify required test inputs. Review and run the test before relying on replay.

## TUI and authentication

Open `/interactions` (or press `i` in the workflow panel). `p` takes control;
`r` resumes; `v` captures a preview; `f` toggles live observation while workflow
snapshots are refreshed. Select the control row and press Enter to view a bounded
color thumbnail. Full PNG paths and before/after artifact paths remain visible.
Preview capture uses the existing tool permissions and enabled settings.

Use `handoff` when CAPTCHA, passkey, authentication, or another manual step
requires the user. This persists a pause and stops the agent turn. Subsequent
automation is blocked until the user explicitly resumes in the TUI. A currently
running native input may finish before ownership is released. CAPTCHA must be
completed by the user; Atlas does not defeat CAPTCHA or bypass authentication.

For authorized authenticator access, configure a base32 TOTP seed in a process
environment variable named `ATLAS_TOTP_<ACCOUNT>`. Keep it outside project files.
The browser `auth_code` action accepts that variable name in `secret_env` and a
fresh OTP `ref` or selector. Atlas generates the standard six-digit SHA-1,
30-second RFC 6238 code internally and enters it without returning or tracing
the code. Other authenticator profiles, SMS/email codes, passkeys and unavailable
credentials use the user handoff. Verify the actual login result afterward.

## Verification

Real Chromium fixtures cover semantic input/assertions, shadow DOM, iframe
input, crop capture, upload/download and tab selection. Windows fixtures verify
UI Automation discovery, Unicode value entry, invocation and assertions in an
ephemeral WPF window, plus OCR on a generated image. Set
`ATLAS_DESKTOP_FIXTURE=1` to enable the temporary GUI test. Short mode excludes
native-runtime fixtures; a skipped fixture is not evidence of runtime success.
