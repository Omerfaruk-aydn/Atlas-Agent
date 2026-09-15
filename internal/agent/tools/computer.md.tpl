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
