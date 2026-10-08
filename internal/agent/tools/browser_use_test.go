package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/browser"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/stretchr/testify/require"
)

func newTestEngineeringStore(t *testing.T, dir string) *engineering.Store {
	t.Helper()
	return engineering.NewStore(filepath.Join(dir, ".atlas"))
}

// observingSession is a fake driver with document and tab identity, field
// commit, typed key input and bounded eval, for tool-level behavior tests.
type observingSession struct {
	*fakeBrowserSession
	tab, doc    string
	instance    string
	elements    []browser.SnapshotElement
	keys        []browser.Request
	fields      []browser.Request
	fieldResult browser.FieldResult
	fieldErr    error
	readValue   string
	evalTimeout time.Duration
}

func (o *observingSession) Observe(_ context.Context, req browser.ObserveRequest) (browser.PageObservation, error) {
	elements := o.elements
	if req.Scope != "" {
		elements = elements[:1]
	}
	return browser.PageObservation{TabID: o.tab, Document: o.doc, URL: "https://app.test/doc", Elements: elements}, nil
}

func (o *observingSession) Snapshot(bool) ([]browser.SnapshotElement, error) { return o.elements, nil }

func (o *observingSession) URL() (string, error) { return "https://app.test/doc", nil }
func (o *observingSession) ActiveTab() string    { return o.tab }

func (o *observingSession) ExecutionIdentity(context.Context) browser.ExecutionIdentity {
	return browser.ExecutionIdentity{Instance: o.instance, TabID: o.tab, Document: o.doc}
}

func (o *observingSession) identity() browser.FieldIdentity {
	return browser.FieldIdentity{TabID: o.tab, DocumentID: o.doc}
}

func (o *observingSession) Input(_ context.Context, p browser.Request) (browser.InputResult, error) {
	o.keys = append(o.keys, p)
	return browser.InputResult{Action: p.Action, EventsSent: 2}, nil
}

func (o *observingSession) EvalContext(_ context.Context, _ string, timeout time.Duration) (string, error) {
	o.evalTimeout = timeout
	return "1", nil
}

func (o *observingSession) SetField(_ context.Context, p browser.Request) (browser.FieldResult, error) {
	o.fields = append(o.fields, p)
	r := o.fieldResult
	r.Requested = p.Value
	r.Identity = o.identity()
	if r.Accepted == "" && o.fieldErr == nil {
		r.Accepted, r.Status, r.Verification = p.Value, browser.FieldApplied, "verified"
		r.CommitAttempts, r.Commit = []string{"enter"}, "enter"
	}
	o.readValue = r.Accepted
	return r, o.fieldErr
}

func (o *observingSession) ReadField(context.Context, browser.Request) (browser.FieldResult, error) {
	return browser.FieldResult{Before: o.readValue, Accepted: o.readValue, Status: "read", Identity: o.identity()}, nil
}

type observingSessions struct{ sess browser.Session }

func (f *observingSessions) Session(string) (browser.Session, error) { return f.sess, nil }
func (f *observingSessions) Close(string)                            {}

func newObservingSession() *observingSession {
	return &observingSession{fakeBrowserSession: &fakeBrowserSession{}, tab: "tab-1", doc: "doc-1", instance: "browser-1", elements: []browser.SnapshotElement{
		{Ref: "e1", Role: "textbox", Tag: "input", Name: "Width", Value: "100", Context: `region "Layout"`, Visible: true},
		{Ref: "e2", Role: "textbox", Tag: "input", Hint: "Corner radius", Value: "0", Context: `region "Appearance"`, Visible: true},
		{Ref: "e3", Role: "button", Tag: "button", Name: "Close", Context: `region "Layers"`, Visible: true},
	}}
}

// browserHarness runs one tool instance against one fake driver, so state
// keyed by the session manager stays isolated per test.
type browserHarness struct {
	t     *testing.T
	tool  fantasy.AgentTool
	root  string
	ctx   context.Context
	calls int
}

