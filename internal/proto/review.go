package proto

// ReviewRequest asks for a second look at the changes a turn made. The
// review is done by a read-only agent that starts with no conversation: it
// is given the task and the files, and reads the code itself.
type ReviewRequest struct {
	// Task is the request the changes answer. Optional.
	Task string `json:"task,omitempty"`
	// Paths are the files that changed.
	Paths []string `json:"paths"`
}

// Review states.
const (
	ReviewRunning  = "running"
	ReviewDone     = "done"
	ReviewFailed   = "failed"
	ReviewCanceled = "canceled"
)

// ReviewFinding is one defect a review found. Line 0 means the finding is
// about the file as a whole.
type ReviewFinding struct {
	// Severity is one of critical, high, medium, low, info.
	Severity    string `json:"severity"`
	Title       string `json:"title"`
	File        string `json:"file,omitempty"`
	Line        int    `json:"line,omitempty"`
	Explanation string `json:"explanation,omitempty"`
	Suggestion  string `json:"suggestion,omitempty"`
}

// Review is a review and, once it has finished, its outcome. Findings are
// ordered worst first.
type Review struct {
	ID        string `json:"id"`
	SessionID string `json:"session_id"`
	// State is running, done, failed or canceled.
	State    string          `json:"state"`
	Summary  string          `json:"summary,omitempty"`
	Findings []ReviewFinding `json:"findings"`
	// Structured is false when the reviewer's answer could not be read as
	// findings; Summary then carries its text as written.
	Structured bool   `json:"structured"`
	Model      string `json:"model,omitempty"`
	Error      string `json:"error,omitempty"`
	StartedAt  int64  `json:"started_at"`
	FinishedAt int64  `json:"finished_at,omitempty"`
}
