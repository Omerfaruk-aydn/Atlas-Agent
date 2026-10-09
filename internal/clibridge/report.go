package clibridge

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
)

// Outcome separates what actually happened to a run.
type Outcome string

const (
	// OutcomePassed: the process exited and every declared expectation held.
	OutcomePassed Outcome = "passed"
	// OutcomeFailed: the process exited but an expectation did not hold.
	OutcomeFailed Outcome = "failed"
	// OutcomeTimeout: the profile's time limit stopped the process.
	OutcomeTimeout Outcome = "timeout"
	// OutcomeCanceled: the caller canceled; the result is unknown and the
	// run is never retried automatically.
	OutcomeCanceled Outcome = "canceled"
	// OutcomeDenied: a permission prompt was refused; nothing started.
	OutcomeDenied Outcome = "permission_denied"
	// OutcomeHookDenied: a PreToolUse hook blocked the command.
	OutcomeHookDenied Outcome = "hook_denied"
	// OutcomeBlocked: command policy, ownership or execution policy
	// refused the command before it started.
	OutcomeBlocked Outcome = "blocked"
	// OutcomeRunning: the command continues as a background job.
	OutcomeRunning Outcome = "running"
	// OutcomeError: the outcome could not be observed.
	OutcomeError Outcome = "error"
)

// OutputObservation is what was found on disk after the run.
type OutputObservation struct {
	Param   string `json:"param"`
	Path    string `json:"path"`
	Exists  bool   `json:"exists"`
	Size    int64  `json:"size,omitempty"`
	SHA256  string `json:"sha256,omitempty"`
	Changed bool   `json:"changed"`
}

// Expectation is one checked claim.
type Expectation struct {
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
	Detail string `json:"detail"`
}

// RunReport is the persisted evidence of one run.
type RunReport struct {
	RunID          string              `json:"run_id"`
	ProfileID      string              `json:"profile_id"`
	Scope          Scope               `json:"scope"`
	ProfileHash    string              `json:"profile_hash"`
	Operation      string              `json:"operation"`
	PrepareID      string              `json:"prepare_id"`
	Executable     Identity            `json:"executable"`
	Argv           []string            `json:"argv"`
	WorkingDir     string              `json:"working_dir"`
	StartedAt      int64               `json:"started_at"`
	FinishedAt     int64               `json:"finished_at,omitempty"`
	DurationMS     int64               `json:"duration_ms,omitempty"`
	Outcome        Outcome             `json:"outcome"`
	Observed       bool                `json:"observed"`
	Passed         bool                `json:"passed"`
	ExitCode       *int                `json:"exit_code,omitempty"`
	Background     bool                `json:"background,omitempty"`
	JobID          string              `json:"job_id,omitempty"`
	Stdout         string              `json:"stdout,omitempty"`
	Stderr         string              `json:"stderr,omitempty"`
	StdoutBytes    int64               `json:"stdout_bytes"`
	StderrBytes    int64               `json:"stderr_bytes"`
	Truncated      bool                `json:"truncated,omitempty"`
	Outputs        []OutputObservation `json:"outputs,omitempty"`
	Expectations   []Expectation       `json:"expectations,omitempty"`
	Detail         string              `json:"detail,omitempty"`
	StdoutArtifact string              `json:"stdout_artifact,omitempty"`
	StderrArtifact string              `json:"stderr_artifact,omitempty"`
}

// NewReport starts a report for a prepared invocation.
func NewReport(runID string, p Prepared, started time.Time) RunReport {
	return RunReport{
		RunID: runID, ProfileID: p.ProfileID, Scope: p.Scope, ProfileHash: p.ProfileHash, Operation: p.Operation,
		PrepareID: p.ID, Executable: p.Executable, Argv: p.DisplayArgv, WorkingDir: p.WorkingDir, StartedAt: started.UnixMilli(),
	}
}

