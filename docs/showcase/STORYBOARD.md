# README showcase: shot list

Twenty-one short clips for the README, in the style of a product film: one feature per clip, one real
task, real models, no fake data. Each clip is 8 to 20 seconds, 1440×900 for the app and web UI and a
120×34 terminal for the CLI, exported as an animated WebP with a PNG poster.

Rules for every take:

- A real model through a real login (Claude Sonnet 5.5 or GPT-6.2 Sol unless the shot says otherwise).
- A clean demo project (`docs/showcase/demo-project`), no personal paths, names or keys on screen.
- Dark theme, English UI, cursor and typing at a human pace, long waits cut out.
- The last frame holds for a second on the result.

| # | Feature | Surface | What happens on screen |
| --- | --- | --- | --- |
| 01 | Orchestrator mode | App | Pick the orchestrator mode, ask for "add rate limiting to the API with tests"; the team board fills with three workers, the verifier passes. |
| 02 | Any model, mid-session | Web | Answer with Claude, open the model picker, switch to GPT-6.2 Sol, the next answer continues the same session. |
| 03 | Plans you already pay for | CLI | `atlas-agent login` lists ChatGPT, Claude, Copilot, Antigravity; pick one, the browser hand-off, back to a ready prompt. |
| 04 | Subagents keep context small | CLI | "How does auth work here?" spawns an explorer subagent; only its paragraph returns; the context meter barely moves. |
| 05 | Debate | CLI | `debate` on "SQLite or Postgres for this service?" with two models; rounds stream; the judge's verdict with the remaining disagreement. |
| 06 | Goals that keep going | CLI | `atlas-agent run --goal "make go test pass"`; turns tick by; the judge confirms done. |
| 07 | Configure it by talking | CLI | "Use the cheap model for titles and turn the browser tool on"; `atlas_config` shows the two changes. |
| 08 | Review before you keep | App | A finished edit shows "2 files changed"; open the diff, undo one file, keep the other. |
| 09 | Checkpoints and rewind | App | Rewind a session to before a bad edit; the files and the thread go back together. |
| 10 | LSP refactor | CLI | "Rename `ParseConfig` to `LoadConfig`"; the rename goes through the language server; build passes. |
| 11 | Safe by default | CLI | The agent wants to run `rm -rf build`; the permission prompt; deny; it adapts. |
| 12 | Downloaded files stay quarantined | CLI | It downloads a script and tries to run it; the policy layer refuses until it is read and cleared. |
| 13 | Hooks can veto | CLI | A pre-tool hook blocks edits to `migrations/`; the agent explains and asks. |
| 14 | Built-in browser | App | "Check the signup page for broken links"; the browser pane opens the local site, finds one, fixes it. |
| 15 | Hands on the Windows desktop | App | `/computer-use`: it opens Notepad, writes a release note, saves it. |
| 16 | Pull request review | CLI | `github_pr_view` on a real PR of this repo, then a review with priorities and a verdict. |
| 17 | Worktrees and teams | CLI | Two tasks in two worktrees side by side, each with its own branch and diff. |
| 18 | MCP in one line | CLI | Add an MCP server from chat; its tools appear and get used in the next answer. |
| 19 | Skills | CLI | A project skill ("release checklist") is found and followed step by step. |
| 20 | Voice input | App | Hold the mic, speak a task, the transcript lands in the composer and runs. |
| 21 | Headless in CI | CLI | `atlas-agent run --json` in a script; one JSON object with result, tokens and cost. |

Order in the README follows the table. A clip that cannot be shot honestly (the feature is not
finished, or it needs an account the recording machine does not have) is left out rather than staged.

## What was shot

Every clip below was recorded with a real model through `app/test/film.mjs` (the desktop app) or
`app/test/film-tui.mjs` (the real terminal UI in a bare Electron window), then encoded with `encode.py`.
Scenes live in `app/test/film-scenes/`.

| README | Clip | Surface | Scene |
| --- | --- | --- | --- |
| hero | `clip-hero` | App | `film.mjs hero` |
| 01 | `clip-orchestrate` | CLI | `film-scenes/orchestrate.mjs` |
| 02 | `clip-subagents` | CLI | `film-scenes/subagents.mjs` |
| 03 | `clip-debate` | CLI | `film-scenes/debate.mjs` |
| 04 | `clip-goal` | CLI | `film-scenes/goal.mjs` |
| 05 | `clip-config` | CLI | `film-scenes/config-chat.mjs` |
| 06 | `clip-review` | App | `film.mjs review` |
| 07 | `clip-rewind` | App | `film.mjs rewind` |
| 08 | `clip-safe` | CLI | `film-scenes/safe.mjs` |
| 09 | `clip-hooks` | CLI | `film-scenes/hooks.mjs` |
| 10 | `clip-browser` | App | `film.mjs browser` |
| 11 | `clip-rename` | CLI | `film-scenes/rename.mjs` |
| 12 | `clip-skills` | CLI | `film-scenes/skills.mjs` |
| 13 | `clip-mcp` | CLI | `film-scenes/mcp.mjs` |
| 14 | `clip-worktree` | CLI | `film-scenes/worktree.mjs` |
| 15 | `clip-pr-review` | CLI | `film-scenes/pr-review.mjs` |
| 16 | `clip-headless` | CLI | `film-scenes/headless.mjs` |
| 17 | `clip-desktop` | Notepad window | `film-desktop.mjs desktop` |
| 18 | `clip-voice` | App | `film.mjs voice` (needs `FILM_ELECTRON_ARGS`, a spoken WAV and the offline Vosk model) |
| any model | `clip-model-switch` | App | `film.mjs model-switch` |

Window clips (`clip-browser`, `clip-desktop`) come from `film-desktop.mjs`, which records only the target window and
crops anything personal out of the frame.

README clips are GIFs: `encode.py` writes an animated WebP and `togif.py` turns it into the optimized GIF the README
embeds (`python docs/showcase/togif.py clip.webp --width 800 --colors 48`).