func newBrowserHarness(t *testing.T, sess browser.Session) *browserHarness {
	root := t.TempDir()
	sessionID := "harness-" + t.Name()
	resetBrowserLedger(sessionID)
	t.Cleanup(func() { resetBrowserLedger(sessionID) })
	ctx := context.WithValue(t.Context(), SessionIDContextKey, sessionID)
	return &browserHarness{t: t, tool: newBrowserTool(&mockPermissionService{}, root, &observingSessions{sess}, browserDescription(false)), root: root, ctx: ctx}
}

func (h *browserHarness) run(input string) fantasy.ToolResponse {
	h.t.Helper()
	h.calls++
	resp, err := h.tool.Run(h.ctx, fantasy.ToolCall{ID: fmt.Sprintf("call-%d", h.calls), Name: BrowserToolName, Input: input})
	require.NoError(h.t, err)
	return resp
}

func opOf(t *testing.T, content string) BrowserOp {
	t.Helper()
	var op BrowserOp
	if _, line, ok := strings.Cut(content, "[op] "); ok {
		require.NoError(t, json.Unmarshal([]byte(line), &op))
		return op
	}
	var object struct {
		Op BrowserOp `json:"op"`
	}
	require.NoError(t, json.Unmarshal([]byte(content), &object), content)
	return object.Op
}

func TestBrowserObservationDeltaAndInvalidation(t *testing.T) {
	t.Parallel()
	sess := newObservingSession()
	h := newBrowserHarness(t, sess)

	first := h.run(`{"action":"navigate","url":"https://app.test/doc"}`)
	require.Contains(t, first.Content, "Interactive elements now:")
	require.Contains(t, first.Content, "mode=full")
	require.Contains(t, first.Content, `(label? "Corner radius")`, "unnamed fields carry their nearby label")
	require.Contains(t, first.Content, `region "Layers":`, "elements are grouped under their container")

	sess.elements[1].Value = "12"
	sess.elements = append(sess.elements[:2], browser.SnapshotElement{Ref: "e9", Role: "dialog", Tag: "div", Name: "Export", Visible: true})
	delta := h.run(`{"action":"key","key":"enter"}`)
	require.Contains(t, delta.Content, "Changes since o1")
	require.Contains(t, delta.Content, "base_observation_id=o1")
	require.Contains(t, delta.Content, `<- was value="0"`)
	require.Contains(t, delta.Content, `[e9] dialog "Export"`)
	require.Contains(t, delta.Content, "removed (1): e3")
	require.NotContains(t, delta.Content, `"Width"`, "unchanged elements are not resent")

	quiet := h.run(`{"action":"observe"}`)
	require.Contains(t, quiet.Content, "No changes since o2")

	sess.tab = "tab-2"
	switched := h.run(`{"action":"observe"}`)
	require.Contains(t, switched.Content, "full because a different tab is selected")
	sess.doc = "doc-2"
	navigated := h.run(`{"action":"observe"}`)
	require.Contains(t, navigated.Content, "full because the page navigated")

	scoped := h.run(`{"action":"observe","advanced":{"mode":"scope","scope":"#panel"}}`)
	require.Contains(t, scoped.Content, "mode=scope")
	after := h.run(`{"action":"observe"}`)
	require.Contains(t, after.Content, "No changes", "a scoped observation does not replace the page base")
}

func TestBrowserObservationTruncationContinues(t *testing.T) {
	t.Parallel()
	sess := newObservingSession()
	sess.elements = nil
	for i := 1; i <= 170; i++ {
		sess.elements = append(sess.elements, browser.SnapshotElement{Ref: fmt.Sprintf("e%d", i), Role: "button", Tag: "button", Name: fmt.Sprintf("Item %d", i)})
	}
	sess.elements[160].Invalid, sess.elements[160].Error = true, "Required"
	h := newBrowserHarness(t, sess)
	resp := h.run(`{"action":"observe","advanced":{"mode":"full"}}`)
	require.Contains(t, resp.Content, "has_more=true")
	require.Contains(t, resp.Content, "advanced.cursor=150")
	require.Contains(t, resp.Content, `attention:`)
	require.Contains(t, resp.Content, `error="Required"`, "critical state is never cut by truncation")
	next := h.run(`{"action":"observe","advanced":{"mode":"full","cursor":150}}`)
	require.Contains(t, next.Content, `"Item 170"`)
	require.NotContains(t, next.Content, "has_more")
}