// Finish records an observed exit and evaluates every expectation. A zero
// exit code alone never passes a run that declares other expectations.
func (r *RunReport) Finish(p Prepared, exitCode int, stdout, stderr string, finished time.Time) {
	r.FinishedAt = finished.UnixMilli()
	r.DurationMS = max(0, r.FinishedAt-r.StartedAt)
	r.ExitCode = &exitCode
	r.Observed = true
	r.Expectations = nil
	r.Expectations = append(r.Expectations, Expectation{
		Name: "exit_code", Passed: slices.Contains(p.ExpectedExitCodes, exitCode),
		Detail: fmt.Sprintf("observed %d, expected one of %v", exitCode, p.ExpectedExitCodes),
	})
	if p.StdoutContains != "" {
		r.Expectations = append(r.Expectations, Expectation{
			Name: "stdout_contains", Passed: strings.Contains(stdout, p.StdoutContains),
			Detail: fmt.Sprintf("stdout must contain %q", p.StdoutContains),
		})
	}
	r.Outputs = nil
	for _, out := range p.Outputs {
		observed := observeOutput(out, r.StartedAt)
		r.Outputs = append(r.Outputs, observed)
		for _, expectation := range p.Expectations {
			if expectation.Param != out.Param {
				continue
			}
			passed, detail := checkOutput(observed, expectation)
			r.Expectations = append(r.Expectations, Expectation{Name: "output:" + out.Param, Passed: passed, Detail: detail})
		}
	}
	r.Passed = true
	for _, e := range r.Expectations {
		r.Passed = r.Passed && e.Passed
	}
	r.Outcome = OutcomeFailed
	if r.Passed {
		r.Outcome = OutcomePassed
	}
}

func observeOutput(ref FileRef, startedMS int64) OutputObservation {
	o := OutputObservation{Param: ref.Param, Path: ref.Path}
	info, err := os.Lstat(ref.Path)
	if err != nil || !info.Mode().IsRegular() {
		return o
	}
	o.Exists, o.Size = true, info.Size()
	if sum, err := fileSHA256(ref.Path, maxHashBytes); err == nil {
		o.SHA256 = sum
	}
	// Require a verified content difference or a changed snapshot timestamp
	// at or after the run. A recently created, untouched file is not evidence.
	contentChanged := ref.SHA256 != "" && o.SHA256 != "" && o.SHA256 != ref.SHA256
	metadataChanged := ref.ModTimeNS != 0 && info.ModTime().UnixNano() != ref.ModTimeNS && info.ModTime().UnixMilli() >= startedMS
	o.Changed = !ref.Existed || contentChanged || metadataChanged
	return o
}

func checkOutput(o OutputObservation, e OutputExpectation) (bool, string) {
	switch {
	case !o.Exists:
		return false, o.Path + " was not created"
	case !o.Changed:
		return false, o.Path + " existed before the run and was not rewritten"
	case o.Size < e.MinBytes:
		return false, fmt.Sprintf("%s has %d bytes, expected at least %d", o.Path, o.Size, e.MinBytes)
	case e.MaxBytes > 0 && o.Size > e.MaxBytes:
		return false, fmt.Sprintf("%s has %d bytes, expected at most %d", o.Path, o.Size, e.MaxBytes)
	}
	return true, fmt.Sprintf("%s exists with %d bytes (sha256 %s)", o.Path, o.Size, shortHash(o.SHA256))
}

// BoundText keeps the head and tail of output within limit bytes.
func BoundText(text string, limit int) (string, bool) {
	if limit <= 0 || len(text) <= limit {
		return text, false
	}
	marker := "\n…[truncated]…\n"
	half := max(0, (limit-len(marker))/2)
	head := strings.ToValidUTF8(text[:half], "")
	tail := strings.ToValidUTF8(text[len(text)-half:], "")
	return head + marker + tail, true
}

// Redact removes secret values, including their JSON-escaped spellings,
// from text that may be shown or persisted.
func Redact(text string, values []string) string {
	for _, value := range values {
		if value == "" {
			continue
		}
		text = strings.ReplaceAll(text, value, "[REDACTED]")
		if encoded, err := json.Marshal(value); err == nil && len(encoded) > 2 {
			text = strings.ReplaceAll(text, string(encoded[1:len(encoded)-1]), "[REDACTED]")
		}
	}
	return text
}

// RedactReport applies Redact to every free-text field.
func RedactReport(r RunReport, values []string) RunReport {
	r.Stdout, r.Stderr, r.Detail = Redact(r.Stdout, values), Redact(r.Stderr, values), Redact(r.Detail, values)
	for i := range r.Argv {
		r.Argv[i] = Redact(r.Argv[i], values)
	}
	for i := range r.Expectations {
		r.Expectations[i].Detail = Redact(r.Expectations[i].Detail, values)
	}
	return r
}

// RunNamespace and PreparedNamespace are engineering record namespaces
// scoped to one session.
func RunNamespace(session string) string { return "cli-bridge-runs-" + engineering.Hash(session) }

// PreparedNamespace holds previews. Secrets are never stored there.
func PreparedNamespace(session string) string {
	return "cli-bridge-prepared-" + engineering.Hash(session)
}

// ErrNotFound reports a missing run or preview record.
var ErrNotFound = errors.New("record not found in this session")
