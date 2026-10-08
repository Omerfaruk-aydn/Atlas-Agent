# CLI Bridge

CLI Bridge turns a program that is already installed (ffmpeg, rg, git, a
project test runner) into a small set of reviewed, typed operations that
Atlas can call without a custom MCP server. A **profile** declares one
executable, the operations it may perform and the parameters each operation
accepts. Atlas validates every value, renders a literal argv you can review,
and runs it through the ordinary guarded `bash` path.

Nothing in a profile grants permission. The profile describes what a run
looks like; permissions, hooks, command policy, task ownership, read-only
mode and execution policy still decide whether it runs.

## Quick start

```bash
atlas-agent cli-bridge examples ffmpeg-video > video.json
atlas-agent cli-bridge validate video.json --operation convert --arg input=clips/in.mov --arg output=out/in.mp4
atlas-agent cli-bridge import video.json
atlas-agent cli-bridge preview video convert input=clips/in.mov output=out/in.mp4
atlas-agent cli-bridge run video convert input=clips/in.mov output=out/in.mp4 --allow-execution
```

`validate`, `import`, `list`, `inspect`, `preview`, `pin`, `remove` and
`examples` never start a program. `run` and `draft` (without `--help-file`)
do, and require `--allow-execution`.

Inside a session, the model uses the `cli_bridge` tool (discoverable through
`tool_search`, category *shell*), or you can start from the slash commands
`/cli-bridge` (profile, operation, arguments) and `/cli-bridge-list`.

## Commands

| Command | Effect |
| --- | --- |
| `cli-bridge list` | Installed profiles in both scopes, their operations and executable status. |
| `cli-bridge inspect <profile>` | Full contract, pinned executable identity, required environment variable names. |
| `cli-bridge validate <file.json> [--operation op --arg k=v ...]` | Strict validation; optionally renders one argv. |
| `cli-bridge import <file.json> [--scope project\|user] [--replace]` | Validates, refuses embedded credentials, pins the executable by content, writes atomically. |
| `cli-bridge pin <profile>` | Re-resolves and trusts the executable after you reviewed an install or upgrade. |
| `cli-bridge remove <profile>` | Deletes an installed profile. |
| `cli-bridge preview <profile> <op> [k=v ...]` | Literal argv, working directory, files read/created, `prepare_id`. |
| `cli-bridge run <profile> <op> [k=v ...] --allow-execution [--prepare-id ID] [--session-id S]` | One run through hooks, command policy and engineering records. Prints the report. |
| `cli-bridge result <run-id> --session-id S` | Stored report of an earlier run. |
| `cli-bridge draft <program> --id ID (--help-file F \| --allow-execution [--help-arg A ...])` | Inactive draft built from help text. |
| `cli-bridge examples [name]` | Bundled example profiles (`ffmpeg-video`, `rg-search`, `go-test`). |

A profile reference is `id`, `project:id` or `user:id`. Project profiles live
in `<project>/.atlas/cli-bridge/profiles/`; user profiles in
`~/.config/atlas/cli-bridge/profiles/` (override with
`ATLAS_AGENT_CLI_BRIDGE_DIR`). Import refuses an id that already exists in the
other scope; if a copy appears there anyway, unqualified references fail as
ambiguous instead of guessing.

The command-line `run` is the explicit approval for that one run (it uses a
bypass permission service like `workflow scenario run`); configured
PreToolUse/PostToolUse hooks, command policy, execution policy and
engineering accounting still apply. Inside the agent, ordinary permission
prompts apply.

## Profile format (version 1)

```json
{
  "kind": "atlas.cli-bridge.profile",
  "version": 1,
  "id": "video",
  "description": "What this profile is for.",
  "executable": { "name": "ffmpeg" },
  "env": ["OPTIONAL_TOKEN_NAME"],
  "operations": [
    {
      "id": "convert",
      "description": "What this operation does.",
      "args": ["-hide_banner", "-nostdin", "-n", "-i", "{input}", "-crf", "{crf}", "{output}"],
      "params": [
        { "name": "input", "type": "input_file", "required": true, "extensions": [".mov"] },
        { "name": "crf", "type": "integer", "min": 0, "max": 51, "default": 23 },
        { "name": "output", "type": "output_file", "required": true, "extensions": [".mp4"] }
      ],
      "working_dir": ".",
      "timeout_seconds": 1800,
      "max_output_bytes": 65536,
      "expected_exit_codes": [0],
      "outputs": [{ "param": "output", "min_bytes": 1024 }],
      "stdout_contains": "",
      "background": true
    }
  ]
}
```

