# Desktop recipe contract and recovery, v23

The latest MiMo v2.6 Flash session, `8f64ab3d-362f-4e36-a7cc-a925f904033a`,
used 33 top-level calls and produced eight error responses. The v22 keyboard
normalization worked; the repeated empty-key errors were absent. This run
exposed additional independent problems, so keyboard compatibility alone did
not make the full desktop workflow reliable.

Changes in the shared runtime:

- A checkpoint may use one equivalent nested `automation` object. Duplicate
  target/condition fields must agree. No missing expectation, window or selector
  is inferred. Invalid types and deeper wrappers fail before dispatch.
- A text assertion compares UIA.Name. An exact name selector asking for a
  different expected name is inconsistent and now fails before recipe input.
  This prevents arithmetic from running against a checkpoint that cannot pass.
- Calculator observations expose a fresh `CalculatorResults` runtime ID and
  actual localized label as checkpoint guidance. Guidance never treats the
  current value as proof of a future operation. Runtime IDs are not durable.
- Transition accepts an outer window_id only when it agrees with scoped input.
  Rename tolerates semantic/auto hints while retaining native verification.
- act/fill_submit can explicitly request focus_window. Native PID/class/process
  identity and foreground are confirmed before input. Denial or handle reuse
  stops without typing into another window.
- If a dismissing input was already sent and UIA fails while its source window
  disappears, fresh native window evidence can establish absence. The result
  retains condition_verified:false; it is not a claim that saving succeeded.

Main and desktop-specialist guidance now includes flat checkpoint examples,
changing-label selectors, exact localized text comparisons, focus switching,
read-before-replay recovery and literal file paths. HTML-encoded path strings
are not automatically rewritten into a different filesystem target.

All providers use these tool contracts. These changes do not guarantee that a
model will choose the right UI control or complete a task at a particular speed.
The recorded task eventually produced the file and closed the requested apps,
but this is not used to dismiss the errors or the excessive recovery turns.

Validation: full tools (77.662s), computer (16.420s), prompt (6.051s) and subagents
(1.088s) suites passed. Scoped lint returned zero issues. Additional focus tests
cover successful activation, denial and PID changes before input. No live v23
benchmark has been performed.

Candidate: `D:/Atlas/.atlas/atlas-desktop-flow-v23-dev.exe`, version
`v0.15.6-desktop-flow-v23-dev`.
