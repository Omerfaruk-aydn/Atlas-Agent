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
  /** Set by the server; a side chat carries "side-of:<session id>". */
  tags?: string[];
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
  mcp: { name: string; state: string; enabled?: boolean; error?: string; tools?: number; prompts?: number; resources?: number }[];
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
  /** The branch it follows, like origin/main; empty without one. */
  upstream?: string;
}

export interface HubEvent {
  seq: number;
  type: string;
  payload: unknown;
}

/** What the main process reports about a built-in browser tab. */
export interface BrowserState {
  id: string;
  url: string;
  title: string;
  favicon: string;
  loading: boolean;
  canGoBack: boolean;
  canGoForward: boolean;
  zoom: number;
  device: { id: string; name: string; width: number; height: number } | null;
  /** The page shows a login form. */
  login: boolean;
  /** The page shows an address form. */
  address: boolean;
}

export interface AddressFields {
  name: string;
  organization: string;
  street: string;
  street2: string;
  city: string;
  region: string;
  postal: string;
  country: string;
  phone: string;
  email: string;
}

export interface AddressRecord extends AddressFields {
  id: string;
  updated: number;
}

export interface BrowserSettings {
  searchEngine: "duckduckgo" | "google" | "bing";
  offerSave: boolean;
  addressAutofill: boolean;
}

export interface DownloadItem {
  id: number;
  name: string;
  host: string;
  state: "progressing" | "paused" | "completed" | "cancelled" | "interrupted";
  received: number;
  total: number;
  path: string;
}

export interface HistoryEntry {
  id: string;
  url: string;
  title: string;
  at: number;
}

export interface PageNote {
  id: string;
  text: string;
  quote: string;
  created: number;
  updated: number;
}

export interface ExtensionInfo {
  id: string;
  name: string;
  version: string;
  description: string;
  permissions: string[];
  hostPermissions: string[];
  enabled: boolean;
  hidden: boolean;
  source: "folder" | "store";
  path: string;
  icon: string;
  action: { title: string; popup: boolean } | null;
  sig?: string;
  missing?: boolean;
}

export interface ExtensionPreview {
  ok: boolean;
  error?: string;
  path?: string;
  name?: string;
  version?: string;
  description?: string;
  permissions?: string[];
  hostPermissions?: string[];
  sig?: string;
  icon?: string;
}

export interface VaultEntry {
  id: string;
  origin: string;
  username: string;
  created: number;
  updated: number;
}

export interface VaultOffer {
  id: string;
  tab: string;
  host: string;
  username: string;
  update: boolean;
  unavailable: boolean;
}

export interface DevicePreset {
  id: string;
  name: string;
  width: number;
  height: number;
  ratio: number;
  mobile: boolean;
}

