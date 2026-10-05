# Native browser control indicator

Local, visible Chrome/Chromium sessions on Windows now use an independent
desktop activity renderer instead of injecting the control indicator into
each document. The RGB edges cover the monitor, including browser chrome
and the taskbar, using the same surfaces as computer use. The native banner
also uses the desktop character engine.

## Ownership and continuity

- One renderer belongs to the browser session's activity manager.
- The complete agent run retains visual ownership during provider waits
  and navigation; changing documents does not destroy its surfaces.
- The Windows renderer replaces the system cursor only after successfully
  presenting its agent cursor, and restores it through the existing
  cancellation, question/handoff, close, and recovery paths.
- Browser activity does not inject a second page cursor or page banner.
- CDP document screenshots do not include native desktop windows, so they
  do not suspend the indicator or restart its animation.
- Physical Escape cancellation is routed through the same activity manager
  and requires the foreground window to belong to the launched browser.

## Coordinates

The adapter reads viewport size and `devicePixelRatio` in a CDP isolated
world. It resolves the containing window against CDP window bounds and the
owned browser process ID, using physical Win32 coordinates. A matching
render-widget child supplies the viewport origin when available. A bounded
client-area fallback supports dedicated browser windows without docked
DevTools/sidebars. CSS zoom and fractional pointer coordinates are included
in the conversion to desktop pixels.

During document replacement or unavailable geometry, the last verified
desktop point stays visible. Unresolved CSS coordinates are never passed
directly to the desktop renderer, and a stale location cannot receive new
click feedback. This mapping is visual only; browser input remains CDP.

The single replacement cursor also responds to physical mouse movement during
agent waits. Physical movement takes presentation ownership until a fresh
verified agent pointer event arrives; stale agent targets do not pull it back.
This does not send input or grant new permissions. An unchanged browser window
and DPI can reuse the last verified projection during CDP document replacement
so a new aim does not freeze. Unavailable document geometry suppresses click/
press feedback rather than claiming a click at an old viewport location.

Attached Chrome on the same Windows machine also uses the native renderer.
For loopback HTTP/WebSocket endpoints, the browser PID reported by CDP must
own the exact listening TCP port in the OS (IPv4 or IPv6), and the selected
target must match a visible Chromium window. A localhost proxy to another
machine cannot pass the PID/listener check. This also handles a visible
attached browser when the launch configuration still has headless enabled;
the actual verified window determines whether native presentation is possible.
Old document indicators are removed when native ownership starts and when
an existing tab is selected, preventing duplicate page cursors/banners.

Actual remote browsers and platforms without a native renderer retain the
page indicator. A remote desktop is not treated as the local Windows desktop.
Headless browsers without a visible matching window keep the page policy.

## Coordination and validation

The banner/character implementation is not modified by this change. Native
browser activity reuses it, including subsequent changes made by the
concurrent character implementation.

Added regression tests cover zoom/DPI and negative monitor coordinates,
navigation fallback, stale click suppression, invalid geometry, native
render routing without a document context, capture continuity, stop/close
forwarding, and native browser window bounds matching.

The original combined build and `go test ./...` passed. After correcting the
attached-browser routing, live tests against local Chrome on port 9222 verified
native renderer selection, no page overlay, four monitor boundaries including
the toolbar/taskbar, and activity lifetime through tab changes. A separate live
Windows test verified real system cursor replacement and restoration. Tests
also cover exact listener PID/port matching and rejection of non-local endpoints.
The live tests require explicit environment variables and otherwise skip:

```powershell
$env:ATLAS_BROWSER_OVERLAY_LIVE_URL='http://127.0.0.1:9222'
go test ./internal/browser -run '^TestNativeOverlayAttachedChromeLive$' -count=1
$env:ATLAS_DESKTOP_FIXTURE='1'
go test ./internal/activity -run '^TestNativePersistentCursorReplacementLive$' -count=1
```

Visible tests change system cursor presentation temporarily and restore it
on teardown. Their test binaries handle the same recovery-watchdog entry point
as the application before testing's normal flag parsing. DPI/zoom configurations,
Escape and question handoff beyond the covered tests still require their own
scenario validation; the passing suite does not imply every display setup was
tested visually.
