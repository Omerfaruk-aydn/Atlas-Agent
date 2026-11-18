package agent

import (
	"fmt"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/message"
	"github.com/tidwall/gjson"
)

// SubAgentRun is what one delegated run did, reported next to its answer so
// the caller can judge it (how much work it took, what it changed) without
// reading its whole session.
type SubAgentRun struct {
	DurationMs   int64          `json:"duration_ms"`
	Steps        int            `json:"steps"`
	ToolCalls    map[string]int `json:"tool_calls,omitempty"`
	FilesChanged []string       `json:"files_changed,omitempty"`
	// OutsideOwned lists changed files that are not under the run's owned paths.
	OutsideOwned     []string `json:"outside_owned,omitempty"`
	PromptTokens     int64    `json:"prompt_tokens,omitempty"`
	CompletionTokens int64    `json:"completion_tokens,omitempty"`
	CostUSD          float64  `json:"cost_usd,omitempty"`
}

// maxReportedFiles keeps the note to the caller short on a sweeping change.
const maxReportedFiles = 20

// writingTools are the tools whose file_path input names a file they change.
var writingTools = []string{"edit", "multiedit", "write"}

// summarizeRun counts a sub-agent's tool calls and collects the files its
// writing tools named, from the messages of its session.
func summarizeRun(messages []message.Message) SubAgentRun {
	run := SubAgentRun{ToolCalls: map[string]int{}}
	seen := map[string]bool{}
	for _, m := range messages {
		if m.Role != message.Assistant {
			continue
		}
		run.Steps++
		for _, call := range m.ToolCalls() {
			run.ToolCalls[call.Name]++
			if !slices.Contains(writingTools, call.Name) {
				continue
			}
			if path := strings.TrimSpace(gjson.Get(call.Input, "file_path").String()); path != "" && !seen[path] {
				seen[path] = true
				run.FilesChanged = append(run.FilesChanged, path)
			}
		}
	}
	sort.Strings(run.FilesChanged)
	if len(run.ToolCalls) == 0 {
		run.ToolCalls = nil
	}
	return run
}

// note is the line appended to the answer the caller reads. It is empty when
// the run changed nothing: a read-only answer needs no footer.
func (r SubAgentRun) note() string {
	if len(r.FilesChanged) == 0 {
		// Only a shell command wrote anything: still say so when it left its paths.
		if len(r.OutsideOwned) > 0 {
			return fmt.Sprintf("\n\n[Files changed outside its owned paths: %s. Review these before accepting the work.]", strings.Join(r.OutsideOwned, ", "))
		}
		return ""
	}
	files := r.FilesChanged
	more := ""
	if len(files) > maxReportedFiles {
		more = fmt.Sprintf(" and %d more", len(files)-maxReportedFiles)
		files = files[:maxReportedFiles]
	}
	took := (time.Duration(r.DurationMs) * time.Millisecond).Round(time.Second)
	note := fmt.Sprintf("\n\n[Sub-agent changed %d file(s) in %s: %s%s]", len(r.FilesChanged), took, strings.Join(files, ", "), more)
	if len(r.OutsideOwned) > 0 {
		note += fmt.Sprintf("\n[Outside its owned paths: %s. Review these before accepting the work.]", strings.Join(r.OutsideOwned, ", "))
	}
	return note
}

// outsideOwned returns the changed files that are not inside any owned path.
// Owned paths are relative to root; an empty list owns everything. Only files
// named by the edit tools are known here: a shell command can write elsewhere,
// which is what an isolated workspace and its patch review are for.
func outsideOwned(files []string, root string, owned []string) []string {
	if len(owned) == 0 {
		return nil
	}
	clean := func(p string) string {
		return strings.ToLower(path.Clean(strings.ReplaceAll(p, `\`, "/")))
	}
	var out []string
	for _, file := range files {
		rel := file
		if filepath.IsAbs(file) {
			r, err := filepath.Rel(root, file)
			if err != nil {
				out = append(out, file)
				continue
			}
			rel = r
		}
		rel = clean(rel)
		inside := false
		for _, o := range owned {
			o = clean(o)
			if o == "." || rel == o || strings.HasPrefix(rel, o+"/") {
				inside = true
				break
			}
		}
		if !inside {
			out = append(out, file)
		}
	}
	return out
}
