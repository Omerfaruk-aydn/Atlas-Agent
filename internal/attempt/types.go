// Package attempt runs several independent attempts at one task, each in
// its own disposable git worktree, scores them with deterministic checks
// plus an independent judge model, and applies only the winner when the
// caller approves.
//
// The package never touches the user's working tree while attempts run.
// Every attempt's result is captured as a patch relative to a snapshot of
// the tree at start, and the worktrees are removed when the run ends, so
// applying a winner later is a plain "git apply" of a stored patch.
package attempt

import (
	"context"
	"time"
)

// Attempt status values.
const (
	StatusCompleted   = "completed"
	StatusNoChanges   = "no_changes"
	StatusAgentError  = "agent_error"
	StatusTimeout     = "timeout"
	StatusSetupFailed = "setup_failed"
	StatusCanceled    = "canceled"
	StatusInfra       = "infrastructure_error"
)

// Confidence values for a report's winner.
const (
	ConfidenceNone   = "none"
	ConfidenceLow    = "low"
	ConfidenceMedium = "medium"
	ConfidenceHigh   = "high"
)

// AgentJob is one headless agent invocation.
type AgentJob struct {
	// Dir is the working directory the agent runs in.
	Dir string
	// StateDir is a private data directory kept outside Dir so the agent's
	// own bookkeeping never appears in the attempt's patch.
	StateDir string
	// Model is a "model" or "provider/model" reference; empty means the
	// configured default.
	Model  string
	Prompt string
	// Yolo auto-approves the agent's tool permissions.
	Yolo bool
}

// AgentResult is what an agent invocation reports back.
type AgentResult struct {
	Response         string
	SessionID        string
	PromptTokens     int64
	CompletionTokens int64
	Cost             float64
	ToolCalls        int
	ToolErrors       int
	FinishReason     string
	FinishError      string
	Provider         string
	Model            string
}

// AgentFunc runs one agent invocation to completion.
type AgentFunc func(ctx context.Context, job AgentJob) (AgentResult, error)

// CheckResult is the outcome of one verification or lint command.
type CheckResult struct {
	Command    string `json:"command"`
	Passed     bool   `json:"passed"`
	ExitCode   int    `json:"exit_code"`
	TimedOut   bool   `json:"timed_out,omitempty"`
	DurationMS int64  `json:"duration_ms"`
	// Output is the tail of the combined output.
	Output string `json:"output,omitempty"`
}

// CheckFunc runs one shell command in dir.
type CheckFunc func(ctx context.Context, dir, command string, timeout time.Duration) CheckResult

