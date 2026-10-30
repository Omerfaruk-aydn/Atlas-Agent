package codereview

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"unicode"
)

// Request is one model call made by the pipeline.
type Request struct {
	// Kind is "review" for a reviewer pass or "verify" for a verifier.
	Kind   string
	Prompt string
	// Model is the optional model override from [Options].
	Model string
}

// Runner executes one request and returns the model's final text. The
// pipeline gives reviewers and verifiers read-only work, so a Runner
// should run them under a read-only role.
type Runner interface {
	Run(ctx context.Context, req Request) (string, error)
}

// RunnerFunc adapts a function to [Runner].
type RunnerFunc func(ctx context.Context, req Request) (string, error)

// Run implements [Runner].
func (f RunnerFunc) Run(ctx context.Context, req Request) (string, error) { return f(ctx, req) }

// Options tunes a review.
type Options struct {
	Effort Effort
	// Model and VerifyModel override the reviewer and verifier models.
	Model       string
	VerifyModel string
	// Concurrency bounds simultaneous model requests. Default 3.
	Concurrency int
	// MinConfidence drops findings below it. Default 0.35.
	MinConfidence float64
	// MaxVerify caps how many findings are verified, highest severity
	// first; the rest are dropped as unverified. Default 40.
	MaxVerify int
	// BatchSize is how many findings one verifier request checks.
	// Default 5.
	BatchSize int
	// Progress receives short status lines. It may be nil.
	Progress func(string)
}

func (o *Options) defaults() {
	if o.Effort == "" {
		o.Effort = EffortMedium
	}
	if o.Concurrency <= 0 {
		o.Concurrency = 3
	}
	if o.MinConfidence <= 0 {
		o.MinConfidence = 0.35
	}
	if o.MaxVerify <= 0 {
		o.MaxVerify = 40
	}
	if o.BatchSize <= 0 {
		o.BatchSize = 5
	}
	if o.Progress == nil {
		o.Progress = func(string) {}
	}
}

// Review runs the reviewer passes over target, merges and verifies the
// findings, and returns the ranked report.
func Review(ctx context.Context, runner Runner, target Target, opts Options) (*Report, error) {
	opts.defaults()
	plan := PlanFor(opts.Effort)
	report := &Report{Target: target.Label, Kind: target.Kind, Effort: opts.Effort, Truncated: target.Truncated}
	if strings.TrimSpace(target.Diff) == "" {
		report.Summary = "Nothing to review: the selected scope has no changes."
		return report, nil
	}

	known := make(map[string]bool, len(target.Files))
	for _, f := range target.Files {
		known[f] = true
	}

	opts.Progress(fmt.Sprintf("Running %d review pass(es)...", plan.Passes))
	results := runPasses(ctx, runner, target, plan.Passes, opts)
	report.Passes = plan.Passes

	var candidates []Finding
	var failed int
	for _, res := range results {
		if res.err != nil {
			failed++
			report.Warnings = append(report.Warnings, fmt.Sprintf("review pass %d failed: %v", res.index+1, res.err))
			continue
		}
		parsed, ok := parseReview(res.text)
		if !ok {
			failed++
			report.Warnings = append(report.Warnings, fmt.Sprintf("review pass %d returned no parsable findings", res.index+1))
			continue
		}
		if report.Summary == "" {
			report.Summary = clip(parsed.Summary, 600)
		}
		for _, w := range parsed.Findings {
			f, reason := toFinding(w, target.Root, known)
			if reason != "" {
				report.Dropped = append(report.Dropped, Dropped{Path: f.Path, Line: f.Line, Title: f.Title, Reason: reason})
				continue
			}
			if reason := ground(target, f, known); reason != "" {
				report.Dropped = append(report.Dropped, Dropped{Path: f.Path, Line: f.Line, Title: f.Title, Reason: reason})
				continue
			}
			candidates = append(candidates, f)
		}
	}
	if failed == len(results) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("every review pass failed: %s", strings.Join(report.Warnings, "; "))
	}

	merged := merge(candidates)
	if plan.Verifiers > 0 && len(merged) > 0 {
		opts.Progress(fmt.Sprintf("Verifying %d finding(s)...", len(merged)))
		merged = verify(ctx, runner, target, merged, plan.Verifiers, opts, report)
	}

	var kept []Finding
	for _, f := range merged {
		if f.Confidence < opts.MinConfidence {
			report.Dropped = append(report.Dropped, Dropped{Path: f.Path, Line: f.Line, Title: f.Title, Reason: fmt.Sprintf("confidence %.2f below %.2f", f.Confidence, opts.MinConfidence)})
			continue
		}
		kept = append(kept, f)
	}
	rank(kept)
	for i := range kept {
		kept[i].ID = fmt.Sprintf("F%d", i+1)
	}
	report.Findings = kept
	if report.Summary == "" {
		report.Summary = fmt.Sprintf("%d finding(s) in %s.", len(kept), target.Label)
	}
	return report, nil
}

