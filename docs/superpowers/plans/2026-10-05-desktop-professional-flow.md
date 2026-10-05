# Professional Desktop Flow Implementation Plan

> **For agentic workers:** Use superpowers:executing-plans or scoped parallel workers to implement the approved work task-by-task. Steps use checkbox syntax.

**Goal:** Integrate the user's selected improvements 1, 2, 3, 4, 6, 7 and 8 into the existing desktop tools without losing intermediate or final verification.

**Architecture:** Extend existing guarded `tool_pipeline` desktop recipes and computer observation/Windows automation contracts. All child operations continue through normal hooks, permissions and desktop leases. Semantic observations, structured checkpoints and bounded recovery reuse the existing execution pipeline.

**Tech Stack:** Go, Windows native APIs, UI Automation, existing PowerShell backend and hermetic tool fixtures.

**Spec:** The user-approved seven-point design in this conversation; existing invariants in docs/desktop-control.md and docs/desktop-multi-app-recovery.md.

## Global Constraints

- Work in D:/Atlas; preserve existing uncommitted changes, without new branches or checkouts.
- Keep existing tool behavior and visual observation default compatible.
- Do not add the unselected persistent helper, permanent window cache or performance subsystem.
- Stop on denial, cancellation, handoff, ambiguous targets or failed checkpoint; never replay uncertain mutations.
- Actual requested GUI operations and their verification remain mandatory.

## Review Focus

- Localized names, Explorer shell components and multiple matching windows must not select unrelated targets.
- Password values and inaccessible UIA providers must not disclose secrets or imply known values.
- Modal transitions must not reuse a parent's window or silently adopt an unrelated foreground window.
- Condition waits must be bounded and cancel promptly; successful input is not readiness proof.
- Semantic/capped observations and partial checkpoints must not claim task completion.

## Task 1: Native application identity and UIA value reading

Files: internal/computer/windows_observation.go, automation.ps1, Windows regression fixtures.

- [x] Reproduce localized Explorer and value-observation gaps with tests.
- [x] Resolve supported Explorer names through registered/native identity, excluding non-folder shell windows.
- [x] Expose bounded value/text, availability and keyboard-focus metadata; redact password controls.
- [x] Verify mocked registration, real read-only native metadata and protected/unsupported value fixtures.

## Task 2: Observation modes

Files: internal/agent/tools/computer_observation.go and dedicated tests; ComputerParams gains `observation`.

- [x] Keep `visual` as default; add `semantic` without screenshot and `auto` with conservative fallback.
- [x] Preserve numbering, snapshot guards, truncation and target/foreground identity.
- [x] Require readable, non-password actionable evidence for automatic semantic selection.
- [x] Verify providers with failed screenshots, incomplete UIA and password controls.

## Task 3: Guarded sequences and transitions

Files: desktop_workflow.go, new focused sequence/transition helpers and tests.

- [x] Add a bounded sequence recipe with explicit window-scoped inputs and checkpoint assertions.
- [x] Validate the whole sequence before any mutation; collect actual readback after each checkpoint.
- [x] Add transition waiting that identifies an expected application or task-related dialog and observes it in the same call.
- [x] Replace arbitrary settle sleep with explicit bounded readiness waits when conditions are supplied.
- [x] Stop before dependent input on any failed checkpoint or uncertain transition.

## Task 4: Recovery and integration

- [x] Produce typed recovery guidance with a fresh observation for wrong-window, missing-target and unsupported-pattern errors without replaying input.
- [x] Retain prepare's bounded handle/frame recovery; no focus/control bypass.
- [x] Update self-documenting tool descriptions and desktop role with examples and failure boundaries.
- [x] Run affected tests/lint/build, inspect integration and produce a development executable.
- [x] Report exact coverage and limitations; no live speedup claim until measured.
