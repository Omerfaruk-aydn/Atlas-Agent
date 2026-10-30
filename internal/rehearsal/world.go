package rehearsal

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/diff"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/shell"
)

// Verdicts summarise a rehearsal. Only breaks_checks and failed need an
// explicit override before the change can reach the real tree.
const (
	VerdictOK          = "ok"
	VerdictUnchecked   = "unchecked"
	VerdictAPIChanges  = "api_changes"
	VerdictBreakChecks = "breaks_checks"
	VerdictFailed      = "failed"
	VerdictNoChanges   = "no_changes"
)

// Defaults for one rehearsal.
const (
	DefaultCommandTimeout = 10 * time.Minute
	captureLimit          = 256 << 10
	reportedOutput        = 4 << 10
	maxStoredFileBytes    = 64 << 20
	maxReportedPatchBytes = 256 << 10
)

// CommandRunner runs one shell command in dir and returns its combined
// output and exit code. A non-nil error means the command could not be
// run at all, as opposed to running and failing.
type CommandRunner func(ctx context.Context, dir, command string, env []string) (output string, exit int, err error)

// AgentFunc performs the change inside the disposable copy at dir and
// returns a short summary of what it did.
type AgentFunc func(ctx context.Context, dir string) (string, error)

// Options describes one rehearsal.
type Options struct {
	// Root is the real project directory. It is only ever read.
	Root string
	// Store receives the report, the patch and the rehearsed file contents.
	Store *Store
	// Task is a free-form description of the change.
	Task string
	// Steps are the shell commands that make the change, run in order in
	// the copy. The first failing step stops the rehearsal.
	Steps []string
	// Files are written into the copy before Steps run.
	Files []File
	// Agent optionally makes the change itself, after Steps.
	Agent AgentFunc
	// Checks are shell commands (tests, builds, linters) run against the
	// result, and against the untouched copy first unless SkipBaseline.
	Checks         []string
	SkipBaseline   bool
	CommandTimeout time.Duration
	MaxCopyBytes   int64
	Env            []string
	// Run replaces the shell runner; tests use it.
	Run CommandRunner
}

// File is a file to create or replace in the copy.
type File struct {
	Path    string `json:"path" description:"Project-relative file path."`
	Content string `json:"content" description:"Complete new file content."`
}

// CommandResult is one executed command.
type CommandResult struct {
	Phase    string `json:"phase"` // "baseline", "step", "agent" or "check".
	Command  string `json:"command"`
	ExitCode int    `json:"exit_code"`
	TimedOut bool   `json:"timed_out,omitempty"`
	Millis   int64  `json:"millis"`
	Output   string `json:"output,omitempty"`
	// FailedTests lists test names recognised as failing in the output.
	FailedTests []string `json:"failed_tests,omitempty"`
}

// FileChange is one file the rehearsal would change in the real tree.
type FileChange struct {
	Path    string `json:"path"`
	Status  string `json:"status"` // "added", "modified" or "deleted".
	Added   int    `json:"added_lines"`
	Removed int    `json:"removed_lines"`
	Binary  bool   `json:"binary,omitempty"`
	// OriginalHash is the real file's hash when rehearsed, empty when the
	// file did not exist. Apply refuses when the real file has moved on.
	OriginalHash string `json:"original_hash,omitempty"`
	NewHash      string `json:"new_hash,omitempty"`
	Mode         uint32 `json:"mode,omitempty"`
}

// CheckDelta compares one check before and after the change.
type CheckDelta struct {
	Command  string `json:"command"`
	Baseline *int   `json:"baseline_exit,omitempty"`
	After    int    `json:"after_exit"`
	// Verdict is newly_failing, still_failing, newly_passing, passing or
	// no_baseline.
	Verdict           string   `json:"verdict"`
	NewlyFailingTests []string `json:"newly_failing_tests,omitempty"`
}

// APIDiff lists exported Go declarations the change adds, removes or
// alters, keyed as "dir: signature".
type APIDiff struct {
	Added   []string `json:"added,omitempty"`
	Removed []string `json:"removed,omitempty"`
	Changed []string `json:"changed,omitempty"`
}

// Empty reports whether the exported surface is unchanged.
func (d *APIDiff) Empty() bool {
	return d == nil || len(d.Added)+len(d.Removed)+len(d.Changed) == 0
}

