// Hand-written from internal/proto and internal/webui. The renderer has no
// generated types and shares nothing with web/.

export type Role = "user" | "assistant" | "system" | "tool";

export interface ToolCall {
  id: string;
  name: string;
  input: string;
  finished?: boolean;
}

export interface ToolResult {
  tool_call_id: string;
  name: string;
  content: string;
  metadata: string;
  is_error: boolean;
}

export interface Finish {
  reason: string;
  time: number;
  message?: string;
  details?: string;
}

export type Part =
  | { type: "text"; data: { text: string } }
  | { type: "reasoning"; data: { thinking: string; started_at?: number; finished_at?: number } }
  | { type: "tool_call"; data: ToolCall }
  | { type: "tool_result"; data: ToolResult }
  | { type: "finish"; data: Finish }
  | { type: "shell_command"; data: { command: string; output: string; exit_code: number } }
  | { type: "image_url"; data: { url: string } }
  | { type: "binary"; data: { Path?: string; MIMEType?: string; Data?: string } };

export interface Message {
  id: string;
  role: Role;
  session_id: string;
  parts: Part[];
  model: string;
  provider: string;
  created_at: number;
  updated_at: number;
  is_summary_message?: boolean;
}

export interface Session {
  id: string;
  parent_session_id: string;
  title: string;
  message_count: number;
  prompt_tokens: number;
  completion_tokens: number;
  cost: number;
  created_at: number;
  updated_at: number;
  is_busy?: boolean;
}

export interface WorkspaceView {
  id: string;
  path: string;
  name: string;
  connected: boolean;
  server_version?: string;
  agent_ready: boolean;
  configured: boolean;
  permission_mode?: string;
}

export interface Bootstrap {
  csrf: string;
  instance: string;
  version: string;
  server: { address: string; reachable: boolean; version: string; platform: string };
  workspaces: WorkspaceView[];
}

export interface Project {
  path: string;
  name: string;
  last_used?: number;
}

export interface PermissionRequest {
  id: string;
  session_id: string;
  tool_call_id: string;
  tool_name: string;
  description: string;
  action: string;
  params: unknown;
  path: string;
}

export interface QuestionChoice {
  id: string;
  label: string;
  description?: string;
}

export interface QuestionItem {
  id: string;
  type: string;
  label?: string;
  question: string;
  description?: string;
  choices?: QuestionChoice[];
}

export interface QuestionRequest {
  id: string;
  session_id: string;
  tool_call_id: string;
  questions: QuestionItem[];
  confirm_title?: string;
  confirm_description?: string;
}

export interface QuestionResponse {
  request_id: string;
  selected_ids?: string[];
  fill_in_text?: string;
  yes?: boolean;
}

export interface ModelView {
  id: string;
  name: string;
  context_window?: number;
  can_reason: boolean;
  reasoning_levels?: string[];
  default_reasoning_effort?: string;
  supports_images: boolean;
  cost_per_1m_in?: number;
  cost_per_1m_out?: number;
}

export interface ProviderView {
  id: string;
  name: string;
  models: ModelView[];
}

export interface SelectedModel {
  provider: string;
  model: string;
  reasoning_effort?: string;
  think?: boolean;
}

export interface Catalog {
  providers: ProviderView[] | null;
  selected: Record<string, SelectedModel>;
  active?: SelectedModel;
}

export type PermissionMode = "manual" | "auto_accept_edits" | "plan" | "bypass";

export interface Settings {
  session_mode: string;
  modes: { name: string; description: string }[];
  permission_mode: PermissionMode;
  auto_compact: boolean;
  auto_compact_at: number;
  mcp: { name: string; state: string; error?: string; tools?: number; prompts?: number; resources?: number }[];
  skills: { name: string; path: string; state: string; error?: string }[];
  lsp: { name: string; state: string; diagnostics?: number }[];
}

export interface DirectoryEntry {
  name: string;
  path: string;
  is_dir: boolean;
  size?: number;
  modified_at?: number;
}

export interface DirectoryListing {
  path: string;
  entries: DirectoryEntry[] | null;
  truncated?: boolean;
}

export interface FileContent {
  path: string;
  content: string;
  size: number;
  modified_at: number;
  sha256: string;
  truncated?: boolean;
  binary?: boolean;
}

export interface Job {
  id: string;
  command: string;
  description: string;
  working_dir: string;
  started_at: string;
  done: boolean;
  status: string;
  exit_err?: string;
}

export interface JobOutput {
  stdout: string;
  stderr: string;
  done: boolean;
  error?: string;
  truncated: boolean;
}

export interface SlashCommand {
  id: string;
  name: string;
  description?: string;
  kind: "custom" | "workflow" | "skill";
  arguments?: { id: string; title: string; description?: string; required: boolean }[];
}

export interface ProviderChoice {
  id: string;
  name: string;
  models: number;
  configured: boolean;
  /** Signs in through a browser instead of with a key. */
  account?: boolean;
}

export interface LoginView {
  state: "waiting" | "done" | "failed";
  url?: string;
  user_code?: string;
  message?: string;
  error?: string;
}

export interface Attachment {
  file_name: string;
  mime_type: string;
  content: string;
}

export interface RewindPreview {
  files_to_write: number;
  files_to_delete: number;
}

export interface RewindResult {
  session: Session;
  files_written: number;
  files_deleted: number;
}

export interface GitStatus {
  branch: string;
  dirty: number;
  ahead: number;
  behind: number;
}

export interface HubEvent {
  seq: number;
  type: string;
  payload: unknown;
}

declare global {
  interface Window {
    __atlasBoot?: { theme: "system" | "light" | "dark"; dark: boolean; motion: "system" | "full" | "reduce"; ui: Record<string, unknown> } | null;
    atlas?: {
      appInfo(): Promise<{ version: string; core: string; platform: string; packaged: boolean; electron: string }>;
      setTheme(theme: "system" | "light" | "dark"): Promise<void>;
      onTheme(cb: (scheme: "light" | "dark") => void): () => void;
      setMotion(motion: "system" | "full" | "reduce"): Promise<void>;
      setUI(key: string, value: unknown): Promise<void>;
      pickFolder(): Promise<string>;
      openProject(project: string): Promise<boolean>;
      openExternal(url: string): Promise<boolean>;
      reveal(target: string): Promise<void>;
      gitStatus(project: string): Promise<GitStatus | null>;
      favicon(host: string): Promise<string>;
      notify(note: { title: string; body: string; session: string }): Promise<void>;
      setBadge(count: number, image: string): Promise<void>;
      onOpenSession(cb: (session: string) => void): () => void;
    };
  }
}