// FileStat is one changed file in a patch.
type FileStat struct {
	Path      string `json:"path"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
	Binary    bool   `json:"binary,omitempty"`
}

// Verdict is the judge's opinion of one attempt.
type Verdict struct {
	// Score is 0-10.
	Score     float64  `json:"score"`
	Rationale string   `json:"rationale,omitempty"`
	Concerns  []string `json:"concerns,omitempty"`
}

// Attempt is the full record of one attempt.
type Attempt struct {
	Index    int    `json:"index"`
	Label    string `json:"label"`
	Model    string `json:"model,omitempty"`
	Strategy string `json:"strategy,omitempty"`
	Status   string `json:"status"`
	Error    string `json:"error,omitempty"`
	Response string `json:"response,omitempty"`

	SessionID        string  `json:"session_id,omitempty"`
	PromptTokens     int64   `json:"prompt_tokens,omitempty"`
	CompletionTokens int64   `json:"completion_tokens,omitempty"`
	Cost             float64 `json:"cost,omitempty"`
	ToolCalls        int     `json:"tool_calls,omitempty"`
	DurationMS       int64   `json:"duration_ms"`

	Files      []FileStat `json:"files,omitempty"`
	Additions  int        `json:"additions"`
	Deletions  int        `json:"deletions"`
	PatchBytes int        `json:"patch_bytes"`
	PatchHash  string     `json:"patch_hash,omitempty"`
	PatchFile  string     `json:"patch_file,omitempty"`
	// DuplicateOf is the index of an earlier attempt with an identical patch.
	DuplicateOf int `json:"duplicate_of,omitempty"`
	// Applies reports whether the patch applies cleanly to the tree the run
	// started from.
	Applies *bool `json:"applies,omitempty"`

	Checks []CheckResult `json:"checks,omitempty"`
	Lint   []CheckResult `json:"lint,omitempty"`

	// VerifyPassed is true when every verify command passed (vacuously true
	// when none are configured).
	VerifyPassed  bool     `json:"verify_passed"`
	Eligible      bool     `json:"eligible"`
	Deterministic float64  `json:"deterministic_score"`
	Notes         []string `json:"notes,omitempty"`
	Judge         *Verdict `json:"judge,omitempty"`
	Final         float64  `json:"final_score"`
	// Rank is 1 for the best eligible attempt; 0 means not ranked.
	Rank int `json:"rank"`
}

// JudgeInfo records the judging stage of a run.
type JudgeInfo struct {
	Model   string  `json:"model,omitempty"`
	Ran     bool    `json:"ran"`
	Skipped string  `json:"skipped,omitempty"`
	Error   string  `json:"error,omitempty"`
	Winner  string  `json:"winner,omitempty"`
	Summary string  `json:"summary,omitempty"`
	Cost    float64 `json:"cost,omitempty"`
}

// Report is the persisted result of one run.
type Report struct {
	ID           string    `json:"id"`
	Task         string    `json:"task"`
	CreatedAt    time.Time `json:"created_at"`
	Root         string    `json:"root"`
	BaseHead     string    `json:"base_head"`
	VerifySource string    `json:"verify_source"`
	Verify       []string  `json:"verify,omitempty"`
	Lint         []string  `json:"lint,omitempty"`
	Attempts     []Attempt `json:"attempts"`
	Judge        JudgeInfo `json:"judge"`
	// Winner is the index of the recommended attempt, or 0 when there is
	// none.
	Winner            int      `json:"winner"`
	Confidence        string   `json:"confidence"`
	ConfidenceReasons []string `json:"confidence_reasons,omitempty"`
	Warnings          []string `json:"warnings,omitempty"`
	TotalCost         float64  `json:"total_cost,omitempty"`
	// Applied records which attempt was applied to the tree, if any.
	Applied   int       `json:"applied,omitempty"`
	AppliedAt time.Time `json:"applied_at,omitempty"`
}

// Options configures one run.
type Options struct {
	// Root is the repository whose tree is attempted. It must be a git
	// repository with at least one commit.
	Root string
	// StateDir holds persisted reports and patches.
	StateDir string
	// WorkDir is where disposable worktrees are created; it should be
	// outside Root. Defaults to a directory under os.TempDir.
	WorkDir string

	Task string
	// N is the number of attempts (1-MaxAttempts).
	N int
	// Models are model references handed out to attempts in order and
	// cycled. Empty entries use the configured default.
	Models []string
	// Strategies override the built-in strategy hints, cycled over attempts.
	Strategies []string

	// Judge is the judge model reference. NoJudge disables judging.
	Judge   string
	NoJudge bool

	// Verify and Lint override detection of the check commands.
	Verify []string
	Lint   []string
	// Setup runs once in every fresh worktree before the agent starts, for
	// example to install dependencies that are not tracked by git.
	Setup string

	// Parallel bounds concurrently running attempts (default 3).
	Parallel int
	// Timeout bounds one attempt's agent run (default 20 minutes).
	Timeout time.Duration
	// CheckTimeout bounds each check command (default 10 minutes).
	CheckTimeout time.Duration
	// Yolo auto-approves tool permissions inside the attempts.
	Yolo bool
	// Keep leaves the worktrees in place after the run.
	Keep bool

	// Progress receives one-line status updates. It may be nil and may be
	// called from several goroutines.
	Progress func(string)

	// Agent and Check default to the real subprocess and shell runners.
	Agent AgentFunc
	Check CheckFunc
}

// Limits.
const (
	MaxAttempts         = 8
	defaultParallel     = 3
	defaultTimeout      = 20 * time.Minute
	defaultCheckTimeout = 10 * time.Minute
)
