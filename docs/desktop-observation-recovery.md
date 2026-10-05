# Desktop observation and recovery, v22

The user's v21 desktop benchmark took 1:45. Session
`72f3da7a-06c6-477e-88a9-67530d9b391f` contains 24 top-level tool calls
and four error responses. The final flow closed Notepad and Calculator in
one call and verified both window identities were absent.

The four errors and changes:

1. The model requested desktop mode `observe`, which the runtime did not
   support. This now dispatches one guarded Computer observation of an explicit
   window. Semantic is the default. Launch, focus, input and mixed recipe
   parameters are rejected before dispatch.
2. Calculator preparation matched multiple windows. Ambiguity remains an
   error; preparation now returns observed candidate identities and directs
   inspection instead of closing or relaunching an unverified existing window.
3. A Computer batch used `capture_window` as its checkpoint. This remains
   invalid. The diagnostic and main prompt explicitly require native `assert`
   with an observed selector and expected result. Rejection happens before input.
4. Explorer displayed `hesap` while the model requested `hesap.txt`. An exact
   missing target now returns a bounded list of fresh visible ListItem names
   through guarded native reads. No input is sent and no extension is guessed.
   The model must choose observed display names and separately establish any
   required actual extension.

These changes reduce avoidable recovery turns; they do not establish a new
elapsed time or guarantee that a model will always choose valid arguments.
Native ambiguity, foreground checks and hook/permission denials remain enforced.

Candidate: `D:/Atlas/.atlas/atlas-desktop-flow-v22-dev.exe`, version
`v0.15.6-desktop-flow-v22-dev`. Real desktop timing is pending.

The subsequent MiMo v2.6 Flash run (Xiaomi token-plan SGP), session
`c7eaa730-854f-40be-9e73-8bfc64c4d36a`, repeatedly placed keyboard fields
inside `automation`. The shared Computer argument decoder now accepts those
equivalent key/modifier representations, including batch inputs and nested
pipeline/flow inputs. Explicit combined modifier keys normalize to hotkey;
conflicts and invalid combinations fail before input. Literal `+` remains a
literal key. The decoder does not choose windows, complete missing graph nodes,
invent checkpoints or change permission/hook enforcement. No provider-specific
enablement is required. All-provider live performance has not been measured.

Pre-tool hooks see canonical direct keyboard arguments, including the effective
hotkey action, before deciding whether to allow or deny. A regression test
denies the normalized shortcut and proves the underlying tool never runs.
Full tools (80.036s), prompt (4.758s), subagents (0.995s) and agent (28.493s)
suites passed. Final contract/hook regressions passed after hook integration.
Scoped lint returned zero issues; formatting, diff checks, build and executable
version verification passed.
