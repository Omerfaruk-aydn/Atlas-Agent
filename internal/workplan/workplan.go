// Package workplan reads the work plan an agent keeps for substantial work:
// a verification contract, a feature list and a progress log stored in the
// project's .atlas directory.
//
// The files belong to the agent. It writes them with its ordinary file tools
// before implementing, so the plan survives compaction, model switches and
// new sessions, and so completion can be judged against commands rather than
// against the agent's own report. Atlas only reads them: it shows a compact
// summary at the start of each request and checks the contract when a turn
// that changed files ends. Nothing here is a permission grant, and a missing
// or malformed plan simply means there is no contract to enforce.
package workplan

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	// Dir is the plan's directory relative to the working directory.
	Dir = ".atlas"
	// PlanFile holds the contract and the feature list.
	PlanFile = "work.json"
	// ProgressFile holds the agent's running notes.
	ProgressFile = "progress.md"
)

// Limits keep a plan that grew by accident from flooding every request.
const (
	maxPlanBytes      = 256 << 10
	maxProgressBytes  = 256 << 10
	maxChecks         = 16
	maxFeatures       = 200
	maxCommandLen     = 1000
	maxTextLen        = 600
	maxShownPending   = 8
	maxProgressTail   = 1500
	maxSummaryCommand = 160
	maxAssumptions    = 30
	maxShownAssumed   = 6
)

// Check is one command whose success is part of the definition of done.
type Check struct {
	Name    string `json:"name,omitempty"`
	Command string `json:"command"`
}

// Feature is one observable outcome of the work.
type Feature struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	Acceptance string `json:"acceptance,omitempty"`
	// Verify is a command that proves this feature in particular. It is
	// required to pass once the feature is marked done.
	Verify string `json:"verify,omitempty"`
	Done   bool   `json:"done,omitempty"`
}

// Plan is the contents of work.json.
type Plan struct {
	Goal     string    `json:"goal,omitempty"`
	Verify   []Check   `json:"verify,omitempty"`
	Features []Feature `json:"features,omitempty"`
	// Assumptions are the choices the agent made where the request was
	// silent. Keeping them visible lets the user correct a wrong one with
	// a glance instead of discovering it in the finished work.
	Assumptions []string `json:"assumptions,omitempty"`
	// Previewed is set once the user has been shown the first working
	// slice, so the reminder to show one stops.
	Previewed bool `json:"previewed,omitempty"`
}

// DoneIDs lists the features marked done.
func (p *Plan) DoneIDs() []string {
	if p == nil {
		return nil
	}
	var out []string
	for _, feature := range p.Features {
		if feature.Done {
			out = append(out, feature.ID)
		}
	}
	return out
}

// Feature returns the feature with id.
func (p *Plan) Feature(id string) (Feature, bool) {
	if p == nil {
		return Feature{}, false
	}
	for _, feature := range p.Features {
		if feature.ID == id {
			return feature, true
		}
	}
	return Feature{}, false
}

// ReviewKey identifies the parts of the plan a reviewer judges: the goal,
// the contract and each feature's definition, but not progress flags, so
// marking features done does not trigger another review.
func (p *Plan) ReviewKey() string {
	if p == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString(p.Goal)
	for _, check := range p.Verify {
		b.WriteString("\x00" + check.Command)
	}
	for _, feature := range p.Features {
		b.WriteString("\x00" + feature.ID + "\x01" + feature.Title + "\x01" + feature.Acceptance + "\x01" + feature.Verify)
	}
	return b.String()
}

// Requirement is a command the contract currently requires to pass, and
// what requires it.
type Requirement struct {
	Command string
	Reason  string
}

// Load reads the plan and the progress notes under root. It returns a nil
// plan and no error when there is no plan file. A progress file without a
// plan is ignored: notes alone are not a contract.
func Load(root string) (*Plan, string, error) {
	if root == "" {
		return nil, "", nil
	}
	data, err := readBounded(filepath.Join(root, Dir, PlanFile), maxPlanBytes)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, "", nil
	}
	if err != nil {
		return nil, "", err
	}
	plan, err := Parse(data)
	if err != nil {
		return nil, "", err
	}
	progress, err := readBounded(filepath.Join(root, Dir, ProgressFile), maxProgressBytes)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return plan, "", err
	}
	return plan, string(progress), nil
}

// Parse reads and validates the contents of a plan file.
func Parse(data []byte) (*Plan, error) {
	var plan Plan
	if err := json.Unmarshal(data, &plan); err != nil {
		return nil, fmt.Errorf("%s/%s is not valid JSON: %w", Dir, PlanFile, err)
	}
	if err := plan.validate(); err != nil {
		return nil, fmt.Errorf("%s/%s: %w", Dir, PlanFile, err)
	}
	return &plan, nil
}

