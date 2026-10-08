package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/computer"
	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/outcome"
)

// DesktopFlowParams describes a bounded graph, independent of runtime handles.
type DesktopFlowParams struct {
	RunID  string            `json:"run_id,omitempty" description:"Optional durable run identity within this session. Reuse with resume:true and exactly the same nodes after interruption."`
	Resume bool              `json:"resume,omitempty"`
	Nodes  []DesktopFlowNode `json:"nodes" description:"1-64 nodes; edges must point forward. First node is entry. Window refs point to preceding prepare/resolve nodes. Operation needs a checkpoint; rename/close verify their own outcome."`
}

// DesktopFlowNode links explicit outcomes without arbitrary argument evaluation.
type DesktopFlowNode struct {
	Rename      *DesktopRenameParams       `json:"rename,omitempty" description:"For rename: exact displayed old/new item names in the bound Explorer window."`
	ID          string                     `json:"id"`
	Kind        string                     `json:"kind" description:"prepare (find/launch/focus/bind), resolve (bind an open window), branch, operation, verify, file_check (read-only file-system check), rename or close (focus and verify closure)."`
	Next        string                     `json:"next,omitempty" description:"Next node ID, or omitted for successful terminal."`
	Then        string                     `json:"then,omitempty" description:"Branch true edge, or omitted for terminal."`
	Else        string                     `json:"else,omitempty" description:"Branch false edge, or omitted for terminal."`
	WindowRef   string                     `json:"window_ref,omitempty" description:"Earlier prepare/resolve node ID; runtime window IDs are deliberately not supplied or persisted."`
	Application string                     `json:"application,omitempty" description:"For prepare: installed application to find/launch/focus. For resolve: identity to find only, without launching."`
	Title       string                     `json:"title,omitempty" description:"For resolve: exact window title, optionally restricting application identity."`
	OwnerRef    string                     `json:"owner_ref,omitempty" description:"For resolve: earlier window binding owning this exact-title dialog; requires verified owner and process."`
	WaitMS      int                        `json:"wait_ms,omitempty" description:"Prepare/resolve wait: maximum 15000, default 5000."`
	Focus       bool                       `json:"focus,omitempty" description:"Operation: optionally activate the bound window. Rename and close always activate their exact bound window and confirm foreground."`
	Input       ComputerParams             `json:"input,omitempty"`
	Inputs      []ComputerParams           `json:"inputs,omitempty"`
	Checkpoint  computer.AutomationRequest `json:"checkpoint,omitempty" description:"Targeted required native assertion for operation/verify or immediate predicate for branch; window_id omitted and bound at runtime."`
	FailureCrop bool                       `json:"failure_crop,omitempty" description:"After failed verification only, return a crop of the uniquely resolved non-password target. Diagnostic image never proves success."`
	// CheckpointRef reads the checkpoint in another bound window.
	CheckpointRef string `json:"checkpoint_ref,omitempty" description:"For operation/verify: earlier prepare/resolve binding whose window the checkpoint reads instead of window_ref, e.g. the main window after its Save dialog closed."`
	// ClosesWindow proves the window_ref window is gone after the inputs.
	ClosesWindow bool `json:"closes_window,omitempty" description:"For operation: the inputs dismiss the window_ref window (e.g. Enter in a Save dialog). Its absence is proven, then checkpoint_ref must be foreground again before its checkpoint is read."`
	// AllowUnowned lets a close node close a window the task did not open.
	Capture *DesktopFlowCapture `json:"capture,omitempty" description:"For operation/verify: name the checkpoint readback for later {{name}} use in text/expected fields. A verify node may omit checkpoint.expected to read the current value; the pattern is then the check."`
	// File is the file_check node's read-only file-system check.
	File *DesktopFileCheck `json:"file,omitempty" description:"For file_check: absolute path and condition (exists, absent, content, sha256)."`

	AllowUnowned bool `json:"allow_unowned,omitempty" description:"For close: also close a window this task did not open. Only when the user explicitly asked to close that window; otherwise close refuses windows that existed before the task."`
}

type desktopFlowStoreKey struct{}

// desktopFlowTimeoutKey carries an explicit tool_pipeline timeout_ms to a
// flow. Without it a flow keeps its default five-minute bound.
type desktopFlowTimeoutKey struct{}

// Desktop flow capacity. The tool description renders these values, so the
// model is told exactly what the runtime enforces.
const (
	defaultDesktopFlowTimeout    = 5 * time.Minute
	maxDesktopFlowNodes          = 64
	maxDesktopFlowEstimatedCalls = 384
	maxDesktopFlowChildCalls     = 512
)

func desktopFlowTimeout(ctx context.Context) time.Duration {
	if timeout, ok := ctx.Value(desktopFlowTimeoutKey{}).(time.Duration); ok && timeout > 0 && timeout <= maxPipelineTimeout {
		return timeout
	}
	return defaultDesktopFlowTimeout
}

// flowError is the structured preflight refusal of a flow plan. It names the
// node and the field path, so the model can fix that node instead of
// guessing, and states that nothing was sent.
func flowError(index int, id, field, message string) error {
	path := "desktop.flow"
	if index >= 0 {
		path = fmt.Sprintf("desktop.flow.nodes[%d]", index)
		if id != "" {
			message = fmt.Sprintf("node %q: %s", id, message)
		}
	}
	if field != "" {
		path += "." + field
	}
	return computer.NewContractError("invalid_flow", path, message+". The whole flow was validated before any input; nothing was sent",
		"Fix this field of the plan and send the corrected flow; do not observe or focus first, the plan itself was refused.", "")
}

