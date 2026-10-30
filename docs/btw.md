# /btw: side questions

`/btw <question>` asks a question while the main agent keeps working (or
while you are just thinking), without disturbing the conversation.

```
/btw what does the retry loop in client.go do?
```

The answer appears in a panel over the chat. Press `esc` (or `q`/`enter`) to
dismiss it; `up`/`down`/`pgup`/`pgdn` scroll. Dismissing the panel while a
question is still running cancels it.

## What it guarantees

- **Not in the history.** The question and answer are never added to the
  session. They are not saved, and they are gone when the panel closes.
- **Main prompt untouched.** The main agent's system prompt, tool list and
  message history are not modified, so prompt caching of the main
  conversation is unaffected.
- **Read-only.** The side agent can only read: `view`, `glob`, `grep`, `ls`,
  the `lsp_*` code-intelligence tools, `git_status/diff/log/blame`,
  `import_graph` and `project_map`. It cannot edit, run commands, use MCP
  tools, reach the network or start other agents. A tool disabled for the
  main agent is also unavailable here.
- **Works while the agent is busy.** The side agent runs in its own scratch
  session (a child of the current one, deleted right afterwards) and does not
  wait for or queue behind the main run.
- **Knows the conversation.** It is given a bounded excerpt (about 12 KB) of
  the most recent user/assistant turns, with tool calls reduced to their
  names, so "why did you do that?" style questions have context.
- **Cost is accounted.** Tokens spent are added to the session's cost.

Before the first message there is no conversation yet; `/btw` then runs
without background and does not create a session.

## Choosing the model

By default the side agent uses the session's large model. Route it to
another model with the `btw` model role:

```json
{ "options": { "model_roles": { "btw": { "provider": "anthropic", "model": "claude-haiku-4-5" } } } }
```

## Limits

A question is at most 4000 characters and a run is cut off after 5 minutes.

## Where it lives

- Agent: `internal/agent/btw.go` (`Coordinator.AskSideQuestion`).
- Server/client: `POST /v1/workspaces/{id}/agent/sessions/{sid}/btw`, so the
  TUI works the same against a remote server.
- TUI: `internal/ui/model/btw.go` and `internal/ui/dialog/side_question.go`.
