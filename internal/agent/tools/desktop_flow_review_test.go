package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/computer"
	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/stretchr/testify/require"
)

// fieldDesktop answers flow children for two windows of the same application,
// "11" (Main A, foreground) and "22" (Main B). Each window has an Edit
// control named Field and may also have a Text label named Field, so tests
// can place the same name and value in another window or another control.
type fieldDesktop struct {
	t      *testing.T
	mu     sync.Mutex
	values map[string]string // window -> Edit value
	labels map[string]string // window -> Text label value; absent = no label
	ids    map[string]string // window -> Edit element ID
	// windows overrides the window list (for example a restarted window).
	windows string
	writes  []string
	// write, when set, answers set_value instead of the default success.
	write func(input ComputerParams) (fantasy.ToolResponse, error)
	// keyed, when set, answers key/type/hotkey inputs.
	keyed func(input ComputerParams) (fantasy.ToolResponse, error)
	keys  []string
}

func newFieldDesktop(t *testing.T) *fieldDesktop {
	return &fieldDesktop{t: t, values: map[string]string{"11": "", "22": ""}, labels: map[string]string{}, ids: map[string]string{"11": "11:edit", "22": "22:edit"}}
}

func (d *fieldDesktop) controls(window string) []desktopElement {
	out := []desktopElement{{ID: d.ids[window], Name: "Field", Role: "ControlType.Edit", Enabled: true, Patterns: []string{"Value"}, ValueAvailable: true, Value: d.values[window], Width: 30, Height: 20}}
	if label, ok := d.labels[window]; ok {
		out = append(out, desktopElement{ID: window + ":label", Name: "Field", Role: "ControlType.Text", Enabled: true, ValueAvailable: true, Value: label, Width: 30, Height: 20})
	}
	return out
}

func (d *fieldDesktop) matches(q computer.AutomationRequest) []desktopElement {
	var out []desktopElement
	for _, e := range d.controls(q.WindowID) {
		if matchesSelector(e, q) {
			out = append(out, e)
		}
	}
	return out
}

func (d *fieldDesktop) invoke(_ context.Context, c fantasy.ToolCall) (fantasy.ToolResponse, error) {
	var input ComputerParams
	require.NoError(d.t, json.Unmarshal([]byte(c.Input), &input))
	d.mu.Lock()
	defer d.mu.Unlock()
	switch input.Action {
	case "windows":
		if d.windows != "" {
			return fantasy.NewTextResponse(d.windows), nil
		}
		return fantasy.NewTextResponse(`{"result":[{"window_id":"11","name":"Main A","process_id":17,"process_name":"Notepad.exe","class_name":"Notepad","foreground":true},{"window_id":"22","name":"Main B","process_id":18,"process_name":"Notepad.exe","class_name":"Notepad","foreground":false}]}`), nil
	case "focus":
		return fantasy.NewTextResponse(`{"result":{"focused":true}}`), nil
	case "find":
		data, err := json.Marshal(map[string]any{"result": map[string]any{"matches": d.matches(input.Automation), "truncated": false}})
		require.NoError(d.t, err)
		return fantasy.NewTextResponse(string(data)), nil
	case "assert":
		found := d.matches(input.Automation)
		if len(found) != 1 {
			return fantasy.NewTextResponse(`{"passed":false,"reason":"target_missing"}`), nil
		}
		data, err := json.Marshal(map[string]any{"window_id": input.Automation.WindowID, "actual": found[0].Value, "passed": found[0].Value == input.Automation.Expected})
		require.NoError(d.t, err)
		return fantasy.NewTextResponse(string(data)), nil
	case "set_value":
		d.writes = append(d.writes, input.Automation.WindowID+":"+input.Automation.ElementID+"="+input.Automation.Text)
		if d.write != nil {
			return d.write(input)
		}
		d.apply(input)
		return fantasy.NewTextResponse(`{"result":{"action_sent":true}}`), nil
	case "key", "type", "hotkey":
		d.keys = append(d.keys, input.Automation.WindowID+":"+input.Action+":"+input.Key+input.Text)
		if d.keyed != nil {
			return d.keyed(input)
		}
		return fantasy.NewTextResponse("Sent"), nil
	}
	d.t.Fatalf("Unexpected action %s", input.Action)
	return fantasy.ToolResponse{}, nil
}

// apply writes the value to the control the input targeted.
func (d *fieldDesktop) apply(input ComputerParams) {
	if d.ids[input.Automation.WindowID] == input.Automation.ElementID {
		d.values[input.Automation.WindowID] = input.Automation.Text
	}
}

func (d *fieldDesktop) writeCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.writes)
}

func (d *fieldDesktop) keyCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.keys)
}