func validateDesktopFlow(p DesktopWorkflowParams) error {
	f := p.Flow
	if f == nil || len(f.Nodes) == 0 || len(f.Nodes) > maxDesktopFlowNodes || p.Result != "" || p.Application != "" || p.WindowID != "" || p.WaitMS != 0 || p.Input.Action != "" || len(p.Inputs)+len(p.Steps)+len(p.AdaptiveSteps) != 0 || p.Transition != nil || p.WaitFor.Condition != "" || p.Observation == "visual" {
		return flowError(-1, "", "nodes", "flow requires 1-64 nodes without other recipe parameters or visual observation")
	}
	if len(f.RunID) > 128 || (f.Resume && f.RunID == "") {
		return flowError(-1, "", "run_id", "invalid durable flow identity: run_id is at most 128 bytes and resume requires it")
	}
	if p.MaxElements != 0 || (p.Observation != "" && p.Observation != "auto" && p.Observation != "semantic") {
		return flowError(-1, "", "", "flow uses targeted checkpoint max_elements and auto/semantic verification")
	}
	ids := map[string]int{}
	for i, n := range f.Nodes {
		if strings.TrimSpace(n.ID) == "" || len(n.ID) > 64 {
			return flowError(i, "", "id", "invalid flow node identity: 1-64 bytes required")
		}
		if _, exists := ids[n.ID]; exists {
			return flowError(i, n.ID, "id", "duplicate flow node identity")
		}
		ids[n.ID] = i
	}
	calls := 0
	for i, n := range f.Nodes {
		for _, edge := range [][2]string{{"next", n.Next}, {"then", n.Then}, {"else", n.Else}} {
			if edge[1] == "" {
				continue
			}
			j, ok := ids[edge[1]]
			if !ok {
				return flowError(i, n.ID, edge[0], fmt.Sprintf("edge names unknown node %q", edge[1]))
			}
			if j <= i {
				return flowError(i, n.ID, edge[0], fmt.Sprintf("edge to %q points backward; flow edges must name later nodes and cycles are prohibited", edge[1]))
			}
		}
		for _, ref := range [][2]string{{"window_ref", n.WindowRef}, {"owner_ref", n.OwnerRef}, {"checkpoint_ref", n.CheckpointRef}} {
			if ref[1] == "" {
				continue
			}
			j, ok := ids[ref[1]]
			if !ok || j >= i || (f.Nodes[j].Kind != "resolve" && f.Nodes[j].Kind != "prepare") {
				return flowError(i, n.ID, ref[0], fmt.Sprintf("window binding %q must refer to a preceding prepare/resolve node", ref[1]))
			}
		}
		if err := validateDesktopFlowNode(f, i, n, &calls); err != nil {
			return err
		}
	}
	if calls > maxDesktopFlowEstimatedCalls {
		return flowError(-1, "", "nodes", "flow exceeds bounded operation budget; split at a verified boundary")
	}
	if err := validateDesktopFlowCaptures(f.Nodes, ids); err != nil {
		return err
	}
	return validateDesktopFlowClosedRefs(f.Nodes, ids)
}

