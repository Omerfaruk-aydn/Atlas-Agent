Define the audience, primary task, actual content, target platform, density, brand
constraints and relevant states before styling. Inspect existing primitives/tokens.
For substantial work record a design brief and acceptance criteria with workflow
design; preserve established identity unless a redesign was requested.

Load ui-ux-pro-max for web/mobile design and use available design_search to compare
style, typography, color, UX and actual-stack candidates. Combine compatible choices
into semantic tokens; rankings are references, not a generated design guarantee.
Load apple-design for requested Apple-like interaction craft or appropriate native
Apple work. Load tui-design for terminal interfaces: web blur, CSS and SVG guidance
does not translate directly to terminal rendering. Respect disabled skills/tools.

Use a deliberate visual hierarchy, type/spacing scale and content-specific layout.
Do not make every region an identical card or impose gradients/glass regardless of
the product. Implement the real flow before polishing. Cover loading, empty, error,
partial, stale, disabled and success states as applicable. Preserve entered data on
failure, prevent duplicate mutations and keep focus useful after navigation/cancel.

For web/mobile, verify keyboard access, semantic controls, labels, actual contrast,
zoom/text scaling, reduced motion and responsive density. Direct manipulation needs
continuous feedback and interruptible transitions; activation occurs on the intended
commit event, not blindly on pointer-down. Translucency needs a legible fallback.
For TUI, verify ANSI/Unicode display widths, tiny and wide windows, wrapping, scroll,
focus, shortcuts, theme tokens and resize/cancellation during pending work.

Capture and inspect actual output at relevant narrow/wide sizes and representative
states. Exercise the main interaction plus failure/recovery. Use ui_verify and the
available visual/a11y/interaction checks for their documented targets. Record real
terminal artifacts for TUI. A screenshot proves neither keyboard behavior nor visual
excellence; code reading is not rendered inspection. Critique against the brief,
fix observed defects and recapture after changes. Report unseen criteria honestly.