// Report is the preview a rehearsal produces.
type Report struct {
	ID        string          `json:"id"`
	Task      string          `json:"task,omitempty"`
	Root      string          `json:"root"`
	CreatedAt time.Time       `json:"created_at"`
	Status    string          `json:"status"` // "previewed", "applied" or "discarded".
	Verdict   string          `json:"verdict"`
	Files     []FileChange    `json:"files"`
	Commands  []CommandResult `json:"commands"`
	Checks    []CheckDelta    `json:"checks,omitempty"`
	API       *APIDiff        `json:"api,omitempty"`
	// IgnoredChanges counts changed paths git ignores in the real project
	// (build output and the like); they are never applied.
	IgnoredChanges int        `json:"ignored_changes,omitempty"`
	Warnings       []string   `json:"warnings,omitempty"`
	PatchBytes     int        `json:"patch_bytes"`
	AppliedAt      *time.Time `json:"applied_at,omitempty"`
}

// Run rehearses a change: it copies the project into a temporary
// directory, runs the baseline checks, makes the change, runs the checks
// again and stores a reviewable preview. The real project is never
// written.
func Run(ctx context.Context, opts Options) (*Report, error) {
	root, err := filepath.Abs(opts.Root)
	if err != nil {
		return nil, err
	}
	if info, err := os.Stat(root); err != nil || !info.IsDir() {
		return nil, fmt.Errorf("rehearsal root %q is not a directory", opts.Root)
	}
	if opts.Store == nil {
		return nil, errors.New("rehearsal requires a store")
	}
	if len(opts.Steps) == 0 && len(opts.Files) == 0 && opts.Agent == nil {
		return nil, errors.New("nothing to rehearse: supply at least one step command")
	}
	for _, command := range append(append([]string{}, opts.Steps...), opts.Checks...) {
		if strings.TrimSpace(command) == "" {
			return nil, errors.New("empty command in rehearsal plan")
		}
		if referencesRoot(command, root) {
			return nil, fmt.Errorf("command %q names the real project path; rehearsal commands must use relative paths so they act on the copy", command)
		}
	}
	runner := opts.Run
	if runner == nil {
		runner = shellRunner
	}
	timeout := opts.CommandTimeout
	if timeout <= 0 {
		timeout = DefaultCommandTimeout
	}
	env := opts.Env
	if env == nil {
		env = os.Environ()
	}
	env = append(append([]string{}, env...), "ATLAS_REHEARSAL=1")

	work, err := os.MkdirTemp("", "atlas-rehearsal-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(work)

	base, links, err := copyTree(ctx, root, work, opts.MaxCopyBytes)
	if err != nil {
		return nil, fmt.Errorf("copy project: %w", err)
	}
	report := &Report{Task: opts.Task, Root: root, CreatedAt: time.Now().UTC(), Status: "previewed"}
	if report.ID, err = newID(report.CreatedAt); err != nil {
		return nil, err
	}
	if links > 0 {
		report.Warnings = append(report.Warnings, fmt.Sprintf("%d symbolic link(s) were not copied", links))
	}

	runCmd := func(phase, command string) CommandResult {
		cctx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		start := time.Now()
		output, code, err := runner(cctx, work, command, env)
		res := CommandResult{Phase: phase, Command: command, ExitCode: code, Millis: time.Since(start).Milliseconds()}
		if err != nil {
			res.ExitCode = max(code, 1)
			output += "\n" + err.Error()
		}
		if errors.Is(cctx.Err(), context.DeadlineExceeded) {
			res.TimedOut = true
		}
		if res.ExitCode != 0 {
			res.FailedTests = FailedTests(output)
		}
		res.Output = tail(output, reportedOutput)
		return res
	}

	baseline := map[string]CommandResult{}
	if !opts.SkipBaseline && len(opts.Checks) > 0 {
		for _, check := range opts.Checks {
			baseline[check] = runCmd("baseline", check)
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := restore(ctx, root, work, base); err != nil {
			return nil, fmt.Errorf("reset copy after baseline: %w", err)
		}
	}

	stepFailed := false
	for _, f := range opts.Files {
		res := CommandResult{Phase: "write", Command: "write " + f.Path}
		target, err := within(work, f.Path)
		if err == nil {
			err = writeAtomic(target, []byte(f.Content), 0o644)
		}
		if err != nil {
			res.ExitCode, res.Output, stepFailed = 1, err.Error(), true
		}
		report.Commands = append(report.Commands, res)
		if stepFailed {
			break
		}
	}
	for _, step := range opts.Steps {
		if stepFailed {
			break
		}
		res := runCmd("step", step)
		report.Commands = append(report.Commands, res)
		if res.ExitCode != 0 {
			stepFailed = true
			break
		}
	}
	if !stepFailed && opts.Agent != nil {
		summary, err := opts.Agent(ctx, work)
		res := CommandResult{Phase: "agent", Command: "agent", Output: tail(summary, reportedOutput)}
		if err != nil {
			res.ExitCode, stepFailed = 1, true
			res.Output = tail(summary+"\n"+err.Error(), reportedOutput)
		}
		report.Commands = append(report.Commands, res)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	changes, err := scan(ctx, work, base)
	if err != nil {
		return nil, fmt.Errorf("scan rehearsal: %w", err)
	}
	changes, report.IgnoredChanges = dropIgnored(ctx, root, changes, report)
	patch, err := record(report, opts.Store, root, work, base, changes)
	if err != nil {
		return nil, err
	}

	if !stepFailed {
		for _, check := range opts.Checks {
			res := runCmd("check", check)
			report.Commands = append(report.Commands, res)
			report.Checks = append(report.Checks, delta(check, baseline, res))
		}
	}
	report.API = apiDiff(root, work, changes)
	report.Verdict = verdict(report, stepFailed, len(opts.Checks) > 0)
	if err := opts.Store.save(report, patch); err != nil {
		return nil, err
	}
	return report, nil
}

// record fills the report's file list and returns the unified patch, and
// stores the rehearsed contents so Apply needs no second run.
func record(report *Report, store *Store, root, work string, base manifest, changes []change) (string, error) {
	var patch strings.Builder
	var stored int64
	for _, c := range changes {
		fc := FileChange{Path: c.Path, Status: c.Status}
		realPath := filepath.Join(root, filepath.FromSlash(c.Path))
		workPath := filepath.Join(work, filepath.FromSlash(c.Path))
		var before, after []byte
		if c.Status != "added" {
			fc.OriginalHash = base[c.Path].Hash
			fc.Mode = uint32(base[c.Path].Mode)
			data, err := os.ReadFile(realPath)
			if err != nil {
				return "", fmt.Errorf("read original %s: %w", c.Path, err)
			}
			before = data
		}
		if c.Status != "deleted" {
			data, err := os.ReadFile(workPath)
			if err != nil {
				return "", fmt.Errorf("read rehearsed %s: %w", c.Path, err)
			}
			after = data
			fc.NewHash = hashBytes(after)
			if info, err := os.Stat(workPath); err == nil {
				fc.Mode = uint32(info.Mode().Perm())
			}
			stored += int64(len(after))
			if stored > maxStoredFileBytes {
				return "", fmt.Errorf("rehearsed changes exceed %d MiB; split the task", maxStoredFileBytes>>20)
			}
			if err := store.putFile(report.ID, c.Path, after); err != nil {
				return "", err
			}
		}
		if bytes.IndexByte(before, 0) >= 0 || bytes.IndexByte(after, 0) >= 0 {
			fc.Binary = true
		} else {
			text, add, del := diff.GenerateDiff(string(before), string(after), c.Path)
			fc.Added, fc.Removed = add, del
			patch.WriteString(text)
			if !strings.HasSuffix(text, "\n") {
				patch.WriteString("\n")
			}
		}
		report.Files = append(report.Files, fc)
	}
	report.PatchBytes = patch.Len()
	return patch.String(), nil
}

func delta(check string, baseline map[string]CommandResult, after CommandResult) CheckDelta {
	d := CheckDelta{Command: check, After: after.ExitCode}
	before, ok := baseline[check]
	if !ok {
		d.Verdict = "no_baseline"
		if after.ExitCode == 0 {
			d.Verdict = "passing"
		}
		return d
	}
	code := before.ExitCode
	d.Baseline = &code
	was := map[string]bool{}
	for _, name := range before.FailedTests {
		was[name] = true
	}
	for _, name := range after.FailedTests {
		if !was[name] {
			d.NewlyFailingTests = append(d.NewlyFailingTests, name)
		}
	}
	switch {
	case before.ExitCode == 0 && after.ExitCode != 0:
		d.Verdict = "newly_failing"
	case before.ExitCode != 0 && after.ExitCode != 0 && len(d.NewlyFailingTests) > 0:
		d.Verdict = "newly_failing"
	case before.ExitCode != 0 && after.ExitCode != 0:
		d.Verdict = "still_failing"
	case before.ExitCode != 0:
		d.Verdict = "newly_passing"
	default:
		d.Verdict = "passing"
	}
	return d
}

func verdict(r *Report, stepFailed, hadChecks bool) string {
	if stepFailed {
		return VerdictFailed
	}
	for _, c := range r.Checks {
		if c.Verdict == "newly_failing" || (c.Verdict == "no_baseline" && c.After != 0) {
			return VerdictBreakChecks
		}
	}
	switch {
	case len(r.Files) == 0:
		return VerdictNoChanges
	case !r.API.Empty() && (len(r.API.Removed) > 0 || len(r.API.Changed) > 0):
		return VerdictAPIChanges
	case !hadChecks:
		return VerdictUnchecked
	}
	return VerdictOK
}

// dropIgnored removes changes to paths the real project's git ignores.
func dropIgnored(ctx context.Context, root string, changes []change, report *Report) ([]change, int) {
	if len(changes) == 0 {
		return changes, 0
	}
	var in bytes.Buffer
	for _, c := range changes {
		in.WriteString(c.Path)
		in.WriteByte(0)
	}
	cmd := exec.CommandContext(ctx, "git", "check-ignore", "-z", "--stdin")
	cmd.Dir = root
	cmd.Stdin = &in
	out, err := cmd.Output()
	var exitErr *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &exitErr) && exitErr.ExitCode() == 1:
		return changes, 0 // Nothing ignored.
	default:
		report.Warnings = append(report.Warnings, "git ignore rules unavailable; build output in the copy may appear as changes")
		return changes, 0
	}
	ignored := map[string]bool{}
	for p := range strings.SplitSeq(string(out), "\x00") {
		if p != "" {
			ignored[p] = true
		}
	}
	kept := changes[:0:0]
	for _, c := range changes {
		if !ignored[c.Path] {
			kept = append(kept, c)
		}
	}
	return kept, len(changes) - len(kept)
}

func referencesRoot(command, root string) bool {
	norm := func(s string) string {
		s = strings.ReplaceAll(s, "\\", "/")
		if runtime.GOOS == "windows" {
			s = strings.ToLower(s)
		}
		return strings.TrimRight(s, "/")
	}
	r := norm(root)
	return r != "" && strings.Contains(norm(command), r)
}

func newID(now time.Time) (string, error) {
	b := make([]byte, 3)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "rh-" + now.Format("20060102-150405") + "-" + hex.EncodeToString(b), nil
}

func tail(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return "...[truncated]\n" + s[len(s)-n:]
}

// tailBuffer keeps the last limit bytes written to it.
type tailBuffer struct {
	mu    sync.Mutex
	limit int
	buf   []byte
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, p...)
	if len(t.buf) > 2*t.limit {
		t.buf = append([]byte(nil), t.buf[len(t.buf)-t.limit:]...)
	}
	return len(p), nil
}

func (t *tailBuffer) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.buf) > t.limit {
		return string(t.buf[len(t.buf)-t.limit:])
	}
	return string(t.buf)
}

var _ io.Writer = (*tailBuffer)(nil)

// shellRunner runs a command through Atlas' own shell interpreter, so
// rehearsals behave the same on every platform.
func shellRunner(ctx context.Context, dir, command string, env []string) (string, int, error) {
	out := &tailBuffer{limit: captureLimit}
	err := shell.Run(ctx, shell.RunOptions{Command: command, Cwd: dir, Env: env, Stdout: out, Stderr: out})
	if err == nil {
		return out.String(), 0, nil
	}
	if shell.ObservedExit(err) {
		return out.String(), shell.ExitCode(err), nil
	}
	return out.String(), 1, err
}