// validateDesktopFlowNode checks one node's own fields and adds its
// estimated child calls.
func validateDesktopFlowNode(f *DesktopFlowParams, i int, n DesktopFlowNode, calls *int) error {
	fail := func(field, message string) error { return flowError(i, n.ID, field, message) }
	if n.Kind != "branch" && (n.Then != "" || n.Else != "") {
		return fail("then", "then/else require kind branch")
	}
	if n.Kind == "branch" && n.Next != "" {
		return fail("next", "branch uses then/else instead of next")
	}
	if n.Rename != nil && n.Kind != "rename" {
		return fail("rename", "rename names require a rename node")
	}
	if n.CheckpointRef != "" && n.Kind != "operation" && n.Kind != "verify" {
		return fail("checkpoint_ref", "checkpoint_ref requires an operation or verify node")
	}
	if n.ClosesWindow && (n.Kind != "operation" || n.CheckpointRef == "" || n.CheckpointRef == n.WindowRef) {
		return fail("closes_window", "closes_window requires an operation node with a checkpoint_ref naming another window binding")
	}
	if n.AllowUnowned && n.Kind != "close" {
		return fail("allow_unowned", "allow_unowned applies only to close nodes")
	}
	if n.Capture != nil && n.Kind != "operation" && n.Kind != "verify" {
		return fail("capture", "capture requires an operation or verify node")
	}
	if n.File != nil && n.Kind != "file_check" {
		return fail("file", "file requires kind file_check")
	}
	switch n.Kind {
	case "prepare":
		if n.Application == "" || n.Title != "" || n.OwnerRef != "" || n.WindowRef != "" || n.Input.Action != "" || len(n.Inputs) != 0 || n.Checkpoint != (computer.AutomationRequest{}) || n.Focus || n.FailureCrop || n.WaitMS < 0 || n.WaitMS > 15000 {
			return fail("", "prepare requires application and optional wait_ms without other node parameters")
		}
		*calls += 32
		return nil
	case "resolve":
		if (n.Application == "" && n.Title == "") || (n.OwnerRef != "" && n.Title == "") || n.WaitMS < 0 || n.WaitMS > 15000 || n.WindowRef != "" || n.Input.Action != "" || len(n.Inputs) > 0 || n.Checkpoint.Condition != "" || n.Focus || n.FailureCrop {
			return fail("", "resolve requires application or exact title and optional owner_ref/wait_ms, without operations")
		}
		*calls += 2
		return nil
	case "file_check":
		if n.WindowRef != "" || n.Application != "" || n.Title != "" || n.OwnerRef != "" || n.WaitMS != 0 || n.Input.Action != "" || len(n.Inputs) != 0 || n.Checkpoint != (computer.AutomationRequest{}) || n.Focus || n.FailureCrop {
			return fail("", "file_check takes only file and next; it reads the file system, never a window")
		}
		if err := validateDesktopFileCheck(n.File); err != nil {
			return fail("file", err.Error())
		}
		*calls++
		return nil
	}
	if n.WindowRef == "" {
		return fail("window_ref", "this node kind requires window_ref naming an earlier prepare/resolve node")
	}
	if n.Application != "" || n.Title != "" || n.OwnerRef != "" || n.WaitMS != 0 {
		return fail("", "application, title, owner_ref and wait_ms belong to prepare/resolve nodes")
	}
	if n.Checkpoint.WindowID != "" {
		return fail("checkpoint.window_id", "omit window_id; the binding supplies it at run time")
	}
	if n.Kind == "close" || n.Kind == "rename" {
		if n.Input.Action != "" || len(n.Inputs) > 0 || n.Checkpoint != (computer.AutomationRequest{}) || n.FailureCrop {
			return fail("", "close/rename verify their own result and cannot include other inputs/checkpoints")
		}
		if n.Kind == "rename" {
			if n.Rename == nil || !desktopRenameName(n.Rename.OldName) || !desktopRenameName(n.Rename.NewName) {
				return fail("rename", "rename requires exact safe old_name/new_name")
			}
			if err := validateRenameConflictPolicy(n.Rename); err != nil {
				return fail("rename.on_conflict", err.Error())
			}
		}
		*calls += 20
		return nil
	}
	q := n.Checkpoint
	if f.RunID != "" && q.ElementID != "" {
		return fail("checkpoint.element_id", "durable flows require stable named/role selectors, not runtime element IDs")
	}
	q.WindowID = "1"
	if n.Capture != nil && q.Expected == "" {
		if n.Kind != "verify" {
			return fail("checkpoint.expected", "an operation's checkpoint needs expected; read an unknown value with a following verify node that captures it")
		}
		// A capture reads a value that is not known yet; its pattern is the
		// check. Stand in for the expected text only to validate the shape.
		q.Expected = "capture"
		if q.Condition == "text" && q.Name != "" {
			return fail("checkpoint.name", "a text capture cannot select by name, because name is the label it reads; select the observed element_id or use value/document_text")
		}
	}
	q.Expected = desktopFlowPlaceholderProbeText(q.Expected)
	if err := validateDesktopCheckpoint(q, "1"); err != nil {
		return fail("checkpoint", err.Error())
	}
	if len([]rune(q.Expected)) > 4096 {
		return fail("checkpoint.expected", "flow checkpoint exceeds readback bound")
	}
	if n.Capture != nil {
		if err := validateDesktopCapture(n.Capture, n.Checkpoint); err != nil {
			return fail("capture", err.Error())
		}
	}
	switch n.Kind {
	case "branch", "verify":
		if n.Input.Action != "" || len(n.Inputs) != 0 || n.Focus || (n.Kind == "branch" && q.WaitMS != 0) {
			return fail("", "branch/verify cannot mutate; branch predicates cannot wait")
		}
	case "operation":
		inputs := desktopFlowInputs(n)
		if len(inputs) == 0 || len(inputs) > 16 || (n.Input.Action != "" && len(n.Inputs) > 0) {
			return fail("inputs", "flow operation requires input or 1-16 inputs")
		}
		for k, input := range inputs {
			field := "input"
			if len(n.Inputs) > 0 {
				field = fmt.Sprintf("inputs[%d]", k)
			}
			if f.RunID != "" && input.Automation.ElementID != "" {
				return fail(field+".automation.element_id", "durable flows require fresh control resolution from named/role selectors")
			}
			if input.Automation.WindowID != "" {
				return fail(field+".automation.window_id", "flow input window_id must be omitted; use window_ref")
			}
			input.Automation.WindowID = "1"
			input.Text = desktopFlowPlaceholderProbeText(input.Text)
			input.Automation.Text = desktopFlowPlaceholderProbeText(input.Automation.Text)
			if err := validateDesktopInput(input); err != nil {
				return fail(field, err.Error())
			}
		}
		*calls += len(inputs)*2 + 2
	default:
		return fail("kind", fmt.Sprintf("unsupported flow node kind %q; use prepare, resolve, operation, verify, branch, file_check, rename or close", n.Kind))
	}
	*calls += 2
	return nil
}

// validateDesktopFlowClosedRefs rejects a plan that uses a window binding on
// a path after a node that closes that window. The handle is gone; a later
// node must bind the window again with its own resolve node.
func validateDesktopFlowClosedRefs(nodes []DesktopFlowNode, ids map[string]int) error {
	for i, n := range nodes {
		closed := ""
		switch {
		case n.Kind == "close":
			closed = n.WindowRef
		case n.ClosesWindow:
			closed = n.WindowRef
		}
		if closed == "" {
			continue
		}
		seen := map[int]bool{}
		queue := []int{}
		push := func(edges ...string) {
			for _, edge := range edges {
				if j, ok := ids[edge]; ok && edge != "" && !seen[j] {
					seen[j] = true
					queue = append(queue, j)
				}
			}
		}
		push(n.Next, n.Then, n.Else)
		for len(queue) > 0 {
			later := nodes[queue[0]]
			queue = queue[1:]
			if later.WindowRef == closed || later.CheckpointRef == closed || later.OwnerRef == closed {
				return flowError(ids[later.ID], later.ID, "window_ref", fmt.Sprintf("window binding %s is closed by node %s before this node uses it; bind the window again with a later resolve node", closed, nodes[i].ID))
			}
			push(later.Next, later.Then, later.Else)
		}
	}
	return nil
}

func desktopFlowInputs(n DesktopFlowNode) []ComputerParams {
	if len(n.Inputs) > 0 {
		return n.Inputs
	}
	if n.Input.Action != "" {
		return []ComputerParams{n.Input}
	}
	return nil
}

// desktopFlowReplacement reports a plan whose checkpoint selects the very
// field and value its single set_value writes. It is a plan-level fact only:
// the runtime must still prove that the checkpoint reads the input's own
// window and control before its readback may stand for the write.
func desktopFlowReplacement(n DesktopFlowNode) bool {
	inputs := desktopFlowInputs(n)
	if len(inputs) != 1 || inputs[0].Action != "set_value" {
		return false
	}
	i, q := inputs[0].Automation, n.Checkpoint
	inputRole, inputErr := computer.NormalizeRole(i.Role)
	checkRole, checkErr := computer.NormalizeRole(q.Role)
	return inputErr == nil && checkErr == nil && q.Condition == "value" && q.Expected == i.Text && q.ElementID == i.ElementID && q.Name == i.Name && inputRole == checkRole
}

