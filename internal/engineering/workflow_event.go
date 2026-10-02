package engineering

// WorkflowChanged requests an authoritative refresh, never a speculative patch.
type WorkflowChanged struct {
	SessionID string `json:"session_id"`
	Revision  string `json:"revision,omitempty"`
}
