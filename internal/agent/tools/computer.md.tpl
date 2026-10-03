See and control the Windows desktop: screenshots plus mouse and keyboard input.

Workflow: take a screenshot, find your target in the returned image, then act on it with click / type / key / hotkey. Take another screenshot to verify the result. Coordinates are physical pixels with (0, 0) at the top-left corner. The screenshot is downscaled for readability: when it is, the reply states the scale factor, so multiply image coordinates by that factor to get screen pixels (pass full_res true for a native-resolution capture).

Available actions:
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
- hotkey: press a key while holding modifiers, e.g. modifiers ctrl with key s saves, modifiers "ctrl+shift" with key s is usually save-as. Modifiers are any combination of ctrl, alt, shift, win joined with +.

Rules:
- Always screenshot first in a fresh situation; never guess coordinates from a stale screenshot after the screen may have changed.
- Coordinates outside the screen are rejected with the screen size: re-aim instead of retrying the same numbers.
- Prefer keyboard over mouse for precision: hotkeys and Tab navigation beat pixel-hunting.
- Type into a field only after clicking it and confirming focus; verify with a screenshot when it matters.
- This tool drives the user's real desktop. Stay inside the task the user asked for: do not open unrelated apps, do not submit forms or confirm dialogs the user did not approve, and stop when the goal is reached.
- Windows only: on any other OS every action reports that computer-use is unsupported there.
Advanced desktop actions:
- `windows` lists top-level window IDs; `focus` selects automation.window_id; `inspect` returns a bounded accessibility tree (500 controls); `find` matches exact automation.name/role/element_id. Roles use ControlType.Button, ControlType.Edit, etc. Runtime element IDs must be observed again after UI changes.
- `invoke` uses a control's Invoke pattern; `set_value` uses Value pattern with automation.text. Both need a unique enabled, visible match within automation.window_id. If a provider lacks a pattern, inspect OCR/screenshot and use the existing pixel tools rather than pretending the action succeeded.
- `assert` waits for automation.condition (visible/hidden/enabled/text/value) with automation.expected. An accepted click or input is not proof of success. Never inspect a password value.
- `monitors` lists physical displays. UIA uses physical desktop coordinates; subtract returned screen_origin to aim in screenshot coordinates. OCR uses screenshot coordinates directly. Existing pointer operations use screenshot coordinates, including on monitors left/above the primary.
- `ocr` reads screen text through installed Windows OCR languages. `capture_region` uses outer x/y/width/height and returns native pixels (maximum 4096 per dimension) with its origin. `capture_window` uses automation.window_id to crop the visible desktop rectangle of a window; it can include occluding windows. Use relevant crops to reduce image context.
- Supply automation.window_id on pixel/keyboard input to guard against foreground-window changes. Inspect/refocus instead of retrying a possibly completed submission.
- `handoff` persists a pause and stops this turn for CAPTCHA/passkey/manual authentication. The user resumes via `/interactions`; never bypass authentication or resume yourself.
- `status`/`trace` inspect parent-chat control and recent metadata. `trace_start`/`trace_stop` explicitly toggle before/after captures for supported non-text actions. Captures may contain visible personal data; text-entry actions are omitted. Desktop input is serialized across agents, and a timed-out native driver retains ownership until it finishes.
