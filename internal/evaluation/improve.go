package evaluation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
)

// The improvement loop closes the circle between real failures and harness
// changes: failures from the corpus become hard bench tasks, a candidate
// harness variant (a prompt, gate or option set expressed as an Atlas config
// fragment) runs against the baseline on them with repeats, and the result is
// written down as a proposal. The loop never applies a change. A human reads
// the proposal, decides, and edits the configuration themselves.

const (
	// ImproveBaseline and ImproveCandidate name the two variants in records.
	ImproveBaseline  = "baseline"
	ImproveCandidate = "candidate"

	// DefaultImproveRepeats is how often each task runs per variant.
	DefaultImproveRepeats = 3

	proposalVersion = 1
)

// ImproveOptions describe one A/B run.
type ImproveOptions struct {
	Suite string
	Model string
	// Tasks restricts the run to these task IDs; empty means the whole suite.
	Tasks []string
	// BaselineConfig and CandidateConfig are Atlas config fragments (JSON
	// objects) written to .atlas.json in each workspace. The baseline may be
	// empty; the candidate may not.
	BaselineConfig  json.RawMessage
	CandidateConfig json.RawMessage
	Repeats         int
	TimeoutSeconds  int
	AllowTools      bool
	// Program is the Atlas executable the bench runs. Output is where the
	// raw bench records are written.
	Program string
	Output  string
	// Runner replaces process execution; tests use it with mock agents.
	Runner CommandRunner
}

// ImproveResult is the outcome of RunImprove.
type ImproveResult struct {
	Records    []BenchRecord
	Comparison ABComparison
}

// Manifest returns the bench manifest the options describe.
func (o ImproveOptions) Manifest() BenchManifest {
	repeats := o.Repeats
	if repeats == 0 {
		repeats = DefaultImproveRepeats
	}
	return BenchManifest{
		Suite: o.Suite,
		Model: o.Model,
		Tasks: o.Tasks,
		Variants: []BenchVariant{
			{Name: ImproveBaseline, Config: o.BaselineConfig},
			{Name: ImproveCandidate, Config: o.CandidateConfig},
		},
		Repeats:        repeats,
		TimeoutSeconds: o.TimeoutSeconds,
	}
}

// Validate checks the options before any model is run.
func (o ImproveOptions) Validate() error {
	if len(bytes.TrimSpace(o.CandidateConfig)) == 0 {
		return errors.New("a candidate config is required: the candidate must differ from the baseline")
	}
	if bytes.Equal(compactJSON(o.BaselineConfig), compactJSON(o.CandidateConfig)) {
		return errors.New("the candidate config is identical to the baseline, so there is nothing to compare")
	}
	tasks, err := LoadBenchSuite(o.Suite, o.Tasks)
	if err != nil {
		return err
	}
	return ValidateBench(o.Manifest(), tasks)
}

func compactJSON(raw json.RawMessage) []byte {
	var out bytes.Buffer
	if err := json.Compact(&out, raw); err != nil {
		return bytes.TrimSpace(raw)
	}
	return out.Bytes()
}

// RunImprove runs the baseline and the candidate on the suite and compares
// them. It spends model tokens through the bench; nothing is applied.
func RunImprove(ctx context.Context, o ImproveOptions) (ImproveResult, error) {
	if err := o.Validate(); err != nil {
		return ImproveResult{}, err
	}
	records, err := RunBench(ctx, o.Manifest(), o.Program, o.Output, o.AllowTools, o.Runner)
	result := ImproveResult{Records: records, Comparison: CompareVariants(records, ImproveBaseline, ImproveCandidate)}
	return result, err
}

// Proposal is the file an improvement run leaves for a human to review.
type Proposal struct {
	Version     int       `json:"version"`
	GeneratedAt time.Time `json:"generated_at"`
	Model       string    `json:"model"`
	Suite       string    `json:"suite"`
	Repeats     int       `json:"repeats"`
	// Applied is always false: the loop never changes configuration.
	Applied         bool            `json:"applied"`
	BaselineConfig  json.RawMessage `json:"baseline_config,omitempty"`
	CandidateConfig json.RawMessage `json:"candidate_config"`
	Mined           []MinedTask     `json:"mined,omitempty"`
	Comparison      ABComparison    `json:"comparison"`
	Recommendation  string          `json:"recommendation"`
	Limits          []string        `json:"limits"`
}

