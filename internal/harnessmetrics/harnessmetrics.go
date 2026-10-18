// Package harnessmetrics records how the agent harness behaves in real use:
// how often the verification contract sends work back, whether tool search
// comes back empty, whether a shell nudge was followed, when a turn was
// stopped for looping, and so on. The point is to choose the next
// improvement from evidence instead of guesses, without a benchmark: the
// events come from the user's own sessions and never leave the machine.
//
// Events are appended as JSON lines under the data directory. Recording is
// best effort: a failure to write is logged once and never affects a turn.
package harnessmetrics

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

// Kind names one harness event.
type Kind string

const (
	ContractPassed     Kind = "contract_passed"
	ContractSentBack   Kind = "contract_sent_back"
	ContractUnresolved Kind = "contract_unresolved"
	StepBack           Kind = "step_back"
	Alternatives       Kind = "alternatives"
	Regression         Kind = "regression"
	VacuousCheck       Kind = "vacuous_check"
	PlanReviewed       Kind = "plan_reviewed"
	PlanReviewFindings Kind = "plan_review_findings"
	BreakerPass        Kind = "breaker_pass"
	EvaluatorPass      Kind = "evaluator_pass"
	EvaluatorFail      Kind = "evaluator_fail"
	VisualReview       Kind = "visual_review"
	ToolSearchHit      Kind = "tool_search_hit"
	ToolSearchClosest  Kind = "tool_search_closest"
	ToolSearchEmpty    Kind = "tool_search_empty"
	NudgeGiven         Kind = "nudge_given"
	NudgeFollowed      Kind = "nudge_followed"
	LoopStopped        Kind = "loop_stopped"
	Compaction         Kind = "compaction"
	OutputSpilled      Kind = "output_spilled"
	RecipeLearned      Kind = "recipe_learned"

	ChecksWeakened       Kind = "checks_weakened"
	Leftovers            Kind = "leftovers"
	ChecklistAsked       Kind = "checklist_asked"
	AlternativesPrepared Kind = "alternatives_prepared"
	AlternativeWon       Kind = "alternative_won"
	FlakyCheck           Kind = "flaky_check"
	ErrorLearned         Kind = "error_learned"
	ErrorRecalled        Kind = "error_recalled"
	CorpusSaved          Kind = "corpus_saved"
)

const (
	// Dir is the events directory under the data directory.
	Dir = "harness"
	// File is the current events file.
	File = "events.jsonl"
	// maxFileBytes rotates the file so a long-lived project cannot grow it
	// without bound; one previous generation is kept.
	maxFileBytes = 4 << 20
)

// Event is one recorded occurrence.
type Event struct {
	Time    int64             `json:"t"`
	Kind    Kind              `json:"kind"`
	Session string            `json:"session,omitempty"`
	Attrs   map[string]string `json:"attrs,omitempty"`
}

// Recorder appends events for one data directory. The zero value and a nil
// Recorder record nothing.
type Recorder struct {
	dir    func() string
	mu     sync.Mutex
	warned bool
	now    func() time.Time
}

// New returns a recorder that writes under dir(), read on every record so
// a data directory changed by configuration takes effect immediately.
func New(dir func() string) *Recorder {
	return &Recorder{dir: dir, now: time.Now}
}

// Root returns the data directory events are written under, or "".
func (r *Recorder) Root() string {
	if r == nil || r.dir == nil {
		return ""
	}
	return r.dir()
}

// Record appends one event. Attribute values are clipped so a stray long
// string cannot bloat the log.
func (r *Recorder) Record(kind Kind, session string, attrs map[string]string) {
	if r == nil || r.dir == nil {
		return
	}
	root := r.dir()
	if root == "" {
		return
	}
	for key, value := range attrs {
		if len(value) > 200 {
			attrs[key] = value[:200]
		}
	}
	line, err := json.Marshal(Event{Time: r.now().UnixMilli(), Kind: kind, Session: session, Attrs: attrs})
	if err != nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := appendLine(filepath.Join(root, Dir), line); err != nil && !r.warned {
		r.warned = true
		slog.Warn("Failed to record harness metrics; further failures are not logged", "error", err)
	}
}

func appendLine(dir string, line []byte) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(dir, File)
	if info, err := os.Stat(path); err == nil && info.Size() > maxFileBytes {
		if err := os.Rename(path, filepath.Join(dir, "events.1.jsonl")); err != nil {
			return err
		}
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	_, writeErr := f.Write(append(line, '\n'))
	return errors.Join(writeErr, f.Close())
}

// Load reads the events under root recorded at or after since, oldest
// first, including the previous rotated generation. Malformed lines are
// skipped: a log cut off by a crash must still be readable.
func Load(root string, since time.Time) ([]Event, error) {
	var events []Event
	for _, name := range []string{"events.1.jsonl", File} {
		f, err := os.Open(filepath.Join(root, Dir, name))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 64<<10), 1<<20)
		for scanner.Scan() {
			var e Event
			if json.Unmarshal(scanner.Bytes(), &e) != nil || e.Kind == "" {
				continue
			}
			if e.Time >= since.UnixMilli() {
				events = append(events, e)
			}
		}
		err = errors.Join(scanner.Err(), f.Close())
		if err != nil {
			return nil, err
		}
	}
	return events, nil
}