Validation is strict: unknown fields, a different `kind` or `version`, empty
or duplicate ids, unreferenced or doubly referenced params, fields that do not
apply to a param's type (for example `values` on an integer), defaults of the
wrong type and invalid bounds are all errors.

### Executable

Exactly one of `name` (a bare program name, resolved through `PATH` **once** at
import or `pin`) or `path` (absolute). `sha256` optionally pins the expected
content. Import records the resolved real path, size, modification time and
SHA-256. Each preview and run re-hashes that pinned file; it never consults
`PATH` again, so a changed `PATH` cannot silently select another program. A
changed file is reported as `changed` and refuses to run until you re-pin.

Classification happens without running anything:

- **native** – ELF/Mach-O on Unix, a PE image (`MZ`) on Windows.
- **script** – a Unix file with an absolute shebang; the kernel starts the
  interpreter with the literal argv. Scripts are rejected on Windows.
- **batch** – `.cmd`/`.bat` on Windows. Rejected: cmd.exe re-parses its command
  line, so a literal argv cannot be guaranteed. For npm-style shims, point
  `executable.path` at `node.exe` and put the script path in the fixed args.
- **launcher** – Windows App Execution Aliases (zero-byte entries under
  `WindowsApps`). Rejected because their target cannot be identified; use the
  real program path.
- Anything else, including text without a shebang (which a shell would run as
  shell source), is **unsupported**.

A missing program is reported as `unavailable` (or `unpinned` when it was
missing at import); the profile stays installed and inactive.

### Args and placeholders

`args` lists literal argv elements after the program. An element may contain
**one** `{name}` placeholder; `{{` and `}}` are literal braces. Nothing else is
interpreted: no shell, template language, variables or globbing. Rendering
order is the order of `args`:

- A whole-element placeholder emits the value, or nothing when an optional
  param is omitted.
- With `flag`, a scalar emits `flag value`; a boolean emits `flag` only when
  `true`; a list emits `flag item` for each item.
- An embedded placeholder (`--name={name}`) must be required or defaulted, and
  cannot be a list, boolean or flagged param.

Example (`rg-search`): `pattern=TODO glob=*.go glob=!vendor/**` renders
`rg --no-config --line-number --color never --max-count 50 --glob *.go --glob !vendor/** --regexp TODO -- .`

### Parameter types

| Type | Constraints | Notes |
| --- | --- | --- |
| `string` | `min_length`, `max_length` (≤4096, default 1024), `pattern` (anchored RE2), `allow_leading_dash`, `secret` | Values starting with `-` are refused unless allowed, so a value cannot become an option. |
| `integer` | `min`, `max` (required, integral, within ±2^53) | |
| `number` | `min`, `max` (required, finite) | Rendered without exponent noise. |
| `boolean` | `flag` (required) | `true`/`false` only. |
| `enum` | `values` (1–64 unique) | |
| `input_file` | `extensions`, `allow_external` | Project-relative or absolute inside the project; must be an existing regular file; symlinked project components are refused. Rendered as an absolute path. |
| `output_file` | `extensions`, `overwrite` | Must be inside the project, its directory must exist, and it must not exist unless `overwrite` is set. |
| `list` | `item_type` (string, integer, number, enum, input_file), `min_items`, `max_items` (≤64) plus item constraints | Repeat `name=value` per item. |

Callers pass values as `name=value` strings. Unknown names, repeated scalars,
type mismatches and bound violations are rejected before anything runs.

External input files (outside the project) require `allow_external` **and** an
explicit read permission request at run time. Outputs outside the project are
not supported. Output files are also checked against the assigned workspace
and task ownership.

