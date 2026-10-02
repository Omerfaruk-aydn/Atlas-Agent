# Interaction scenarios

The `scenario` tool validates, runs and reports version-1 web or terminal
scenarios without a model being required to execute the scenario itself.
Runs persist intent before effects, retain immutable artifact references and
record observed source fingerprints. A changed source invalidates old proof.
Validation never executes project configuration, starts a browser or creates a
session database. Reports inspect saved observations; they never replay them.

```sh
atlas-agent workflow scenario validate scenario.json
atlas-agent workflow scenario run scenario.json --session-id SESSION --allow-execution
atlas-agent workflow scenario report RUN_UUID --session-id SESSION
```

Run evaluates the ordinary project configuration and keeps configured hooks,
browser enablement, execution isolation and workflow budgets. Explicit CLI
approval covers the declared command or browser effects. Inspection uses the
default `.atlas` store; specify `--data-dir` for a configured custom store.
No model request or automatic dependency installation is part of this command.

Web steps support navigation, click, type, keys, reload and viewport resize.
Assertions inspect actual text, value, visibility or focus. The adapter captures
DOM, console and a bounded, decoded PNG through the existing browser tool.
Enable the browser in project configuration before running a web scenario.

Terminal scenarios take literal `argv`, initial character dimensions and real
PTY input, keys, resize, wait-for-text and cancellation steps. Assertions inspect
the raw transcript, actual exit code and observed process status. A pipe log or
screenshot cannot replace terminal proof. Windows uses ConPTY; Linux uses a PTY.
Other platforms report unavailable. Required isolated execution needs a runner
with terminal support; it never falls back to a host terminal.

```json
{
  "id": "terminal-smoke",
  "version": 1,
  "target": "tui",
  "argv": ["my-cli", "interactive"],
  "width": 80,
  "height": 24,
  "timeout_ms": 30000,
  "steps": [
    {"action": "wait_text", "value": "Ready"},
    {"action": "input", "value": "quit\r"}
  ],
  "assertions": [
    {"kind": "transcript", "expected": "Goodbye"},
    {"kind": "exit", "expected": "0"}
  ]
}
```

At most 64 steps, 64 assertions and ten minutes are accepted. Terminal input
is capped at 4 KiB per step, transcripts at 16 MiB and artifact storage at 32 MiB.
Raw terminal transcripts preserve control sequences. A separate bounded JSON
screen interpretation handles ordinary text, cursor movement, erasure and
scrolling without executing OSC/clipboard controls. Unsupported terminal modes
are recorded as gaps. It interprets the transcript at the final viewport size;
it does not reconstruct every historical resize or certify full VT fidelity,
focus or accessibility. Visual critique remains a separate requirement. Passed scenarios register source-bound
UI evidence linked to the retained run record; modified proof is rejected.

Current verification includes actual Chrome persistence after page navigation
and Windows terminal input, resize, completion and owned cancellation. Linux
cross-compilation is verified; Linux runtime verification is unavailable in this
environment because WSL virtualization is disabled. No Linux runtime pass is
claimed.