func TestBrowserOpRecordsRealCountsAndUnifiedErrors(t *testing.T) {
	t.Parallel()
	sess := newObservingSession()
	h := newBrowserHarness(t, sess)
	h.run(`{"action":"navigate","url":"https://app.test/doc"}`)
	ok := h.run(`{"action":"key","key":"f"}`)
	require.False(t, ok.IsError, ok.Content)
	require.Equal(t, "f", sess.keys[0].Key, "single letters go through the typed keyboard path")
	chord := h.run(`{"action":"key","key":"Ctrl+A"}`)
	require.False(t, chord.IsError)
	require.Equal(t, []string{"ctrl"}, sess.keys[1].Modifiers)

	bad := h.run(`{"action":"key","key":"hyper+q"}`)
	require.True(t, bad.IsError)
	op := opOf(t, bad.Content)
	require.Equal(t, "unsupported_key", op.ErrorCode)
	require.Equal(t, "none", op.Dispatch)
	require.Equal(t, browserOpID(4), op.ID)

	status := h.run(`{"action":"status"}`)
	var state struct {
		Metrics BrowserMetrics `json:"browser_metrics"`
	}
	require.NoError(t, json.Unmarshal([]byte(status.Content), &state))
	require.Equal(t, 4, state.Metrics.Calls)
	require.Equal(t, 1, state.Metrics.Errors)
	require.Equal(t, 3, state.Metrics.ByAction["key"])
}

func TestBrowserRepeatedFailureIsBlockedUntilSomethingSucceeds(t *testing.T) {
	t.Parallel()
	sess := newObservingSession()
	sess.clickErr = errors.New("ambiguous_target: selector resolves to 2 elements")
	h := newBrowserHarness(t, sess)
	for i := 0; i < 2; i++ {
		resp := h.run(`{"action":"click","selector":".save"}`)
		require.True(t, resp.IsError)
		require.NotContains(t, resp.Content, "repeated_failure")
	}
	sess.clicked = ""
	blocked := h.run(`{"action":"click","selector":".save"}`)
	require.Contains(t, blocked.Content, "repeated_failure")
	require.Empty(t, sess.clicked, "the blocked call never reached the driver")
	h.run(`{"action":"observe"}`)
	again := h.run(`{"action":"click","selector":".save"}`)
	require.Contains(t, again.Content, "repeated_failure", "observing alone must not unlock identical failed input")
	require.Empty(t, sess.clicked)
	sess.clickErr = nil
	recovered := h.run(`{"action":"click","selector":".other-route"}`)
	require.False(t, recovered.IsError)
	again = h.run(`{"action":"click","selector":".save"}`)
	require.False(t, again.IsError, "successful input through another route lifts the block")
}

func TestBrowserEvalGuardsClipboardAndBoundsAwait(t *testing.T) {
	t.Parallel()
	sess := newObservingSession()
	h := newBrowserHarness(t, sess)
	blocked := h.run(`{"action":"eval","script":"navigator.clipboard.writeText('probe')"}`)
	require.True(t, blocked.IsError)
	require.Contains(t, blocked.Content, "clipboard_blocked")
	require.Zero(t, sess.evalTimeout, "nothing ran")
	h.run(`{"action":"eval","script":"1","advanced":{"timeout_ms":1500}}`)
	require.Equal(t, 1500*time.Millisecond, sess.evalTimeout)
}

