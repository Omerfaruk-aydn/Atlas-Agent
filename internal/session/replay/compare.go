package replay

import (
	"sort"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/message"
)

// outcomeWidth bounds the excerpt of a session's final answer.
const outcomeWidth = 200

// Side is everything Compare needs to know about one session.
type Side struct {
	ID               string
	Title            string
	Messages         []message.Message
	PromptTokens     int64
	CompletionTokens int64
	Cost             float64
	// Files maps each file the session changed to its final content.
	Files map[string]string
}

// ToolCount is how often a session called a tool and how many results were
// errors.
type ToolCount struct {
	Name   string `json:"name"`
	Calls  int    `json:"calls"`
	Errors int    `json:"errors"`
}

// SideSummary is the per-session half of a comparison.
type SideSummary struct {
	ID               string      `json:"id"`
	Title            string      `json:"title"`
	Messages         int         `json:"messages"`
	Models           []string    `json:"models,omitempty"`
	Tools            []ToolCount `json:"tools,omitempty"`
	ToolCalls        int         `json:"tool_calls"`
	ToolErrors       int         `json:"tool_errors"`
	FilesChanged     int         `json:"files_changed"`
	PromptTokens     int64       `json:"prompt_tokens"`
	CompletionTokens int64       `json:"completion_tokens"`
	Cost             float64     `json:"cost"`
	Finish           string      `json:"finish,omitempty"`
	Outcome          string      `json:"outcome,omitempty"`
}

// Divergence is the first step at which two sessions stop agreeing.
type Divergence struct {
	// CommonSteps is how many leading steps are identical.
	CommonSteps int `json:"common_steps"`
	// A and B describe the first differing step of each, empty when that
	// session ends there.
	A string `json:"a,omitempty"`
	B string `json:"b,omitempty"`
}

// ToolDelta is one tool's call count in each session.
type ToolDelta struct {
	Name string `json:"name"`
	A    int    `json:"a"`
	B    int    `json:"b"`
}

// FileDiff groups changed files by how the two sessions relate on them.
type FileDiff struct {
	OnlyA []string `json:"only_a,omitempty"`
	OnlyB []string `json:"only_b,omitempty"`
	// Differ lists files both changed but ended with different content.
	Differ []string `json:"differ,omitempty"`
	// Same lists files both changed to identical content.
	Same []string `json:"same,omitempty"`
}

// Comparison is the result of Compare.
type Comparison struct {
	A          SideSummary `json:"a"`
	B          SideSummary `json:"b"`
	Divergence Divergence  `json:"divergence"`
	Tools      []ToolDelta `json:"tools,omitempty"`
	Files      FileDiff    `json:"files"`
}

// Compare diffs two sessions: where their messages diverge, how tool use and
// file changes differ, and what each cost and concluded.
func Compare(a, b Side) Comparison {
	sa, ca := summarizeSide(a)
	sb, cb := summarizeSide(b)

	names := map[string]struct{}{}
	for n := range ca {
		names[n] = struct{}{}
	}
	for n := range cb {
		names[n] = struct{}{}
	}
	var deltas []ToolDelta
	for n := range names {
		if ca[n] != cb[n] {
			deltas = append(deltas, ToolDelta{Name: n, A: ca[n], B: cb[n]})
		}
	}
	sort.Slice(deltas, func(i, j int) bool { return deltas[i].Name < deltas[j].Name })

	return Comparison{
		A:          sa,
		B:          sb,
		Divergence: diverge(a.Messages, b.Messages),
		Tools:      deltas,
		Files:      diffFiles(a.Files, b.Files),
	}
}

func summarizeSide(s Side) (SideSummary, map[string]int) {
	calls := map[string]int{}
	errs := map[string]int{}
	callName := map[string]string{}
	models := map[string]struct{}{}
	sum := SideSummary{
		ID:               s.ID,
		Title:            s.Title,
		Messages:         len(s.Messages),
		FilesChanged:     len(s.Files),
		PromptTokens:     s.PromptTokens,
		CompletionTokens: s.CompletionTokens,
		Cost:             s.Cost,
	}
	for i := range s.Messages {
		m := &s.Messages[i]
		if m.Role == message.Assistant && m.Model != "" {
			models[m.Model] = struct{}{}
		}
		for _, c := range m.ToolCalls() {
			calls[c.Name]++
			callName[c.ID] = c.Name
			sum.ToolCalls++
		}
		for _, r := range m.ToolResults() {
			if !r.IsError {
				continue
			}
			name := r.Name
			if name == "" {
				name = callName[r.ToolCallID]
			}
			errs[name]++
			sum.ToolErrors++
		}
	}
	for n := range models {
		sum.Models = append(sum.Models, n)
	}
	sort.Strings(sum.Models)
	for n, c := range calls {
		sum.Tools = append(sum.Tools, ToolCount{Name: n, Calls: c, Errors: errs[n]})
	}
	sort.Slice(sum.Tools, func(i, j int) bool {
		if sum.Tools[i].Calls != sum.Tools[j].Calls {
			return sum.Tools[i].Calls > sum.Tools[j].Calls
		}
		return sum.Tools[i].Name < sum.Tools[j].Name
	})
	for i := len(s.Messages) - 1; i >= 0; i-- {
		m := &s.Messages[i]
		if m.Role != message.Assistant {
			continue
		}
		if sum.Finish == "" {
			sum.Finish = string(m.FinishReason())
		}
		if text := strings.Join(strings.Fields(m.Content().Text), " "); text != "" {
			sum.Outcome = truncate(text, outcomeWidth)
			break
		}
	}
	return sum, calls
}

// signature identifies a message by what it said and did, ignoring IDs and
// timestamps, which differ between a session and its branch.
func signature(m *message.Message) string {
	var sb strings.Builder
	sb.WriteString(string(m.Role))
	sb.WriteByte('\x00')
	sb.WriteString(m.Content().Text)
	for _, c := range m.ToolCalls() {
		sb.WriteString("\x00call:" + c.Name + ":" + c.Input)
	}
	for _, r := range m.ToolResults() {
		sb.WriteString("\x00result:" + r.Name + ":" + r.Content)
	}
	return sb.String()
}

func diverge(a, b []message.Message) Divergence {
	n := 0
	for n < len(a) && n < len(b) && signature(&a[n]) == signature(&b[n]) {
		n++
	}
	d := Divergence{CommonSteps: n}
	if n < len(a) {
		d.A = describeStep(&a[n])
	}
	if n < len(b) {
		d.B = describeStep(&b[n])
	}
	return d
}

func describeStep(m *message.Message) string {
	return string(m.Role) + ": " + summarize(m)
}

func diffFiles(a, b map[string]string) FileDiff {
	var d FileDiff
	for p, ac := range a {
		bc, ok := b[p]
		switch {
		case !ok:
			d.OnlyA = append(d.OnlyA, p)
		case ac == bc:
			d.Same = append(d.Same, p)
		default:
			d.Differ = append(d.Differ, p)
		}
	}
	for p := range b {
		if _, ok := a[p]; !ok {
			d.OnlyB = append(d.OnlyB, p)
		}
	}
	sort.Strings(d.OnlyA)
	sort.Strings(d.OnlyB)
	sort.Strings(d.Differ)
	sort.Strings(d.Same)
	return d
}
