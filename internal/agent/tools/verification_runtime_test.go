package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"os"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/stretchr/testify/require"
)

func TestVerifyReadsExitStatusAndStopsAtFailure(t *testing.T) {
	s := engineering.NewStore(t.TempDir())
	calls := 0
	invoke := func(ctx context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
		calls++
		return fantasy.WithResponseMetadata(fantasy.NewTextResponse("looks successful"), BashResponseMetadata{ExitCode: new(1)}), nil
	}
	tool := NewVerifyTool(t.TempDir(), s, invoke)
	ctx := context.WithValue(t.Context(), SessionIDContextKey, "s")
	input, _ := json.Marshal(VerifyParams{Action: "run", Checks: []VerificationStep{{Name: "first", Tool: "bash", Input: json.RawMessage(`{"command":"false"}`)}, {Name: "second", Tool: "bash", Input: json.RawMessage(`{"command":"true"}`)}}})
	resp, err := tool.Run(ctx, fantasy.ToolCall{ID: "check", Name: "verify", Input: string(input)})
	require.NoError(t, err)
	require.True(t, resp.IsError)
	require.Equal(t, 1, calls)
	state, err := s.Read(ctx, "s")
	require.NoError(t, err)
	require.Len(t, state.Checks, 1)
	require.False(t, state.Checks[0].Passed)
	require.False(t, ToolSucceeded("bash", fantasy.NewTextResponse("PASS"), nil))
	require.False(t, ToolSucceeded("test_run", fantasy.WithResponseMetadata(fantasy.NewTextResponse("PASS"), TestRunResponseMetadata{OK: false}), nil))
}

func TestUIVerifyBindsAnActualImageToTheCurrentSource(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	_, err := engineering.Git(t.Context(), root, "init")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(root+"/page.txt", []byte("source"), 0o644))
	store := engineering.NewStore(t.TempDir())
	var imageData bytes.Buffer
	require.NoError(t, png.Encode(&imageData, image.NewRGBA(image.Rect(0, 0, 2, 2))))
	invoke := func(ctx context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
		var p BrowserParams
		require.NoError(t, json.Unmarshal([]byte(call.Input), &p))
		if p.Action == "screenshot" {
			return fantasy.NewImageResponse(imageData.Bytes(), "image/png"), nil
		}
		return fantasy.NewTextResponse("observed"), nil
	}
	ctx := context.WithValue(t.Context(), SessionIDContextKey, "session")
	response, err := NewUIVerifyTool(store, invoke, root).Run(ctx, fantasy.ToolCall{ID: "capture", Input: `{"url":"http://localhost:3000","width":375,"height":800}`})
	require.NoError(t, err)
	require.False(t, response.IsError)
	state, err := store.Read(ctx, "session")
	require.NoError(t, err)
	require.Len(t, state.UIEvidence, 1)
	require.NotEmpty(t, state.UIEvidence[0].SourceFingerprint)
	require.NoError(t, engineering.ValidateUIArtifact(ctx, state.UIEvidence[0]))
	mutating := func(ctx context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
		require.NoError(t, os.WriteFile(root+"/page.txt", []byte("changed"), 0o644))
		return invoke(ctx, call)
	}
	response, err = NewUIVerifyTool(store, mutating, root).Run(ctx, fantasy.ToolCall{ID: "changed", Input: `{"url":"http://localhost:3000"}`})
	require.NoError(t, err)
	require.True(t, response.IsError)
	require.Contains(t, response.Content, "source changed")
}

func TestVerifyDiscoveryUsesPermittedShellWhenQualityToolsAreDisabled(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	require.NoError(t, os.WriteFile(root+"/go.mod", []byte("module fixture\n"), 0o644))
	tool := NewVerifyTool(root, engineering.NewStore(t.TempDir()), nil, []string{"bash"})
	resp, err := tool.Run(t.Context(), fantasy.ToolCall{ID: "plan", Input: `{"action":"plan"}`})
	require.NoError(t, err)
	var checks []VerificationStep
	require.NoError(t, json.Unmarshal([]byte(resp.Content), &checks))
	require.Len(t, checks, 3)
	for _, check := range checks {
		require.Equal(t, "bash", check.Tool)
	}
	var p BashParams
	require.NoError(t, json.Unmarshal(checks[1].Input, &p))
	require.Equal(t, "go test -count=1 ./...", p.Command)
}

func TestUIVerifyRunsActualAssertionsAndSavesScreenshot(t *testing.T) {
	s := engineering.NewStore(t.TempDir())
	var actions []string
	invoke := func(ctx context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
		var p BrowserParams
		require.NoError(t, json.Unmarshal([]byte(call.Input), &p))
		actions = append(actions, p.Action)
		if p.Action == "screenshot" {
			return fantasy.NewImageResponse([]byte("png-fixture"), "image/png"), nil
		}
		if p.Action == "eval" {
			return fantasy.NewTextResponse("true"), nil
		}
		return fantasy.NewTextResponse("observed"), nil
	}
	ctx := context.WithValue(t.Context(), SessionIDContextKey, "s")
	params := UIVerifyParams{URL: "http://localhost:3000", Width: 375, Height: 800, Steps: []BrowserParams{{Action: "key", Key: "tab"}}, Assertions: []UIAssertion{{Selector: "button", Focused: true, Visible: true}}}
	input, _ := json.Marshal(params)
	resp, err := NewUIVerifyTool(s, invoke).Run(ctx, fantasy.ToolCall{ID: "ui", Name: "ui_verify", Input: string(input)})
	require.NoError(t, err)
	require.False(t, resp.IsError)
	require.Equal(t, []string{"cdp", "navigate", "key", "eval", "snapshot", "console", "screenshot"}, actions)
	var report struct {
		Screenshot               string `json:"screenshot"`
		VisualInspectionRequired bool   `json:"visual_inspection_required"`
	}
	require.NoError(t, json.Unmarshal([]byte(resp.Content), &report))
	require.True(t, report.VisualInspectionRequired)
	data, err := os.ReadFile(report.Screenshot)
	require.NoError(t, err)
	require.Equal(t, resp.Data, data)
	failed := NewUIVerifyTool(s, func(context.Context, fantasy.ToolCall) (fantasy.ToolResponse, error) {
		return fantasy.ToolResponse{}, errors.New("browser unavailable")
	})
	resp, err = failed.Run(ctx, fantasy.ToolCall{ID: "missing", Name: "ui_verify", Input: string(input)})
	require.NoError(t, err)
	require.True(t, resp.IsError)
}
