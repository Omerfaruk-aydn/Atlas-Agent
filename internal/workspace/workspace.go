// Package workspace defines the Workspace interface used by all
// frontends (TUI, CLI) to interact with a running workspace. Two
// implementations exist: one wrapping a local app.App instance and one
// wrapping the HTTP client SDK.
package workspace

import (
	"context"
	"errors"
	"time"

	mcptools "github.com/Omerfaruk-aydn/Atlas-Agent/internal/agent/tools/mcp"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/commands"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-models/pkg/catwalk"
	tea "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ui/v2"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/history"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/lsp"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/message"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/oauth"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/permission"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/proto"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/question"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/session"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/session/rewind"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/shell"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/skills"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/subagents"
)

// Reasons the coder agent may be unavailable, returned by
// Workspace.AgentReadyErr so callers can tell a genuinely
// uninitialized agent apart from a lost server connection.
var (
	// ErrAgentNotInitialized means the workspace exists but its coder
	// agent has not been configured/initialized (e.g. no model set).
	ErrAgentNotInitialized = errors.New("coder agent is not initialized")
	// ErrServerUnreachable means the client could not reach the server
	// to determine the agent's status (server down, or the workspace was
	// torn down out from under the client).
	ErrServerUnreachable = errors.New("lost connection to the Atlas-Agent server")
	// ErrWorkspaceGone means the server is reachable but no longer knows
	// this client's workspace: it was torn down, or the server was
	// replaced underneath the client. The subscription loop re-registers
	// the workspace in the background when it sees this.
	ErrWorkspaceGone = errors.New("the server reset this workspace; reconnecting")
	// ErrStreamClosed means an established event stream ended.
	// Resubscribing usually succeeds immediately, but events published in
	// the meantime are lost for good, so the client treats it as a
	// degraded link that requires a resync.
	ErrStreamClosed = errors.New("the event stream closed; reconnecting")
)

// ConnectionState describes the health of the client-server link as
// reported by the [ClientWorkspace] subscription loop.
type ConnectionState int

const (
	// ConnectionDegraded means the event stream is down (or the workspace
	// was lost server-side) and the client is retrying or re-registering
	// in the background.
	ConnectionDegraded ConnectionState = iota
	// ConnectionRecovered means the event stream was re-established,
	// possibly against a re-created workspace.
	ConnectionRecovered
)

// ConnectionEvent is delivered to the TUI as a tea.Msg on degraded and
// recovered transitions of the client-server link. Local (in-process)
// workspaces never emit it.
type ConnectionEvent struct {
	State ConnectionState
	// Err is the most recent failure, set when State is
	// ConnectionDegraded.
	Err error
	// Stuck marks a degraded connection that has resisted repeated
	// recovery attempts. The loop keeps retrying regardless; the UI
	// should escalate from a transient notice to a persistent error.
	Stuck bool
}

// LSPClientInfo holds information about an LSP client's state. This is
// the frontend-facing type; implementations translate from the
// underlying app or proto representation.
type LSPClientInfo struct {
	Name            string
	State           lsp.ServerState
	Error           error
	DiagnosticCount int
	ConnectedAt     time.Time
}

// LSPEventType represents the type of LSP event.
type LSPEventType string

const (
	LSPEventStateChanged       LSPEventType = "state_changed"
	LSPEventDiagnosticsChanged LSPEventType = "diagnostics_changed"
)

// JobsEvent is a refresh signal for the background jobs sidebar, forwarded
// from the server in client/server mode. Its payload carries no state on
// purpose — consumers always re-fetch via BackgroundJobsList/
// SubAgentRunsList, mirroring how LSPEvent is used purely to trigger a
// refresh.
type JobsEvent struct{}

// LSPEvent represents an LSP event forwarded to the TUI.
type LSPEvent struct {
	Type            LSPEventType
	Name            string
	State           lsp.ServerState
	Error           error
	DiagnosticCount int
}

// AgentModel holds the model information exposed to the UI.
type AgentModel struct {
	CatwalkCfg catwalk.Model
	ModelCfg   config.SelectedModel
}

// SubAgentRunInfo describes one in-flight sub-agent run for the background
// jobs sidebar/dialog.
type SubAgentRunInfo struct {
	SessionID string
	Title     string
	StartedAt time.Time
}

// AgentHubEntry describes one sub-agent session spawned under a top-level
// session, for the Agent Hub dialog -- unlike SubAgentRunInfo this covers
// every sub-agent that has run in the session, finished or not, so the
// Hub is a history, not just a live-jobs list.
type AgentHubEntry struct {
	SessionID string
	Title     string
	StartedAt time.Time
	Cost      float64
	// MessageCount is the sub-agent session's own message count, a rough
	// proxy for how much work it did.
	MessageCount int64
	// Busy reports whether this sub-agent is still actively running.
	Busy bool
}