func fieldWrite(window, checkpoint string) DesktopFlowNode {
	return DesktopFlowNode{ID: "write", Kind: "operation", WindowRef: window, CheckpointRef: checkpoint, Input: ComputerParams{Action: "set_value", Automation: computer.AutomationRequest{Name: "Field", Role: "ControlType.Edit", Text: "private document"}}, Checkpoint: computer.AutomationRequest{Name: "Field", Role: "ControlType.Edit", Condition: "value", Expected: "private document"}}
}

func twoWindowPlan(write DesktopFlowNode) DesktopWorkflowParams {
	return DesktopWorkflowParams{Mode: "flow", Flow: &DesktopFlowParams{Nodes: []DesktopFlowNode{
		{ID: "a", Kind: "resolve", Title: "Main A", Next: "b"},
		{ID: "b", Kind: "resolve", Title: "Main B", Next: "write"},
		write,
	}}}
}

func durableFlowContext(t *testing.T, session string) context.Context {
	t.Helper()
	ctx := context.WithValue(t.Context(), SessionIDContextKey, session)
	return context.WithValue(ctx, desktopFlowStoreKey{}, t.TempDir())
}

// flowJournal reads the journal of a durable run the way resume does.
func flowJournal(t *testing.T, ctx context.Context, runID string) desktopFlowState {
	t.Helper()
	dir := ctx.Value(desktopFlowStoreKey{}).(string)
	data, err := os.ReadFile(filepath.Join(dir, "desktop-runs", engineering.Hash(desktopFlowSession(ctx)+"\x00"+runID)+".json"))
	require.NoError(t, err)
	var state desktopFlowState
	require.NoError(t, json.Unmarshal(data, &state))
	return state
}

func flowStates(t *testing.T, r fantasy.ToolResponse) map[string]string {
	t.Helper()
	var meta struct {
		Flow struct {
			Nodes []map[string]string `json:"nodes"`
		} `json:"flow"`
	}
	require.NoError(t, json.Unmarshal([]byte(r.Metadata), &meta), r.Metadata)
	out := map[string]string{}
	for _, n := range meta.Flow.Nodes {
		out[n["id"]] = n["state"]
	}
	return out
}

// Bug 1: a value in another window must not stand for the write. The input
// is sent to window A; window B's checkpoint is then read, as planned.
func TestDesktopFlowCrossWindowCheckpointNeverStandsForTheWrite(t *testing.T) {
	t.Parallel()
	d := newFieldDesktop(t)
	d.values["22"] = "private document"
	r, err := runDesktopWorkflow(t.Context(), twoWindowPlan(fieldWrite("a", "b")), fantasy.ToolCall{}, d.invoke)
	require.NoError(t, err)
	require.False(t, r.IsError, r.Content)
	require.Equal(t, []string{"11:11:edit=private document"}, d.writes, "the write reached the input window's own control")
	require.Equal(t, "private document", d.values["11"])
	require.Contains(t, r.Content, "not_applicable: checkpoint reads another window")
	require.NotContains(t, r.Content, "replacement_already_verified")
	require.Equal(t, flowNodeVerified, flowStates(t, r)["write"])
}

// A replacement may be skipped only when the checkpoint reads the input's own
// live window and the same control; the same name elsewhere is no evidence.
func TestDesktopFlowReplacementSkipRequiresTheSameWindowAndControl(t *testing.T) {
	t.Parallel()
	t.Run("same binding and control", func(t *testing.T) {
		t.Parallel()
		d := newFieldDesktop(t)
		d.values["11"] = "private document"
		r, err := runDesktopWorkflow(t.Context(), twoWindowPlan(fieldWrite("a", "")), fantasy.ToolCall{}, d.invoke)
		require.NoError(t, err)
		require.False(t, r.IsError, r.Content)
		require.Empty(t, d.writes)
		require.Contains(t, r.Content, "replacement_already_verified")
	})
	t.Run("another binding of the same live window", func(t *testing.T) {
		t.Parallel()
		d := newFieldDesktop(t)
		d.values["11"] = "private document"
		p := twoWindowPlan(fieldWrite("a", "again"))
		p.Flow.Nodes[1] = DesktopFlowNode{ID: "again", Kind: "resolve", Title: "Main A", Next: "write"}
		p.Flow.Nodes[0].Next = "again"
		r, err := runDesktopWorkflow(t.Context(), p, fantasy.ToolCall{}, d.invoke)
		require.NoError(t, err)
		require.False(t, r.IsError, r.Content)
		require.Empty(t, d.writes, "both bindings name the same window, process and class")
	})
	t.Run("same name on another control", func(t *testing.T) {
		t.Parallel()
		d := newFieldDesktop(t)
		d.labels["11"] = "private document"
		write := fieldWrite("a", "")
		write.Checkpoint.Role = "ControlType.Text"
		r, err := runDesktopWorkflow(t.Context(), twoWindowPlan(write), fantasy.ToolCall{}, d.invoke)
		require.NoError(t, err)
		require.False(t, r.IsError, r.Content)
		require.Equal(t, []string{"11:11:edit=private document"}, d.writes, "a label named Field is not the edit control")
		require.NotContains(t, r.Content, "replacement_already_verified")
	})
}