func readBounded(path string, limit int64) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.Size() > limit {
		return nil, fmt.Errorf("%s is larger than %d KiB", filepath.Base(path), limit>>10)
	}
	return os.ReadFile(path)
}

func (p *Plan) validate() error {
	if len(p.Verify) > maxChecks {
		return fmt.Errorf("at most %d verify commands", maxChecks)
	}
	if len(p.Features) > maxFeatures {
		return fmt.Errorf("at most %d features", maxFeatures)
	}
	if len(p.Assumptions) > maxAssumptions {
		return fmt.Errorf("at most %d assumptions", maxAssumptions)
	}
	for i, check := range p.Verify {
		command := normalize(check.Command)
		if command == "" {
			return fmt.Errorf("verify[%d] has no command", i)
		}
		if len(command) > maxCommandLen {
			return fmt.Errorf("verify[%d] command is longer than %d bytes", i, maxCommandLen)
		}
		p.Verify[i].Command = command
	}
	seen := map[string]bool{}
	for i, feature := range p.Features {
		id := strings.TrimSpace(feature.ID)
		if id == "" {
			return fmt.Errorf("features[%d] has no id", i)
		}
		if seen[id] {
			return fmt.Errorf("feature id %q is used twice", id)
		}
		seen[id] = true
		command := normalize(feature.Verify)
		if len(command) > maxCommandLen {
			return fmt.Errorf("feature %s verify command is longer than %d bytes", id, maxCommandLen)
		}
		p.Features[i].ID, p.Features[i].Verify = id, command
	}
	return nil
}

// Requirements lists the commands that must pass for the work to count as
// verified: every contract check, plus the check of each feature marked
// done. A feature still open is work in progress, so its command is not yet
// required.
func (p *Plan) Requirements() []Requirement {
	if p == nil {
		return nil
	}
	var out []Requirement
	add := func(command, reason string) {
		if CheckProblem(command) != "" {
			return // Reported by CheckProblems instead; it could never pass.
		}
		for i, r := range out {
			if r.Command == command {
				out[i].Reason += "; " + reason
				return
			}
		}
		out = append(out, Requirement{Command: command, Reason: reason})
	}
	for _, check := range p.Verify {
		add(check.Command, "contract check "+cmp.Or(strings.TrimSpace(check.Name), check.Command))
	}
	for _, feature := range p.Features {
		if feature.Done && feature.Verify != "" {
			add(feature.Verify, "feature "+feature.ID+" is marked done")
		}
	}
	return out
}

// Open returns the features not yet marked done, in plan order.
func (p *Plan) Open() []Feature {
	if p == nil {
		return nil
	}
	var out []Feature
	for _, feature := range p.Features {
		if !feature.Done {
			out = append(out, feature)
		}
	}
	return out
}

// IsPlanFile reports whether path names the plan or the progress file.
// Editing them records progress, not implementation, so it does not make
// earlier verification stale.
func IsPlanFile(path string) bool {
	path = filepath.ToSlash(filepath.Clean(path))
	for _, name := range []string{PlanFile, ProgressFile} {
		suffix := Dir + "/" + name
		if path == suffix || strings.HasSuffix(path, "/"+suffix) {
			return true
		}
	}
	return false
}

// IsAtlasPath reports whether path is under the project's .atlas
// directory: the plan, notes and the agent's own probe scripts, none of
// which the project's checks build or run.
func IsAtlasPath(path string) bool {
	path = filepath.ToSlash(filepath.Clean(path))
	return strings.HasPrefix(path, Dir+"/") || strings.Contains(path, "/"+Dir+"/")
}

// Satisfies reports whether a command that was run counts as running the
// required one. The run may wrap it (cd into a directory first, set an
// environment variable, pipe the output to a pager), but it must not
// discard the exit status with "|| ..." after it, since then a passing run
// proves nothing. Whitespace differences are ignored.
func Satisfies(ran, required string) bool {
	ran, required = normalize(ran), normalize(required)
	if required == "" {
		return false
	}
	at := strings.Index(ran, required)
	if at < 0 {
		return false
	}
	rest := ran[at+len(required):]
	return !strings.Contains(rest, "||") && !strings.Contains(rest, "; exit 0") && !strings.Contains(ran[:at], "! ")
}

func normalize(command string) string {
	return strings.Join(strings.Fields(command), " ")
}