// Workspace is the main abstraction consumed by the TUI and CLI. It
// groups every operation a frontend needs to perform against a running
// workspace, regardless of whether the workspace is in-process or
// remote.
type Workspace interface {
	// Sessions
	CreateSession(ctx context.Context, title string) (session.Session, error)
	GetSession(ctx context.Context, sessionID string) (session.Session, error)
	ListSessions(ctx context.Context) ([]session.Session, error)
	// SearchSessions returns sessions with at least one matching message,
	// best-matching session first. Unlike ListSessions this searches message
	// content, not just titles.
	SearchSessions(ctx context.Context, query string) ([]session.Session, error)
	SaveSession(ctx context.Context, sess session.Session) (session.Session, error)
	DeleteSession(ctx context.Context, sessionID string) error
	// RewindPreview reports how many files RewindTo would write and delete
	// for the given checkpoint, without applying anything. Used to show an
	// accurate confirmation before the (hard to reverse) RewindTo call.
	RewindPreview(ctx context.Context, sourceSessionID, upToMessageID string) (filesToWrite, filesToDelete int, err error)
	// RewindTo forks sourceSessionID at upToMessageID (inclusive): the new
	// child session gets a verbatim copy of the messages up to that point,
	// and the working directory's files are restored to their content as
	// of that message. sourceSessionID is never modified or deleted.
	RewindTo(ctx context.Context, sourceSessionID, upToMessageID string) (rewind.Result, error)
	CreateAgentToolSessionID(messageID, toolCallID string) string
	ParseAgentToolSessionID(sessionID string) (messageID string, toolCallID string, ok bool)
	// SetCurrentSession reports the session this client is currently
	// viewing. Empty sessionID clears the entry (e.g. landing screen).
	// In single-client local mode this is a no-op. In client/server
	// mode it informs the server's per-client presence map so other
	// observers can compute attached-client counts per session.
	SetCurrentSession(ctx context.Context, sessionID string) error

	// Messages
	ListMessages(ctx context.Context, sessionID string) ([]message.Message, error)
	ListUserMessages(ctx context.Context, sessionID string) ([]message.Message, error)
	ListAllUserMessages(ctx context.Context) ([]message.Message, error)

	// Agent
	AgentRun(ctx context.Context, sessionID, prompt string, attachments ...message.Attachment) error
	AgentRunShellCommand(ctx context.Context, sessionID, command string, termWidth int, onProgress func(string), isFirstMessage bool) (proto.ShellCommandResponse, error)
	AgentCancel(sessionID string)
	AgentIsBusy() bool
	AgentIsSessionBusy(sessionID string) bool
	AgentModel() AgentModel
	AgentIsReady() bool
	// AgentReadyErr reports nil when the coder agent is ready to accept
	// work, or a descriptive error otherwise: ErrAgentNotInitialized
	// when the agent simply isn't set up, or ErrServerUnreachable
	// (wrapped) when the client could not reach the server to find out.
	// It lets the UI show an actionable message instead of collapsing
	// both cases into "agent offline".
	AgentReadyErr() error
	AgentQueuedPrompts(sessionID string) int
	AgentQueuedPromptsList(sessionID string) []string
	AgentClearQueue(sessionID string)
	AgentSummarize(ctx context.Context, sessionID string) error
	// AgentSetGoal puts a session into an autonomous run towards goal,
	// where it keeps taking turns of its own until the goal is reached
	// or its budget runs out. An empty goal ends the run.
	AgentSetGoal(ctx context.Context, sessionID, goal string) error
	UpdateAgentModel(ctx context.Context) error
	InitCoderAgent(ctx context.Context) error
	InitCoderAgentNonInteractive(ctx context.Context) error
	GetDefaultSmallModel(providerID string) config.SelectedModel

	// Permissions
	//
	// PermissionGrant, PermissionGrantPersistent, and PermissionDeny
	// return true if the call resolved the pending request and false if
	// it had already been resolved by another subscriber (or is no
	// longer pending). A false return is not an error; the modal can
	// still close locally because the resolution will arrive via the
	// PermissionNotification event stream regardless of which client
	// won the race.
	PermissionGrant(perm permission.PermissionRequest) bool
	PermissionGrantPersistent(perm permission.PermissionRequest) bool
	PermissionDeny(perm permission.PermissionRequest) bool
	PermissionSkipRequests() bool
	PermissionSetSkipRequests(skip bool)
	PermissionMode() permission.PermissionMode
	PermissionSetMode(mode permission.PermissionMode)

	// Questions
	//
	// QuestionAnswer resolves the pending question with responses.
	QuestionAnswer(responses []question.Answer) bool

	// QuestionCancel cancels the pending question.
	QuestionCancel() bool

	// FileTracker
	FileTrackerRecordRead(ctx context.Context, sessionID, path string)
	FileTrackerLastReadTime(ctx context.Context, sessionID, path string) time.Time
	FileTrackerListReadFiles(ctx context.Context, sessionID string) ([]string, error)

	// History
	ListSessionHistory(ctx context.Context, sessionID string) ([]history.File, error)

	// LSP
	LSPStart(ctx context.Context, path string)
	LSPStopAll(ctx context.Context)
	LSPGetStates() map[string]LSPClientInfo
	LSPGetDiagnosticCounts(name string) lsp.DiagnosticCounts

	// Background jobs
	BackgroundJobsList() []shell.BackgroundShellInfo
	BackgroundJobKill(id string) error

	// SubAgentRunsList returns the sub-agent runs currently in flight under
	// sessionID (i.e. spawned by the "agent"/"agentic_fetch" tools during
	// that session's current turn).
	SubAgentRunsList(ctx context.Context, sessionID string) []SubAgentRunInfo

	// AgentHubEntries returns every sub-agent session spawned under
	// sessionID, finished or still running -- the full history behind the
	// Agent Hub dialog (Alt+A), as opposed to SubAgentRunsList's
	// currently-in-flight-only view.
	AgentHubEntries(ctx context.Context, sessionID string) []AgentHubEntry

	// Config (read-only data)
	Config() *config.Config
	WorkingDir() string
	Resolver() config.VariableResolver

	// Config mutations (proxied to server in client mode)
	UpdatePreferredModel(scope config.Scope, modelType config.SelectedModelType, model config.SelectedModel) error
	SetCompactMode(scope config.Scope, enabled bool) error
	SetProviderAPIKey(scope config.Scope, providerID string, apiKey any) error
	SetConfigField(scope config.Scope, key string, value any) error
	RemoveConfigField(scope config.Scope, key string) error
	ImportCopilot() (*oauth.Token, bool)
	RefreshOAuthToken(ctx context.Context, scope config.Scope, providerID string) error

	// Subagents: named, model-routable agent definitions (see
	// internal/subagents). Unlike model roles and fallbacks, these are
	// files rather than config fields, so they need their own
	// endpoints rather than going through SetConfigField.
	ListSubagents(ctx context.Context) ([]subagents.Subagent, error)
	// SaveSubagent creates or updates sub, returning the path it was
	// written to. userScope is ignored when a subagent by this name
	// already exists -- see workspace.saveSubagent.
	SaveSubagent(ctx context.Context, sub subagents.Subagent, userScope bool) (string, error)
	DeleteSubagent(ctx context.Context, name string) error

	// Project lifecycle
	ProjectNeedsInitialization() (bool, error)
	MarkProjectInitialized() error
	InitializePrompt() (string, error)
	ListSkills(ctx context.Context) ([]skills.CatalogEntry, error)
	ReadSkill(ctx context.Context, skillID string) ([]byte, skills.SkillReadResult, error)

	// MCP operations (server-side in client mode)
	MCPGetStates() map[string]mcptools.ClientInfo
	MCPRefreshPrompts(ctx context.Context, name string)
	MCPRefreshResources(ctx context.Context, name string)
	RefreshMCPTools(ctx context.Context, name string)
	ReadMCPResource(ctx context.Context, name, uri string) ([]MCPResourceContents, error)
	ListMCPPrompts(ctx context.Context) ([]commands.MCPPrompt, error)
	GetMCPPrompt(clientID, promptID string, args map[string]string) (string, error)
	EnableDockerMCP(ctx context.Context) error
	DisableDockerMCP() error
	MCPAuthenticate(ctx context.Context, name string) error
	MCPPendingAuth() []mcptools.PendingAuthServer
	MCPAuthURL(name string) string

	// Events
	Subscribe(program *tea.Program)
	Shutdown()
}

// MCPResourceContents holds the contents of an MCP resource.
type MCPResourceContents struct {
	URI      string `json:"uri"`
	MIMEType string `json:"mime_type,omitempty"`
	Text     string `json:"text,omitempty"`
	Blob     []byte `json:"blob,omitempty"`
}
