# RGB control edges

The user's fourth phone photo is the visual reference: a thin outer rim and
soft mist feathering inward along the monitor perimeter. Replace blue with
RGB circulation. Preserve the existing outlined arrow, whole-run
persistence and normal control handoff. Quality takes priority over resource
use. No additional badge or model calls are needed.

Palette: rose #ff5a81, amber #ffc452, green #80ff9a, cyan #4aefff,
blue #7a93ff and violet #e76fff. Use a 6-second cycle and two drifting
smoke bands within 112 logical pixels of feather depth, with a 60 FPS target.
Keep the center clear and preserve the rounded main frame.

Implementation:

1. Shared native feather/color rendering with 4K, alpha and geometry tests.
2. Four pass-through Windows surfaces with DPI scaling and transactional
   resource replacement. Preserve the existing cursor recovery mechanism.
3. Browser shadow surfaces with a continuous rounded frame, removable cursor
   suppression, requestAnimationFrame pacing and reduced-motion support.
4. Verify real native/browser lifecycles, visual artifacts, frame pacing,
   affected tests, lint and cross-platform builds. Produce the local
   development executable without committing or publishing a release.

Independent review found partial-resize cache corruption, reversed bottom
colors and overlapping browser corner alpha. Transactional replacement,
atomic resource replacement addresses the native failure. Browser rendering
uses one continuous color field behind an SVG feather mask, removing corner
color and opacity seams.

The mask now lives in the page's SVG DOM; it has no image-fetch dependency.
A restrictive `img-src 'self'` fixture checks actual rendered edge pixels.
The final user clarification requires rounding the main frame itself.
Separate corner shoulders are removed. Native feathering follows the signed
distance to an 18-pixel rounded frame; the browser mask uses continuous
inset rounded rectangles. Both leave the square outer corners transparent.

Final verification: seven affected package suites passed. After the last
visual revisions, native and browser suites passed again, including a
31-second wait and a 3840x2160 browser capture at DPR 2. Measured pacing was
60.0 native frames/s and 59.98 browser frames/s. Reduced-motion configuration
and browser OS preference, input, observations, cleanup and resource failure
paths passed. Lint finished with zero issues. Windows executable build and
Linux amd64/Darwin arm64 activity/browser package builds passed.

Executable: D:/Atlas/.atlas/atlas-rgb-dev.exe, v0.15.4-rgb-dev.
Logs: .atlas/rgb-tests-final.log, .atlas/rgb-visual-tests-final.log,
.atlas/rgb-lint-final.log. Native artwork: .atlas/rgb-native-4k.png.
Actual browser fixture capture: .atlas/rgb-browser-4k.png.
No commit or release was performed for this revision.

Rounded-frame correction: native/browser suites passed, including transparent
outer-corner pixels and removal of inset brackets, the actual 4K browser
fixture and lifecycle checks. Measured 60.0 native and 59.96 browser frames/s.
Log: .atlas/rgb-rounded-tests.log.

Smoke/brightness revision: the RGB cycle is now 8 seconds and the feather
area is 112 logical pixels. A brighter palette and base haze support two
drifting bands. Native surfaces sample a shared perimeter field with smooth
corner interpolation; browser bands use softly blurred wavy SVG paths inside
the rounded clip. Reduced motion freezes both layers. Regression tests verify
visible smoke 30 pixels inside the rim, density changes, native surface joins,
transparent rounded outer corners and the actual browser capture under CSP.
Native/browser suites and lint passed. Measured 60.0 native frames/s and 59.96
browser frames/s in the 4K browser fixture. The executable was rebuilt.
Logs: .atlas/rgb-smoke-tests.log, .atlas/rgb-smoke-native.log,
.atlas/rgb-smoke-lint.log.

Corner smoothing revision: the inner haze no longer uses the minimum of two
edge distances, which created the circled dark miter. Native fog independently
samples horizontal/vertical contributions and blends their opacity. The outer
rounded rim is retained. Browser haze uses overlapping gradient fields and
independent open cloud paths instead of closed rectangle elbows. Native drift
precision is 1/16 logical pixel and the animation cycle is 6 seconds.
Native/browser suites passed; 4K previews were visually inspected. Regression
tests cover diagonal corner curvature, surface joins, smoke visibility,
premultiplied alpha, reduced motion, input and cleanup.
Log: .atlas/rgb-soft-corners-tests.log.
