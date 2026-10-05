# Agent Activity Overlay Implementation Plan

> Execute inline with superpowers:executing-plans and test-driven-development.

**Goal:** Show Atlas activity without affecting desktop or browser input and observations.
**Architecture:** A shared bounded lifecycle manager dispatches metadata to native Windows and browser renderers. Capture and extraction suspend visuals; cleanup is operation-scoped.
**Tech Stack:** Go, Win32/GDI, chromedp, isolated browser Shadow DOM.
**Spec:** ../specs/2026-10-04-agent-activity-overlay-design.md

## Constraints

Work in D:/Atlas as requested. No release, no model turns, no credentials in visuals.
Preserve permission/ownership checks. Disabled and headless modes create no effects.

## Tasks

- [x] Shared lifecycle: internal/activity; test cancellation, stale completion, suspension and close before implementation.
- [x] Native renderer: dedicated Windows thread; input-transparent windows, bounded animation, exact target bounds, exclusion from discovery and capture. Test with owned windows.
- [x] Browser renderer: closed Shadow DOM, generic labels, target coordinates; strip before screenshot and extraction, restore afterward; test against a local fixture.
- [x] Wire config, computer/browser tools, pause and shutdown; document controls and provide a local development build.
- [x] Format, affected tests, lint, cross-platform build and browser visual fixture check. Native interactive tuning is documented below.

## Ledger

User approved written spec and instructed implementation. Inline execution chosen;
user explicitly prefers the existing main checkout. No worktree needed.
Initial implementation starts with shared lifecycle tests.


Implementation includes a 550 ms completion marker, operation identity checks,
registry-wide handoff cleanup and control-revision checks around visual starts.
Browser close and lazy activity initialization share a closing flag.

A local Chrome fixture verified actual click/type behavior and byte-identical
clean screenshots, including raw CDP capture. The browser preview was inspected
at D:/Atlas/.atlas/browser-overlay-preview.png. Native tests verified window
styles, unchanged foreground, capture suspension and resource destruction.
Actual native input through an underlying application and visual tuning on
mixed-DPI monitors remain part of the user's interactive preview; they were
not asserted by the automated test.

Windows development executable: D:/Atlas/.atlas/atlas-overlay-dev.exe,
version v0.15.4-overlay-dev. Linux amd64 and Darwin arm64 affected-package
cross-builds passed. Lint reported zero issues.

Final verification: all seven affected packages passed with fresh tests
(.atlas/overlay-tests-final.log); lint passed (.atlas/overlay-lint-final.log).
The development binary reports v0.15.4-overlay-dev. No commits, publication or
release were performed for this feature request.


User visual revision completed: reference arrow and soft blue halo replace the
badge, labels, borders and click ring. Native rendering now uses one transparent
layered surface and premultiplied-alpha pixels; SVG serves browser sessions.
Supersampled native output and hotspot alignment passed tests at 125%, 150%,
175%, 200%, 300% and 400% DPI scales. Physical mixed-monitor visual review is
still performed through the user's preview, not asserted as automated coverage.

Fresh revision verification: all seven affected packages passed
(.atlas/cursor-tests-final.log), lint reported zero issues
(.atlas/cursor-lint-final.log), Windows development build reports
v0.15.4-cursor-dev, and Linux amd64 / Darwin arm64 cross-builds passed.
Read-only review found no material regression in alpha, GDI cleanup,
hotspot alignment or lifecycle/capture behavior. No release was published.


## Persistence revision — 2026-10-05

Run ownership is now attached to the session agent's active generation context,
so finishing a tool leaves the indicator visible through reasoning and provider
waits. Cancellation, run completion and manual handoff clear it. Nested workers
reuse their parent's flow. Switching desktop/browser retires the previous
surface. Run-owned observations do not hide the indicator; HTML extraction
removes only the owned node from a clone. Persistent screenshots may contain
the cursor, as documented in both tool descriptions.

The glowing disk is removed. The design is a small outlined arrow with dark
fill and a restrained shadow. Windows standard cursor suppression starts only
after a usable surface is presented and a separate recovery process is ready.
That process retains a session-wide mutex through restoration. Presentation
and DPI-allocation failures restore normal cursors. Browser suppression stays
inside the controlled page, preserving normal pointer use outside it.

Verification passed: seven affected package suites, selected agent run/cancel/
subagent tests, native ownership and cursor-handle preservation tests, and a
31-second browser wait with no indicator removal during screenshot/HTML/
snapshot observations. Watchdog readiness, real cross-process exclusion and
clean reacquisition passed against the built executable without changing
system cursors. EOF and heartbeat-timeout restoration logic passed unit tests.
Lint reports zero issues; Linux amd64 and Darwin arm64 cross-builds passed.

Current executable: D:/Atlas/.atlas/atlas-control-dev.exe,
version v0.15.4-control-dev. Logs: .atlas/control-tests-final.log,
.atlas/control-agent-tests.log and .atlas/control-lint-final.log. Native live
suppression/restoration should be checked in the user's interactive preview;
automated verification preserved/copied real handles without replacing the
user's cursors. No commit or release was performed for this revision.
