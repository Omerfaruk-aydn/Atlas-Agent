// Wire types of the Atlas web gateway (internal/webui). Message and
// session shapes are the server's own (internal/proto); the gateway relays
// them unchanged.

export type Role = "user" | "assistant" | "system" | "tool"

export type FinishReason = "end_turn" | "max_tokens" | "tool_use" | "canceled" | "error" | "content_filter" | (string & {})

export type Part =
  | { type: "text"; data: { text: string } }
  | { type: "reasoning"; data: { thinking: string; signature?: string; started_at?: number; finished_at?: number } }
  | { type: "image_url"; data: { url: string; detail?: string } }
  | { type: "binary"; data: { Path: string; MIMEType: string; Data: string } }
  | { type: "tool_call"; data: { id: string; name: string; input: string; type?: string; finished?: boolean } }
  | {
      type: "tool_result"
      data: {
        tool_call_id: string
        name: string
        content: string
        data?: string
        mime_type?: string
        metadata?: string
        is_error: boolean
      }
    }
  | { type: "finish"; data: { reason: FinishReason; time: number; message?: string; details?: string } }
  | { type: "shell_command"; data: { command: string; output: string; exit_code: number } }

export interface Message {
  id: string
  role: Role
  session_id: string
  parts: Part[]
  model: string
  provider: string
  created_at: number
  updated_at: number
  is_summary_message?: boolean
}

export interface Todo {
  content: string
  status: string
  active_form: string
}

export interface Session {
  id: string
  parent_session_id: string
  title: string
  message_count: number
  prompt_tokens: number
  completion_tokens: number
  summary_message_id: string
  cost: number
  todos?: Todo[]
  created_at: number
  updated_at: number
  is_busy: boolean
  attached_clients: number
}

export interface WorkspaceView {
  id: string
  path: string
  name: string
  connected: boolean
  server_version?: string
  usage_profile?: string
  agent_ready: boolean
  configured: boolean
  permission_mode?: PermissionMode
}

export interface Bootstrap {
  csrf: string
  instance: string
  version: string
  started_at: number
  server: { address: string; reachable: boolean; version: string; platform: string }
  workspaces: WorkspaceView[]
}

export interface Project {
  path: string
  name: string
  last_used?: number
}

export type PermissionMode = "manual" | "auto_accept_edits" | "plan" | "bypass"

export interface PermissionRequest {
  id: string
  session_id: string
  tool_call_id: string
  tool_name: string
  description: string
  action: string
  params: unknown
  path: string
}

export interface QuestionChoice {
  id: string
  label: string
  description?: string
}

export interface QuestionItem {
  id: string
  type: "yes_no" | "single_choice" | "multi_choice" | "free_text" | (string & {})
  label?: string
  question: string
  description?: string
  choices?: QuestionChoice[]
}

export interface QuestionRequest {
  id: string
  session_id: string
  tool_call_id: string
  questions: QuestionItem[]
  confirm_title?: string
  confirm_description?: string
}

export interface QuestionResponse {
  request_id: string
  selected_ids?: string[]
  fill_in_text?: string
  yes?: boolean
}

export interface Pending {
  permissions: PermissionRequest[]
  questions: QuestionRequest[]
}

export interface ModelInfo {
  id: string
  name: string
  context_window?: number
  can_reason: boolean
  reasoning_levels?: string[]
  default_reasoning_effort?: string
  supports_images: boolean
  cost_per_1m_in?: number
  cost_per_1m_out?: number
}

export interface ProviderInfo {
  id: string
  name: string
  models: ModelInfo[]
}

export interface SelectedModel {
  provider: string
  model: string
  reasoning_effort?: string
  think?: boolean
}

export interface Catalog {
  providers: ProviderInfo[] | null
  selected: Record<string, SelectedModel>
  active?: SelectedModel
}

export interface Settings {
  session_mode: string
  modes: { name: string; description: string }[]
  permission_mode: PermissionMode
  /** Whether sessions summarize themselves as the context window fills. */
  auto_compact: boolean
  /** Percent of the context window used before that happens; 0 for the built-in thresholds. */
  auto_compact_at: number
  mcp: { name: string; state: string; error?: string; tools?: number; prompts?: number; resources?: number }[]
  skills: { name: string; path: string; state: string; error?: string }[]
  lsp: { name: string; state: string; diagnostics?: number }[]
}

export interface DirectoryEntry {
  name: string
  path: string
  is_dir: boolean
  size?: number
  modified_at?: number
}

export interface DirectoryListing {
  path: string
  entries: DirectoryEntry[] | null
  truncated?: boolean
}

export interface FileContent {
  path: string
  content: string
  size: number
  modified_at: number
  sha256: string
  truncated?: boolean
  binary?: boolean
}

export interface HistoryFile {
  id: string
  session_id: string
  path: string
  content: string
  version: number
  created_at: number
  updated_at: number
}

export interface Job {
  id: string
  command: string
  description: string
  working_dir: string
  started_at: string
  done: boolean
  status: string
  exit_err?: string
}

export interface JobOutput {
  stdout: string
  stderr: string
  done: boolean
  error?: string
  truncated: boolean
}

export interface AgentHubEntry {
  session_id: string
  title: string
  started_at: string
  cost: number
  message_count: number
  busy: boolean
}

export interface Attachment {
  file_name: string
  mime_type: string
  content: string
}

export interface RunComplete {
  session_id: string
  run_id?: string
  message_id: string
  text?: string
  error?: string
  cancelled?: boolean
}

export interface SessionStatus {
  session_id: string
  busy: boolean
  queued: number
}

/** An event as the gateway relays it: a sequence number, a type, and the
 * server's payload ({type: "created"|"updated"|"deleted", payload: T} for
 * pubsub events). */
export interface HubEvent {
  seq: number
  type: string
  payload: any
}

export interface ApiErrorBody {
  message: string
}

export interface SlashArgument {
  id: string
  title: string
  description?: string
  required: boolean
}

/** An entry of the composer's "/" menu, as the gateway lists it. */
export interface SlashCommand {
  id: string
  name: string
  description?: string
  kind: "custom" | "workflow" | "skill"
  arguments?: SlashArgument[]
}