// An uncertain write is never reconciled from another window's value, and a
// resume never replays it.
func TestDesktopFlowOtherWindowEvidenceCannotReconcileUncertainWrite(t *testing.T) {
	t.Parallel()
	ctx := durableFlowContext(t, "cross-window-uncertain")
	d := newFieldDesktop(t)
	d.values["22"] = "private document"
	d.write = func(ComputerParams) (fantasy.ToolResponse, error) {
		return fantasy.NewTextErrorResponse("provider disconnected after input"), nil
	}
	p := twoWindowPlan(fieldWrite("a", "b"))
	p.Flow.RunID = "cross"
	r, err := runDesktopWorkflow(ctx, p, fantasy.ToolCall{}, d.invoke)
	require.NoError(t, err)
	require.True(t, r.IsError, r.Content)
	require.NotContains(t, r.Content, "replacement_reconciled")
	require.Equal(t, flowNodeUncertain, flowStates(t, r)["write"])
	require.Equal(t, "write", flowJournal(t, ctx, "cross").Pending)

	p.Flow.Resume = true
	r, err = runDesktopWorkflow(ctx, p, fantasy.ToolCall{}, d.invoke)
	require.NoError(t, err)
	require.True(t, r.IsError)
	require.True(t, strings.HasPrefix(r.Content, "uncertain_effect:"), r.Content)
	require.Equal(t, 1, d.writeCount(), "the uncertain write was not replayed")
}

// Reconciliation reads the control the write targeted: a replaced control
// with the right value does not prove that the write took effect.
func TestDesktopFlowReconcileRequiresTheTargetedElement(t *testing.T) {
	t.Parallel()
	d := newFieldDesktop(t)
	d.write = func(input ComputerParams) (fantasy.ToolResponse, error) {
		d.apply(input)
		d.ids["11"] = "11:edit-replaced"
		return fantasy.NewTextErrorResponse("provider reported error after applying value"), nil
	}
	r, err := runDesktopWorkflow(t.Context(), twoWindowPlan(fieldWrite("a", "")), fantasy.ToolCall{}, d.invoke)
	require.NoError(t, err)
	require.True(t, r.IsError, r.Content)
	require.NotContains(t, r.Content, "replacement_reconciled")
	require.Equal(t, 1, d.writeCount())
	require.Equal(t, flowNodeUncertain, flowStates(t, r)["write"])
}

// The Save dialog pattern keeps working: the dialog's inputs close it and the
// checkpoint is read in the main document window through checkpoint_ref.
func TestDesktopFlowSaveDialogCheckpointRefStillVerifiesTheMainWindow(t *testing.T) {
	t.Parallel()
	d, sim, ctx, _ := longWorkflowFixture(t, nil)
	plan := `{"desktop":{"mode":"flow","flow":{"nodes":[
 {"id":"editor","kind":"prepare","application":"Not Defteri","next":"write"},
 {"id":"write","kind":"operation","window_ref":"editor","focus":true,"inputs":[{"action":"type","text":"rapor"},{"action":"hotkey","modifiers":"ctrl","key":"s"}],"checkpoint":{"role":"ControlType.Document","name":"Metin düzenleyicisi","condition":"document_text","expected":"rapor"},"next":"dialog"},
 {"id":"dialog","kind":"resolve","title":"Farklı Kaydet","owner_ref":"editor","wait_ms":5000,"next":"name"},
 {"id":"name","kind":"operation","window_ref":"dialog","input":{"action":"set_value","automation":{"name":"Dosya adı:","role":"ControlType.Edit","text":"rapor.txt"}},"checkpoint":{"name":"Dosya adı:","role":"ControlType.Edit","condition":"value","expected":"rapor.txt"},"next":"submit"},
 {"id":"submit","kind":"operation","window_ref":"dialog","input":{"action":"key","key":"enter"},"closes_window":true,"checkpoint_ref":"editor","checkpoint":{"role":"ControlType.Window","name":"rapor - Not Defteri","condition":"visible"}}]}}}`
	resp := sim.pipeline(t, ctx, plan)
	require.False(t, resp.IsError, resp.Content)
	d.mu.Lock()
	defer d.mu.Unlock()
	require.Equal(t, "rapor", d.files[d.desktopDir+`\Atlas-Test\rapor.txt`])
	require.Contains(t, d.inputs, "set_value:rapor.txt")
	for id, state := range nodeStates(flowReportOf(t, resp.Content)) {
		require.Contains(t, []string{flowNodeBound, flowNodeVerified}, state, id)
	}
}

