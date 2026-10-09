Built-in Atlas tool (no package to install): run commands and move files on other computers the user has paired with `atlas node pair`, and combine their results in one task.

<when_to_use>
- A task spans machines: run tests on a Linux/WSL node and check the app on Windows; build on one node and copy the artifact to another.
- Use `list` first; only paired node names (plus `local` inside a plan) are valid targets.
- Commands on this computer use `bash`, not this tool (except as plan steps on node `local`).
</when_to_use>

<actions>
- `list` → paired nodes with os, network (loopback/lan/internet), capabilities, root.
- `capabilities` {node} → live node info; fails clearly if the node is offline.
- `exec` {node, argv, cwd?, timeout_ms?} → literal argv under the node root (no shell). Result: state, exit_code, stdout, stderr, operation_id.
- `push` {node, local_path, remote_path, overwrite?} / `pull` {node, remote_path, local_path, overwrite?} → size limit, SHA-256 verified; paths are relative (workspace / node root); `..`, absolute paths and symlink escapes are rejected.
- `status` / `cancel` {node, operation_id} → reconcile or stop an earlier operation.
- `plan` {steps} → up to 16 steps; independent steps run in parallel, `depends_on` steps run after their dependencies complete; a step whose dependency failed is canceled, never run.
</actions>

<example>
{"action":"plan","steps":[
 {"id":"tests","node":"wsl","action":"exec","argv":["go","test","./..."],"cwd":"."},
 {"id":"build","node":"local","action":"exec","argv":["go","build","-o","app.exe","."]},
 {"id":"smoke","node":"local","action":"exec","argv":["app.exe","--self-check"],"depends_on":["build","tests"]}
]}
</example>

<rules>
- Results carry `node`, `node_id` and `os`. Never use a path, window id or coordinate from one node on another.
- Side effects are journaled by operation_id: a lost connection is retried with the same id and the node returns the recorded result instead of running twice. If the outcome is `effect:"unknown"`, call `status` before doing anything else.
- Local plan commands also use a durable operation journal and a cross-process lock. Re-running `atlas node plan --id <same-id>` replays completed local steps as well as remote steps. Changed arguments or workspace under the same identity are rejected. An interrupted local step is reported as unknown and never re-executed automatically; inspect its effects before starting new work with a new plan id.
- `capability_denied`, `unauthorized` and `fingerprint_mismatch` need the user; do not work around them.
- Report partial plans step by step (`partial:true`); never summarize a failed or canceled step as done.
</rules>
