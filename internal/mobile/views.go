package mobile

import (
	"encoding/json"
	"os"
	"runtime"
	"strings"
	"unicode/utf8"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/diff"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/message"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/permission"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/session"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/vault"
)

// Limits keep phone payloads small; the computer keeps the full data.
const (
	maxDiffBytes    = 12 << 10
	maxCommandBytes = 2 << 10
	maxPathBytes    = 512
	maxTodoBytes    = 300
)

// computerOS names the operating system for display only.
func computerOS() string {
	switch runtime.GOOS {
	case "windows":
		return "Windows"
	case "darwin":
		return "macOS"
	case "linux":
		return "Linux"
	default:
		return runtime.GOOS
	}
}

// computerInfo is shown to an already paired phone. The hostname is the
// name the owner gave this computer; it is never sent before pairing.
func computerInfo() map[string]string {
	name, _ := os.Hostname()
	return map[string]string{"name": truncateUTF8(strings.TrimSpace(name), 64), "os": computerOS()}
}

// truncateUTF8 shortens s to at most n bytes without splitting a rune.
func truncateUTF8(s string, n int) string {
	if len(s) <= n {
		return s
	}
	s = s[:n]
	for !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s + "…"
}

// todoViews exposes the task's real step list for the activity panel.
func todoViews(s session.Session) []map[string]string {
	out := make([]map[string]string, 0, len(s.Todos))
	for _, t := range s.Todos {
		out = append(out, map[string]string{
			"content":     truncateUTF8(vault.Redact(s.ID, t.Content), maxTodoBytes),
			"active_form": truncateUTF8(vault.Redact(s.ID, t.ActiveForm), maxTodoBytes),
			"status":      string(t.Status),
		})
	}
	return out
}

// toolCallViews names each tool call and, when its input names one, the
// file it works on. Other input stays on the computer.
func toolCallViews(m message.Message) []map[string]any {
	var out []map[string]any
	for _, tc := range m.ToolCalls() {
		v := map[string]any{"name": tc.Name, "finished": tc.Finished}
		var in map[string]any
		if json.Unmarshal([]byte(tc.Input), &in) == nil {
			for _, key := range []string{"file_path", "path"} {
				if p, ok := in[key].(string); ok && p != "" {
					v["path"] = truncateUTF8(vault.Redact(m.SessionID, p), maxPathBytes)
					break
				}
			}
		}
		out = append(out, v)
	}
	return out
}

// permissionDetails adds what the request really changes: a unified diff
// for file edits and the command for shell calls. Nothing is synthesized
// when the tool did not supply the content.
func permissionDetails(p permission.PermissionRequest, view map[string]any) {
	data, err := json.Marshal(p.Params)
	if err != nil {
		return
	}
	var params map[string]any
	if json.Unmarshal(data, &params) != nil {
		return
	}
	str := func(key string) string { v, _ := params[key].(string); return v }
	if fp := str("file_path"); fp != "" {
		view["file_path"] = truncateUTF8(vault.Redact(p.SessionID, fp), maxPathBytes)
	}
	_, hasOld := params["old_content"]
	_, hasNew := params["new_content"]
	if hasOld || hasNew {
		name := str("file_path")
		if name == "" {
			name = p.Path
		}
		unified, additions, removals := diff.GenerateDiff(str("old_content"), str("new_content"), name)
		if unified != "" {
			view["diff"] = truncateUTF8(vault.Redact(p.SessionID, unified), maxDiffBytes)
			view["additions"], view["removals"] = additions, removals
		}
	}
	if cmd := str("command"); cmd != "" {
		view["command"] = truncateUTF8(permission.RedactForDisplay(vault.Redact(p.SessionID, cmd)), maxCommandBytes)
	}
}