// desktopFlowSameWindow compares the live identities of two bindings. The
// same field name and value in another window, or in a window that replaced
// the bound one, is not evidence about the input's target.
func desktopFlowSameWindow(a, b desktopWindowInfo) bool {
	return a.ID != "" && a.ID == b.ID && a.ProcessID == b.ProcessID && a.ProcessName == b.ProcessName && a.WindowClass == b.WindowClass
}

func runDesktopFlow(ctx context.Context, p DesktopWorkflowParams, parent fantasy.ToolCall, invoke func(context.Context, fantasy.ToolCall) (fantasy.ToolResponse, error)) (fantasy.ToolResponse, error) {
	if err := validateDesktopFlow(p); err != nil {
		return errorResponse(err), nil
	}
	ctx, cancel := context.WithTimeout(ctx, desktopFlowTimeout(ctx))
	defer cancel()
	f := *p.Flow
	state, save, release, err := openDesktopFlow(ctx, f)
	if err != nil {
		return fantasy.NewTextErrorResponse(err.Error()), nil
	}
	defer release()
	// effects counts mutating children that may have reached the desktop:
	// every invoked mutation except one whose adapter-owned outcome proves
	// dispatch:none. A missing or unreadable outcome counts as dispatched.
	// Children refused before invoke (validation, cancellation) never count.
	effects := 0
	subjects := []string{}
	if state.Pending != "" {
		// A journal deliberately contains no runtime handles. An inherited
		// effect must not be attributed to a newly resolved window.
		subjects = append(subjects, "*")
	}
	counted := func(ctx context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
		r, err := invoke(ctx, call)
		var input ComputerParams
		_ = json.Unmarshal([]byte(call.Input), &input)
		if computerMutates(strings.ToLower(strings.TrimSpace(input.Action))) {
			if native, ok := ResponseOutcome(r); !ok || native.Dispatch != "none" {
				effects++
				targets := native.Subjects
				if !ok || len(targets) == 0 {
					targets = []string{"*"}
					if input.Automation.WindowID != "" {
						targets = []string{"desktop:" + input.Automation.WindowID}
					}
				}
				for _, subject := range targets {
					if subject == "" || subject == "desktop:" {
						// Launch adapters may have no window yet. Use the
						// bound input window if known, otherwise invalidate
						// conservatively instead of inventing an empty scope.
						subject = "*"
						if input.Automation.WindowID != "" {
							subject = "desktop:" + input.Automation.WindowID
						}
					}
					if !slices.Contains(subjects, subject) {
						subjects = append(subjects, subject)
					}
				}
			}
		}
		return r, err
	}
	child := desktopRecipeChild(parent, counted)
	calls := 0
	readRetries := 0
	bounded := func(ctx context.Context, input ComputerParams) (fantasy.ToolResponse, error) {
		for {
			calls++
			if calls > maxDesktopFlowChildCalls {
				return fantasy.NewTextErrorResponse("flow call budget exhausted; resume only from saved progress"), nil
			}
			r, err := child(ctx, input)
			read := input.Action == "find" || input.Action == "windows" || input.Action == "assert"
			transient := strings.HasPrefix(r.Content, "accessibility_unavailable:") || strings.HasPrefix(r.Content, "observation_incomplete:")
			if !read || r.StopTurn || ctx.Err() != nil || readRetries >= 2 || (err == nil && (!r.IsError || !transient)) {
				return r, err
			}
			readRetries++
		}
	}
	bindings := map[string]desktopWindowInfo{}
	// closedBy names the node that closed a binding's window, so a later use
	// fails with that reason instead of addressing a dead handle.
	closedBy := map[string]string{}
	// preexisting holds the windows open before this call. A close node only
	// closes windows this task opened: absent from that list, or reported as
	// task-opened by the input tracker in an earlier call.
	var preexisting map[string]bool
	needsOwnership := slices.ContainsFunc(f.Nodes, func(n DesktopFlowNode) bool { return n.Kind == "close" && !n.AllowUnowned })
	trace := []map[string]any{}
	finish := func(r fantasy.ToolResponse, e error) (fantasy.ToolResponse, error) {
		flow := map[string]any{"run_id": f.RunID, "next": state.Next, "pending": state.Pending, "completed": state.Completed, "calls": calls, "read_retries": readRetries, "steps": trace, "durable": f.RunID != "", "task_completion_requires_verification": true}
		ledger, resume := desktopFlowLedger(f, state, r, e, ctx.Err() != nil)
		flow["nodes"] = ledger
		if resume != "" {
			flow["resume"] = resume
		}
		data := desktopFlowReport(flow, trace)
		// The child's metadata (question, permission, hook, pause and
		// outcome evidence) stays where its consumers read it; the flow
		// ledger is added beside it, never in its place.
		aggregate := ToolOutcome{Dispatch: "none", Source: "tool_pipeline:desktop_flow", Kind: outcome.KindObservation, Claim: "desktop_flow"}
		if effects > 0 || state.Pending != "" {
			aggregate.Dispatch, aggregate.Mutation, aggregate.Kind = "sent", true, outcome.KindOutput
			aggregate.Subjects = subjects
			if state.Pending != "" {
				aggregate.Dispatch = "unknown"
				if len(aggregate.Subjects) == 0 {
					aggregate.Subjects = []string{"*"}
				}
			}
		}
		// Checkpoints verify individual nodes, not every affected subject or
		// the user's whole task. Never inherit the last child's verified bit.
		r.Metadata = desktopFlowMetadata(r.Metadata, data, aggregate)
		if report := `{"flow":` + string(data) + `}`; len(r.Content)+len(report) < 64*1024 {
			r.Content += "\n" + report
		}
		return r, e
	}
	advance := func(n DesktopFlowNode, next string) error {
		state.Completed = append(state.Completed, desktopFlowRecord{ID: n.ID, Next: next})
		state.Pending, state.Next = "", next
		return save(state)
	}
	storeCapture := func(n DesktopFlowNode, q computer.AutomationRequest, r fantasy.ToolResponse, entry map[string]any) error {
		if n.Capture == nil {
			return nil
		}
		value, err := captureFlowValue(r.Content, q, n.Capture)
		if err != nil {
			return err
		}
		if state.Captures == nil {
			state.Captures = map[string]string{}
		}
		state.Captures[n.Capture.Name] = value
		entry["captured"] = map[string]string{n.Capture.Name: value}
		return nil
	}
	// stopEffect ends the flow at an effect node. When the node recorded its
	// intent in this call (fresh) and no child since mark can have reached the
	// desktop, the intent is withdrawn: the node stopped before its effect
	// (permission, question, hook, cancellation, a refused target) and its
	// attempt is not consumed, so a resume runs it with fresh checks. Any
	// possible dispatch keeps the intent, so resume reconciles instead of
	// replaying. A journal that cannot be updated keeps the intent too.
	stopEffect := func(n DesktopFlowNode, entry map[string]any, fresh bool, mark int, r fantasy.ToolResponse, e error) (fantasy.ToolResponse, error) {
		if fresh && state.Pending == n.ID && effects == mark {
			state.Pending = ""
			if state.Attempts[n.ID]--; state.Attempts[n.ID] <= 0 {
				delete(state.Attempts, n.ID)
			}
			if err := save(state); err != nil {
				state.Pending = n.ID
				state.Attempts[n.ID]++
				entry["journal_error"] = err.Error()
			} else {
				entry["dispatch"] = "none"
			}
		}
		return finish(r, e)
	}
	index := map[string]DesktopFlowNode{}
	for _, n := range f.Nodes {
		index[n.ID] = n
	}
	// Rebuild only bindings needed by remaining work. Closed earlier windows do
	// not block a later, independent application. Never replay completed input.
	completedBindings := map[string]bool{}
	for _, record := range state.Completed {
		if index[record.ID].Kind == "resolve" || index[record.ID].Kind == "prepare" {
			completedBindings[record.ID] = true
		}
	}
	var restore func(string) (fantasy.ToolResponse, error)
	restore = func(ref string) (fantasy.ToolResponse, error) {
		if ref == "" {
			return fantasy.NewTextResponse(""), nil
		}
		if _, ok := bindings[ref]; ok {
			return fantasy.NewTextResponse(""), nil
		}
		if !f.Resume || !completedBindings[ref] {
			return fantasy.NewTextErrorResponse("missing_binding: selected path did not resolve this window"), nil
		}
		n := index[ref]
		// Restoring a prepare binding only resolves; it never launches again.
		n.Kind = "resolve"
		if r, e := restore(n.OwnerRef); desktopRecipeStopped(r, e) {
			return r, e
		}
		w, r, e := desktopFlowResolve(ctx, n, bindings, bounded)
		if !desktopRecipeStopped(r, e) {
			bindings[ref] = w
		}
		return r, e
	}
	for state.Next != "" {
		n := index[state.Next]
		if err := ctx.Err(); err != nil {
			return finish(fantasy.ToolResponse{}, err)
		}
		entry := map[string]any{"id": n.ID, "kind": n.Kind}
		trace = append(trace, entry)
		// Captured values fill the node's text fields before anything of it
		// runs; a missing value stops here with nothing sent.
		n, err = substituteFlowNode(n, state.Captures)
		if err != nil {
			return finish(fantasy.NewTextErrorResponse(err.Error()), nil)
		}
		if n.Kind == "file_check" {
			r, e := bounded(ctx, ComputerParams{Action: "file_check", File: n.File})
			if desktopRecipeStopped(r, e) {
				return finish(r, e)
			}
			var evidence struct {
				Path          string `json:"path"`
				RequestedPath string `json:"requested_path"`
				Condition     string `json:"condition"`
				Passed        bool   `json:"passed"`
			}
			if json.Unmarshal([]byte(r.Content), &evidence) != nil || !evidence.Passed || evidence.Condition != n.File.Condition || (evidence.Path != n.File.Path && evidence.RequestedPath != n.File.Path) {
				return finish(fantasy.NewTextErrorResponse("checkpoint_unverified: file check returned no matching native evidence"), nil)
			}
			entry["status"], entry["evidence"] = "verified", json.RawMessage(r.Content)
			if err := advance(n, n.Next); err != nil {
				return finish(fantasy.NewTextErrorResponse(err.Error()), nil)
			}
			continue
		}
		if state.Pending == n.ID && n.Kind != "operation" {
			return finish(fantasy.NewTextErrorResponse("uncertain_effect: interrupted prepare/rename/close cannot be replayed; inspect fresh state"), nil)
		}
		if needsOwnership && preexisting == nil {
			// Taken before the first node of this call runs, after the
			// journal check, so an interrupted run still dispatches nothing.
			windows, r, e := desktopFlowWindows(ctx, bounded)
			if desktopRecipeStopped(r, e) {
				return finish(r, e)
			}
			preexisting = map[string]bool{}
			for _, w := range windows {
				preexisting[w.ID] = true
			}
		}
		if n.Kind == "prepare" {
			if err := desktopFlowIntent(n, state, save); err != nil {
				return finish(fantasy.NewTextErrorResponse(err.Error()), nil)
			}
			mark := effects
			w, r, e := desktopFlowPrepare(ctx, n, parent, bounded)
			if desktopRecipeStopped(r, e) {
				return stopEffect(n, entry, true, mark, r, e)
			}
			bindings[n.ID] = w
			entry["window_id"], entry["status"] = w.ID, "prepared"
			if err := advance(n, n.Next); err != nil {
				return finish(fantasy.NewTextErrorResponse(err.Error()), nil)
			}
			continue
		}
		if n.Kind == "resolve" {
			if r, e := restore(n.OwnerRef); desktopRecipeStopped(r, e) {
				return finish(r, e)
			}
			w, r, e := desktopFlowResolve(ctx, n, bindings, bounded)
			if desktopRecipeStopped(r, e) {
				return finish(r, e)
			}
			bindings[n.ID] = w
			entry["window_id"] = w.ID
			if err := advance(n, n.Next); err != nil {
				return finish(fantasy.NewTextErrorResponse(err.Error()), nil)
			}
			continue
		}
		if r, e := restore(n.WindowRef); desktopRecipeStopped(r, e) {
			return finish(r, e)
		}
		window, ok := bindings[n.WindowRef]
		if !ok {
			if by := closedBy[n.WindowRef]; by != "" {
				return finish(fantasy.NewTextErrorResponse("window_closed: binding "+n.WindowRef+" was closed by node "+by+"; bind the window again with a resolve node"), nil)
			}
			return finish(fantasy.NewTextErrorResponse("missing_binding: selected branch did not resolve the required window"), nil)
		}
		if n.Kind == "close" && !n.AllowUnowned && preexisting[window.ID] && !desktopTaskOpenedWindow(ctx, window.ID) {
			return finish(fantasy.NewTextErrorResponse("not_task_owned: window "+window.ID+" was open before this task and was not opened by it; nothing was sent and focus was not changed. Close it only if the user explicitly asked, with allow_unowned:true on this close node."), nil)
		}
		if r, e := desktopFlowIdentity(ctx, window, n.Focus || n.Kind == "close" || n.Kind == "rename", bounded); desktopRecipeStopped(r, e) {
			return finish(r, e)
		}
		if n.Kind == "rename" || n.Kind == "close" {
			if err := desktopFlowIntent(n, state, save); err != nil {
				return finish(fantasy.NewTextErrorResponse(err.Error()), nil)
			}
			mark := effects
			r, e := desktopFlowRecipe(ctx, n, window, parent, bounded)
			if desktopRecipeStopped(r, e) {
				return stopEffect(n, entry, true, mark, r, e)
			}
			if n.Kind == "close" {
				delete(bindings, n.WindowRef)
				closedBy[n.WindowRef] = n.ID
				entry["closed_window_id"] = window.ID
			}
			entry["status"], entry["assertion"] = "verified", json.RawMessage(r.Content)
			if err := advance(n, n.Next); err != nil {
				return finish(fantasy.NewTextErrorResponse(err.Error()), nil)
			}
			continue
		}
		q := n.Checkpoint
		q.WindowID = window.ID
		checkpointWindow := window
		if n.CheckpointRef != "" {
			if r, e := restore(n.CheckpointRef); desktopRecipeStopped(r, e) {
				return finish(r, e)
			}
			target, ok := bindings[n.CheckpointRef]
			if !ok {
				if by := closedBy[n.CheckpointRef]; by != "" {
					return finish(fantasy.NewTextErrorResponse("window_closed: checkpoint binding "+n.CheckpointRef+" was closed by node "+by), nil)
				}
				return finish(fantasy.NewTextErrorResponse("missing_binding: checkpoint_ref did not resolve on this path"), nil)
			}
			checkpointWindow = target
			q.WindowID = target.ID
		}
		if n.Kind == "branch" {
			matched, r, e := desktopFlowPredicate(ctx, q, bounded)
			if desktopRecipeStopped(r, e) {
				return finish(r, e)
			}
			next := n.Else
			if matched {
				next = n.Then
			}
			entry["matched"], entry["next"] = matched, next
			if err := advance(n, next); err != nil {
				return finish(fantasy.NewTextErrorResponse(err.Error()), nil)
			}
			continue
		}
		fresh, mark := false, effects
		if n.Kind == "operation" {
			// A checkpoint may stand for the write only when it reads the
			// input's own live window: the same field name and value in
			// another window (checkpoint_ref) proves nothing about the
			// input's target, so the write is sent and that window's
			// checkpoint is read afterwards as the plan says.
			replacement := desktopFlowReplacement(n)
			if replacement && !desktopFlowSameWindow(window, checkpointWindow) {
				replacement = false
				entry["replacement"] = "not_applicable: checkpoint reads another window"
			}
			if state.Pending == n.ID && !replacement {
				return finish(fantasy.NewTextErrorResponse("uncertain_effect: interrupted mutation must be reconciled from fresh evidence; no automatic replay"), nil)
			}
			if replacement {
				matched, r, e := desktopFlowPredicate(ctx, q, bounded)
				if desktopRecipeStopped(r, e) {
					return finish(r, e)
				}
				if matched {
					if n.Capture != nil {
						// The predicate's find response is not an assert readback.
						// Recheck the value and capture it before advancing the
						// journal, even when no replacement input is needed.
						r, e = bounded(ctx, ComputerParams{Action: "assert", Automation: q})
						if desktopRecipeStopped(r, e) {
							return finish(r, e)
						}
						if !desktopCheckpointReadback(r.Content, q) {
							return finish(fantasy.NewTextErrorResponse("checkpoint_unverified: replacement capture needs a matching readback"), nil)
						}
						if err := storeCapture(n, q, r, entry); err != nil {
							return finish(fantasy.NewTextErrorResponse(err.Error()), nil)
						}
					}
					entry["status"] = "replacement_already_verified"
					if err := advance(n, n.Next); err != nil {
						return finish(fantasy.NewTextErrorResponse(err.Error()), nil)
					}
					continue
				}
			}
			if state.Attempts[n.ID] >= 2 {
				return finish(fantasy.NewTextErrorResponse("recovery_exhausted: operation reached its durable attempt limit"), nil)
			}
			// Persist intent before any mutation, including multi-input
			// operations. An intent inherited from an interrupted run is not
			// fresh: it may have taken effect and is never withdrawn here.
			fresh = state.Pending != n.ID
			state.Pending = n.ID
			state.Attempts[n.ID]++
			if err := save(state); err != nil {
				state.Attempts[n.ID]--
				if fresh {
					state.Pending = ""
					if state.Attempts[n.ID] <= 0 {
						delete(state.Attempts, n.ID)
					}
				}
				return finish(fantasy.NewTextErrorResponse(err.Error()), nil)
			}
			mark = effects
			for _, input := range desktopFlowInputs(n) {
				input.Automation.WindowID = window.ID
				var target *desktopElement
				if input.Action == "invoke" || input.Action == "set_value" {
					e, r, er := desktopFlowTarget(ctx, input.Automation, bounded)
					if desktopRecipeStopped(r, er) {
						return stopEffect(n, entry, fresh, mark, r, er)
					}
					if !e.Enabled || e.Offscreen || e.Password {
						return stopEffect(n, entry, fresh, mark, fantasy.NewTextErrorResponse("target_not_actionable: flow control disabled, offscreen or password"), nil)
					}
					input.Automation.ElementID = e.ID
					target = e
				}
				r, e := bounded(ctx, input)
				if desktopRecipeStopped(r, e) {
					// Read-only reconciliation can establish an applied
					// replacement, but only from the control the write
					// targeted: same window, same fresh element.
					if replacement && target != nil && !r.StopTurn && e == nil && ctx.Err() == nil {
						reconciled, stop := desktopFlowReconcile(ctx, q, input.Automation, target.ID, bounded)
						if reconciled {
							entry["status"] = "replacement_reconciled"
							break
						}
						r.StopTurn = r.StopTurn || stop
					}
					return stopEffect(n, entry, fresh, mark, r, e)
				}
			}
		}
		if n.ClosesWindow {
			// The inputs dismissed the window_ref window (a dialog). Prove it
			// is gone and that the checkpoint window is foreground again
			// before reading the result there; never resend the input.
			closed, ce := desktopFlowAwaitAbsence(ctx, window, bounded)
			if desktopRecipeStopped(closed, ce) {
				return stopEffect(n, entry, fresh, mark, closed, ce)
			}
			delete(bindings, n.WindowRef)
			closedBy[n.WindowRef] = n.ID
			entry["closed_window_id"] = window.ID
			if r, e := desktopFlowIdentity(ctx, checkpointWindow, false, bounded); desktopRecipeStopped(r, e) {
				return stopEffect(n, entry, fresh, mark, r, e)
			}
			entry["returned_to"] = checkpointWindow.ID
		}
		// A capture without expected reads the current value once: an assert
		// has nothing to wait for, so the one control is read with find and
		// the capture pattern is the check.
		readOnly := n.Capture != nil && q.Expected == ""
		var r fantasy.ToolResponse
		var e error
		if readOnly {
			r, e = desktopFlowReadValue(ctx, q, bounded)
		} else {
			r, e = bounded(ctx, ComputerParams{Action: "assert", Automation: q})
		}
		if desktopRecipeStopped(r, e) || (!readOnly && !desktopCheckpointReadback(r.Content, q)) {
			if e == nil && !r.IsError {
				r = fantasy.NewTextErrorResponse("checkpoint_unverified: required native readback missing or mismatched")
			}
			if n.FailureCrop && !r.StopTurn && ctx.Err() == nil {
				r = desktopFlowFailureCrop(ctx, q, checkpointWindow, r, bounded)
			}
			return stopEffect(n, entry, fresh, mark, r, e)
		}
		if err := storeCapture(n, q, r, entry); err != nil {
			return stopEffect(n, entry, fresh, mark, fantasy.NewTextErrorResponse(err.Error()), nil)
		}
		entry["status"], entry["assertion"] = "verified", json.RawMessage(r.Content)
		entry["assertion_hash"] = engineering.Hash(r.Content)
		if err := advance(n, n.Next); err != nil {
			return finish(fantasy.NewTextErrorResponse(err.Error()), nil)
		}
	}
	return finish(fantasy.NewTextResponse("Flow reached its verified terminal; inspect acceptance criteria before reporting the entire task complete."), nil)
}