func TestBrowserSetFieldUndoOnlyWhenUnchanged(t *testing.T) {
	t.Parallel()
	sess := newObservingSession()
	h := newBrowserHarness(t, sess)
	stale := h.run(`{"action":"set_field","ref":"e2","advanced":{"value":"12"}}`)
	require.Contains(t, stale.Content, "stale_ref", "a ref this process never observed is refused")
	require.Empty(t, sess.fields)
	h.run(`{"action":"observe"}`)
	set := h.run(`{"action":"set_field","ref":"e2","advanced":{"value":"12"}}`)
	require.False(t, set.IsError, set.Content)
	require.Contains(t, set.Content, `"status":"applied"`)
	require.Equal(t, `[data-atlas-ref="e2"]`, sess.fields[0].Selector)
	require.Equal(t, "structural", opOf(t, set.Content).Verification)

	sess.readValue = "16" // Someone else changed the field afterwards.
	unsafe := h.run(`{"action":"undo"}`)
	require.True(t, unsafe.IsError)
	require.Contains(t, unsafe.Content, "undo_unsafe")
	require.Len(t, sess.fields, 1, "nothing was written")

	sess.readValue = "12"
	sess.fieldResult = browser.FieldResult{}
	undone := h.run(`{"action":"undo"}`)
	require.False(t, undone.IsError, undone.Content)
	require.Equal(t, "", sess.fields[1].Value)

	sess.fieldErr = fmt.Errorf("%w: still 0", browser.ErrFieldNotApplied)
	sess.fieldResult = browser.FieldResult{Status: browser.FieldNotApplied, Accepted: "0", Dispatch: "sent", Verification: "failed"}
	failed := h.run(`{"action":"set_field","advanced":{"label":"Corner radius","value":"12"}}`)
	require.True(t, failed.IsError)
	op := opOf(t, failed.Content)
	require.Equal(t, "value_not_applied", op.ErrorCode)
	require.Equal(t, "sent", op.Dispatch)
	require.Equal(t, "failed", op.Verification)
}

func TestBrowserCompletionContractEvidenceAndBlockers(t *testing.T) {
	t.Parallel()
	sess := newObservingSession()
	h := newBrowserHarness(t, sess)
	sessionID := "harness-" + t.Name()
	plan := h.run(`{"action":"task_plan","task":{"goal":"Mobile control screen","requirements":[{"id":"frame","text":"390x844 frame","verify":"structural"},{"id":"look","text":"Six sections render","verify":"visual"},{"id":"save","text":"File saved"}]}}`)
	require.False(t, plan.IsError, plan.Content)
	require.Contains(t, plan.Content, `"open":3`)

	noEvidence := h.run(`{"action":"task_update","task":{"id":"frame","status":"verified"}}`)
	require.True(t, noEvidence.IsError)
	set := h.run(`{"action":"set_field","advanced":{"label":"Width","value":"390"}}`)
	setOp := opOf(t, set.Content).ID
	wrongKind := h.run(fmt.Sprintf(`{"action":"task_update","task":{"id":"look","status":"verified","evidence":[%q]}}`, setOp))
	require.True(t, wrongKind.IsError, "a field readback does not prove a visual requirement")
	verified := h.run(fmt.Sprintf(`{"action":"task_update","task":{"id":"frame","status":"verified","evidence":[%q]}}`, setOp))
	require.False(t, verified.IsError, verified.Content)
	invented := h.run(`{"action":"task_update","task":{"id":"save","status":"verified","evidence":["b999"]}}`)
	require.True(t, invented.IsError)

	effort := h.run(`{"action":"task_update","task":{"id":"look","status":"blocked","blocker":"too_many_steps"}}`)
	require.True(t, effort.IsError)
	require.Contains(t, effort.Content, "not blockers")

	prompt, ok := BrowserTaskContinuation(h.root, sessionID)
	require.True(t, ok)
	require.Contains(t, prompt, "look (pending)")
	require.NotContains(t, prompt, "frame (")
	// No new step and no new verification: the second idle continuation stops.
	_, ok = BrowserTaskContinuation(h.root, sessionID)
	require.True(t, ok)
	_, ok = BrowserTaskContinuation(h.root, sessionID)
	require.False(t, ok, "continuations without progress end instead of looping")

	unproven := h.run(`{"action":"task_update","task":{"id":"look","status":"blocked","blocker":"account_or_plan_limit"}}`)
	require.True(t, unproven.IsError, "a page blocker needs the op that showed it")
	shot := opOf(t, h.run(`{"action":"observe"}`).Content).ID
	blocked := h.run(fmt.Sprintf(`{"action":"task_update","task":{"id":"look","status":"blocked","blocker":"account_or_plan_limit","evidence":[%q]}}`, shot))
	require.False(t, blocked.IsError, blocked.Content)
	silent := h.run(`{"action":"task_update","task":{"id":"save","status":"blocked","blocker":"needs_user_approval"}}`)
	require.True(t, silent.IsError, "a conversation blocker needs a note")
	h.run(`{"action":"task_update","task":{"id":"save","status":"blocked","blocker":"needs_user_approval","note":"User must confirm the save location"}}`)
	_, ok = BrowserTaskContinuation(h.root, sessionID)
	require.False(t, ok, "nothing open: no continuation")
}

