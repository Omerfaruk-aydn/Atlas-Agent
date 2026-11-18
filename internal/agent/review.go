package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/subagents"
)

// A review is a second look at the changes a turn made. It is done by a
// separate agent that is read-only by construction (it is handed only
// reading tools, so it cannot edit a file or run a command whatever it is
// told), in a scratch session that starts empty. It sees the task that was
// asked and the files that changed, never the author's reasoning, so it
// judges the code and not the story told about it. The scratch session is
// deleted afterwards; what comes back is a list of findings.

const (
	// reviewRole names the model role that may route reviews to a model of
	// their own (options.model_roles.review). Without one they run on the
	// task agent's model.
	reviewRole = "review"

	// ReviewMaxPaths bounds the files named in one review.
	ReviewMaxPaths = 60

	// ReviewTaskMaxLength bounds the task text, in bytes. It is the request
	// the changes answer, kept for intent, not as an essay.
	ReviewTaskMaxLength = 6000

	// reviewTimeout ends a review that never finishes. The caller's own
	// cancellation still applies and ends it sooner.
	reviewTimeout = 15 * time.Minute

	reviewMaxFindings = 40

	maxFindingTitle       = 200
	maxFindingFile        = 500
	maxFindingExplanation = 2500
	maxFindingSuggestion  = 2500
	maxReviewSummary      = 2000
)

// reviewTools is everything a reviewer may use: read the code, its history
// and its structure, never write, run a command or reach the network.
var reviewTools = []string{
	"view", "glob", "grep", "ls",
	"lsp_symbols", "lsp_definition", "lsp_call_hierarchy", "lsp_references",
	"git_status", "git_diff", "git_log", "git_blame",
	"import_graph", "project_map",
}

const reviewInstructions = `You are an independent code reviewer. Another agent has just changed the files you are given. You have not seen its conversation or its reasoning, and you should not want to: judge the code as it stands.

How to work:
1. Look at what changed. Use git_diff (per file, or git_status first). If the project has no version control or a file is new, read it with view.
2. Read around the change: the functions it calls, what calls it (lsp_references, grep), the types and contracts it touches, and the tests that cover it. A change is only right in the context it lives in.
3. Look for what a careful maintainer would reject: logic errors and wrong edge cases, unhandled errors, off-by-one and nil/undefined cases, broken contracts with callers, security problems (injection, unsafe paths, secrets, missing validation), concurrency and resource leaks, data loss, and behaviour that the change adds without any test.
4. Check the change against the task you were given. Say so if it does less than was asked, or something else.

Rules for findings:
- Report only what you can point at. Each finding needs the file and the line in the NEW version of the file, and a concrete way it goes wrong ("with an empty list this indexes element 0"). Do not report guesses, taste, formatting or naming unless it causes a defect.
- Do not praise, summarise the diff or restate what the code does.
- Do not report the same defect twice; merge it into one finding that names every place.
- Severity: "critical" is data loss, a security hole or a crash on a common path; "high" is wrong behaviour on a normal path; "medium" is wrong behaviour on an edge case or a missing test for risky logic; "low" is a minor defect or a hazard; "info" is worth knowing but needs no change.
- You are read-only. Never claim to have changed anything, and do not offer to. Suggest the fix in words (or a short snippet) in "suggestion".
- If you find nothing worth raising, say so plainly: an empty list with a summary of what you checked is a good answer.

Output format. You may write a short note first, but your answer must END with exactly one fenced json block, and nothing after it:

` + "```json" + `
{
  "summary": "One or two sentences: what you checked and the overall verdict.",
  "findings": [
    {
      "severity": "critical | high | medium | low | info",
      "title": "A short headline for the defect",
      "file": "path/relative/to/the/project.go",
      "line": 42,
      "explanation": "What is wrong and the concrete way it fails.",
      "suggestion": "How to fix it."
    }
  ]
}
` + "```"

// ReviewInput is what a review is asked to look at.
type ReviewInput struct {
	// Task is the request the changes answer. It may be empty.
	Task string
	// Paths are the files that changed, as the author's tools named them.
	Paths []string
}

// ReviewFinding is one defect a review found.
type ReviewFinding struct {
	Severity    string
	Title       string
	File        string
	Line        int
	Explanation string
	Suggestion  string
}