// desktopFlowReconcile decides, read-only, whether a set_value whose result
// was reported as an error took effect anyway: the checkpoint in the input's
// own window must pass and the selector must still resolve to the element
// the write targeted. It returns whether the write is reconciled and whether
// a read stopped the turn.
func desktopFlowReconcile(ctx context.Context, q, selector computer.AutomationRequest, elementID string, child desktopFlowChild) (bool, bool) {
	checked, err := child(ctx, ComputerParams{Action: "assert", Automation: q})
	if desktopRecipeStopped(checked, err) || !desktopCheckpointReadback(checked.Content, q) {
		return false, checked.StopTurn
	}
	e, r, err := desktopFlowTarget(ctx, selector, child)
	if desktopRecipeStopped(r, err) {
		return false, r.StopTurn
	}
	return e.ID == elementID, false
}

// Desktop flow report budget. Readbacks are dropped first, then per-node
// evidence; the node ledger, cursor and resume advice always remain.
const maxDesktopFlowReport = 48 * 1024

func desktopFlowReport(flow map[string]any, trace []map[string]any) []byte {
	data, _ := json.Marshal(flow)
	if len(data) > maxDesktopFlowReport {
		for _, entry := range trace {
			delete(entry, "assertion")
		}
		flow["readbacks_omitted"] = true
		data, _ = json.Marshal(flow)
	}
	if len(data) > maxDesktopFlowReport {
		delete(flow, "steps")
		flow["steps_omitted"] = len(trace)
		data, _ = json.Marshal(flow)
	}
	return data
}