// BuildProposal turns a finished run into a proposal.
func BuildProposal(o ImproveOptions, mined []MinedTask, r ImproveResult, now time.Time) Proposal {
	m := o.Manifest()
	return Proposal{
		Version:         proposalVersion,
		GeneratedAt:     now.UTC(),
		Model:           o.Model,
		Suite:           o.Suite,
		Repeats:         m.Repeats,
		BaselineConfig:  o.BaselineConfig,
		CandidateConfig: o.CandidateConfig,
		Mined:           mined,
		Comparison:      r.Comparison,
		Recommendation:  recommendation(r.Comparison),
		Limits: []string{
			"Results hold for this model, these tasks and these repeats only.",
			"Mined tasks have no reference solution; the check is only known to fail at the start, not to be passable.",
			"Hidden tests derived from a failed run were not written by a person and may be wrong; review them before trusting a loss.",
			"Nothing was applied. Copy the candidate config into your atlasrc or atlas.json yourself if you accept it.",
		},
	}
}

func recommendation(c ABComparison) string {
	switch c.Verdict {
	case VerdictImproved:
		return fmt.Sprintf("Evidence favors the candidate: %d of %d paired tasks improved against %d worse (sign test p=%.3f, score %+.1f, 95%% interval %+.1f to %+.1f). Review the warnings, then adopt it manually if the cost and the limits are acceptable.", c.Wins, c.PairedTasks, c.Losses, c.SignTestP, c.ScoreDelta, c.DeltaLow, c.DeltaHigh)
	case VerdictRegressed:
		return fmt.Sprintf("Do not adopt the candidate: it did worse on %d of %d paired tasks against %d better (sign test p=%.3f, score %+.1f).", c.Losses, c.PairedTasks, c.Wins, c.SignTestP, c.ScoreDelta)
	case VerdictNoData:
		return "No comparison was possible. Fix the suite or the model setup and run again."
	default:
		return fmt.Sprintf("Inconclusive: the data do not separate the candidate from the baseline (%d better, %d worse, %d tied; sign test p=%.3f). Do not adopt on this evidence; add tasks or repeats and run again.", c.Wins, c.Losses, c.Ties, c.SignTestP)
	}
}

// WriteProposal writes the proposal as indented JSON.
func WriteProposal(path string, p Proposal) error {
	encoded, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	return engineering.AtomicWrite(path, append(encoded, '\n'))
}

// FormatProposal renders the proposal for the terminal.
func FormatProposal(p Proposal) string {
	var sb strings.Builder
	c := p.Comparison
	fmt.Fprintf(&sb, "Verdict: %s\n%s\n\n", strings.ToUpper(string(c.Verdict)), p.Recommendation)
	fmt.Fprintf(&sb, "%-10s %6s %7s %9s %17s %6s %9s %10s %6s\n", "variant", "valid", "passed", "pass rate", "95% interval", "score", "run-to-run", "cost/run", "flaky")
	for _, s := range []VariantStats{c.Baseline, c.Candidate} {
		fmt.Fprintf(&sb, "%-10s %6d %7d %8.0f%% %7.0f%% - %4.0f%% %6.1f %8.0f pt %10.4f %6d\n", s.Variant, s.Valid, s.Passed, s.PassRate*100, s.PassLow*100, s.PassHigh*100, s.MeanScore, s.RepeatStdDev*100, s.MeanCost, s.FlakyTasks)
	}
	fmt.Fprintf(&sb, "\nPaired tasks: %d (%d better, %d worse, %d tied); sign test p=%.3f; score %+.1f (95%% interval %+.1f to %+.1f); cost %+.0f%%, tokens %+.0f%%.\n", c.PairedTasks, c.Wins, c.Losses, c.Ties, c.SignTestP, c.ScoreDelta, c.DeltaLow, c.DeltaHigh, c.CostDelta, c.TokensDelta)
	for _, d := range c.Deltas {
		fmt.Fprintf(&sb, "  %-32s baseline %3.0f%%  candidate %3.0f%%\n", d.Task, d.BaselineRate*100, d.CandidateRate*100)
	}
	if len(c.Warnings) > 0 {
		sb.WriteString("\nWarnings:\n")
		for _, w := range c.Warnings {
			sb.WriteString("- " + w + "\n")
		}
	}
	sb.WriteString("\nNothing was applied.\n")
	return sb.String()
}

// FormatMined renders the mining outcome for the terminal.
func FormatMined(mined []MinedTask) string {
	var sb strings.Builder
	valid := 0
	for _, m := range mined {
		status := "ok      "
		detail := strings.Join(m.Reasons, "; ")
		if m.Valid {
			valid++
		} else {
			status = "rejected"
			detail = m.Rejected
		}
		fmt.Fprintf(&sb, "%s hardness %2d  %s  %s\n", status, m.Hardness, m.ID, detail)
	}
	fmt.Fprintf(&sb, "%d of %d corpus cases are valid red-at-start tasks.\n", valid, len(mined))
	return sb.String()
}
