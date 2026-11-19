# Permissions and safety

What asks first, what is refused, and how to change it.

[Back to the README](../../README.md) · [All guides](README.md)

## Permissions and safety

Every side-effecting tool call — bash, writes, fetches, config changes — goes through a permission
layer before it runs.

| Mode | Behavior |
| --- | --- |
| **manual** | Ask before every side-effecting action. The default. |
| **auto-accept edits** | File edits go through; commands and fetches still ask. |
| **plan (read-only)** | Nothing is changed at all; the agent researches and presents a plan. |
| **bypass (yolo)** | Nothing asks. For throwaway sandboxes and containers. |

Switch with `/mode` in-session.

Beyond modes:

| Control | What it does |
| --- | --- |
| `options.allowed_commands` | Lift specific commands out of the bash tool's built-in block list. |
| `options.blocked_commands` | Extra commands the bash tool may never run. |
| `options.allowed_domains` | The only domains `fetch`/`download` may reach. |
| `options.blocked_domains` | Domains they may never reach, checked first. |
| `options.restrict_writes_to_working_dir` | Refuse writes outside the working directory outright. |
| `options.max_download_bytes` | Refuse downloads over a size. |
| `options.sandbox` | Contain shell-spawned processes in an OS-level container (Windows Job Objects today; a no-op elsewhere). |
| Hooks | `PreToolUse` can deny a call outright based on your own logic. |

Credentials are stored in the platform's own credential store where one is available, and API keys
are never written into session history.