// Summary is what the events say about the harness.
type Summary struct {
	Sessions int
	Counts   map[Kind]int
	// TopEmptySearches are the most frequent tool searches that found
	// nothing, the clearest signal of a missing alias or hint.
	TopEmptySearches []string
	// NudgedTools counts nudges per tool.
	NudgedTools map[string]int
	// Checks lists, per project kind, the commands that verified work
	// most often.
	Checks map[string][]string
}

// Summarize aggregates events.
func Summarize(events []Event) Summary {
	s := Summary{Counts: map[Kind]int{}, NudgedTools: map[string]int{}}
	sessions := map[string]bool{}
	empty := map[string]int{}
	for _, e := range events {
		s.Counts[e.Kind]++
		if e.Session != "" {
			sessions[e.Session] = true
		}
		switch e.Kind {
		case ToolSearchEmpty:
			if q := strings.TrimSpace(e.Attrs["query"]); q != "" {
				empty[strings.ToLower(q)]++
			}
		case NudgeGiven:
			s.NudgedTools[e.Attrs["tool"]]++
		}
	}
	s.Sessions = len(sessions)
	s.Checks = map[string][]string{}
	for kind, counts := range checksByKind(events) {
		for _, c := range counts[:min(len(counts), 5)] {
			s.Checks[kind] = append(s.Checks[kind], fmt.Sprintf("%s (%d)", c.command, c.count))
		}
	}
	for query := range empty {
		s.TopEmptySearches = append(s.TopEmptySearches, query)
	}
	slices.SortFunc(s.TopEmptySearches, func(a, b string) int {
		if empty[a] != empty[b] {
			return empty[b] - empty[a]
		}
		return strings.Compare(a, b)
	})
	s.TopEmptySearches = s.TopEmptySearches[:min(len(s.TopEmptySearches), 10)]
	return s
}

// Format renders a summary as a short plain-text report with the ratios
// that point at a next improvement.
func Format(s Summary, since time.Time) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Harness report since %s (%d sessions)\n\n", since.Format("2006-01-02"), s.Sessions)
	if len(s.Counts) == 0 {
		b.WriteString("No harness events recorded yet. They are written as Atlas works, under <data dir>/harness/.\n")
		return b.String()
	}
	c := s.Counts
	section := func(title string) { b.WriteString(title + "\n") }
	line := func(label string, value string) { fmt.Fprintf(&b, "  %-34s %s\n", label, value) }
	count := func(k Kind) string { return fmt.Sprint(c[k]) }

	section("Verification contract")
	line("passed", count(ContractPassed))
	line("sent back", count(ContractSentBack))
	line("unresolved after the round limit", count(ContractUnresolved))
	line("step-back prompts", count(StepBack))
	line("alternative attempts suggested", count(Alternatives))
	line("regressions caught", count(Regression))
	line("checks that could not fail", count(VacuousCheck))
	line("pass rate", ratio(c[ContractPassed], c[ContractPassed]+c[ContractUnresolved]))
	line("alternatives prepared (one passed)", fmt.Sprintf("%d (%d)", c[AlternativesPrepared], c[AlternativeWon]))
	line("flaky checks found", count(FlakyCheck))

	section("\nDiff and request checks")
	line("turns with weakened checks", count(ChecksWeakened))
	line("turns with leftovers", count(Leftovers))
	line("checklist confirmations asked", count(ChecklistAsked))
	line("failed requests saved to corpus", count(CorpusSaved))

	section("\nReviews")
	line("plans reviewed (with findings)", fmt.Sprintf("%d (%d)", c[PlanReviewed], c[PlanReviewFindings]))
	line("breaker passes", count(BreakerPass))
	line("evaluator pass / fail", fmt.Sprintf("%d / %d", c[EvaluatorPass], c[EvaluatorFail]))
	line("visual reviews", count(VisualReview))

	section("\nTool discovery")
	searches := c[ToolSearchHit] + c[ToolSearchClosest] + c[ToolSearchEmpty]
	line("searches", fmt.Sprint(searches))
	line("found nothing", fmt.Sprintf("%d (%s)", c[ToolSearchEmpty], ratio(c[ToolSearchEmpty], searches)))
	line("rescued by closest match", count(ToolSearchClosest))
	line("shell nudges (followed)", fmt.Sprintf("%d (%s)", c[NudgeGiven], ratio(c[NudgeFollowed], c[NudgeGiven])))

	section("\nContext and loops")
	line("compactions", count(Compaction))
	line("turns stopped for looping", count(LoopStopped))
	line("long outputs saved to logs", count(OutputSpilled))
	line("recipes learned", count(RecipeLearned))
	line("error fixes learned (recalled)", fmt.Sprintf("%d (%d)", c[ErrorLearned], c[ErrorRecalled]))

	if len(s.Checks) > 0 {
		section("\nChecks that verified work, by project kind")
		kinds := make([]string, 0, len(s.Checks))
		for kind := range s.Checks {
			kinds = append(kinds, kind)
		}
		slices.Sort(kinds)
		for _, kind := range kinds {
			b.WriteString("  " + kind + ": " + strings.Join(s.Checks[kind], ", ") + "\n")
		}
	}
	if len(s.TopEmptySearches) > 0 {
		section("\nSearches that found nothing (add aliases for these)")
		for _, q := range s.TopEmptySearches {
			b.WriteString("  - " + q + "\n")
		}
	}
	return b.String()
}

func ratio(part, whole int) string {
	if whole == 0 {
		return "n/a"
	}
	return fmt.Sprintf("%.0f%%", 100*float64(part)/float64(whole))
}