// ReviewOutput is the outcome of a review.
type ReviewOutput struct {
	Summary  string
	Findings []ReviewFinding
	// Model is the model that wrote it, for display.
	Model string
	// Structured is false when the reviewer's answer could not be read as
	// the findings format; Summary then holds its text as written.
	Structured bool
}

// reviewSubagent describes the throwaway agent that reviews.
func reviewSubagent() *subagents.Subagent {
	return &subagents.Subagent{
		Name:         reviewRole,
		Description:  "Reviews the changes a turn made, read-only, and reports findings.",
		ReadOnly:     true,
		Tools:        reviewTools,
		Instructions: reviewInstructions,
	}
}

// ReviewChanges reviews the given files in a separate read-only context.
// The session is only the place to bill cost to and to hang the scratch
// session off: nothing is appended to it, and none of its history is read.
func (c *coordinator) ReviewChanges(ctx context.Context, sessionID string, in ReviewInput) (ReviewOutput, error) {
	in, err := normalizeReviewInput(in)
	if err != nil {
		return ReviewOutput{}, err
	}
	agentCfg, ok := c.cfg.Config().Agents[config.AgentTask]
	if !ok {
		return ReviewOutput{}, errors.New("task agent not configured")
	}
	agent, err := c.buildSubagentSessionAgent(ctx, agentCfg, reviewSubagent())
	if err != nil {
		return ReviewOutput{}, fmt.Errorf("prepare review: %w", err)
	}
	text, model, err := c.runScratch(ctx, agent, scratchRun{
		ParentSessionID: sessionID,
		Label:           "review",
		Title:           "Review",
		Prompt:          buildReviewPrompt(in),
		Timeout:         reviewTimeout,
	})
	if err != nil {
		return ReviewOutput{}, err
	}
	out := ParseReview(text)
	out.Model = model
	return out, nil
}

// normalizeReviewInput trims and bounds a request, and rejects one with
// nothing to look at.
func normalizeReviewInput(in ReviewInput) (ReviewInput, error) {
	seen := map[string]bool{}
	var paths []string
	for _, p := range in.Paths {
		p = strings.TrimSpace(p)
		if p == "" || seen[p] || len(p) > maxFindingFile {
			continue
		}
		seen[p] = true
		paths = append(paths, p)
	}
	if len(paths) == 0 {
		return ReviewInput{}, errors.New("a review needs at least one file")
	}
	if len(paths) > ReviewMaxPaths {
		return ReviewInput{}, fmt.Errorf("a review is limited to %d files", ReviewMaxPaths)
	}
	task := strings.TrimSpace(in.Task)
	if len(task) > ReviewTaskMaxLength {
		cut := ReviewTaskMaxLength
		for cut > 0 && !isRuneStart(task[cut]) {
			cut--
		}
		task = task[:cut] + " [truncated]"
	}
	return ReviewInput{Task: task, Paths: paths}, nil
}

// buildReviewPrompt is the one user message the reviewer receives.
func buildReviewPrompt(in ReviewInput) string {
	var b strings.Builder
	if in.Task != "" {
		b.WriteString("<task>\n")
		b.WriteString(in.Task)
		b.WriteString("\n</task>\n\n")
	} else {
		b.WriteString("<task>\n(No task text was given. Judge the changes on their own.)\n</task>\n\n")
	}
	b.WriteString("<changed_files>\n")
	for _, p := range in.Paths {
		b.WriteString("- ")
		b.WriteString(p)
		b.WriteString("\n")
	}
	b.WriteString("</changed_files>\n\nReview these changes now.")
	return b.String()
}

var reviewFence = regexp.MustCompile("(?s)```(?:json)?\\s*\n(.*?)```")