// desktopFlowMetadata adds the flow ledger to the metadata of the response
// that ended the flow without losing any of it:
//   - a JSON object keeps every key where its consumers read it (question,
//     permission, hook, waiting_for_user, error codes, settle evidence);
//   - anything else is kept verbatim as native_metadata, as withToolOutcome
//     does;
//   - a child key named flow is kept as child_flow, and flow holds the ledger;
//   - atlas_outcome always describes the whole tool call, including when the
//     child has no outcome. It names dispatched mutation subjects, never
//     the last read's subject; the child is kept as child_atlas_outcome.
func desktopFlowMetadata(child string, flow []byte, aggregate ToolOutcome) string {
	meta := map[string]json.RawMessage{}
	if child != "" {
		if json.Unmarshal([]byte(child), &meta) != nil || meta == nil {
			meta = map[string]json.RawMessage{"native_metadata": json.RawMessage(strconvJSON(child))}
		}
	}
	if _, ok := meta["child_flow"]; ok {
		meta["flow_metadata_parent"] = json.RawMessage(strconvJSON(child))
	}
	if _, ok := meta["child_atlas_outcome"]; ok {
		meta["flow_metadata_parent"] = json.RawMessage(strconvJSON(child))
	}
	if previous, ok := meta["flow"]; ok {
		meta["child_flow"] = previous
	}
	if previous, ok := meta["atlas_outcome"]; ok {
		meta["child_atlas_outcome"] = previous
	}
	meta["atlas_outcome"], _ = json.Marshal(aggregate)
	meta["flow"] = flow
	data, err := json.Marshal(meta)
	if err != nil {
		return `{"flow":` + string(flow) + `,"native_metadata":` + strconvJSON(child) + `}`
	}
	return string(data)
}