// Bug 2: a halted child keeps its question metadata, its stop contract and
// its content; the flow ledger is added beside it and nothing later runs.
func TestDesktopFlowHaltKeepsQuestionMetadata(t *testing.T) {
	t.Parallel()
	d := newFieldDesktop(t)
	d.keyed = func(ComputerParams) (fantasy.ToolResponse, error) {
		r := fantasy.NewTextResponse("Waiting for the user's answer")
		r.StopTurn = true
		r.Metadata = `{"question":{"id":"q-7","options":[{"id":"yes","label":"Yes"},{"id":"no","label":"No"}]}}`
		return withToolOutcome(r, ToolOutcome{Dispatch: "none", Mutation: true}), nil
	}
	p := DesktopWorkflowParams{Mode: "flow", Flow: &DesktopFlowParams{Nodes: []DesktopFlowNode{
		{ID: "a", Kind: "resolve", Title: "Main A", Next: "ask"},
		{ID: "ask", Kind: "operation", WindowRef: "a", Input: ComputerParams{Action: "key", Key: "enter"}, Checkpoint: computer.AutomationRequest{Name: "Field", Role: "ControlType.Edit", Condition: "value", Expected: "x"}, Next: "write"},
		fieldWrite("a", ""),
	}}}
	r, err := runDesktopWorkflow(t.Context(), p, fantasy.ToolCall{}, d.invoke)
	require.NoError(t, err)
	require.True(t, r.StopTurn)
	require.False(t, r.IsError)
	require.Equal(t, "text", r.Type)
	require.True(t, strings.HasPrefix(r.Content, "Waiting for the user's answer\n"), r.Content)
	var meta struct {
		Question struct {
			ID      string `json:"id"`
			Options []struct {
				ID string `json:"id"`
			} `json:"options"`
		} `json:"question"`
	}
	require.NoError(t, json.Unmarshal([]byte(r.Metadata), &meta))
	require.Equal(t, "q-7", meta.Question.ID)
	require.Len(t, meta.Question.Options, 2)
	states := flowStates(t, r)
	require.Equal(t, flowNodeBlocked, states["ask"])
	require.Equal(t, flowNodeNotRun, states["write"])
	require.Empty(t, d.writes, "no step after the halt ran")
	native, ok := ResponseOutcome(r)
	require.True(t, ok)
	require.Equal(t, "none", native.Dispatch, "nothing reached the desktop in this call")
}

// A permission halt keeps the permission metadata and the error contract.
func TestDesktopFlowHaltKeepsPermissionMetadata(t *testing.T) {
	t.Parallel()
	d := newFieldDesktop(t)
	d.keyed = func(input ComputerParams) (fantasy.ToolResponse, error) {
		r := NewPermissionDeniedResponse(nil)
		r.Metadata = `{"permission":{"id":"perm-3","tool_name":"computer","action":"key"}}`
		return withToolOutcome(r, computerOutcome("key", input, r, false)), nil
	}
	p := DesktopWorkflowParams{Mode: "flow", Flow: &DesktopFlowParams{Nodes: []DesktopFlowNode{
		{ID: "a", Kind: "resolve", Title: "Main A", Next: "press"},
		{ID: "press", Kind: "operation", WindowRef: "a", Input: ComputerParams{Action: "key", Key: "enter"}, Checkpoint: computer.AutomationRequest{Name: "Field", Role: "ControlType.Edit", Condition: "value", Expected: "x"}},
	}}}
	r, err := runDesktopWorkflow(t.Context(), p, fantasy.ToolCall{}, d.invoke)
	require.NoError(t, err)
	require.True(t, r.StopTurn)
	require.True(t, r.IsError)
	require.True(t, strings.HasPrefix(r.Content, "User denied permission"), r.Content)
	var meta struct {
		Permission map[string]string `json:"permission"`
		Flow       map[string]any    `json:"flow"`
	}
	require.NoError(t, json.Unmarshal([]byte(r.Metadata), &meta))
	require.Equal(t, map[string]string{"id": "perm-3", "tool_name": "computer", "action": "key"}, meta.Permission)
	require.NotEmpty(t, meta.Flow["nodes"], "the ledger is available beside the permission metadata")
}

