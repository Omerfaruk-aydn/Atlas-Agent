// The "/" commands the terminal UI offers in its command palette, so the
// web composer lists the same set. Each says what it does here: open a web
// screen, start a prompt, or (for the ones that only make sense inside a
// terminal) tell the reader so.

export type CliAction =
  | { type: "new" }
  | { type: "home" }
  | { type: "help" }
  | { type: "settings"; section: "preferences" | "appearance" | "models" | "agent" | "extensions" | "server" }
  | { type: "prompt"; text: string }
  | { type: "dialog"; name: "rewind" | "jobs" | "usage" | "files" | "agents" | "history" | "search" | "find" | "snippets" | "goal" | "compact-at" | "workflow" }
  | { type: "workflow"; section?: "board" | "batches" | "interactions" | "source_memories" | "work" | "platform_jobs"; title: string }
  | { type: "run"; name: "compact" | "fresh" | "correct" | "yolo" | "permissions" | "autocompact" }
  | { type: "ui"; name: "sidebar" | "pills" | "attach" | "back" | "transparent" | "quit" | "editor" }
  | { type: "terminal" }

export type CliCommand = { name: string; description: string; action: CliAction; aliases?: string[] }

const settings = (section: Extract<CliAction, { type: "settings" }>["section"]): CliAction => ({ type: "settings", section })
const terminal: CliAction = { type: "terminal" }
const ui = (name: Extract<CliAction, { type: "ui" }>["name"]): CliAction => ({ type: "ui", name })
const workflow = (title: string, section?: Extract<CliAction, { type: "workflow" }>["section"]): CliAction => ({ type: "workflow", title, section })
const dialog = (name: Extract<CliAction, { type: "dialog" }>["name"]): CliAction => ({ type: "dialog", name })
const run = (name: Extract<CliAction, { type: "run" }>["name"]): CliAction => ({ type: "run", name })

export const cliCommands: CliCommand[] = [
  { name: "new", description: "Start a new chat, clearing this one", action: { type: "new" }, aliases: ["clear"] },
  { name: "sessions", description: "Browse and switch between sessions", action: { type: "home" } },
  { name: "model", description: "Choose the model this session runs on", action: settings("models") },
  { name: "models", description: "Choose the model this session runs on", action: settings("models") },
  { name: "language", description: "Choose interface language", action: settings("preferences") },
  { name: "settings", description: "Open settings", action: settings("preferences") },
  { name: "goal", description: "Work towards a goal, taking turns unprompted until it is reached", action: dialog("goal") },
  { name: "back", description: "Return to the session you stepped in from", action: ui("back") },
  { name: "compact", description: "Summarize the session to free up context", action: run("compact") },
  { name: "fresh", description: "Recover a session stuck mid-run", action: run("fresh") },
  { name: "correct", description: "Interrupt the run and correct the prompt", action: run("correct") },
  { name: "auto-compact", description: "Turn automatic summarizing on or off", action: run("autocompact") },
  { name: "auto-compact-at", description: "Set how full context gets before summarizing", action: dialog("compact-at") },
  { name: "thinking", description: "Turn extended thinking on or off", action: settings("models") },
  { name: "effort", description: "Set how hard the model reasons", action: settings("models") },
  { name: "sidebar", description: "Show or hide the sidebar", action: ui("sidebar") },
  { name: "attach", description: "Attach an image or file to the prompt", action: ui("attach") },
  { name: "editor", description: "Write the prompt in $EDITOR", action: ui("editor") },
  { name: "docker-mcp", description: "Enable or disable the Docker MCP catalog", action: settings("extensions") },
  { name: "pills", description: "Show or hide the to-do and queue strip", action: ui("pills") },
  { name: "notifications", description: "Choose how you are notified when a run ends", action: settings("preferences") },
  { name: "yolo", description: "Run every tool without asking first", action: run("yolo") },
  { name: "computer-use", description: "Let the agent see and control the screen (Windows)", action: settings("extensions") },
  { name: "mark", description: "Mark a region or element on screen and attach it to your message", action: terminal },
  { name: "remote-control", description: "Connect your VPS, install the relay and pair phones from anywhere", action: terminal, aliases: ["rc"] },
  { name: "integrations", description: "See what Atlas can reach and check that it really works", action: settings("extensions") },
  { name: "app", description: "Build, run, back up and update apps on this computer", action: terminal, aliases: ["apps"] },
  { name: "support", description: "Get help on this computer or support another one with a one-time code", action: terminal },
  { name: "mobile", description: "Pair a phone to follow tasks and answer permissions", action: terminal },
  { name: "permissions", description: "Cycle what Atlas is allowed to do unasked", action: run("permissions") },
  { name: "rewind", description: "Roll the session back to a checkpoint", action: dialog("rewind") },
  { name: "jobs", description: "View and manage background jobs", action: dialog("jobs") },
  { name: "agents", description: "View and switch between running agents", action: dialog("agents") },
  { name: "workflow", description: "Inspect tasks, budgets and revision-checked controls", action: { type: "dialog", name: "workflow" } },
  { name: "context", description: "See how much of the context window is used", action: dialog("usage") },
  { name: "profiles", description: "Select model, tools, execution limits and output expectations together", action: terminal },
  { name: "batches", description: "Inspect durable per-item results and failed batch rows", action: workflow("Batches", "batches") },
  { name: "interactions", description: "Inspect action history; pause for manual authentication and resume control", action: workflow("Interactions", "interactions") },
  { name: "resume-use", description: "Return control after a manual step and continue the task", action: terminal },
  { name: "automation", description: "Inspect heartbeat and cron jobs; pause or resume explicitly", action: workflow("Automation", "platform_jobs") },
  { name: "board", description: "Inspect worker ownership and evidence; accept review tasks", action: workflow("Board", "board") },
  { name: "source-memory", description: "Inspect memory provenance, expiry and source freshness", action: workflow("Source memory", "source_memories") },
  { name: "work", description: "Show the task contract, method, hypotheses, candidates and what the agent waits for", action: workflow("Work", "work") },
  { name: "mode", description: "Switch the session's working mode", action: settings("agent") },
  { name: "roles", description: "Assign models to roles like title and summary", action: settings("models") },
  { name: "fallbacks", description: "Set which models to fall back to on failure", action: settings("models") },
  { name: "subagents", description: "Configure the subagents available to Atlas", action: settings("agent") },
  { name: "tools", description: "Choose which tools Atlas may use", action: settings("extensions") },
  { name: "fast", description: "Small model, lowest reasoning", action: settings("models") },
  { name: "quality", description: "Large model, highest reasoning", action: settings("models") },
  { name: "search", description: "Search this conversation", action: dialog("search") },
  { name: "files", description: "Show files changed in this session", action: dialog("files") },
  { name: "usage", description: "Show token usage and cost", action: dialog("usage") },
  { name: "snippets", description: "Insert a saved prompt snippet", action: dialog("snippets") },
  { name: "history", description: "Search prompts you have sent before", action: dialog("history") },
  { name: "find", description: "Search across all your sessions", action: dialog("find") },
  { name: "help", description: "Show or hide the keyboard help", action: { type: "help" } },
  { name: "init", description: "Create an AGENTS.md for this project", action: { type: "prompt", text: "Analyze this project and create an AGENTS.md describing its layout, build and test commands and conventions." } },
  { name: "transparent", description: "Use the terminal's own background", action: ui("transparent") },
  { name: "quit", description: "Exit Atlas", action: ui("quit"), aliases: ["exit"] },
]