func desktopFlowSession(ctx context.Context) string {
	return engineering.GetScope(ctx, GetSessionFromContext(ctx)).SessionID
}

// desktopTaskOpenedWindow reports a window the input tracker saw appear
// after an input of this session and reported to the caller.
func desktopTaskOpenedWindow(ctx context.Context, id string) bool {
	desktopTransitions.Lock()
	state := desktopTransitions.sessions[desktopFlowSession(ctx)]
	desktopTransitions.Unlock()
	if state == nil {
		return false
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	_, ok := state.taskOpened[id]
	return ok
}

// desktopFlowAwaitAbsence proves that w closed after an operation's inputs.
// A foreground dialog owned by it stops the flow with that dialog's identity
// instead of being dismissed blindly.
func desktopFlowAwaitAbsence(ctx context.Context, w desktopWindowInfo, child desktopFlowChild) (fantasy.ToolResponse, error) {
	waitCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	for {
		windows, r, err := desktopFlowWindows(waitCtx, child)
		if desktopRecipeStopped(r, err) {
			if ctx.Err() == nil && waitCtx.Err() != nil {
				return fantasy.NewTextErrorResponse("close_unverified: the window did not close after the operation; the input was not replayed"), nil
			}
			return r, err
		}
		present := false
		for _, current := range windows {
			if current.ID == w.ID {
				present = true
			}
			if current.Owner == w.ID && current.ProcessID == w.ProcessID && current.Foreground {
				data, _ := json.Marshal(current)
				return fantasy.NewTextErrorResponse("close_blocked: the window opened an owned dialog instead of closing; a new grounded decision is required and no further key was sent. " + string(data)), nil
			}
		}
		if !present {
			return fantasy.NewTextResponse(""), nil
		}
		timer := time.NewTimer(100 * time.Millisecond)
		select {
		case <-waitCtx.Done():
			timer.Stop()
			if ctx.Err() != nil {
				return fantasy.ToolResponse{}, ctx.Err()
			}
			return fantasy.NewTextErrorResponse("close_unverified: the window is still present after the operation; the input was not replayed"), nil
		case <-timer.C:
		}
	}
}

// Desktop flow node states in the result ledger. Only verified means the
// node's own checkpoint passed; bound and evaluated are identity and branch
// facts; uncertain means input may have been sent and must be reconciled.
const (
	flowNodeBound     = "bound"
	flowNodeEvaluated = "evaluated"
	flowNodeVerified  = "verified"
	flowNodeUncertain = "uncertain"
	flowNodeFailed    = "failed"
	flowNodeBlocked   = "blocked"
	flowNodeCanceled  = "canceled"
	flowNodeNotRun    = "not_run"
	flowNodeSkipped   = "skipped_branch"
)

// desktopFlowLedger states every node's outcome so a partial result never
// hides what already took effect, and says where a follow-up continues.
func desktopFlowLedger(f DesktopFlowParams, state *desktopFlowState, r fantasy.ToolResponse, err error, canceled bool) ([]map[string]string, string) {
	done := map[string]bool{}
	for _, record := range state.Completed {
		done[record.ID] = true
	}
	failed := err != nil || r.IsError || r.StopTurn
	ledger := make([]map[string]string, 0, len(f.Nodes))
	for _, n := range f.Nodes {
		status := flowNodeNotRun
		switch {
		case done[n.ID] && (n.Kind == "prepare" || n.Kind == "resolve"):
			status = flowNodeBound
		case done[n.ID] && n.Kind == "branch":
			status = flowNodeEvaluated
		case done[n.ID]:
			status = flowNodeVerified
		case n.ID == state.Next && failed:
			switch {
			case state.Pending == n.ID:
				status = flowNodeUncertain
			case canceled:
				status = flowNodeCanceled
			case r.StopTurn:
				status = flowNodeBlocked
			default:
				status = flowNodeFailed
			}
		case !failed:
			status = flowNodeSkipped
		}
		ledger = append(ledger, map[string]string{"id": n.ID, "kind": n.Kind, "state": status})
	}
	resume := ""
	switch {
	case !failed:
	case state.Pending != "" && state.Pending == state.Next:
		resume = "Node " + state.Next + " may have taken effect: reconcile it from fresh evidence. Verified nodes must not be repeated; window handles are re-resolved, never reused."
	default:
		resume = "Stopped at node " + state.Next + " before its effect. Verified nodes must not be repeated; continue with only the remaining nodes (or the same plan with run_id and resume:true)."
	}
	return ledger, resume
}
