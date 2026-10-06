package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/computer"
	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
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
	Kind        string                     `json:"kind" description:"prepare (find/launch/focus/bind), resolve (bind an open window), branch, operation, verify, rename or close (focus and verify closure)."`
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
}

type desktopFlowStoreKey struct{}

func validateDesktopFlow(p DesktopWorkflowParams) error {
	f := p.Flow
	if f == nil || len(f.Nodes) == 0 || len(f.Nodes) > 64 || p.Result != "" || p.Application != "" || p.WindowID != "" || p.WaitMS != 0 || p.Input.Action != "" || len(p.Inputs)+len(p.Steps)+len(p.AdaptiveSteps) != 0 || p.Transition != nil || p.WaitFor.Condition != "" || p.Observation == "visual" {
		return fmt.Errorf("flow requires 1-64 nodes without other recipe parameters or visual observation")
	}
	if len(f.RunID) > 128 || (f.Resume && f.RunID == "") {
		return fmt.Errorf("invalid durable flow identity")
	}
	if p.MaxElements != 0 || (p.Observation != "" && p.Observation != "auto" && p.Observation != "semantic") {
		return fmt.Errorf("flow uses targeted checkpoint max_elements and auto/semantic verification")
	}
	ids := map[string]int{}
	for i, n := range f.Nodes {
		if strings.TrimSpace(n.ID) == "" || len(n.ID) > 64 {
			return fmt.Errorf("invalid flow node identity")
		}
		if _, exists := ids[n.ID]; exists {
			return fmt.Errorf("duplicate flow node identity")
		}
		ids[n.ID] = i
	}
	calls := 0
	for i, n := range f.Nodes {
		for _, edge := range []string{n.Next, n.Then, n.Else} {
			if edge != "" {
				j, ok := ids[edge]
				if !ok || j <= i {
					return fmt.Errorf("flow edges must name later nodes; cycles are prohibited")
				}
			}
		}
		for _, ref := range []string{n.WindowRef, n.OwnerRef} {
			if ref != "" {
				j, ok := ids[ref]
				if !ok || j >= i || (f.Nodes[j].Kind != "resolve" && f.Nodes[j].Kind != "prepare") {
					return fmt.Errorf("window binding must refer to a preceding prepare/resolve node")
				}
			}
		}
		if n.Kind != "branch" && (n.Then != "" || n.Else != "") {
			return fmt.Errorf("then/else require branch")
		}
		if n.Kind == "branch" && n.Next != "" {
			return fmt.Errorf("branch uses then/else instead of next")
		}
		if n.Rename != nil && n.Kind != "rename" {
			return fmt.Errorf("rename names require rename node")
		}
		if n.Kind == "prepare" {
			if n.Application == "" || n.Title != "" || n.OwnerRef != "" || n.WindowRef != "" || n.Input.Action != "" || len(n.Inputs) != 0 || n.Checkpoint != (computer.AutomationRequest{}) || n.Focus || n.FailureCrop || n.WaitMS < 0 || n.WaitMS > 15000 {
				return fmt.Errorf("prepare requires application and optional wait_ms without other node parameters")
			}
			calls += 32
			continue
		}
		if n.Kind == "resolve" {
			if (n.Application == "" && n.Title == "") || (n.OwnerRef != "" && n.Title == "") || n.WaitMS < 0 || n.WaitMS > 15000 || n.WindowRef != "" || n.Input.Action != "" || len(n.Inputs) > 0 || n.Checkpoint.Condition != "" || n.Focus || n.FailureCrop {
				return fmt.Errorf("resolve requires application or exact title and optional owner, without operations")
			}
			calls += 2
			continue
		}
		if n.WindowRef == "" || n.Application != "" || n.Title != "" || n.OwnerRef != "" || n.WaitMS != 0 || n.Checkpoint.WindowID != "" {
			return fmt.Errorf("flow operation requires window_ref and unbound checkpoint")
		}
		if n.Kind == "close" || n.Kind == "rename" {
			if n.Input.Action != "" || len(n.Inputs) > 0 || n.Checkpoint != (computer.AutomationRequest{}) || n.FailureCrop {
				return fmt.Errorf("close/rename verify their own result and cannot include other inputs/checkpoints")
			}
			if n.Kind == "rename" && (n.Rename == nil || !desktopRenameName(n.Rename.OldName) || !desktopRenameName(n.Rename.NewName)) {
				return fmt.Errorf("rename requires exact safe old_name/new_name")
			}
			calls += 20
			continue
		}
		q := n.Checkpoint
		if f.RunID != "" && q.ElementID != "" {
			return fmt.Errorf("durable flows require stable named/role selectors, not runtime element IDs")
		}
		q.WindowID = "1"
		if err := validateDesktopCheckpoint(q, "1"); err != nil {
			return err
		}
		if len([]rune(q.Expected)) > 4096 {
			return fmt.Errorf("flow checkpoint exceeds readback bound")
		}
		switch n.Kind {
		case "branch", "verify":
			if n.Input.Action != "" || len(n.Inputs) != 0 || n.Focus || (n.Kind == "branch" && q.WaitMS != 0) {
				return fmt.Errorf("branch/verify cannot mutate; branch predicates cannot wait")
			}
		case "operation":
			inputs := desktopFlowInputs(n)
			if len(inputs) == 0 || len(inputs) > 16 || (n.Input.Action != "" && len(n.Inputs) > 0) {
				return fmt.Errorf("flow operation requires input or 1-16 inputs")
			}
			for _, input := range inputs {
				if f.RunID != "" && input.Automation.ElementID != "" {
					return fmt.Errorf("durable flows require fresh control resolution from named/role selectors")
				}
				if input.Automation.WindowID != "" {
					return fmt.Errorf("flow input window_id must be omitted; use window_ref")
				}
				input.Automation.WindowID = "1"
				if err := validateDesktopInput(input); err != nil {
					return err
				}
			}
			calls += len(inputs)*2 + 2
		default:
			return fmt.Errorf("unsupported flow node kind")
		}
		calls += 2
	}
	if calls > 384 {
		return fmt.Errorf("flow exceeds bounded operation budget; split at a verified boundary")
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

// Replacement reconciliation requires the very field and value being written.
func desktopFlowReplacement(n DesktopFlowNode) bool {
	inputs := desktopFlowInputs(n)
	if len(inputs) != 1 || inputs[0].Action != "set_value" {
		return false
	}
	i, q := inputs[0].Automation, n.Checkpoint
	return q.Condition == "value" && q.Expected == i.Text && q.ElementID == i.ElementID && q.Name == i.Name && q.Role == i.Role
}

func runDesktopFlow(ctx context.Context, p DesktopWorkflowParams, parent fantasy.ToolCall, invoke func(context.Context, fantasy.ToolCall) (fantasy.ToolResponse, error)) (fantasy.ToolResponse, error) {
	if err := validateDesktopFlow(p); err != nil {
		return fantasy.NewTextErrorResponse(err.Error()), nil
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	f := *p.Flow
	state, save, release, err := openDesktopFlow(ctx, f)
	if err != nil {
		return fantasy.NewTextErrorResponse(err.Error()), nil
	}
	defer release()
	child := desktopRecipeChild(parent, invoke)
	calls := 0
	readRetries := 0
	bounded := func(ctx context.Context, input ComputerParams) (fantasy.ToolResponse, error) {
		for {
			calls++
			if calls > 512 {
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
	trace := []map[string]any{}
	finish := func(r fantasy.ToolResponse, e error) (fantasy.ToolResponse, error) {
		flow := map[string]any{"run_id": f.RunID, "next": state.Next, "pending": state.Pending, "completed": state.Completed, "calls": calls, "read_retries": readRetries, "steps": trace, "durable": f.RunID != "", "task_completion_requires_verification": true}
		meta, _ := json.Marshal(map[string]any{"flow": flow})
		if len(meta) > 48*1024 {
			for _, entry := range trace {
				delete(entry, "assertion")
			}
			flow["readbacks_omitted"] = true
			meta, _ = json.Marshal(map[string]any{"flow": flow})
		}
		r.Metadata = string(meta)
		if len(r.Content)+len(meta) < 64*1024 {
			r.Content += "\n" + string(meta)
		}
		return r, e
	}
	advance := func(n DesktopFlowNode, next string) error {
		state.Completed = append(state.Completed, desktopFlowRecord{ID: n.ID, Next: next})
		state.Pending, state.Next = "", next
		return save(state)
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
		if state.Pending == n.ID && n.Kind != "operation" {
			return finish(fantasy.NewTextErrorResponse("uncertain_effect: interrupted prepare/rename/close cannot be replayed; inspect fresh state"), nil)
		}
		if n.Kind == "prepare" {
			if err := desktopFlowIntent(n, state, save); err != nil {
				return finish(fantasy.NewTextErrorResponse(err.Error()), nil)
			}
			w, r, e := desktopFlowPrepare(ctx, n, parent, bounded)
			if desktopRecipeStopped(r, e) {
				return finish(r, e)
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
			return finish(fantasy.NewTextErrorResponse("missing_binding: selected branch did not resolve the required window"), nil)
		}
		if r, e := desktopFlowIdentity(ctx, window, n.Focus || n.Kind == "close" || n.Kind == "rename", bounded); desktopRecipeStopped(r, e) {
			return finish(r, e)
		}
		if n.Kind == "rename" || n.Kind == "close" {
			if err := desktopFlowIntent(n, state, save); err != nil {
				return finish(fantasy.NewTextErrorResponse(err.Error()), nil)
			}
			r, e := desktopFlowRecipe(ctx, n, window, parent, bounded)
			if desktopRecipeStopped(r, e) {
				return finish(r, e)
			}
			entry["status"], entry["assertion"] = "verified", json.RawMessage(r.Content)
			if err := advance(n, n.Next); err != nil {
				return finish(fantasy.NewTextErrorResponse(err.Error()), nil)
			}
			continue
		}
		q := n.Checkpoint
		q.WindowID = window.ID
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
		if n.Kind == "operation" {
			replacement := desktopFlowReplacement(n)
			if state.Pending == n.ID && !replacement {
				return finish(fantasy.NewTextErrorResponse("uncertain_effect: interrupted mutation must be reconciled from fresh evidence; no automatic replay"), nil)
			}
			if replacement {
				matched, r, e := desktopFlowPredicate(ctx, q, bounded)
				if desktopRecipeStopped(r, e) {
					return finish(r, e)
				}
				if matched {
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
			// Persist intent before any mutation, including multi-input operations.
			state.Pending = n.ID
			state.Attempts[n.ID]++
			if err := save(state); err != nil {
				return finish(fantasy.NewTextErrorResponse(err.Error()), nil)
			}
			for _, input := range desktopFlowInputs(n) {
				input.Automation.WindowID = window.ID
				if input.Action == "invoke" || input.Action == "set_value" {
					e, r, er := desktopFlowTarget(ctx, input.Automation, bounded)
					if desktopRecipeStopped(r, er) {
						return finish(r, er)
					}
					if !e.Enabled || e.Offscreen || e.Password {
						return finish(fantasy.NewTextErrorResponse("target_not_actionable: flow control disabled, offscreen or password"), nil)
					}
					input.Automation.ElementID = e.ID
				}
				r, e := bounded(ctx, input)
				if desktopRecipeStopped(r, e) {
					// Read-only reconciliation can establish an applied replacement.
					if replacement && !r.StopTurn && e == nil && ctx.Err() == nil {
						checked, ce := bounded(ctx, ComputerParams{Action: "assert", Automation: q})
						if !desktopRecipeStopped(checked, ce) && desktopCheckpointReadback(checked.Content, q) {
							entry["status"] = "replacement_reconciled"
							break
						}
						r.StopTurn = r.StopTurn || checked.StopTurn
					}
					return finish(r, e)
				}
			}
		}
		r, e := bounded(ctx, ComputerParams{Action: "assert", Automation: q})
		if desktopRecipeStopped(r, e) || !desktopCheckpointReadback(r.Content, q) {
			if e == nil && !r.IsError {
				r = fantasy.NewTextErrorResponse("checkpoint_unverified: required native readback missing or mismatched")
			}
			if n.FailureCrop && !r.StopTurn && ctx.Err() == nil {
				r = desktopFlowFailureCrop(ctx, q, window, r, bounded)
			}
			return finish(r, e)
		}
		entry["status"], entry["assertion"] = "verified", json.RawMessage(r.Content)
		entry["assertion_hash"] = engineering.Hash(r.Content)
		if err := advance(n, n.Next); err != nil {
			return finish(fantasy.NewTextErrorResponse(err.Error()), nil)
		}
	}
	return finish(fantasy.NewTextResponse("Flow reached its verified terminal; inspect acceptance criteria before reporting the entire task complete."), nil)
}

func desktopFlowSession(ctx context.Context) string {
	return engineering.GetScope(ctx, GetSessionFromContext(ctx)).SessionID
}
