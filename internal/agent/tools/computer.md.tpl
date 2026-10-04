See and control the Windows desktop: screenshots plus mouse and keyboard input.

For application tasks, prefer `tool_pipeline` desktop recipes to separate model
turns for known operations. Start with `desktop:{"mode":"prepare","application":
"Exact app name"}` to find or launch, verify focus and return an observation in
one model turn. After choosing a fresh target, use `desktop.mode:"act"` with a
known `input` and its explicit window_id to execute and observe together. Use
`desktop.mode:"fill_submit"` for a supported search field: verified text and field
focus are required before Enter. Include a known `wait_for` expected state when
available. Without it, the returned image must establish the next decision.
Do not spend separate model turns on Windows search, typing an app name and
Enter when prepare can resolve the installed app. Do not request an additional
inspect/capture when the recipe's observation already contains the needed
evidence. Choose new result targets in a fresh model turn; do not batch unseen
clicks. Verify the final requested item and application state before finishing.

Application workflow: prepare the app, select a target from the fresh observation,
act or fill_submit with a new observation, and verify the requested result. Use
primitive screenshot/input calls when a recipe cannot perform the needed step.
Coordinates are physical pixels with (0, 0) at the screenshot's top-left corner.
When a screenshot is downscaled, multiply image coordinates by the returned scale
factor (full_res true returns native resolution). Cropped observations report
their origin; do not mix crop, native desktop and full-screen coordinates.

Available actions:
- launch_app: request launching one exact installed Start-app name via automation.name; ambiguity or unknown names fail. A launch request is not readiness proof. Prefer prepare for launch, focus and observation together.
- screenshot: capture the screen. Returns the image as PNG, downscaled unless full_res is true.
- screen_size: report the screen width and height in pixels.
- cursor_position: report where the pointer currently is.
- move: place the pointer at (x, y) without clicking.
- click: move to (x, y) and press a mouse button (button is left, right, or middle; default left).
- double_click: move to (x, y) and double-click the left button.
- right_click: move to (x, y) and click the right button. Same as click with button right.
- drag: hold the left button at (x, y), move to (end_x, end_y), release. Used for selecting text, moving windows, and sliders.
- scroll: turn the mouse wheel at the current pointer location. scroll_y notches vertically (positive scrolls up), scroll_x horizontally. One notch is a single wheel click.
- type: type text as keystrokes into whatever currently has focus. Click the target field first.
- key: press and release one key. Named keys: enter, tab, esc, space, backspace, delete, insert, home, end, pageup, pagedown, up, down, left, right, f1-f12, shift, ctrl, alt, win, capslock, printscreen. A single character is typed as-is.
- hotkey: press a physical virtual key while holding modifiers, e.g. modifiers ctrl with key s saves, modifiers "ctrl+shift" with key s is usually save-as. Keys are named keys or ASCII letters/digits; unsupported text characters are rejected instead of typed. Modifiers are any combination of ctrl, alt, shift, win joined with +. An accepted hotkey is not proof that the expected panel opened: verify its state before typing into a newly opened application/system dialog.

Rules:
- Always screenshot first in a fresh situation; never guess coordinates from a stale screenshot after the screen may have changed.
- Coordinates outside the screen are rejected with the screen size: re-aim instead of retrying the same numbers.
- Prefer keyboard over mouse for precision: hotkeys and Tab navigation beat pixel-hunting.
- Type into a field only after clicking it and confirming focus; verify with a screenshot when it matters.
- This tool drives the user's real desktop. Stay inside the task the user asked for: do not open unrelated apps, do not submit forms or confirm dialogs the user did not approve, and stop when the goal is reached.
- Windows only: on any other OS every action reports that computer-use is unsupported there.
Advanced desktop actions:
- `windows` quickly lists native window IDs, titles, bounds, minimized state and actual foreground status without starting an accessibility provider. Reuse the intended window ID until it disappears; do not repeatedly enumerate unrelated applications. `focus` selects automation.window_id. `inspect` returns a bounded accessibility tree (default 150 controls, automation.max_elements up to 500). `find` matches exact automation.name/role/element_id. Roles use ControlType.Button, ControlType.Edit, etc. Runtime element IDs must be observed again after UI changes.
- `invoke` uses a control's Invoke pattern; `set_value` uses Value pattern with automation.text. Both need a unique enabled, visible match within automation.window_id. If a provider lacks a pattern, inspect OCR/screenshot and use the existing pixel tools rather than pretending the action succeeded.
- `set_value` reads back non-password values and reports value_verified and keyboard_focused. It does not focus or submit the field. Before sending Enter, use a freshly observed field click to establish keyboard focus; batch that click with set_value/keyboard input and submission when the target is already known. value_not_applied stops the sequence and requires a different input method. Password values are never read back (value_verified is null).
- Mutations resolve unique enabled visible matches; assertions other than hidden resolve visible matches. Offscreen duplicate controls do not make an otherwise unique actionable target ambiguous. Multiple visible matches still require a more precise target.
- `assert` waits for automation.condition (visible/hidden/enabled/text/value) with automation.expected. The default wait is 5000 ms; automation.wait_ms allows 1-15000 ms. Use short waits for ordinary screen transitions. An accepted click or input is not proof of success. Never inspect a password value.
- `monitors` lists physical displays. UIA uses physical desktop coordinates; subtract returned screen_origin to aim in screenshot coordinates. OCR uses screenshot coordinates directly. Existing pointer operations use screenshot coordinates, including on monitors left/above the primary.
- `ocr` reads screen text through installed Windows OCR languages. Supply outer x/y/width/height to scan only the relevant rectangle. Supply automation.name for exact text matching (case, spacing and straight/curly apostrophes normalized); matches return text bounds, center_x/center_y and surrounding line_text. Never choose the first match when match_count exceeds one. Add image_origin to OCR centers for screenshot input coordinates. `capture_region` uses outer x/y/width/height and returns native pixels (maximum 4096 per dimension) with its origin. `capture_window` crops the visible desktop rectangle of automation.window_id; it can include occluding windows.
- Supply automation.window_id on pixel/keyboard input to guard against foreground-window changes. Inspect/refocus instead of retrying a possibly completed submission.
- `focus` activates an explicit native window independently of UI accessibility and reports success only after verifying the foreground window. `focus_denied` means Windows did not grant activation; do not send input until the intended window is confirmed. `target_missing` means list windows again.
- A screenshot or accessibility failure does not establish that the desktop is locked or that Snipping Tool/overlays caused the problem. Report the returned stage and details, distinguish observations from hypotheses, and try one fresh observation before handoff. `accessibility_unavailable` concerns UI controls; native focus and screenshot may still work. On `wrong_window`, refocus and inspect rather than dropping the window guard or sending keys blindly.
- `handoff` persists a pause and stops this turn for CAPTCHA/passkey/manual authentication. The user resumes via `/interactions`; never bypass authentication or resume yourself.
- `status`/`trace` inspect parent-chat control and recent metadata. `trace_start`/`trace_stop` explicitly toggle before/after captures for supported non-text actions. Captures may contain visible personal data; text-entry actions are omitted. Desktop input is serialized across agents, and a timed-out native driver retains ownership until it finishes.

