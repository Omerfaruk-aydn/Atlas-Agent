package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/computer"
	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/stretchr/testify/require"
)

func TestDesktopFlowOutcomeWithoutChildMetadata(t *testing.T) {
	t.Parallel()
	p := desktopFlowTestPlan()
	p.Flow.Nodes[1].Input = ComputerParams{Action: "key", Key: "enter"}
	value, mutations := "empty", 0
	base := desktopFlowFixture(t, &value, &mutations)
	r, err := runDesktopWorkflow(t.Context(), p, fantasy.ToolCall{}, func(ctx context.Context, c fantasy.ToolCall) (fantasy.ToolResponse, error) {
		var input ComputerParams
		require.NoError(t, json.Unmarshal([]byte(c.Input), &input))
		if input.Action == "key" {
			return fantasy.NewTextErrorResponse("No native dispatch information after interruption"), nil
		}
		return base(ctx, c)
	})
	require.NoError(t, err)
	require.True(t, r.IsError)
	require.Contains(t, r.Metadata, `"state":"uncertain"`)
	native, ok := ResponseOutcome(r)
	require.True(t, ok, "the whole-call guard needs an outcome even when a child has none")
	require.True(t, native.Mutation)
	require.Equal(t, "unknown", native.Dispatch)
	require.Contains(t, native.Subjects, "desktop:11")
	require.False(t, native.Verified)
}

func TestDesktopFlowTerminalOutcome(t *testing.T) {
	t.Parallel()
	for _, alreadyWritten := range []bool{false, true} {
		t.Run(map[bool]string{false: "write", true: "already_written"}[alreadyWritten], func(t *testing.T) {
			t.Parallel()
			p := desktopFlowTestPlan()
			value, mutations := "empty", 0
			if alreadyWritten {
				value = "private document"
			}
			r, err := runDesktopWorkflow(t.Context(), p, fantasy.ToolCall{}, desktopFlowFixture(t, &value, &mutations))
			require.NoError(t, err)
			require.False(t, r.IsError, r.Content)
			native, ok := ResponseOutcome(r)
			require.True(t, ok)
			require.Equal(t, !alreadyWritten, native.Mutation)
			require.Equal(t, map[bool]string{false: "sent", true: "none"}[alreadyWritten], native.Dispatch)
			require.False(t, native.Verified, "flow termination alone is not evidence for the entire task")
		})
	}
}

func TestDesktopFlowOutcomeDoesNotInheritLastReadSubject(t *testing.T) {
	t.Parallel()
	d := newFieldDesktop(t)
	base := d.invoke
	p := DesktopWorkflowParams{Mode: "flow", Flow: &DesktopFlowParams{Nodes: []DesktopFlowNode{
		{ID: "a", Kind: "resolve", Title: "Main A", Next: "b"},
		{ID: "b", Kind: "resolve", Title: "Main B", Next: "write"},
		fieldWrite("a", "b"),
	}}}
	d.values["22"] = "unexpected"
	r, err := runDesktopWorkflow(t.Context(), p, fantasy.ToolCall{}, func(ctx context.Context, c fantasy.ToolCall) (fantasy.ToolResponse, error) {
		r, err := base(ctx, c)
		var input ComputerParams
		require.NoError(t, json.Unmarshal([]byte(c.Input), &input))
		return withToolOutcome(r, computerOutcome(input.Action, input, r, computerMutates(input.Action))), err
	})
	require.NoError(t, err)
	require.True(t, r.IsError)
	native, ok := ResponseOutcome(r)
	require.True(t, ok)
	require.Equal(t, []string{"desktop:11"}, native.Subjects, "the failed read in B must not claim the write mutated B")
	require.Equal(t, "unknown", native.Dispatch)
}