// A halt after an earlier dispatched node keeps the child's media and widens
// the call's outcome: the child sent nothing, the flow call did.
func TestDesktopFlowHaltKeepsMediaAndWidensOutcome(t *testing.T) {
	t.Parallel()
	d := newFieldDesktop(t)
	d.keyed = func(input ComputerParams) (fantasy.ToolResponse, error) {
		r := fantasy.ToolResponse{Type: "image", Content: "Paused", Data: []byte{1, 2, 3}, MediaType: "image/png", StopTurn: true}
		return withToolOutcome(r, computerOutcome("key", input, r, false)), nil
	}
	p := DesktopWorkflowParams{Mode: "flow", Flow: &DesktopFlowParams{Nodes: []DesktopFlowNode{
		{ID: "a", Kind: "resolve", Title: "Main A", Next: "write"},
		func() DesktopFlowNode { n := fieldWrite("a", ""); n.Next = "press"; return n }(),
		{ID: "press", Kind: "operation", WindowRef: "a", Input: ComputerParams{Action: "key", Key: "enter"}, Checkpoint: computer.AutomationRequest{Name: "Field", Role: "ControlType.Edit", Condition: "value", Expected: "x"}},
	}}}
	r, err := runDesktopWorkflow(t.Context(), p, fantasy.ToolCall{}, d.invoke)
	require.NoError(t, err)
	require.True(t, r.StopTurn)
	require.Equal(t, "image", r.Type)
	require.Equal(t, []byte{1, 2, 3}, r.Data)
	require.Equal(t, "image/png", r.MediaType)
	native, ok := ResponseOutcome(r)
	require.True(t, ok)
	require.Equal(t, "sent", native.Dispatch, "the write of an earlier node reached the desktop")
	require.True(t, native.Mutation)
	var meta struct {
		Child ToolOutcome `json:"child_atlas_outcome"`
	}
	require.NoError(t, json.Unmarshal([]byte(r.Metadata), &meta))
	require.Equal(t, "none", meta.Child.Dispatch, "the child's own outcome is kept")
}

// Metadata conflicts never lose data: a child flow key moves aside, text
// metadata is kept verbatim, and a large ledger is shortened, not the child's
// interaction metadata.
func TestDesktopFlowMetadataConflictsAndLargeLedger(t *testing.T) {
	t.Parallel()
	halt := func(metadata string) func(ComputerParams) (fantasy.ToolResponse, error) {
		return func(input ComputerParams) (fantasy.ToolResponse, error) {
			r := fantasy.NewTextResponse("Waiting")
			r.StopTurn, r.Metadata = true, metadata
			return withToolOutcome(r, computerOutcome("key", input, r, false)), nil
		}
	}
	plan := func(verifies int) DesktopWorkflowParams {
		nodes := []DesktopFlowNode{{ID: "a", Kind: "resolve", Title: "Main A", Next: "v0"}}
		for i := range verifies {
			next := fmt.Sprintf("v%d", i+1)
			if i == verifies-1 {
				next = "press"
			}
			nodes = append(nodes, DesktopFlowNode{ID: fmt.Sprintf("v%d", i), Kind: "verify", WindowRef: "a", Checkpoint: computer.AutomationRequest{Name: "Field", Role: "ControlType.Edit", Condition: "value", Expected: "ready"}, Next: next})
		}
		nodes = append(nodes, DesktopFlowNode{ID: "press", Kind: "operation", WindowRef: "a", Input: ComputerParams{Action: "key", Key: "enter"}, Checkpoint: computer.AutomationRequest{Name: "Field", Role: "ControlType.Edit", Condition: "value", Expected: "x"}})
		return DesktopWorkflowParams{Mode: "flow", Flow: &DesktopFlowParams{Nodes: nodes}}
	}

	t.Run("child flow key", func(t *testing.T) {
		t.Parallel()
		d := newFieldDesktop(t)
		d.values["11"] = "ready"
		d.keyed = halt(`{"flow":{"from":"child"},"question":{"id":"q"}}`)
		r, err := runDesktopWorkflow(t.Context(), plan(1), fantasy.ToolCall{}, d.invoke)
		require.NoError(t, err)
		var meta struct {
			ChildFlow map[string]string `json:"child_flow"`
			Question  map[string]string `json:"question"`
		}
		require.NoError(t, json.Unmarshal([]byte(r.Metadata), &meta))
		require.Equal(t, map[string]string{"from": "child"}, meta.ChildFlow)
		require.Equal(t, "q", meta.Question["id"])
		require.Equal(t, flowNodeBlocked, flowStates(t, r)["press"])
	})
	t.Run("text metadata", func(t *testing.T) {
		t.Parallel()
		d := newFieldDesktop(t)
		d.values["11"] = "ready"
		d.keyed = halt("pending-question q-9")
		r, err := runDesktopWorkflow(t.Context(), plan(1), fantasy.ToolCall{}, d.invoke)
		require.NoError(t, err)
		var meta struct {
			Native string `json:"native_metadata"`
		}
		require.NoError(t, json.Unmarshal([]byte(r.Metadata), &meta))
		require.Equal(t, "pending-question q-9", meta.Native)
	})
	t.Run("large ledger", func(t *testing.T) {
		t.Parallel()
		d := newFieldDesktop(t)
		d.values["11"] = "ready"
		padding := strings.Repeat("p", 3000)
		base := d.invoke
		invoke := func(ctx context.Context, c fantasy.ToolCall) (fantasy.ToolResponse, error) {
			r, err := base(ctx, c)
			if strings.Contains(c.Input, `"action":"assert"`) {
				r.Content = strings.TrimSuffix(r.Content, "}") + `,"padding":"` + padding + `"}`
			}
			return r, err
		}
		d.keyed = halt(`{"question":{"id":"q-large","options":[{"id":"a"}]}}`)
		r, err := runDesktopWorkflow(t.Context(), plan(40), fantasy.ToolCall{}, invoke)
		require.NoError(t, err)
		require.True(t, r.StopTurn)
		require.Contains(t, r.Metadata, `"readbacks_omitted":true`)
		require.Contains(t, r.Metadata, `"id":"q-large"`)
		require.Len(t, flowStates(t, r), 42, "every node keeps its state")
		require.Less(t, len(r.Content), 64*1024)
	})
}

