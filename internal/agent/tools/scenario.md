Validate or exercise a version-1 web, terminal, isolated desktop or Android
interaction scenario, or report a
persisted run. At most 64 steps and 64 explicit assertions, with a ten-minute
deadline. Validation performs no execution. Web actions retain browser tool
permissions and budgets. Terminal scenarios use real PTY input/resize/exit
observations and ordinary command permissions; required isolation never falls
back to a host terminal. Runs persist intent before effects and link bounded
artifacts to the observed source. Missing capabilities report unavailable and
never pass. Raw terminal transcript, terminal rendering and screenshots are
different artifacts. Source changes invalidate old observations. Scenario success
does not establish overall visual quality or replace independent review.

Native targets use the existing device tools with their hooks and permission
checks. Desktop requires window_id from isolated_desktop windows; node is an
optional paired isolated node name (default sandbox). Android requires serial
and the expected foreground package. Open the app in the foreground before the scenario; no host
desktop fallback is permitted. Native width/height may both be zero.

Native selectors: desktop `id:AutomationId` or `name:ExactAccessibleName`;
Android the exact `package:id/control` resource id. Supported native steps are
click, type (value is appended using the app's typing behavior), and key. Every
step requires a selector; text/key input requires a verified focused control.
Desktop input also verifies the expected foreground window on the guest;
Android text/key input re-checks the observation after permission approval.
Assertions: text (contains), value (exact), visible and focused. Password fields
cannot be used. Duplicate or incomplete observations fail instead of guessing.
Only observations are retried while waiting for assertions. A passing native
run saves a rendered PNG and accessibility tree bound to the workspace source.