func TestBrowserCheckpointResumeRequiresReobservation(t *testing.T) {
	t.Parallel()
	sess := newObservingSession()
	h := newBrowserHarness(t, sess)
	sessionID := "harness-" + t.Name()
	h.run(`{"action":"task_plan","task":{"requirements":[{"id":"a","text":"Name set"}]}}`)
	h.run(`{"action":"set_field","advanced":{"label":"Name","value":"secret-ish value"}}`)
	path := filepath.Join(h.root, browserTaskPath(sessionID))
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NotContains(t, string(data), "secret-ish value", "checkpoints never store typed values")
	require.NotContains(t, string(data), "data-atlas-ref")
	// Simulate a process restart: the stored steps belong to another epoch.
	require.NoError(t, os.WriteFile(path, []byte(strings.ReplaceAll(string(data), browserProcessEpoch, "p-old")), 0o600))
	status := h.run(`{"action":"task_status"}`)
	require.Contains(t, status.Content, "reobserve_required")
}

func TestBrowserWorkflowCompilesCanvasSequenceWithBoundViewport(t *testing.T) {
	t.Parallel()
	steps, err := compileBrowserWorkflow(BrowserWorkflowParams{Steps: []BrowserWorkflowStep{
		{ID: "shot", Action: "screenshot"},
		{ID: "tool", Action: "key", Key: "t"},
		{ID: "place", Action: "pointer_click", Target: browser.Request{X: 705, Y: 66}, Bindings: map[string]string{"advanced.viewport_id": "shot#/viewport_id"}},
		{ID: "text", Action: "insert_text", Text: "Kontrol merkezi"},
		{ID: "done", Action: "key", Key: "escape"},
		{ID: "pick", Action: "click", Ref: "e183"},
		{ID: "chord", Action: "key_combo", Key: "shift+1"},
	}})
	require.NoError(t, err)
	require.Len(t, steps, 7)
	require.Equal(t, "e183", steps[5].Arguments["ref"])
	advanced := steps[6].Arguments["advanced"].(map[string]any)
	require.Equal(t, "1", advanced["key"])
	require.Equal(t, []any{"shift"}, advanced["modifiers"])

	outputs := map[string]json.RawMessage{"shot": pipelineOutput("guide text\nViewport: {\"viewport_id\":\"vp-1\",\"image_width\":10,\"image_height\":10,\"image_scale\":1}")}
	require.NoError(t, bindPipelineArguments(steps[2].Arguments, steps[2].Bindings, outputs))
	require.Equal(t, "vp-1", steps[2].Arguments["advanced"].(map[string]any)["viewport_id"])

	_, err = compileBrowserWorkflow(BrowserWorkflowParams{Steps: []BrowserWorkflowStep{{ID: "k", Action: "key", Key: "hyper+q"}}})
	require.ErrorContains(t, err, "unsupported_key")
}

