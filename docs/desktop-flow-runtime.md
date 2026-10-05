# Desktop flow runtime

The v20 candidate completed the user's desktop benchmark in 1:48, with two
reported tool errors: Windows refused an Explorer activation request, and a
direct Alt+F4 request targeted a background Notepad window. The latter was
blocked. Explorer's verified rename recipe succeeded. The v21 expansion has
not yet been timed on the real task. It groups known state transitions within
one tool call; provider inference time remains outside the native runtime.

Six integrated changes:

1. Forward-only graphs with explicit true/false branch edges. A false condition
   changes the path; incomplete or ambiguous evidence stops, never chooses false.
2. Live application/window/dialog bindings. Exact title, owner HWND and observed
   process identity restrict dialogs; fresh unique runtime controls are resolved
   before accessibility input. Resolve does not launch; explicit prepare nodes
   can launch installed applications. Dialog identity is never guessed.
3. Session-scoped durable progress. Run locks prevent concurrent execution;
   atomic synced writes record intent before input and progress after checkpoint.
   Journals contain a plan fingerprint and node/attempt status, not documents,
   credentials, screenshots or runtime window IDs. Resume rebinds only windows
   needed by remaining work. Changed plans, missing state and corrupt paths stop.
4. Targeted native verification for field, row, document and result selectors.
   Successful paths do not capture images. An optional failure crop uses fresh
   target bounds and virtual-screen origin and keeps the checkpoint failed.
5. Operation-specific recovery. Up to two transient read retries per flow;
   uncertain effects never replay. Single value replacement can reconcile only
   against the same field and desired value; explicit resume is bounded to two
   durable attempts. Denial/cancellation halts further actions.
6. Windows WinEvent subscriptions before assertion reads. Native changes wake
   actual verification; one-second reads cover missing provider notifications.
   Unsupported subscriptions retain 200 ms polling. Hooks run on an owned
   message-pump thread, unregister on exit and obey the assertion deadline.
   Window/dialog discovery separately uses bounded native list polling.

Existing prepare, sequence, adaptive and transition recipes remain available.
Flow allows 1-64 nodes, at most 384 estimated and 512 actual child calls, with a
five-minute deadline and 16 inputs per operation. Child calls retain the same
schema, hooks, permissions and tool availability checks. Durable selectors must
use names/roles rather
than persisted runtime element IDs. Unknown UI states still return to the model.

Prepare nodes now find/launch/focus an application and bind its fresh identity.
Rename nodes compose the verified Explorer rename recipe inside the flow.
Close nodes focus only their bound window, send Alt+F4 once and prove absence.
An owned save/confirmation dialog stops the flow with fresh evidence. Pending
prepare, rename or close effects never replay after interruption; completed
prepare bindings restore by resolving only, without relaunching applications.

An interrupted effect returns `uncertain_effect`; this is intentionally not a
claim that the input failed. Creating another run ID does not establish that
replay is safe. A successful terminal establishes the supplied checkpoints,
not arbitrary acceptance criteria omitted from the graph.

Tests cover both branch edges, graph rejection before dispatch, owned-dialog
binding, unsafe target states, targeted read retries, replacement reconciliation,
cross-call resume without replay, changed plan rejection, journal privacy,
uncertain effect/denial stops, event wake/read ordering, fallback/deadline cleanup,
native hook cleanup and multi-monitor crop coordinates. Mock orchestration
tests establish these contracts; live application success and elapsed time
still require running the candidate on the user's desktop task.

Historical v15 validation: full tools (79.104s), computer (12.708s), agent (31.800s), prompt
(6.836s) and subagents (0.661s) suites passed. Additional flow/prompt/role tests
passed after final guidance changes. Native hook delivery was verified with a
WinEvent notification without altering application state, followed by hook
cleanup checks. Scoped lint reported zero issues. Formatting, diff checks,
build and executable version check passed.

The current v21 candidate adds prepare/rename/close composition. Regression
coverage includes multi-window foreground confirmation before closure,
unsaved-dialog stops, full 64-node and 16-input bounds, interrupted recipe
intent without replay, and child permission/hook denial. Windows can still
refuse activation; the runtime reports that failure without sending input to
another window.

V21 validation: full tools (83.722s), prompt (5.114s), subagents (1.023s)
and skills (0.985s) suites passed. Scoped lint reported zero issues. Formatting,
diff checks and the candidate build passed. No real desktop timing is claimed.

Candidate: `D:/Atlas/.atlas/atlas-desktop-flow-v21-dev.exe`, version
`v0.15.6-desktop-flow-v21-dev`.
