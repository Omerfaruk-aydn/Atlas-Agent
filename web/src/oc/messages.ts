// Converts Atlas messages into Atlas Agent's session message model, so the
// copied Atlas Agent timeline (session-ui) renders them unchanged. Atlas keeps
// a tool's call on the assistant message and its result on a following
// "tool" message; Atlas Agent keeps both on one tool part.

import type { Message, Part } from "../api/types"
import type {
  JsonValue,
  SessionMessageAssistant,
  SessionMessageAssistantTool,
  SessionMessageIdle,
  SessionMessageInfo,
  SessionMessageShell,
  SessionMessageUser,
  ToolContent,
} from "./shims/client"

type ToolResult = Extract<Part, { type: "tool_result" }>["data"]
type ToolCall = Extract<Part, { type: "tool_call" }>["data"]

/** Atlas Agent's name for an Atlas tool, where Atlas Agent draws it specially. */
const toolNames: Record<string, string> = {
  view: "read",
  edit: "edit",
  multiedit: "edit",
  write: "write",
  bash: "shell",
  glob: "glob",
  grep: "grep",
  ls: "list",
  fetch: "webfetch",
  web_fetch: "webfetch",
  agentic_fetch: "webfetch",
  web_search: "websearch",
  agent: "subagent",
  question: "question",
  skill: "skill",
}

function parseInput(raw: string): Record<string, JsonValue> {
  try {
    const value = JSON.parse(raw || "{}")
    return value && typeof value === "object" && !Array.isArray(value) ? value : { input: raw }
  } catch {
    return { input: raw }
  }
}

/** toolInput renames Atlas argument keys to the ones Atlas Agent's renderer reads. */
function toolInput(name: string, input: Record<string, JsonValue>): Record<string, JsonValue> {
  const out: Record<string, JsonValue> = { ...input }
  if (typeof input.file_path === "string") out.path = input.file_path
  if (typeof input.old_string === "string") out.oldString = input.old_string
  if (typeof input.new_string === "string") out.newString = input.new_string
  if (name === "agent" && typeof input.prompt === "string") {
    out.description = (input.description as string) ?? input.prompt
    out.prompt = input.prompt
  }
  return out
}

function parseMetadata(raw?: string): Record<string, JsonValue> | undefined {
  if (!raw) return undefined
  try {
    const value = JSON.parse(raw)
    return value && typeof value === "object" && !Array.isArray(value) ? value : undefined
  } catch {
    return undefined
  }
}

function resultContent(result: ToolResult): [ToolContent, ...ToolContent[]] {
  const parts: ToolContent[] = []
  if (result.content) parts.push({ type: "text", text: result.content })
  if (result.data && result.mime_type) parts.push({ type: "file", uri: `data:${result.mime_type};base64,${result.data}`, mime: result.mime_type })
  return (parts.length ? parts : [{ type: "text", text: "" }]) as [ToolContent, ...ToolContent[]]
}

function toolPart(call: ToolCall, result: ToolResult | undefined, created: number, finished: boolean): SessionMessageAssistantTool {
  const name = toolNames[call.name] ?? call.name
  const input = toolInput(call.name, parseInput(call.input))
  const time = { created, ran: created, completed: result ? created : undefined }
  if (!result) {
    if (!call.finished) return { type: "tool", id: call.id, name, state: { status: "streaming", input: call.input }, time }
    // A call whose result never arrived is still running, unless its turn ended.
    if (finished)
      return {
        type: "tool",
        id: call.id,
        name,
        executed: true,
        state: { status: "error", input, error: { type: "canceled", message: "The tool did not return." } },
        time,
      }
    return { type: "tool", id: call.id, name, executed: true, state: { status: "running", input, metadata: {} }, time }
  }
  const metadata = parseMetadata(result.metadata)
  if (result.is_error)
    return {
      type: "tool",
      id: call.id,
      name,
      executed: true,
      state: { status: "error", input, error: { type: "tool", message: result.content }, content: resultContent(result), metadata },
      time,
    }
  return { type: "tool", id: call.id, name, executed: true, state: { status: "completed", input, content: resultContent(result), metadata }, time }
}