declare global {
  interface Window {
    __atlasBoot?: { theme: "system" | "light" | "dark"; dark: boolean; motion: "system" | "full" | "reduce"; ui: Record<string, unknown> } | null;
    atlas?: {
      appInfo(): Promise<{ version: string; core: string; platform: string; packaged: boolean; electron: string }>;
      rendererReady?(): void;
      setTheme(theme: "system" | "light" | "dark"): Promise<void>;
      onTheme(cb: (scheme: "light" | "dark") => void): () => void;
      setMotion(motion: "system" | "full" | "reduce"): Promise<void>;
      setUI(key: string, value: unknown): Promise<void>;
      pickFolder(): Promise<string>;
      openProject(project: string): Promise<boolean>;
      openExternal(url: string): Promise<boolean>;
      /** Asks where to save, then writes the text. Resolves to the path, or "" when cancelled. */
      saveText(name: string, text: string): Promise<string>;
      /**
       * Saves an export of a design: PNG bytes (base64), SVG text, or a PDF
       * printed from SVG text at width x height. Asks where to save. Resolves
       * to the path, or "" when cancelled.
       */
      designExport?(name: string, format: "png" | "svg" | "pdf", payload: { data: string; width?: number; height?: number }): Promise<string>;
      reveal(target: string): Promise<void>;
      gitStatus(project: string): Promise<GitStatus | null>;
      favicon(host: string): Promise<string>;
      notify(note: { title: string; body: string; session: string }): Promise<void>;
      setBadge(count: number, image: string): Promise<void>;
      onOpenSession(cb: (session: string) => void): () => void;
      setPanel(key: string, panel: unknown): Promise<boolean>;
      setRecent(list: { url: string; host: string; title: string }[]): Promise<{ url: string; host: string; title: string }[]>;
      data: {
        history: { search(query: string, limit?: number): Promise<HistoryEntry[]>; remove(id: string): Promise<boolean>; clear(since?: number): Promise<number> };
        notes: {
          list(url: string): Promise<PageNote[]>;
          add(url: string, text: string, quote: string): Promise<PageNote | null>;
          update(url: string, id: string, text: string): Promise<PageNote | null>;
          remove(url: string, id: string): Promise<boolean>;
        };
        downloads: { list(): Promise<DownloadItem[]>; action(id: number, action: "cancel" | "pause" | "resume" | "open" | "show" | "remove"): Promise<boolean>; onChange(cb: (item: DownloadItem) => void): () => void };
        extensions: {
          list(): Promise<{ items: ExtensionInfo[]; store: boolean; storeError: string }>;
          pick(): Promise<ExtensionPreview | null>;
          inspect(dir: string): Promise<ExtensionPreview>;
          load(dir: string, sig: string): Promise<{ ok: boolean; id?: string; error?: string }>;
          disable(id: string): Promise<{ ok: boolean; error?: string }>;
          remove(id: string): Promise<boolean>;
          hide(id: string, hidden: boolean): Promise<void>;
          popup(id: string, anchor: { x: number; y: number }): Promise<boolean>;
          /** An extension button with no popup: fires the extension's click action in the page in front. */
          click(id: string, tab: string): Promise<boolean>;
          onChange(cb: () => void): () => void;
        };
        vault: {
          status(): Promise<{ available: boolean; count: number }>;
          list(query?: string): Promise<VaultEntry[]>;
          forTab(tab: string): Promise<VaultEntry[]>;
          fill(tab: string, id: string): Promise<{ ok: boolean; error?: string }>;
          reveal(id: string): Promise<{ ok: boolean; password?: string; error?: string }>;
          /** Copies in the main process after a native confirmation; the secret never reaches the page. */
          copy(id: string): Promise<{ ok: boolean; error?: string }>;
          add(origin: string, username: string, password: string): Promise<{ ok: boolean; error?: string; code?: string }>;
          update(id: string, change: { username?: string; password?: string }): Promise<{ ok: boolean; error?: string }>;
          remove(id: string): Promise<boolean>;
          answer(offer: string, answer: "save" | "never" | "dismiss"): Promise<{ ok: boolean; error?: string }>;
          onOffer(cb: (offer: VaultOffer) => void): () => void;
          neverList(): Promise<string[]>;
          neverRemove(origin: string): Promise<string[]>;
        };
        addresses: {
          status(): Promise<{ available: boolean; count: number }>;
          list(): Promise<AddressRecord[]>;
          labels(): Promise<{ id: string; label: string }[]>;
          save(id: string, record: AddressFields): Promise<{ ok: boolean; id?: string; error?: string; code?: string }>;
          remove(id: string): Promise<boolean>;
          fill(tab: string, id: string): Promise<{ ok: boolean; error?: string }>;
        };
        settings: {
          get(): Promise<BrowserSettings>;
          set<K extends keyof BrowserSettings>(key: K, value: BrowserSettings[K]): Promise<BrowserSettings | null>;
          onChange(cb: (settings: BrowserSettings) => void): () => void;
        };
        imports: {
          profiles(): Promise<{ id: string; browser: "firefox" | "chrome" | "edge"; name: string }[]>;
          cookies(profile: string): Promise<{ ok: boolean; error?: string; imported?: number; refused?: number; appBound?: number; failed?: number }>;
          passwords(): Promise<{ ok: boolean; unavailable?: boolean; error?: string; format?: string; saved?: number; replaced?: number; refused?: number; skipped?: number; file?: string } | null>;
        };
      };
      browser: {
        setChords(list: string[]): Promise<void>;
        select(tab: string): Promise<void>;
        find(tab: string, text: string, options?: { forward?: boolean; findNext?: boolean }): Promise<void>;
        stopFind(tab: string): Promise<void>;
        print(tab: string): Promise<{ ok: boolean; cancelled?: boolean; error?: "none" | "failed"; reason?: string }>;
        zoom(tab: string, action: "in" | "out" | "reset"): Promise<number>;
        devices(): Promise<DevicePreset[]>;
        device(tab: string, device: string | { width: number; height: number; ratio?: number } | null): Promise<boolean>;
        screenshotData(tab: string): Promise<{ name: string; mime: string; content: string } | null>;
        screenshotSave(tab: string): Promise<string>;
        navHistory(tab: string): Promise<{ entries: { url: string; title: string }[]; index: number }>;
        goTo(tab: string, index: number): Promise<void>;
        selection(tab: string): Promise<string>;
        clear(options: { history?: boolean; cookies?: boolean; cache?: boolean; siteData?: boolean; passwords?: boolean; since?: number }): Promise<{ history: number }>;
        onFind(cb: (found: { id: string; active: number; matches: number; final: boolean }) => void): () => void;
        onFindOpen(cb: (found: { id: string }) => void): () => void;
        /** Announces that a <webview> for this tab is about to attach (main/webview-gate.cjs). */
        expect(tab: string): Promise<boolean>;
        /** Says which guest the attached <webview> became, and the first address to load into it. */
        attach(tab: string, contentsId: number, url: string): Promise<BrowserState | null>;
        navigate(tab: string, url: string): Promise<boolean>;
        back(tab: string): Promise<void>;
        forward(tab: string): Promise<void>;
        reload(tab: string): Promise<void>;
        stop(tab: string): Promise<void>;
        close(tab: string): Promise<void>;
        onState(cb: (state: BrowserState) => void): () => void;
        onPopup(cb: (popup: { from: string; url: string; id?: string }) => void): () => void;
        onBlur(cb: (e: { id: string }) => void): () => void;
        onKey(cb: (key: { key: string; code: string; ctrl: boolean; alt: boolean; shift: boolean }) => void): () => void;
      };
      terminal: {
        create(opts: { id: string; cwd: string; cols: number; rows: number }): Promise<{ ok: true; shell: string } | { ok: false; error: string }>;
        write(tab: string, data: string): Promise<void>;
        resize(tab: string, cols: number, rows: number): Promise<void>;
        kill(tab: string): Promise<void>;
        onData(cb: (chunk: { id: string; data: string }) => void): () => void;
        onExit(cb: (exit: { id: string; code: number }) => void): () => void;
      };
    };
  }
}

/** How serious a review finding is, worst first. */
export type ReviewSeverity = "critical" | "high" | "medium" | "low" | "info";

export interface ReviewFinding {
  severity: ReviewSeverity;
  title: string;
  file?: string;
  /** 0 or absent: the finding is about the file as a whole. */
  line?: number;
  explanation?: string;
  suggestion?: string;
}

/** A second look at the changes a turn made, done by a read-only agent. */
export interface Review {
  id: string;
  session_id: string;
  state: "running" | "done" | "failed" | "canceled";
  summary?: string;
  findings: ReviewFinding[];
  /** False when the reviewer's answer could not be read as findings; `summary` then holds its text. */
  structured: boolean;
  model?: string;
  error?: string;
  started_at: number;
  finished_at?: number;
}