// PromptBlock is the plan summary shown at the start of a request. It is
// empty without a plan and reports a malformed plan instead of hiding it,
// because a contract the agent believes in but Atlas cannot read would be
// silently unenforced.
func PromptBlock(root string) string {
	plan, progress, err := Load(root)
	if err != nil {
		return "\n\n<work_plan status=\"unreadable\">\n" + clip(err.Error(), maxTextLen) +
			"\nFix the file so the verification contract can be checked.\n</work_plan>"
	}
	if plan == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n\n<work_plan path=\"" + Dir + "/" + PlanFile + "\">\n")
	if goal := strings.TrimSpace(plan.Goal); goal != "" {
		b.WriteString("Goal: " + clip(goal, maxTextLen) + "\n")
	}
	open := plan.Open()
	fmt.Fprintf(&b, "Features: %d of %d done.\n", len(plan.Features)-len(open), len(plan.Features))
	if len(open) > 0 {
		b.WriteString("Open, in order:\n")
		for _, feature := range open[:min(len(open), maxShownPending)] {
			line := "- " + feature.ID + ": " + clip(feature.Title, maxTextLen)
			if feature.Acceptance != "" {
				line += " (done when: " + clip(feature.Acceptance, maxTextLen) + ")"
			}
			b.WriteString(line + "\n")
		}
		if hidden := len(open) - maxShownPending; hidden > 0 {
			fmt.Fprintf(&b, "- … %d more in the file\n", hidden)
		}
	}
	if len(open) > 0 {
		if costs, err := LoadCosts(root); err == nil {
			if cost, ok := costs[open[0].ID]; ok && cost.Turns > 0 {
				line := fmt.Sprintf("Current feature %s so far: %s, %s, %s tokens.", open[0].ID,
					plural(cost.Turns, "turn"), time.Duration(cost.Millis*int64(time.Millisecond)).Round(time.Minute), compactCount(cost.Tokens))
				if warning := costWarning(plan, costs); warning != "" {
					line += " Warning: " + warning + ". Step back: re-read the failure, list hypotheses and change approach instead of pushing on."
				}
				b.WriteString(line + "\n")
			}
		}
	}
	if requirements := plan.Requirements(); len(requirements) > 0 {
		b.WriteString("Verification contract (each must pass after your last code change before the work counts as done):\n")
		for _, r := range requirements {
			b.WriteString("- " + clip(r.Command, maxSummaryCommand) + "\n")
		}
	} else {
		b.WriteString("The plan has no verify commands yet: add the commands that prove the work before implementing.\n")
	}
	if len(plan.Assumptions) > 0 {
		b.WriteString("Assumptions you made (list them in your report so the user can correct one):\n")
		for _, assumption := range plan.Assumptions[:min(len(plan.Assumptions), maxShownAssumed)] {
			b.WriteString("- " + clip(assumption, maxTextLen) + "\n")
		}
		if hidden := len(plan.Assumptions) - maxShownAssumed; hidden > 0 {
			fmt.Fprintf(&b, "- … %d more in the file\n", hidden)
		}
	}
	if flaky := LoadState(root).Flaky; len(flaky) > 0 {
		b.WriteString("Flaky checks (results changed with no code change): ")
		shown := make([]string, 0, len(flaky))
		for _, command := range flaky[:min(len(flaky), maxShownAssumed)] {
			shown = append(shown, "`"+clip(command, maxSummaryCommand)+"`")
		}
		b.WriteString(strings.Join(shown, ", ") + ". A failure of one is not proof of a bug: run it again before debugging, and fix the flakiness (timing, ordering, shared state) when it gets in the way.\n")
	}
	if !plan.Previewed && len(plan.DoneIDs()) > 0 {
		b.WriteString("The first slice works: show the user a preview in your next report (how to run or open it, a screenshot or sample output) so they can redirect early, then set \"previewed\": true. Do not wait for an answer.\n")
	}
	if deps := Dependencies(root); len(deps) > 0 {
		parts := make([]string, 0, len(deps))
		for _, d := range deps {
			parts = append(parts, d.Name+" "+d.Version)
		}
		b.WriteString("Dependency versions in use: " + strings.Join(parts, ", ") + ". Before relying on an API you are not certain exists in these versions, check its documentation for that version.\n")
	}
	if tail := progressTail(progress); tail != "" {
		b.WriteString("Latest progress notes (" + Dir + "/" + ProgressFile + "):\n" + tail + "\n")
	}
	b.WriteString("</work_plan>")
	return b.String()
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}

func compactCount(n int64) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%dk", n/1_000)
	default:
		return fmt.Sprint(n)
	}
}

// progressTail keeps the end of the notes, where the latest state and the
// next step are, starting at a line boundary.
func progressTail(progress string) string {
	progress = strings.TrimSpace(progress)
	if len(progress) <= maxProgressTail {
		return progress
	}
	tail := progress[len(progress)-maxProgressTail:]
	if i := strings.IndexByte(tail, '\n'); i >= 0 {
		tail = tail[i+1:]
	}
	return "…\n" + tail
}

func clip(s string, limit int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= limit {
		return s
	}
	cut := limit
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "…"
}