Efficient, evidence-based desktop execution:
- Prefer tool_pipeline, when available, for short sequences whose arguments are already known: focus plus inspect; or guarded field click, type, Enter and OCR/capture_window. This avoids one model round trip per elementary action. Each child call retains permissions and window checks. End the sequence where a new observation must inform the next target. Never batch guessed coordinates, blind retries or clicks into results not yet observed.
- One relevant observation per decision is normally enough. Do not combine screenshot, screen_size, full OCR and full inspect unless each resolves a specific missing fact. Use windows first to identify the intended application's foreground state; a successful focus already verifies the input window. After a mutation, prefer one focused assertion or OCR/crop to repeated screenshots and tree scans. Verify completion once and stop.
- For a simple task such as finding and playing a song, aim for roughly one minute under ordinary conditions. This is a planning target, not a promise or a reason to skip verification. Use a short sequence: identify/focus the application, inspect the relevant controls once, perform the supported operation, resolve the exact result, verify completion, stop.
- Inspect/find reports supported_patterns. Use invoke only with Invoke, and set_value only with Value. For unsupported_pattern, switch method immediately rather than trying the same action again. Prefer a known application shortcut or a fresh, tightly cropped OCR target.
- A truncated observation is incomplete evidence. Resolve an exact element_id or a narrower crop; never infer uniqueness or absence from a partial tree. Exact runtime IDs bypass unrelated descendants. Do not broadly rescan the full application after each action.
- In dense lists, use exact observed text bounds and their centers. Never infer a song/file/row by counting neighboring rows or reusing a prior row's coordinates. Confirm distinguishing context such as artist and album when available. An OCR text match locates text; it does not prove the surrounding row is actionable.
- If the wrong item opens, observe current state before one focused correction. Do not repeatedly click adjacent rows. Verify the requested item's identity in the resulting player/detail view before claiming success.
- Tool results include elapsed_ms for accessibility/OCR observations; trace includes per-action duration_ms. If a simple task is taking too long, inspect these timings and change a failing strategy rather than accumulating retries. Tool timings exclude model response latency, so do not attribute the entire task duration to the desktop backend.
# Numbered observations and health

For application work, start with `windows`, select the intended window, and use
`observe` with `automation.window_id`. It returns a native window crop together
with numbered accessibility controls, supported patterns and a `snapshot_id`.
The crop description states the image origin. Numbered controls are listed in
JSON; do not interpret their ordinal as a pixel coordinate. Partial coverage is
reported as `truncated:true`; it cannot prove a name is unique or absent.

Use `snapshot_id` plus top-level `element` for `invoke`, `set_value` or `assert`.
References belong to the current conversation, expire after 30 seconds and are
invalidated by any input attempt. The tool freshly checks identity and geometry
before dispatch. On `stale_observation`, observe again; never guess a replacement.
Password controls cannot be used through numbered references.

Use `tool_pipeline` for short, already-resolved sequences. An `assert` step can
set `require_passed:true` to stop unless the observed condition is true. The
`observation_from` field references an earlier single `observe` step's snapshot.
After mutation, obtain a new observation before using another numbered target.
Each child still runs through normal hooks and permission checks. Stop to choose
a target whenever new results introduce ambiguity. Neither a successful invoke
nor verified field text proves submission or task completion.

`health` performs read-only capture, display, window enumeration and foreground
checks. Supply `automation.window_id` for an accessibility check of that window.
It reports independent statuses, timings, error categories and recovery hints;
it does not focus, type, click, install anything or close applications. A listed
window does not prove its pixels or accessibility provider are readable.