func TestDesktopFlowShortRoleSelectors(t *testing.T) {
	t.Parallel()
	for _, role := range []string{"Edit", "edit", "ControlType.Edit", "controltype.edit"} {
		t.Run(role, func(t *testing.T) {
			t.Parallel()
			p := desktopFlowTestPlan()
			p.Flow.Nodes[1].Input.Automation.Role = role
			p.Flow.Nodes[1].Checkpoint.Role = role
			value, mutations := "empty", 0
			r, err := runDesktopWorkflow(t.Context(), p, fantasy.ToolCall{}, desktopFlowFixture(t, &value, &mutations))
			require.NoError(t, err)
			require.False(t, r.IsError, r.Content)
			require.Equal(t, 1, mutations)
			require.Equal(t, "private document", value)
		})
	}
	p := desktopFlowTestPlan()
	p.Flow.Nodes[1].Checkpoint.Role = "textbox"
	r, err := runDesktopWorkflow(t.Context(), p, fantasy.ToolCall{}, func(context.Context, fantasy.ToolCall) (fantasy.ToolResponse, error) {
		t.Fatal("Unknown roles must be rejected before dispatch")
		return fantasy.ToolResponse{}, nil
	})
	require.NoError(t, err)
	require.True(t, r.IsError)
	require.Contains(t, r.Content, "invalid_role")
}

func TestDesktopFlowDiagnosticCropKeepsFailureMetadata(t *testing.T) {
	t.Parallel()
	for _, paused := range []bool{false, true} {
		t.Run(map[bool]string{false: "image", true: "paused_capture"}[paused], func(t *testing.T) {
			t.Parallel()
			original := fantasy.NewTextErrorResponse("checkpoint_unverified: expected content missing")
			original.Metadata = `{"error_code":"checkpoint_unverified","custom_evidence":{"id":"original"}}`
			q := computer.AutomationRequest{WindowID: "11", Name: "Field"}
			window := desktopWindowInfo{ID: "11", ProcessID: 17, ProcessName: "Notepad.exe", WindowClass: "Notepad"}
			value, mutations := "empty", 0
			base := desktopFlowFixture(t, &value, &mutations)
			r := desktopFlowFailureCrop(t.Context(), q, window, original, func(ctx context.Context, input ComputerParams) (fantasy.ToolResponse, error) {
				if input.Action == "capture_region" {
					if paused {
						r := fantasy.NewTextResponse("Waiting for approval")
						r.StopTurn, r.Metadata = true, `{"permission":{"id":"capture-approval"}}`
						return r, nil
					}
					return fantasy.ToolResponse{Type: "image", Data: []byte{4, 5}, MediaType: "image/png", Metadata: `{"capture":"diagnostic"}`}, nil
				}
				data, err := json.Marshal(input)
				require.NoError(t, err)
				return base(ctx, fantasy.ToolCall{Input: string(data)})
			})
			require.True(t, r.IsError)
			require.Contains(t, r.Content, "checkpoint_unverified")
			if paused {
				require.True(t, r.StopTurn)
				require.Contains(t, r.Metadata, "capture-approval")
			} else {
				require.Equal(t, original.Metadata, r.Metadata)
				require.Equal(t, []byte{4, 5}, r.Data)
				require.Equal(t, "image/png", r.MediaType)
			}
		})
	}
}

func TestDesktopFlowMetadataKeepsInvalidAndCollidingEvidence(t *testing.T) {
	t.Parallel()
	child := `{"flow":{"id":"child"},"child_flow":{"id":"grandchild"},"atlas_outcome":"unreadable","child_atlas_outcome":{"dispatch":"none"},"question":{"id":"q"}}`
	r := fantasy.NewTextResponse("Interrupted")
	r.Metadata = desktopFlowMetadata(child, []byte(`{"pending":"press"}`), ToolOutcome{Dispatch: "unknown", Mutation: true, Subjects: []string{"desktop:11"}})
	native, ok := ResponseOutcome(r)
	require.True(t, ok)
	require.Equal(t, "unknown", native.Dispatch)
	var meta map[string]json.RawMessage
	require.NoError(t, json.Unmarshal([]byte(r.Metadata), &meta))
	require.JSONEq(t, `"unreadable"`, string(meta["child_atlas_outcome"]))
	require.JSONEq(t, `{"id":"child"}`, string(meta["child_flow"]))
	require.JSONEq(t, `{"id":"q"}`, string(meta["question"]))
	var parent string
	require.NoError(t, json.Unmarshal(meta["flow_metadata_parent"], &parent))
	require.Equal(t, child, parent, "colliding backups remain recoverable without dropping interaction metadata")
}