func TestPipelineBindingRefusesChoosingAmongMatches(t *testing.T) {
	t.Parallel()
	args := map[string]any{"action": "click"}
	one := map[string]json.RawMessage{"find": json.RawMessage(`{"matches":[{"ref":"e4"}]}`)}
	require.NoError(t, bindPipelineArguments(args, map[string]string{"ref": "find#/matches/0/ref"}, one))
	require.Equal(t, "e4", args["ref"])
	two := map[string]json.RawMessage{"find": json.RawMessage(`{"matches":[{"ref":"e4"},{"ref":"e5"}]}`)}
	err := bindPipelineArguments(map[string]any{}, map[string]string{"ref": "find#/matches/0/ref"}, two)
	require.ErrorContains(t, err, "ambiguous_target")
}

func TestPipelineErrorPolicyStatusesAndRecovery(t *testing.T) {
	t.Parallel()
	var ran []string
	tool := NewToolPipeline(func(_ context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
		var p BrowserParams
		require.NoError(t, json.Unmarshal([]byte(call.Input), &p))
		ran = append(ran, p.Ref+p.Advanced.Name)
		switch p.Advanced.Name {
		case "Optional banner":
			return fantasy.NewTextErrorResponse("target_missing: not present\n[op] {\"dispatch\":\"none\"}"), nil
		case "Primary":
			return fantasy.NewTextErrorResponse("timeout while clicking\n[op] {\"dispatch\":\"unknown\"}"), nil
		}
		return fantasy.NewTextResponse(`{"action_sent":true}`), nil
	})
	p := PipelineParams{Browser: &BrowserWorkflowParams{Steps: []BrowserWorkflowStep{
		{ID: "banner", Action: "semantic_click", Target: browser.Request{Role: "button", Name: "Optional banner"}, OnError: "continue"},
		{ID: "primary", Action: "semantic_click", Target: browser.Request{Role: "button", Name: "Primary"}},
		{ID: "fallback", Action: "semantic_click", Target: browser.Request{Role: "button", Name: "Fallback"}, IfFailed: "primary"},
		{ID: "after", Action: "semantic_click", Target: browser.Request{Role: "button", Name: "After"}},
		{ID: "only-if-primary", Action: "semantic_click", Target: browser.Request{Role: "button", Name: "Dependent"}, IfSuccess: "primary"},
	}}}
	data, _ := json.Marshal(p)
	resp, err := tool.Run(t.Context(), fantasy.ToolCall{ID: "wf", Input: string(data)})
	require.NoError(t, err)
	require.True(t, resp.IsError, "unknown input cannot be reconciled by an arbitrary fallback: %s", resp.Content)
	var results []PipelineResult
	require.NoError(t, json.Unmarshal([]byte(resp.Content), &results))
	status := map[string]string{}
	for _, r := range results {
		status[r.ID] = r.Status
	}
	require.Equal(t, map[string]string{"banner": StepFailed, "primary": StepUncertain, "fallback": StepSkipped, "after": StepSkipped, "only-if-primary": StepSkipped}, status)
	require.Equal(t, []string{"Optional banner", "Primary"}, ran, "no fallback or downstream input after an unknown mutation")
	require.Empty(t, results[0].Resume, "the optional banner did not stop the pipeline")
	require.Contains(t, results[1].Resume, "Step primary has an unknown effect")

	p.Browser.Steps[2].Target.Name = "Primary" // Identical input is also blocked.
	data, _ = json.Marshal(p)
	ran = nil
	resp, err = tool.Run(t.Context(), fantasy.ToolCall{ID: "wf2", Input: string(data)})
	require.NoError(t, err)
	require.True(t, resp.IsError)
	require.Contains(t, resp.Content, `"id":"after","iteration":0,"status":"skipped"`)
	require.Equal(t, []string{"Optional banner", "Primary"}, ran, "the unknown primary must never be replayed by its recovery branch")
}