### Limits and expectations

`timeout_seconds` (1–3600, default 120) stops the process. `max_output_bytes`
(1 KiB–1 MiB, default 64 KiB) bounds each of stdout and stderr in reports; the
full byte counts are still recorded. `expected_exit_codes` defaults to `[0]`.
`outputs` declares files that must exist after the run, be created or
rewritten by it and satisfy `min_bytes`/`max_bytes`. `stdout_contains` adds a
literal check. A run passes only when **every** expectation holds; exit 0 alone
is not success.

`background: true` allows `run` with `background`; the job uses the shared
background job manager (`job_output`/`job_kill` work as usual), keeps the
profile time limit and bounded output, and `cli_bridge status` (or
`cli-bridge result`) evaluates expectations once the job has finished.

### Credentials

Profiles must not contain credentials. Import scans the document with the
built-in secret rules and refuses likely tokens. A program that needs a token
declares the **variable name** under `env`; the value stays in the environment
Atlas runs in, is never stored, and is redacted from reports. A `string` param
marked `secret` is shown as `[REDACTED]` in previews, reports and model output.
Secret values appear only in the argv handed to the process.

## Preview binding

`preview` returns a `prepare_id` derived from the profile hash, operation,
executable path and SHA-256, the full argv and the working directory. Passing
it to `run` refuses the run if any of those changed and names what changed
(profile contract, executable identity, working directory or argv). Preview
records hold no secret values.

## Reports and evidence

Every attempted run is stored in the engineering artifact store
(`cli-bridge-runs-<session hash>`), with stdout/stderr as linked artifacts:

- profile id, scope, profile hash, operation, prepare id;
- executable path, kind and SHA-256; working directory;
- start/finish time and duration; real exit code;
- outcome: `passed`, `failed`, `timeout`, `canceled`, `permission_denied`,
  `hook_denied`, `blocked`, `running` or `error`;
- bounded stdout/stderr with total byte counts;
- observed output files (existence, size, SHA-256, whether this run changed them);
- the background job id, when there is one.

`verify run` accepts `cli_bridge` checks, but only an observed `run` counts:
`list`, `inspect`, `validate`, `preview` and drafts are recorded as successful
calls without observed outcomes and fail as verification checks. Canceled or
timed-out runs are never retried automatically; their partial effects are
reported as unknown.

## Drafts from help text

`cli-bridge draft` (or `cli_bridge` action `draft`) captures help output — a
real, permission-checked run limited to 30 seconds and 64 KiB — or reads saved
text with `--help-file`. The draft lists option candidates with their source
lines and a skeleton with **no operations**. It has its own document kind,
lives in `.atlas/cli-bridge/drafts/`, is never loaded as a profile and cannot be
imported. Help text is untrusted data: instructions in it are not followed and
mentioned options are not authorized. Help text also does not prove what a
program does; verify behavior with real runs on disposable inputs.

## Examples

| Example | Scope of authority |
| --- | --- |
| `ffmpeg-video` | Reads one project video, creates one new MP4 (`-n`: never overwrites), 30 min limit, background allowed. Output must be at least 1 KiB. |
| `rg-search` | Read-only search of the project tree; ignores ripgrep config files; exit 1 (no match) is expected. |
| `go-test` | `go test -count=1` on a `./`-relative package pattern. Runs project test code, which can do anything the tests do. |

Without the program installed, `import` succeeds with `executable_status:
"unavailable"` and runs are refused as `unpinned` until you install it and run
`cli-bridge pin <id>`.

## Limitations

- CLI Bridge refuses to run while a container-required execution policy is
  active: it binds host executables and does not run them outside the
  required container. Use `bash` under the container policy instead.
- `run` is unavailable in read-only mode regardless of what a profile claims.
- A program can still do anything it is capable of with the arguments it is
  given; profiles narrow the arguments, not the program.
- The executable is re-hashed immediately before each run; a replacement in
  the instant between hashing and process start is not detected.
- Background job results are only observable while the Atlas process that
  started the job is running.
- Each session stores at most 128 run reports and 128 previews.
