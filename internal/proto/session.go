package proto

// Session represents a session in the proto layer.
//
// IsBusy is computed on read (it is not persisted with the session) and
// reflects whether an agent run is currently in flight for this session.
// It is populated by REST handlers in internal/server/proto.go from the
// workspace's AgentCoordinator. The Session SSE event path does not set
// it, since SSE consumers can compute presence from other agent signals.
//
// AttachedClients counts the number of clients currently viewing this
// session — i.e. entries in the workspace's clients map whose
// currentSessionID equals this session's ID and which have at least one
// live SSE stream. Hold-only clients (streams == 0) do not contribute.
// Like IsBusy, it is computed on read by REST handlers.
type Session struct {
	ID               string  `json:"id"`
	ParentSessionID  string  `json:"parent_session_id"`
	Title            string  `json:"title"`
	MessageCount     int64   `json:"message_count"`
	PromptTokens     int64   `json:"prompt_tokens"`
	CompletionTokens int64   `json:"completion_tokens"`
	SummaryMessageID string  `json:"summary_message_id"`
	Cost             float64 `json:"cost"`
	Todos            []Todo  `json:"todos,omitempty"`
	CreatedAt        int64   `json:"created_at"`
	UpdatedAt        int64   `json:"updated_at"`
	IsBusy           bool    `json:"is_busy"`
	AttachedClients  int     `json:"attached_clients"`
}

// RewindRequest is the body of a rewind request: fork the session at
// UpToMessageID (inclusive).
type RewindRequest struct {
	UpToMessageID string `json:"up_to_message_id"`
}

// RewindResult is the response to a rewind request.
type RewindResult struct {
	Session      Session `json:"session"`
	FilesWritten int     `json:"files_written"`
	FilesDeleted int     `json:"files_deleted"`
}

// RewindPreview is the response to a rewind preview request: how many
// files would be written/deleted, without anything having been applied.
type RewindPreview struct {
	FilesToWrite  int `json:"files_to_write"`
	FilesToDelete int `json:"files_to_delete"`
}

// Todo represents a single todo entry on a session in the proto layer.
type Todo struct {
	ID                 string         `json:"id,omitempty"`
	DependsOn          []string       `json:"depends_on,omitempty"`
	Agent              string         `json:"agent,omitempty"`
	OwnedPaths         []string       `json:"owned_paths,omitempty"`
	Content            string         `json:"content"`
	Status             string         `json:"status"`
	ActiveForm         string         `json:"active_form"`
	AcceptanceCriteria []string       `json:"acceptance_criteria,omitempty"`
	Verification       string         `json:"verification,omitempty"`
	Evidence           []TodoEvidence `json:"evidence,omitempty"`
}

// TodoEvidence carries reported verification evidence across client boundaries.
type TodoEvidence struct {
	Kind   string `json:"kind"`
	Detail string `json:"detail"`
}