type passResult struct {
	index int
	text  string
	err   error
}

func runPasses(ctx context.Context, runner Runner, target Target, passes int, opts Options) []passResult {
	results := make([]passResult, passes)
	sem := make(chan struct{}, opts.Concurrency)
	var wg sync.WaitGroup
	for i := range passes {
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				results[i] = passResult{index: i, err: ctx.Err()}
				return
			}
			text, err := runner.Run(ctx, Request{Kind: "review", Prompt: reviewPrompt(target, i, passes), Model: opts.Model})
			results[i] = passResult{index: i, text: text, err: err}
		}()
	}
	wg.Wait()
	return results
}

// ground drops findings that point at code that is not there: a file that
// is neither in the patch nor on disk, or a line past the end of the file.
func ground(target Target, f Finding, known map[string]bool) string {
	full := filepath.Join(target.Root, filepath.FromSlash(f.Path))
	data, err := os.ReadFile(full)
	if err != nil {
		if known[f.Path] {
			// Deleted or not checked out (pull request): the patch is
			// the evidence.
			return ""
		}
		return "file not found in the change or the working tree"
	}
	if target.Kind == KindPR {
		// The checkout may not match the pull request head.
		return ""
	}
	lines := bytes.Count(data, []byte("\n"))
	if len(data) > 0 && data[len(data)-1] != '\n' {
		lines++
	}
	if f.Line > lines {
		return fmt.Sprintf("line %d is past the end of %s (%d lines)", f.Line, f.Path, lines)
	}
	return ""
}

// merge folds findings reported by several passes into one. Two findings
// are the same defect when they sit within three lines of each other in
// one file and describe it with similar words.
func merge(in []Finding) []Finding {
	var out []Finding
	for _, f := range in {
		matched := false
		for i := range out {
			g := &out[i]
			if g.Path != f.Path || abs(g.Line-f.Line) > 3 {
				continue
			}
			if g.Line != f.Line && similarity(g.Title, f.Title) < 0.35 && similarity(g.Scenario, f.Scenario) < 0.35 {
				continue
			}
			if f.Severity.Rank() > g.Severity.Rank() {
				g.Severity = f.Severity
			}
			if len(f.Scenario) > len(g.Scenario) {
				g.Scenario = f.Scenario
			}
			if g.Suggestion == "" {
				g.Suggestion = f.Suggestion
			}
			// Independent agreement raises confidence.
			g.Confidence = min(1, max(g.Confidence, f.Confidence)+0.1)
			g.Passes++
			matched = true
			break
		}
		if !matched {
			out = append(out, f)
		}
	}
	return out
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// similarity is the Jaccard overlap of the lowercase word sets.
func similarity(a, b string) float64 {
	wa, wb := words(a), words(b)
	if len(wa) == 0 || len(wb) == 0 {
		return 0
	}
	shared := 0
	for w := range wa {
		if wb[w] {
			shared++
		}
	}
	return float64(shared) / float64(len(wa)+len(wb)-shared)
}

func words(s string) map[string]bool {
	out := map[string]bool{}
	for _, w := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		if len(w) > 2 {
			out[w] = true
		}
	}
	return out
}

// rank sorts findings by severity, then confidence, then location.
func rank(fs []Finding) {
	sort.SliceStable(fs, func(i, j int) bool {
		a, b := fs[i], fs[j]
		if a.Severity.Rank() != b.Severity.Rank() {
			return a.Severity.Rank() > b.Severity.Rank()
		}
		if a.Confidence != b.Confidence {
			return a.Confidence > b.Confidence
		}
		if a.Path != b.Path {
			return a.Path < b.Path
		}
		return a.Line < b.Line
	})
}

// vote is one verifier's opinion about one finding.
type vote struct {
	verdict    string
	confidence float64
	reason     string
	severity   Severity
}

