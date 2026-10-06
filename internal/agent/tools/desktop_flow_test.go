package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/computer"
	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/stretchr/testify/require"
)

func desktopFlowTestPlan() DesktopWorkflowParams {
	return DesktopWorkflowParams{Mode: "flow", Flow: &DesktopFlowParams{Nodes: []DesktopFlowNode{
		{ID: "app", Kind: "resolve", Application: "Notepad", Next: "write"},
		{ID: "write", Kind: "operation", WindowRef: "app", Input: ComputerParams{Action: "set_value", Automation: computer.AutomationRequest{Name: "Field", Text: "private document"}}, Checkpoint: computer.AutomationRequest{Name: "Field", Condition: "value", Expected: "private document"}},
	}}}
}

func desktopFlowFixture(t *testing.T, value *string, mutations *int) func(context.Context, fantasy.ToolCall) (fantasy.ToolResponse, error) {
	t.Helper()
	return func(_ context.Context, c fantasy.ToolCall) (fantasy.ToolResponse, error) {
		var p ComputerParams
		require.NoError(t, json.Unmarshal([]byte(c.Input), &p))
		switch p.Action {
		case "windows":
			return fantasy.NewTextResponse(`{"result":[{"window_id":"11","name":"Untitled - Notepad","process_id":17,"process_name":"Notepad.exe","class_name":"Notepad","foreground":true}]}`), nil
		case "find":
			e := desktopElement{ID: "fresh", Name: "Field", Role: "ControlType.Edit", Enabled: true, ValueAvailable: true, Value: *value, Width: 30, Height: 20}
			data, err := json.Marshal(map[string]any{"result": map[string]any{"matches": []desktopElement{e}, "truncated": false}, "screen_origin": computer.Point{}})
			require.NoError(t, err)
			return fantasy.NewTextResponse(string(data)), nil
		case "set_value":
			require.Equal(t, "fresh", p.Automation.ElementID)
			require.Equal(t, "11", p.Automation.WindowID)
			*mutations++
			*value = p.Automation.Text
			return fantasy.NewTextResponse(`{"result":{"action_sent":true}}`), nil
		case "assert":
			data, err := json.Marshal(map[string]any{"window_id": "11", "passed": *value == p.Automation.Expected, "actual": *value})
			require.NoError(t, err)
			return fantasy.NewTextResponse(string(data)), nil
		case "focus":
			return fantasy.NewTextResponse(`{"result":{"focused":true}}`), nil
		default:
			t.Fatalf("Unexpected action %s", p.Action)
			return fantasy.ToolResponse{}, nil
		}
	}
}

func TestDesktopFlowBranchesWithinOneCallWithoutImages(t *testing.T) {
	for _, match := range []bool{true, false} {
		t.Run(map[bool]string{true: "true", false: "false"}[match], func(t *testing.T) {
			p := desktopFlowTestPlan()
			p.Flow.Nodes[0].Next = "branch"
			branch := DesktopFlowNode{ID: "branch", Kind: "branch", WindowRef: "app", Checkpoint: computer.AutomationRequest{Name: "Field", Condition: "value", Expected: "ready"}, Then: "write", Else: "alternate"}
			alternate := p.Flow.Nodes[1]
			alternate.ID, alternate.Input.Automation.Text, alternate.Checkpoint.Expected = "alternate", "other", "other"
			p.Flow.Nodes = []DesktopFlowNode{p.Flow.Nodes[0], branch, p.Flow.Nodes[1], alternate}
			value := "loading"
			if match {
				value = "ready"
			}
			mutations := 0
			r, err := runDesktopWorkflow(t.Context(), p, fantasy.ToolCall{}, desktopFlowFixture(t, &value, &mutations))
			require.NoError(t, err)
			require.False(t, r.IsError, r.Content)
			require.Equal(t, 1, mutations)
			require.Equal(t, map[bool]string{true: "private document", false: "other"}[match], value)
			require.Equal(t, "text", r.Type)
			require.Contains(t, r.Content, `"matched":`)
		})
	}
}

