# Atlas activity overlay

Atlas shows a small arrow with a white outline, dark fill and a restrained
shadow while computer or browser tools are in use. A narrow RGB rim feathers
into a brighter, wider mist around the controlled display. Colors travel
around the perimeter on a 6-second cycle. Two soft smoke bands drift inside
the 112-logical-pixel feather area; their position and density change without
flashing. The main frame has rounded corners. Its rim and feathering follow
that continuous boundary without separate corner marks or inset brackets.
Smoke from adjacent edges blends as opacity, giving the inner fog a smooth
corner transition instead of a pointed diagonal join. Native drift is sampled
at 1/16 of a logical pixel to reduce visible position steps.
Windows covers the target monitor, including its taskbar. Browser
rendering covers the controlled page viewport.

The indicator starts with the first computer/browser action and remains visible
through model reasoning and subsequent actions in the same agent run. A switch
between desktop and browser retires the previous surface. It disappears when
the run finishes, is canceled, or control is handed back to the user.

A compact status banner sits at the top center, with a dark background,
rounded RGB outline and text in the selected interface language. On Windows,
its Esc hint stops the owning run through a native physical-key listener.
Injected desktop keys, browser/CDP key events and page scripts cannot invoke
that stop path. Browser cancellation checks page focus, including focus inside
an iframe. The banner, cursor and edge mist retire together on cancellation.
Other platforms show the status banner without a global Esc hint.

On Windows desktop runs, standard system cursors are temporarily hidden after
the replacement surface has been presented. Original cursor handles are
restored on normal completion and whenever the replacement surface is hidden,
including temporary capture suspension. An independent same-executable watchdog restores
the configured cursor theme if the owner exits unexpectedly or its heartbeat
stops. A session-wide Windows mutex prevents two Atlas processes from saving
each other's hidden cursors. If presentation or recovery setup fails, cursor
suppression fails open.

Browser suppression is scoped to the controlled page, using a removable owned
stylesheet. The normal cursor remains available outside the page. Browser
rendering uses vector SVG; Windows uses supersampled alpha pixels redrawn at
monitor DPI, including fractional scaling, for high-density displays. Native
smoke strips stay below the status banner and cursor in the window Z order
during every update. The control banner uses a 38-pixel strip with a crisp
pointer outline, a six-pixel corner radius, an opaque body and a fine RGB rim.
The separated Escape hint stays readable when the caption must ellipsize.
Geometry follows monitor DPI, and desktop/browser indicators use the same
outline artwork. Native surfaces show in place without repeatedly raising
the smoke above the banner; all six windows remain nonactivating and topmost.
The RGB
mist also follows monitor DPI. Its four reusable alpha surfaces leave the
center untouched and use a 60 Hz timer. Browser animation follows compositor
frames with a 60 FPS cap and compensates for timing jitter. These are rendering
targets, not guarantees about a user's display refresh or system load.
Browser feathering uses an inline SVG mask, so it needs no external or data-URL
image request on sites with restrictive image policies.

Run-owned indicators stay visible during screenshots and observations.
Screenshots may include the activity cursor and edges; tool descriptions identify them as
agent feedback rather than an application target. HTML extraction strips the
owned surface from a clone without removing it from the displayed page.
Standalone backend operations retain capture suspension. This adds no model
calls. A five-second renderer refresh keeps the browser's recovery timeout
alive through long provider waits.

## Configuration

The overlay defaults to enabled. Browser overlays appear in visible local
browser sessions and connected remote sessions; local headless sessions do
not render them. Desktop overlays are currently supported on Windows.

Merge these settings into the existing `tools` object in `atlas.json`:

```json
{
  "tools": {
    "computer": {
      "overlay": true,
      "overlay_reduced_motion": false
    },
    "browser": {
      "headless": false,
      "overlay": true,
      "overlay_reduced_motion": false
    }
  }
}
```

Set `overlay` to `false` independently for either tool to disable its effects.
The cursor uses a 26-pixel graphite arrow, a fine pale outline, a short shadow
and a bounded RGB halo. Native and browser surfaces share its geometry. DPI
scaling preserves the pointer tip and premultiplied transparency. Movement
uses a 180-millisecond transition; before button input, presentation waits
for the displayed tip to reach the verified input location. Native aiming
initializes from the actual cursor before its coordinate changes. A confirmed
click has a 260-millisecond press response and a fine visible ring; a held
drag keeps its pressed appearance
across browser tool calls. Idle feedback does not pulse or flash.
`overlay_reduced_motion` freezes the RGB colors and mist. Browser rendering
also respects the operating system's `prefers-reduced-motion` setting.

The overlay does not enable computer/browser tools, change permissions or
replace control ownership checks. Browser page scripts can remove the visual
indicator; it is feedback, not a security boundary.

## Local development verification

`internal/activity` tests cover lifecycle cleanup, stale completions and
capture suspension. On Windows, `ATLAS_DESKTOP_FIXTURE=1` enables the native
window test for visibility, nonactivation, transparent styles, destruction and
actual frame pacing. Native edge tests check premultiplied pixels, feathering,
4K strip geometry, zero-allocation steady frames, and transactional monitor
resizing after allocation failure. `ATLAS_EDGE_PREVIEW` exports a 4K image of
the native artwork. `BenchmarkEdgeFrame4K` measures four-strip rasterization.
`internal/browser` runs a local page fixture when Chrome or Chromium is
available, checking actual input and clean extracted content/screenshots.