// verify checks findings with independent verifier requests and returns
// the survivors. A finding survives when no verifier rejects it and at
// least one confirms it. If every verifier for a batch fails to answer,
// the batch is kept unverified and a warning says so: an infrastructure
// failure is not evidence against a finding.
func verify(ctx context.Context, runner Runner, target Target, in []Finding, verifiers int, opts Options, report *Report) []Finding {
	rank(in)
	for i := range in {
		in[i].ID = fmt.Sprintf("F%d", i+1)
	}
	if len(in) > opts.MaxVerify {
		for _, f := range in[opts.MaxVerify:] {
			report.Dropped = append(report.Dropped, Dropped{Path: f.Path, Line: f.Line, Title: f.Title, Reason: "not verified: over the verification limit"})
		}
		in = in[:opts.MaxVerify]
	}

	type job struct {
		batch []Finding
		vote  int
	}
	var jobs []job
	for start := 0; start < len(in); start += opts.BatchSize {
		batch := in[start:min(start+opts.BatchSize, len(in))]
		for v := range verifiers {
			jobs = append(jobs, job{batch: batch, vote: v})
		}
	}

	type jobResult struct {
		answered bool
		votes    map[string]vote
		err      error
	}
	results := make([]jobResult, len(jobs))
	sem := make(chan struct{}, opts.Concurrency)
	var wg sync.WaitGroup
	for i, j := range jobs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				results[i].err = ctx.Err()
				return
			}
			model := opts.VerifyModel
			if model == "" {
				model = opts.Model
			}
			text, err := runner.Run(ctx, Request{Kind: "verify", Prompt: verifyPrompt(target, j.batch, j.vote), Model: model})
			if err != nil {
				results[i].err = err
				return
			}
			parsed, ok := parseVerify(text)
			if !ok {
				results[i].err = errors.New("verifier returned no parsable verdicts")
				return
			}
			results[i].answered = true
			results[i].votes = map[string]vote{}
			for _, v := range parsed.Verdicts {
				var sev Severity
				if strings.TrimSpace(v.Severity) != "" {
					sev = normalizeSeverity(v.Severity)
				}
				results[i].votes[strings.TrimSpace(v.ID)] = vote{
					verdict:    strings.ToLower(strings.TrimSpace(v.Verdict)),
					confidence: normalizeConfidence(float64(v.Confidence)),
					reason:     clip(v.Reason, 400),
					severity:   sev,
				}
			}
		}()
	}
	wg.Wait()
	report.VerifyCalls = len(jobs)

	// Group the answers by finding.
	votes := map[string][]vote{}
	answered := map[string]int{}
	for i, j := range jobs {
		if results[i].err != nil {
			report.Warnings = append(report.Warnings, fmt.Sprintf("verification of %s failed: %v", batchIDs(j.batch), results[i].err))
			continue
		}
		for _, f := range j.batch {
			answered[f.ID]++
			v, ok := results[i].votes[f.ID]
			if !ok {
				v = vote{verdict: "uncertain", reason: "verifier gave no verdict for this finding"}
			}
			votes[f.ID] = append(votes[f.ID], v)
		}
	}

	var out []Finding
	for _, f := range in {
		if answered[f.ID] == 0 {
			f.VerifyNote = "not verified: no verifier answered"
			out = append(out, f)
			continue
		}
		confirmed, rejected := 0, 0
		var conf float64
		var note, rejectNote string
		for _, v := range votes[f.ID] {
			switch v.verdict {
			case "confirmed":
				confirmed++
				conf += v.confidence
				if note == "" {
					note = v.reason
				}
			case "rejected":
				rejected++
				if rejectNote == "" {
					rejectNote = v.reason
				}
			}
		}
		switch {
		case rejected > 0:
			report.Dropped = append(report.Dropped, Dropped{Path: f.Path, Line: f.Line, Title: f.Title, Reason: "rejected by verification: " + rejectNote})
		case confirmed == 0:
			report.Dropped = append(report.Dropped, Dropped{Path: f.Path, Line: f.Line, Title: f.Title, Reason: "verification could not confirm it"})
		default:
			f.Verified = true
			f.VerifyNote = note
			if conf > 0 {
				f.Confidence = (f.Confidence + conf/float64(confirmed)) / 2
			}
			for _, v := range votes[f.ID] {
				if v.verdict == "confirmed" && v.severity != "" && v.severity.Rank() < f.Severity.Rank() {
					// A verifier that downgrades a finding has seen
					// something the reviewer did not; take the lower one.
					f.Severity = v.severity
				}
			}
			out = append(out, f)
		}
	}
	return out
}

func batchIDs(batch []Finding) string {
	ids := make([]string, len(batch))
	for i, f := range batch {
		ids[i] = f.ID
	}
	return strings.Join(ids, ",")
}
