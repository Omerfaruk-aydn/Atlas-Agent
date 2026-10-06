package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/stretchr/testify/require"
)

func TestDesktopPrepareReturnsFreshEvidenceAfterFocusDenied(t *testing.T) {
	for _, explicit := range []bool{false, true} {
		t.Run(map[bool]string{false: "automatic", true: "explicit"}[explicit], func(t *testing.T) {
			p := DesktopWorkflowParams{Mode: "prepare", Application: "Notepad"}
			if explicit {
				p.WindowID = "11"
			}
			var actions []string
			lists := 0
			r, err := runDesktopWorkflow(t.Context(), p, fantasy.ToolCall{}, func(_ context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
				var input ComputerParams
				require.NoError(t, json.Unmarshal([]byte(call.Input), &input))
				actions = append(actions, input.Action)
				if input.Action == "windows" {
					lists++
					if lists == 1 {
						return fantasy.NewTextResponse(`{"result":[{"window_id":"11","name":"Adsız","process_name":"notepad.exe"}]}`), nil
					}
					return fantasy.NewTextResponse(`{"result":[{"window_id":"11","name":"Adsız","process_name":"notepad.exe"},{"window_id":"22","name":"Program Manager","foreground":true}]}`), nil
				}
				require.Equal(t, "focus", input.Action)
				return fantasy.ToolResponse{Content: "focus_denied: foreground=22", IsError: true, Metadata: `{"original":"retained"}`}, nil
			})
			require.NoError(t, err)
			require.True(t, r.IsError)
			require.Equal(t, []string{"windows", "focus", "windows"}, actions)
			require.Contains(t, r.Content, `"foreground":true`)
			require.Contains(t, r.Content, `"window_id":"22"`)
			require.Contains(t, r.Content, "Do not repeat focus")
			require.Contains(t, r.Metadata, `"original":"retained"`)
			require.Contains(t, r.Metadata, `"input_replayed":false`)
		})
	}
}

func TestDesktopFocusEvidenceRejectsInvalidOrExcessiveLists(t *testing.T) {
	for _, listed := range []string{`{`, `{"result":null}`, strings.Repeat("x", 32*1024+1)} {
		r := fantasy.ToolResponse{Content: "focus_denied: fixture", IsError: true, Metadata: `{"original":"retained"}`}
		require.Equal(t, r, desktopFocusEvidence(r, "11", listed))
	}
}
