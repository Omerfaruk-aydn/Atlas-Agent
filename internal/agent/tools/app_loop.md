Built-in Atlas tool: the live development loop — turn requirements into observable criteria, run the project's real app as an owned process, wait for real readiness, exercise the user flow through web, terminal, isolated desktop or Android tools, fix code from concrete evidence, re-run, and report criterion → action → observation → result.

<flow>
1. `plan` {loop_id, criteria, max_iterations?, deadline_minutes?}. Each criterion is either `verify:"scenario"` with the 1-based indices of the scenario assertions that prove it, or `verify:"unverifiable"` with a reason (external service, credentials you do not have). Criteria cannot be changed after the first check.
2. Find the real run command in the project (package.json scripts, Makefile, README) — do not guess. `start` {loop_id, argv, cwd?, port?, url?}. If something already listens on `port`, Atlas attaches and will never stop or restart it.
3. `ready` {loop_id, probe_kind:"http", probe_url, probe_contains?} (or tcp/log). No fixed sleeps. If the app exits, the result says so with its log tail.
4. `check` {loop_id, scenario, change_note?} — a web, isolated desktop, Android or terminal scenario in the `scenario` tool's format. Opening an app alone never proves the required behavior: assertions must observe the result of the user's action.
5. On failure: read the failed criteria, the app log excerpt and the scenario gaps; change the code; `check` again with `change_note`. The loop ends at `max_iterations` (default 5, max 10) or the deadline.
6. `report` → criterion rows, iterations, `git diff --stat`, unverifiable criteria. `stop` ends only an app Atlas started.
</flow>

<example>
{"action":"check","loop_id":"signup-fix","change_note":"fixed disabled submit button","scenario":{"id":"signup","version":1,"target":"web","url":"http://127.0.0.1:5173/signup","width":1280,"height":800,
 "steps":[{"action":"type","selector":"#email","value":"test@example.test"},{"action":"click","selector":"button[type=submit]"}],
 "assertions":[{"kind":"visible","selector":".toast-success"},{"kind":"text","selector":".toast-success","expected":"Account created"}]}}
</example>

<rules>
- Use test data (example.test addresses, fixtures), never the user's real accounts.
- Desktop and Android checks use isolated_desktop/android through the normal tool invoker, preserving hooks and permissions. Desktop scenarios require a fresh numeric window_id and optional paired isolated node; Android requires an explicit serial and foreground package. Open the app with the corresponding device tool before check. Never start a host process as a substitute for a guest/device app.
- Native selectors are exact: desktop `id:AutomationId` or `name:AccessibleName`; Android `package:id/control`. Steps are click/type/key, and assertions are text/value/visible/focused. Native width/height may both be zero; evidence records the actual captured image size.
- A native type/key step clicks its explicit target and then verifies keyboard focus before sending input. Desktop requires the target window in the foreground and re-checks it on the guest before input. Android text/key input re-checks the latest observation's foreground app and geometry after permission approval. Ambiguous, incomplete, password or changed-app observations fail closed. Assertion polling retries observations, never input.
- Replanning after app start, attachment or the first check is rejected. Use a new loop id for new requirements; stop retains the original ownership record.
- Report unverifiable criteria plainly; never claim them as passed.
</rules>
