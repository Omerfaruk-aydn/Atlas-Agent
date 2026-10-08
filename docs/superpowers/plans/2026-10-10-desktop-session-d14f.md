# Desktop Session d14f Reliability Plan

> Execution: superpowers:executing-plans, inline in the existing checkout.

**Goal:** Repair the observed-window contradiction and prevent inappropriate
tool selection in GUI-only tasks, while making existing batching easier to use.

**Evidence:** Session d14f22fc reports Save As HWND 3082172 in a screenshot,
then rejects that same handle as unobserved. It also advertises shell tools
that the GUI-only runtime refuses. Only three of 46 calls use the pipeline.

**Architecture:** Keep native guards and hooks. Carry native settle evidence in
response metadata and record it only when successfully returned. Filter the
advertised and deferred palettes by the same GUI-only policy as execution.
Put concrete, provider-independent sequence guidance ahead of long protocols.

**Constraints:** Work in D:\Atlas on main; preserve unrelated changes. No
commit/push/release, installed binary changes, or input on the user desktop.
Do not infer permission, focus, controls or task success from a window handle.

## Tasks

- [x] RED/GREEN: screenshot settle window identity reaches the guard; plain
  screenshots, failed/canceled reads and historical task handles do not invent
  current identities. Keep image and scale caption intact.
- [x] RED/GREEN: GUI-only palettes exclude forbidden tools across OpenAI,
  Anthropic and Gemini; general coding and a new non-GUI task retain them.
  A forbidden call remains refused, including inside a pipeline.
- [x] Strengthen the leading desktop execution guidance with schema-valid
  calculation checkpoints, grouped save transitions and semantic-first reads.
  Test wire delivery and execute the documented sequence with a fake backend.
- [x] Run affected packages, scoped lint/vet/format and build a separate dev exe.
  Review changes; report measured tests separately from real-model performance.

## Verification ledger

Screenshot and provider regressions failed before the fixes and passed after
them. The documented calculation sequence runs through the real pipeline with
a fake calculator backend: one outer call, seven operations, no screenshots;
a rejected checkpoint prevents subsequent input. This measures tool behavior,
not real model planning or native application performance.

Affected package run: `go test -p 1 -count=1 ./internal/agent/...
./internal/computer`, with `ATLAS_REQUIRE_BROWSER=1`, passed. The two final
discovery filtering changes landed during that run and were separately verified
by the final agent/tools discovery suites, including both schema deferral
settings and all three provider wire formats. Focused regressions passed three
times. Scoped golangci-lint reported zero issues; vet, format, log capitalization
and dev build passed on the final source. Independent review found one discovery
consistency issue, which was reproduced, fixed and re-reviewed with no remaining
concrete findings in scope.

Dev artifact: `dist/dev/atlas-desktop-pipeline-dev.exe`, version
`v0.15.7-desktop-pipeline-dev`. Details and limitations are in
`dist/dev/DESKTOP-PIPELINE-REPORT.md`. No real-model timing or interactive
desktop acceptance task was run; no commit, push or release was made.
