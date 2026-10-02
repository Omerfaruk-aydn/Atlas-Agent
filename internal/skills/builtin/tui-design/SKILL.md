---
name: tui-design
description: Design and review terminal interfaces with cell-aware responsive layout, keyboard navigation, semantic theme tokens, readable status and resilient rendering across terminal capabilities.
---
# Terminal interface design

Read repository UI instructions and inspect the existing model, message flow,
rendering primitives, theme tokens and keymap before editing. Keep established
terminal conventions and architecture; web CSS, blur and pixel font rules do
not transfer to character cells.

Define the user's main task, focus order, information density and terminal size
constraints. Use semantic theme tokens, predictable spacing and clear headings.
Do not use color alone for meaning; pair status with words or stable symbols.
Check low-color and light/dark environments; avoid unsupported glyph assumptions.

Measure display cells rather than bytes or rune count. Account for ANSI escapes,
wide CJK characters, combining marks, emoji and long localized text. Clamp
dimensions, wrap or truncate intentionally and ensure narrow/short terminals
do not panic or obscure essential controls. Test resize during active work.

Provide discoverable keys, visible focus, consistent escape/back behavior,
scrolling and selection. Avoid trapping input in overlays or triggering global
actions while editing. Preserve cursor, drafts, selection and scroll position
when background events update the UI. Make asynchronous loading, empty, error,
retry, cancellation and completion states explicit without visual churn.

Keep rendering pure and cheap; execute work through the framework's commands
and messages. Avoid excessive redraw or animation. Check screen-reader-friendly
linear output and plain/no-color output when the project supports them.

Validate existing tests plus focused layout and key interaction regressions at
narrow, normal and wide sizes. Inspect real terminal output where available;
report visual checks unavailable instead of asserting they passed. For ATLAS
read internal/ui/AGENTS.md and keep quickstyle token-driven.