func TestDesktopFlowValidatesEveryNodeBeforeDispatch(t *testing.T) {
	for _, kind := range []string{"cycle", "missing_edge", "duplicate", "unbound", "mixed", "checkpoint", "too_many", "branch_wait"} {
		t.Run(kind, func(t *testing.T) {
			p := desktopFlowTestPlan()
			switch kind {
			case "cycle":
				p.Flow.Nodes[1].Next = "app"
			case "missing_edge":
				p.Flow.Nodes[1].Next = "missing"
			case "duplicate":
				p.Flow.Nodes[1].ID = "app"
			case "unbound":
				p.Flow.Nodes[1].Input.Automation.WindowID = "11"
			case "mixed":
				p.Result = "checkpoints"
			case "checkpoint":
				p.Flow.Nodes[1].Checkpoint.Condition = ""
			case "too_many":
				p.Flow.Nodes = make([]DesktopFlowNode, 65)
			case "branch_wait":
				p.Flow.Nodes[1].Kind, p.Flow.Nodes[1].Input = "branch", ComputerParams{}
				p.Flow.Nodes[1].Checkpoint.WaitMS = 500
			}
			r, err := runDesktopWorkflow(t.Context(), p, fantasy.ToolCall{}, func(context.Context, fantasy.ToolCall) (fantasy.ToolResponse, error) {
				t.Fatal("Invalid plan dispatched a child")
				return fantasy.ToolResponse{}, nil
			})
			require.NoError(t, err)
			require.True(t, r.IsError)
		})
	}
}

func TestDesktopFlowDurableResumeReconcilesReplacementWithoutReplay(t *testing.T) {
	ctx := context.WithValue(t.Context(), SessionIDContextKey, "session")
	dir := t.TempDir()
	store := engineering.NewStore(dir)
	p := desktopFlowTestPlan()
	p.Flow.RunID = "document"
	value, mutations := "empty", 0
	base := desktopFlowFixture(t, &value, &mutations)
	tool := NewToolPipeline(func(ctx context.Context, c fantasy.ToolCall) (fantasy.ToolResponse, error) {
		r, err := base(ctx, c)
		var input ComputerParams
		require.NoError(t, json.Unmarshal([]byte(c.Input), &input))
		if input.Action == "set_value" {
			return fantasy.NewTextErrorResponse("provider disconnected after input"), nil
		}
		if input.Action == "assert" {
			return fantasy.NewTextErrorResponse("read unavailable"), nil
		}
		return r, err
	}, store)
	run := func(tool fantasy.AgentTool) fantasy.ToolResponse {
		data, err := json.Marshal(PipelineParams{Desktop: &p})
		require.NoError(t, err)
		r, err := tool.Run(ctx, fantasy.ToolCall{Input: string(data)})
		require.NoError(t, err)
		return r
	}
	r := run(tool)
	require.True(t, r.IsError)
	require.Equal(t, 1, mutations)
	files, err := filepath.Glob(filepath.Join(store.Dir(), "desktop-runs", "*.json"))
	require.NoError(t, err)
	require.Len(t, files, 1)
	saved, err := os.ReadFile(files[0])
	require.NoError(t, err)
	require.NotContains(t, string(saved), "private document")
	require.NotContains(t, string(saved), "Notepad")
	require.Contains(t, string(saved), `"pending":"write"`)
	p.Flow.Resume = true
	r = run(NewToolPipeline(base, store))
	require.False(t, r.IsError, r.Content)
	require.Equal(t, 1, mutations)
	require.Contains(t, r.Content, "replacement_already_verified")
	r = run(NewToolPipeline(base, store))
	require.False(t, r.IsError, r.Content)
	require.Equal(t, 1, mutations)
	p.Flow.Nodes[1].Checkpoint.Expected = "changed"
	r = run(NewToolPipeline(base, store))
	require.True(t, r.IsError)
	require.Contains(t, r.Content, "invalid_journal")
}