// Bug 3: an operation stopped by a permission request before any input is
// blocked, not uncertain. Resume repeats the ordinary checks, sends nothing
// while permission is still refused, then runs it once permission is given.
func TestDesktopFlowNoDispatchPermissionStopResumesAfterApproval(t *testing.T) {
	t.Parallel()
	ctx := durableFlowContext(t, "no-dispatch-permission")
	d := newFieldDesktop(t)
	allowed, sent := false, 0
	d.keyed = func(input ComputerParams) (fantasy.ToolResponse, error) {
		if !allowed {
			r := NewPermissionDeniedResponse(nil)
			return withToolOutcome(r, computerOutcome("key", input, r, false)), nil
		}
		sent++
		d.values["11"] = "submitted"
		return withToolOutcome(fantasy.NewTextResponse("Sent"), computerOutcome("key", input, fantasy.ToolResponse{}, true)), nil
	}
	p := DesktopWorkflowParams{Mode: "flow", Flow: &DesktopFlowParams{RunID: "permission", Nodes: []DesktopFlowNode{
		{ID: "a", Kind: "resolve", Title: "Main A", Next: "submit"},
		{ID: "submit", Kind: "operation", WindowRef: "a", Input: ComputerParams{Action: "key", Key: "enter"}, Checkpoint: computer.AutomationRequest{Name: "Field", Role: "ControlType.Edit", Condition: "value", Expected: "submitted"}},
	}}}
	for range 2 {
		r, err := runDesktopWorkflow(ctx, p, fantasy.ToolCall{}, d.invoke)
		require.NoError(t, err)
		require.True(t, r.StopTurn)
		require.Equal(t, flowNodeBlocked, flowStates(t, r)["submit"])
		require.Contains(t, r.Content, `"dispatch":"none"`)
		require.Contains(t, r.Content, "before its effect")
		journal := flowJournal(t, ctx, "permission")
		require.Empty(t, journal.Pending, "a step that was never sent is not pending")
		require.Zero(t, journal.Attempts["submit"], "a refused attempt does not consume the replay bound")
		p.Flow.Resume = true
	}
	require.Zero(t, sent, "nothing is sent while permission is refused")

	allowed = true
	r, err := runDesktopWorkflow(ctx, p, fantasy.ToolCall{}, d.invoke)
	require.NoError(t, err)
	require.False(t, r.IsError, r.Content)
	require.Equal(t, 1, sent)
	require.Equal(t, flowNodeVerified, flowStates(t, r)["submit"])

	// A completed run does not apply anything again.
	r, err = runDesktopWorkflow(ctx, p, fantasy.ToolCall{}, d.invoke)
	require.NoError(t, err)
	require.False(t, r.IsError, r.Content)
	require.Equal(t, 1, sent)
}