func TestPipelineCancellationReachesChildren(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	calls := 0
	tool := NewToolPipeline(func(childCtx context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
		calls++
		cancel()
		<-childCtx.Done()
		return fantasy.ToolResponse{}, childCtx.Err()
	})
	resp, err := tool.Run(ctx, fantasy.ToolCall{ID: "c", Input: `{"browser":{"steps":[{"id":"a","action":"observe"},{"id":"b","action":"observe"}]}}`})
	require.NoError(t, err)
	require.True(t, resp.IsError)
	var ledger []PipelineResult
	require.NoError(t, json.Unmarshal([]byte(resp.Content), &ledger))
	require.Len(t, ledger, 2)
	require.Equal(t, OutcomeCanceled, ledger[0].Outcome)
	require.Equal(t, OutcomeCanceled, ledger[1].Outcome)
	require.Equal(t, 1, calls)
}

func TestBrowserFlowRerunsOnlyFailedItems(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	failName := "Beta"
	var typed []string
	tool := NewToolPipeline(func(_ context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
		var p BrowserParams
		require.NoError(t, json.Unmarshal([]byte(call.Input), &p))
		if p.Action == "set_field" {
			typed = append(typed, p.Advanced.Value)
			if p.Advanced.Value == failName {
				return fantasy.NewTextErrorResponse("value_not_applied: kept old\n[op] {\"dispatch\":\"sent\"}"), nil
			}
		}
		return fantasy.NewTextResponse(`{"passed":true}`), nil
	}, newTestEngineeringStore(t, dir))
	define := `{"flow":{"name":"rename-rows","version":"1.0","inputs":["name"],"save":true,"steps":[{"id":"set","action":"set_field","target":{"label":"Name","value":"{{name}}"}},{"id":"check","action":"assert","target":{"label":"Name","condition":"value","expected":"{{name}}"}}],"items":[{"name":"Alpha"},{"name":"Beta"},{"name":"Gamma"}]}}`
	resp, err := tool.Run(t.Context(), fantasy.ToolCall{ID: "flow", Input: define})
	require.NoError(t, err)
	require.True(t, resp.IsError, "one item failed")
	var out struct {
		RunID  string         `json:"run_id"`
		Counts map[string]int `json:"counts"`
	}
	require.NoError(t, json.Unmarshal([]byte(resp.Content), &out))
	require.Equal(t, map[string]int{"succeeded": 2, "failed": 1}, out.Counts)
	require.Equal(t, []string{"Alpha", "Beta", "Gamma"}, typed)

	failName, typed = "", nil
	resp, err = tool.Run(t.Context(), fantasy.ToolCall{ID: "rerun", Input: fmt.Sprintf(`{"flow":{"name":"rename-rows","rerun":%q}}`, out.RunID)})
	require.NoError(t, err)
	require.False(t, resp.IsError, resp.Content)
	require.Equal(t, []string{"Beta"}, typed, "only the failed item ran again")

	bad := `{"flow":{"name":"canvas","steps":[{"id":"p","action":"pointer_click","target":{"viewport_id":"vp-1","x":1,"y":1}}],"items":[{}]}}`
	resp, err = tool.Run(t.Context(), fantasy.ToolCall{ID: "bad", Input: bad})
	require.NoError(t, err)
	require.Contains(t, resp.Content, "pointer input cannot be stored")
	resp, err = tool.Run(t.Context(), fantasy.ToolCall{ID: "ver", Input: `{"flow":{"name":"rename-rows","version":"2.0","items":[{"name":"x"}]}}`})
	require.NoError(t, err)
	require.Contains(t, resp.Content, "version 1.0")
}