function userMessage(m: Message): SessionMessageUser {
  const text = m.parts
    .filter((p): p is Extract<Part, { type: "text" }> => p.type === "text")
    .map((p) => p.data.text)
    .join("\n")
  const files: SessionMessageUser["files"] = []
  for (const p of m.parts) {
    if (p.type === "binary") {
      files.push({
        mime: p.data.MIMEType,
        filename: p.data.Path.split(/[\\/]/).pop() || p.data.Path,
        source: { type: "uri", uri: `data:${p.data.MIMEType};base64,${p.data.Data}` },
      } as never)
    } else if (p.type === "image_url") {
      files.push({ mime: "image/png", filename: "image", source: { type: "uri", uri: p.data.url } } as never)
    }
  }
  return { id: m.id, type: "user", time: { created: m.created_at * 1000 }, text, files: files.length ? files : undefined }
}

/** toSessionMessages converts one session's Atlas messages, in order. */
export function toSessionMessages(messages: Message[], busy: boolean): SessionMessageInfo[] {
  const results = new Map<string, ToolResult>()
  for (const m of messages) for (const p of m.parts) if (p.type === "tool_result") results.set(p.data.tool_call_id, p.data)

  const out: SessionMessageInfo[] = []
  const last = messages.length - 1
  messages.forEach((m, index) => {
    const created = m.created_at * 1000
    if (m.role === "user") {
      const shell = m.parts.find((p): p is Extract<Part, { type: "shell_command" }> => p.type === "shell_command")
      if (shell) {
        out.push({
          id: m.id,
          type: "shell",
          shellID: m.id,
          command: shell.data.command,
          status: "exited",
          exit: shell.data.exit_code,
          output: { output: shell.data.output, cursor: shell.data.output.length, size: shell.data.output.length, truncated: false },
          time: { created, completed: created },
        } as SessionMessageShell)
        return
      }
      out.push(userMessage(m))
      return
    }
    if (m.role !== "assistant") return
    const finish = m.parts.find((p): p is Extract<Part, { type: "finish" }> => p.type === "finish")
    const done = !!finish || !(busy && index === last)
    const content: SessionMessageAssistant["content"] = []
    for (const p of m.parts) {
      if (p.type === "text" && p.data.text) content.push({ type: "text", text: p.data.text })
      else if (p.type === "reasoning" && p.data.thinking)
        content.push({
          type: "reasoning",
          text: p.data.thinking,
          time: p.data.started_at ? { created: p.data.started_at * 1000, completed: p.data.finished_at ? p.data.finished_at * 1000 : undefined } : undefined,
        })
      else if (p.type === "tool_call") content.push(toolPart(p.data, results.get(p.data.id), created, done))
    }
    const reason = finish?.data.reason
    const assistant: SessionMessageAssistant = {
      id: m.id,
      type: "assistant",
      agent: "build",
      model: { id: m.model, providerID: m.provider },
      content,
      finish: reason === "end_turn" ? "stop" : reason === "max_tokens" ? "length" : reason === "tool_use" ? "tool-calls" : reason === "error" ? "error" : reason ? "unknown" : undefined,
      rawFinish: reason,
      error: reason === "error" ? { type: "error", message: finish?.data.message || finish?.data.details || "Error" } : undefined,
      time: { created, streamed: done ? (finish?.data.time ?? m.updated_at) * 1000 : undefined, completed: done ? (finish?.data.time ?? m.updated_at) * 1000 : undefined },
    }
    out.push(assistant)
    if (reason === "canceled" || reason === "error") {
      out.push({
        id: `${m.id}:idle`,
        type: "idle",
        outcome: reason === "error" ? "failed" : "interrupted",
        error: reason === "error" ? assistant.error : undefined,
        time: { created: created + 1 },
      } as SessionMessageIdle)
    }
  })
  return out
}