// Hook refusals and cancellation before dispatch withdraw the intent; resume
// then runs the step with fresh window resolution and the ordinary checks.
func TestDesktopFlowHookRefusalAndCancellationBeforeDispatch(t *testing.T) {
	t.Parallel()
	plan := func(run string) DesktopWorkflowParams {
		return DesktopWorkflowParams{Mode: "flow", Flow: &DesktopFlowParams{RunID: run, Nodes: []DesktopFlowNode{
			{ID: "a", Kind: "resolve", Title: "Main A", Next: "submit"},
			{ID: "submit", Kind: "operation", WindowRef: "a", Input: ComputerParams{Action: "key", Key: "enter"}, Checkpoint: computer.AutomationRequest{Name: "Field", Role: "ControlType.Edit", Condition: "value", Expected: "submitted"}},
		}}}
	}
	t.Run("hook deny", func(t *testing.T) {
		t.Parallel()
		ctx := durableFlowContext(t, "hook-deny")
		d := newFieldDesktop(t)
		denied := true
		d.keyed = func(input ComputerParams) (fantasy.ToolResponse, error) {
			if denied {
				r := fantasy.NewTextErrorResponse("Tool call blocked by hook. Reason: not now")
				r.Metadata = `{"hook":{"hook_count":1,"decision":"deny","reason":"not now"}}`
				data, _ := json.Marshal(input)
				return WithComputerNotDispatched(r, fantasy.ToolCall{Name: ComputerToolName, Input: string(data)}), nil
			}
			d.values["11"] = "submitted"
			return fantasy.NewTextResponse("Sent"), nil
		}
		p := plan("hook")
		r, err := runDesktopWorkflow(ctx, p, fantasy.ToolCall{}, d.invoke)
		require.NoError(t, err)
		require.True(t, r.IsError)
		require.False(t, r.StopTurn)
		require.Equal(t, flowNodeFailed, flowStates(t, r)["submit"])
		require.Contains(t, r.Metadata, `"hook":{"hook_count":1`)
		require.Empty(t, flowJournal(t, ctx, "hook").Pending)

		// The window was restarted with a new handle: resume re-resolves it.
		d.windows = `{"result":[{"window_id":"33","name":"Main A","process_id":19,"process_name":"Notepad.exe","class_name":"Notepad","foreground":true}]}`
		d.ids["33"], d.values["33"] = "33:edit", ""
		d.keyed = func(ComputerParams) (fantasy.ToolResponse, error) {
			d.values["33"] = "submitted"
			return fantasy.NewTextResponse("Sent"), nil
		}
		denied = false
		p.Flow.Resume = true
		r, err = runDesktopWorkflow(ctx, p, fantasy.ToolCall{}, d.invoke)
		require.NoError(t, err)
		require.False(t, r.IsError, r.Content)
		require.Equal(t, []string{"11:key:enter", "33:key:enter"}, d.keys, "the refused key was never sent to the stale handle again")
	})
	t.Run("cancel before dispatch", func(t *testing.T) {
		t.Parallel()
		ctx := durableFlowContext(t, "cancel-before")
		run, cancel := context.WithCancel(ctx)
		d := newFieldDesktop(t)
		d.keyed = func(input ComputerParams) (fantasy.ToolResponse, error) {
			cancel()
			r := fantasy.NewTextErrorResponse(context.Canceled.Error())
			return withToolOutcome(r, computerOutcome("key", input, r, false)), context.Canceled
		}
		p := plan("cancel")
		r, err := runDesktopWorkflow(run, p, fantasy.ToolCall{}, d.invoke)
		require.ErrorIs(t, err, context.Canceled)
		require.Equal(t, flowNodeCanceled, flowStates(t, r)["submit"])
		require.Empty(t, flowJournal(t, ctx, "cancel").Pending, "a canceled step that was never sent is not pending")

		d.keyed = func(ComputerParams) (fantasy.ToolResponse, error) {
			d.values["11"] = "submitted"
			return fantasy.NewTextResponse("Sent"), nil
		}
		p.Flow.Resume = true
		r, err = runDesktopWorkflow(ctx, p, fantasy.ToolCall{}, d.invoke)
		require.NoError(t, err)
		require.False(t, r.IsError, r.Content)
	})
}

// Only the whole operation can prove that nothing was sent: when the first
// input reached the desktop and the second was refused, the operation stays
// uncertain and resume replays neither input.
func TestDesktopFlowPartialMultiInputStopStaysUncertain(t *testing.T) {
	t.Parallel()
	ctx := durableFlowContext(t, "partial")
	d := newFieldDesktop(t)
	d.keyed = func(input ComputerParams) (fantasy.ToolResponse, error) {
		if input.Action == "type" {
			return withToolOutcome(fantasy.NewTextResponse("Typed"), computerOutcome("type", input, fantasy.ToolResponse{}, true)), nil
		}
		r := NewPermissionDeniedResponse(nil)
		return withToolOutcome(r, computerOutcome("key", input, r, false)), nil
	}
	p := DesktopWorkflowParams{Mode: "flow", Flow: &DesktopFlowParams{RunID: "partial", Nodes: []DesktopFlowNode{
		{ID: "a", Kind: "resolve", Title: "Main A", Next: "submit"},
		{ID: "submit", Kind: "operation", WindowRef: "a", Inputs: []ComputerParams{{Action: "type", Text: "abc"}, {Action: "key", Key: "enter"}}, Checkpoint: computer.AutomationRequest{Name: "Field", Role: "ControlType.Edit", Condition: "value", Expected: "abc"}},
	}}}
	r, err := runDesktopWorkflow(ctx, p, fantasy.ToolCall{}, d.invoke)
	require.NoError(t, err)
	require.True(t, r.StopTurn)
	require.Equal(t, flowNodeUncertain, flowStates(t, r)["submit"])
	require.NotContains(t, r.Content, `"dispatch":"none"`)
	require.Equal(t, "submit", flowJournal(t, ctx, "partial").Pending)
	native, ok := ResponseOutcome(r)
	require.True(t, ok)
	require.Equal(t, "unknown", native.Dispatch, "the call as a whole may have taken effect")

	p.Flow.Resume = true
	d.keyed = func(ComputerParams) (fantasy.ToolResponse, error) {
		t.Fatal("An uncertain operation was replayed")
		return fantasy.ToolResponse{}, nil
	}
	r, err = runDesktopWorkflow(ctx, p, fantasy.ToolCall{}, d.invoke)
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(r.Content, "uncertain_effect:"), r.Content)
	require.Equal(t, 2, d.keyCount())
}

