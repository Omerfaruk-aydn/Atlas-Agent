package tools

import (
	"encoding/json"

	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
)

// DesktopFocusEvidence exposes the already collected state without retrying focus.
func desktopFocusEvidence(r fantasy.ToolResponse, id, listed string) fantasy.ToolResponse {
	if len(listed) > 32*1024 {
		return r
	}
	var envelope struct {
		Result []desktopWindowInfo `json:"result"`
	}
	if json.Unmarshal([]byte(listed), &envelope) != nil || envelope.Result == nil || len(envelope.Result) >= 500 {
		return r
	}
	evidence := map[string]any{
		"code":                "focus_denied",
		"attempted_window_id": id,
		"input_replayed":      false,
		"fresh_windows":       envelope.Result,
		"guidance":            "Do not repeat focus on the same handle without changed state. Reuse this window list; observe the actual foreground or resolve another verified activation method before new input. Do not click guessed coordinates or assume activation succeeded.",
	}
	data, err := json.Marshal(evidence)
	if err != nil || len(r.Content)+len(data)+32 > 64*1024 {
		return r
	}
	metadata := map[string]any{}
	if r.Metadata != "" {
		if json.Unmarshal([]byte(r.Metadata), &metadata) != nil || metadata == nil {
			metadata = map[string]any{"original_metadata": r.Metadata}
		}
	}
	metadata["desktop_recovery"] = evidence
	encoded, err := json.Marshal(metadata)
	if err != nil {
		return r
	}
	r.Metadata = string(encoded)
	r.Content += "\nDesktop recovery evidence: " + string(data)
	return r
}