// ParseReview reads a reviewer's answer. The findings are the last fenced
// JSON object in the text (or the whole text when it is bare JSON). A
// malformed finding is dropped, an unknown severity becomes "medium", and
// every field is bounded, so what reaches the screen is always well formed.
// When no findings object can be read, the text itself is kept as the
// summary and Structured is false, so nothing the reviewer said is lost.
func ParseReview(text string) ReviewOutput {
	raw, ok := lastJSONObject(text)
	if !ok {
		return ReviewOutput{Summary: bound(strings.TrimSpace(text), 6*maxReviewSummary)}
	}
	var envelope struct {
		Summary  string          `json:"summary"`
		Findings json.RawMessage `json:"findings"`
	}
	unstructured := ReviewOutput{Summary: bound(strings.TrimSpace(text), 6*maxReviewSummary)}
	if err := json.Unmarshal([]byte(raw), &envelope); err != nil {
		return unstructured
	}
	// Any object that parses is not a review: the findings array has to be
	// there (an empty one is a clean review), or the answer is kept as text.
	var list []struct {
		Severity    string `json:"severity"`
		Title       string `json:"title"`
		File        string `json:"file"`
		Line        any    `json:"line"`
		Explanation string `json:"explanation"`
		Suggestion  string `json:"suggestion"`
	}
	if len(envelope.Findings) == 0 || json.Unmarshal(envelope.Findings, &list) != nil || list == nil {
		return unstructured
	}
	doc := struct {
		Summary  string
		Findings []struct {
			Severity    string `json:"severity"`
			Title       string `json:"title"`
			File        string `json:"file"`
			Line        any    `json:"line"`
			Explanation string `json:"explanation"`
			Suggestion  string `json:"suggestion"`
		}
	}{Summary: envelope.Summary, Findings: list}
	out := ReviewOutput{Summary: bound(strings.TrimSpace(doc.Summary), maxReviewSummary), Structured: true}
	seen := map[string]bool{}
	for _, f := range doc.Findings {
		finding := ReviewFinding{
			Severity:    normalizeSeverity(f.Severity),
			Title:       bound(strings.TrimSpace(f.Title), maxFindingTitle),
			File:        bound(strings.TrimSpace(f.File), maxFindingFile),
			Line:        lineOf(f.Line),
			Explanation: bound(strings.TrimSpace(f.Explanation), maxFindingExplanation),
			Suggestion:  bound(strings.TrimSpace(f.Suggestion), maxFindingSuggestion),
		}
		if finding.Title == "" && finding.Explanation == "" {
			continue
		}
		if finding.Title == "" {
			finding.Title = bound(finding.Explanation, 80)
		}
		key := finding.File + "\x00" + strconv.Itoa(finding.Line) + "\x00" + strings.ToLower(finding.Title)
		if seen[key] {
			continue
		}
		seen[key] = true
		out.Findings = append(out.Findings, finding)
		if len(out.Findings) == reviewMaxFindings {
			break
		}
	}
	sort.SliceStable(out.Findings, func(i, j int) bool {
		return severityRank(out.Findings[i].Severity) < severityRank(out.Findings[j].Severity)
	})
	return out
}

// lastJSONObject finds the findings object: the last fenced block that holds
// an object, else the text itself when it is an object.
func lastJSONObject(text string) (string, bool) {
	matches := reviewFence.FindAllStringSubmatch(text, -1)
	for i := len(matches) - 1; i >= 0; i-- {
		body := strings.TrimSpace(matches[i][1])
		if strings.HasPrefix(body, "{") && json.Valid([]byte(body)) {
			return body, true
		}
	}
	body := strings.TrimSpace(text)
	if strings.HasPrefix(body, "{") && json.Valid([]byte(body)) {
		return body, true
	}
	return "", false
}

var severities = []string{"critical", "high", "medium", "low", "info"}

func severityRank(s string) int {
	for i, v := range severities {
		if v == s {
			return i
		}
	}
	return len(severities)
}

// normalizeSeverity maps what a model writes onto the five levels.
func normalizeSeverity(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "critical", "blocker", "severe":
		return "critical"
	case "high", "major", "error":
		return "high"
	case "medium", "moderate", "warning", "warn":
		return "medium"
	case "low", "minor":
		return "low"
	case "info", "nit", "note", "suggestion", "informational":
		return "info"
	}
	return "medium"
}

// lineOf reads a line number the model wrote as a number or as text such as
// "42" or "42-50" (the first of a range). Anything else is 0, meaning the
// finding is about the file as a whole.
func lineOf(v any) int {
	switch n := v.(type) {
	case float64:
		if n >= 1 && n < 10_000_000 {
			return int(n)
		}
	case string:
		digits := strings.TrimLeft(strings.TrimSpace(n), "Ll:# ")
		end := 0
		for end < len(digits) && digits[end] >= '0' && digits[end] <= '9' {
			end++
		}
		if end > 0 {
			if v, err := strconv.Atoi(digits[:end]); err == nil && v >= 1 && v < 10_000_000 {
				return v
			}
		}
	}
	return 0
}

// bound cuts s to at most n bytes without splitting a character.
func bound(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := n
	for cut > 0 && !isRuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "…"
}