// A transport failure or an unknown dispatch is never treated as unsent.
func TestDesktopFlowUnknownDispatchIsNeverReplayed(t *testing.T) {
	t.Parallel()
	for name, reply := range map[string]func(ComputerParams) (fantasy.ToolResponse, error){
		"unknown outcome": func(input ComputerParams) (fantasy.ToolResponse, error) {
			r := fantasy.NewTextErrorResponse("computer action timed out; the result may be unknown")
			return withToolOutcome(r, ToolOutcome{Dispatch: "unknown", Mutation: true}), nil
		},
		"transport error without outcome": func(ComputerParams) (fantasy.ToolResponse, error) {
			return fantasy.ToolResponse{}, errors.New("connection reset")
		},
		"error without outcome": func(ComputerParams) (fantasy.ToolResponse, error) {
			return fantasy.NewTextErrorResponse("native error"), nil
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx := durableFlowContext(t, "unknown-"+name)
			d := newFieldDesktop(t)
			d.keyed = reply
			p := DesktopWorkflowParams{Mode: "flow", Flow: &DesktopFlowParams{RunID: "unknown", Nodes: []DesktopFlowNode{
				{ID: "a", Kind: "resolve", Title: "Main A", Next: "submit"},
				{ID: "submit", Kind: "operation", WindowRef: "a", Input: ComputerParams{Action: "key", Key: "enter"}, Checkpoint: computer.AutomationRequest{Name: "Field", Role: "ControlType.Edit", Condition: "value", Expected: "x"}},
			}}}
			r, _ := runDesktopWorkflow(ctx, p, fantasy.ToolCall{}, d.invoke)
			require.Equal(t, flowNodeUncertain, flowStates(t, r)["submit"])
			require.Equal(t, "submit", flowJournal(t, ctx, "unknown").Pending)
			p.Flow.Resume = true
			r, err := runDesktopWorkflow(ctx, p, fantasy.ToolCall{}, d.invoke)
			require.NoError(t, err)
			require.True(t, strings.HasPrefix(r.Content, "uncertain_effect:"), r.Content)
			require.Equal(t, 1, d.keyCount())
		})
	}
}

// Close nodes use the same classification: Alt+F4 refused before dispatch
// leaves the window open and the node resumable; afterwards it is closed.
func TestDesktopFlowCloseRefusedBeforeDispatchResumes(t *testing.T) {
	t.Parallel()
	ctx := durableFlowContext(t, "close-refused")
	d := newFieldDesktop(t)
	allowed := false
	d.keyed = func(input ComputerParams) (fantasy.ToolResponse, error) {
		if !allowed {
			r := NewPermissionDeniedResponse(nil)
			return withToolOutcome(r, computerOutcome("hotkey", input, r, false)), nil
		}
		d.windows = `{"result":[{"window_id":"22","name":"Main B","process_id":18,"process_name":"Notepad.exe","class_name":"Notepad","foreground":true}]}`
		return fantasy.NewTextResponse("Sent"), nil
	}
	p := DesktopWorkflowParams{Mode: "flow", Flow: &DesktopFlowParams{RunID: "close", Nodes: []DesktopFlowNode{
		{ID: "a", Kind: "resolve", Title: "Main A", Next: "close"},
		{ID: "close", Kind: "close", WindowRef: "a", AllowUnowned: true},
	}}}
	r, err := runDesktopWorkflow(ctx, p, fantasy.ToolCall{}, d.invoke)
	require.NoError(t, err)
	require.True(t, r.StopTurn)
	require.Equal(t, flowNodeBlocked, flowStates(t, r)["close"])
	journal := flowJournal(t, ctx, "close")
	require.Empty(t, journal.Pending)
	require.Zero(t, journal.Attempts["close"])

	allowed = true
	p.Flow.Resume = true
	r, err = runDesktopWorkflow(ctx, p, fantasy.ToolCall{}, d.invoke)
	require.NoError(t, err)
	require.False(t, r.IsError, r.Content)
	require.Equal(t, flowNodeVerified, flowStates(t, r)["close"])
}
