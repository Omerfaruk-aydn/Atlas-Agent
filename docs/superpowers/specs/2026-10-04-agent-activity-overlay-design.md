# Atlas agent activity overlay

## Approved intent

Show an Atlas cursor badge, click ripple, subtle active-window/page glow and
activity label while the agent controls a desktop application or browser.
The user wants both computer use and browser use, with iterative visual tuning.
The user approved the initial in-chat visual proposal. This document specifies
the shared subsystem before implementation. Work stays in D:/Atlas.

## Visual design

- Small purple Atlas badge beside the agent's current input target.
- Brief expanding ring at an executed click; no continuous full-screen flashing.
- Thin purple emphasis around the current desktop window or browser viewport.
- Compact label: inspecting, clicking, typing, navigating or waiting for a tool.
- Reduced-motion setting removes interpolation and ripple animation.
- Display setting can disable the entire effect. Default enabled when supported.
- No typed text, passwords, URLs or page titles in the activity label.

Labels describe actual tool execution. Between model calls, the indicator must
not claim that input or observation is executing. A completed action may leave
a brief fading marker, then hide. Continuous provider-wait indication requires
an explicit agent lifecycle event; it must not be inferred from inactivity.

## Shared activity lifecycle

Add an internal activity service accepting bounded metadata: session identity,
resource, operation identity, action kind and optional verified target bounds.
Begin occurs after permission and ownership checks, immediately before execution.
End happens on success, error, cancellation and timeout. Each renderer uses
operation identities so a late finish cannot clear another operation's marker.
Updates are nonblocking and coalesced; animation does not create model turns.
Pause/handoff, session closure and application shutdown clear corresponding
indicators. Hung backends must not leave a permanent 'clicking' indicator.

Existing seams:
- internal/agent/tools/computer.go: runWithTimeout acquires desktop ownership.
- internal/agent/tools/browser.go: session acquisition and browser dispatch.
- internal/interaction/control.go: ownership and pause/resume.
- internal/app/app.go: shutdown cleanup.

## Windows desktop renderer

Use an owned native Win32 overlay with a dedicated message-loop thread.
Transparent, nonactivating and input-transparent; never steal focus or change
the user's cursor theme. The badge represents agent activity, not a replacement
system pointer. Derive location from verified action coordinates or the current
pointer; derive window emphasis from the exact target HWND when available.
Do not highlight an unrelated foreground window when the target is unknown.

Handle physical pixels, negative virtual-desktop origins, DPI changes and
multiple monitors. Overlay windows are excluded from Atlas window discovery
and accessibility traversal. Native capture must exclude or temporarily hide
the renderer with guaranteed restoration and bounded coordination. Windows
display-affinity support alone is not sufficient proof for Atlas GDI capture.
Native resources and HWNDs are released on shutdown. Other OSes use a no-op
desktop renderer without changing their existing tool behavior.

## Browser renderer

Use an owned, isolated browser overlay attached through the existing chromedp
session. Render through a Shadow DOM host with pointer-events:none, no focusable
controls and no modification to target element attributes or page event handlers.
Use viewport CSS pixels; resolve actual clicked targets from current selectors,
refs or coordinate actions. Never display guessed coordinates on unresolved
targets. Navigation recreates the overlay for the new document.

Exclude the host from text/HTML extraction, snapshots and accessible target
enumeration. Hide it for raw and annotated screenshots and restore it in a
finally-style cleanup. Screenshots with and without the overlay must match
outside deliberately returned annotations. Renderer failures are diagnostic
warnings, not failures of the underlying browser task.

Headless sessions retain headless mode and do not show a desktop glow. Visible
and attached browsers get page effects. Browser mode must not be changed merely
to make the overlay visible. Document how to select visible mode for testing.
Sensitive vault/auth operations use generic activity labels only.

## Configuration and delivery

Provide consistent enabled/reduced-motion options through existing configuration
loading and generated schema conventions. Keep initial palette centralized so
visual revisions do not require changes to tool execution. No new slash menu
is required for the first iteration; configuration and a documented preview
command/fixture are sufficient for tuning.

## Verification

1. Lifecycle tests: success, error, cancellation, timeout, concurrent sessions,
   stale completion, pause/handoff and shutdown.
2. Windows owned-fixture test: overlay never becomes foreground, input reaches
   the underlying fixture, overlay excluded from discovery, native screenshots
   remain clean and resources are released.
3. Browser local-fixture test: click/type targets still work, navigation rebuilds
   the indicator, snapshots/text exclude it, screenshots remain clean, headless
   behavior stays unchanged and close clears state.
4. Configuration tests for defaults, disabling and reduced motion.
5. Format Go changes; run affected tests and lint; verify Windows and non-Windows
   builds. Visually inspect the Windows fixture and visible browser fixture.

No release or publishing is included in this feature request. The first working
iteration is a local development build for user feedback.


## User-directed visual revision

The user's supplied screenshot supersedes the initial purple badge, text,
click ring and window-outline design. The visible design is now a small dark
arrow with a white outline and a soft blue halo, without text or page/window
borders. The hand-drawn blue circle in the screenshot is an annotation, not
part of the cursor. The lifecycle and clean-capture requirements stay intact.
Browser drawing uses SVG; native Windows drawing uses a premultiplied alpha
surface rasterized with supersampling at the current monitor DPI. This supports
4K displays without scaling up a low-resolution cursor bitmap.


## User-directed persistence and cursor ownership revision

The user requested persistence through the whole computer/browser workflow,
including provider reasoning between actions, and removal of the glowing disk.
The latest visual is a crisp outlined arrow with a small shadow. Indicators
start lazily on first tool use and end with the actual session-agent run,
cancellation or handoff. Nested workers share their parent's flow. Switching
between desktop/browser surfaces retires the previous surface.

The user explicitly requested that observations no longer make the indicator
blink. This supersedes earlier persistent clean-capture hiding: persistent
screenshots may include the cursor, described in the model's tool contract.
HTML is cloned and the owned node removed from the clone. Standalone operations
continue using capture suspension. Forced tab switching still detaches the old
page surface before restoring it on the newly selected document.

Windows system-cursor suppression requires a successfully presented replacement
and a ready independent recovery process. The recovery process owns a named
session-wide mutex through restoration, restores the configured theme on owner
failure, and does not reset an already restored theme on normal shutdown.
Browser cursor suppression is limited to the page to preserve a usable pointer
outside the browser. Capture, permissions and input ownership remain separate.
