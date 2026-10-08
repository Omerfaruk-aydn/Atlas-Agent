package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/computer"
	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
)

// Desktop input transitions.
//
// A desktop input and the UI change it causes are not simultaneous: a menu,
// a navigated folder or a newly opened window can appear tens to thousands of
// milliseconds after SendInput returns. When an observation is taken in that
// gap, the model sees the old state, concludes the input "did nothing" and
// starts a second mutation (another right-click, a second launch, Ctrl+O
// after Enter on a file). This file tracks each input that may open or change
// a window, waits conditionally before observations that follow it, and
// refuses a new input while a window that appeared after an earlier input has
// not been reported to the model yet. It never sends input itself, never
// replays one and never changes the requested target.

// desktopWindowLister is implemented by native backends that enumerate
// top-level windows cheaply (no UIA provider and no helper process). Backends
// without it keep the previous behavior.
type desktopWindowLister interface {
	ListWindows(ctx context.Context) (json.RawMessage, error)
}

// desktopSettleTiming bounds every wait. The zero value selects defaults.
type desktopSettleTiming struct {
	// MinSettle is the minimum age of the latest input before an
	// observation is captured.
	MinSettle time.Duration
	// Poll is the window-list polling interval.
	Poll time.Duration
	// ReadSettle caps waiting for a stable window list before reads.
	ReadSettle time.Duration
	// OpenWait caps waiting for an expected window change after an input.
	OpenWait time.Duration
	// LaunchWait caps waiting for an application window after launch_app.
	LaunchWait time.Duration
	// Track is how long an unacknowledged transition is remembered.
	Track time.Duration
}

func (t desktopSettleTiming) withDefaults() desktopSettleTiming {
	if t.MinSettle <= 0 {
		t.MinSettle = 300 * time.Millisecond
	}
	if t.Poll <= 0 {
		t.Poll = 50 * time.Millisecond
	}
	if t.ReadSettle <= 0 {
		t.ReadSettle = time.Second
	}
	if t.OpenWait <= 0 {
		t.OpenWait = 1500 * time.Millisecond
	}
	if t.LaunchWait <= 0 {
		t.LaunchWait = 4 * time.Second
	}
	if t.Track <= 0 {
		t.Track = 30 * time.Second
	}
	return t
}

// desktopTransition is one input that may open, close or retitle a window.
type desktopTransition struct {
	Action              string
	ExpectedApplication string
	Target              string
	At                  time.Time
	Baseline            map[string]desktopWindowInfo
	Appeared            []desktopWindowInfo
	ReportedIDs         []string
	// Acknowledged means a correlated new window was reported to the model.
	// ReportedIDs records other observed windows without completing this input.
	Acknowledged bool
	// Settled means a later observation saw the change complete, or a
	// later input to the same target found that target still foreground.
	Settled bool
	// Required means this input is expected to open, close or retitle a
	// window, so an observation waits for that change and reports pending
	// when it does not come. Other inputs only may change windows: their
	// observations wait for a stable list and never report pending.
	Required bool
}

// desktopTransitionState is per desktop control session.
type desktopTransitionState struct {
	mu         sync.Mutex
	pending    *desktopTransition
	lastInput  time.Time
	lastAction string
	taskOpened map[string]desktopWindowInfo
	// lister is the backend that recorded the pending transition.
	lister desktopWindowLister
}

var desktopTransitions = struct {
	sync.Mutex
	sessions map[string]*desktopTransitionState
	order    []string
}{sessions: map[string]*desktopTransitionState{}}

func desktopTransitionsFor(session string) *desktopTransitionState {
	desktopTransitions.Lock()
	defer desktopTransitions.Unlock()
	if state, ok := desktopTransitions.sessions[session]; ok {
		return state
	}
	if len(desktopTransitions.order) >= 64 {
		delete(desktopTransitions.sessions, desktopTransitions.order[0])
		desktopTransitions.order = desktopTransitions.order[1:]
	}
	state := &desktopTransitionState{taskOpened: map[string]desktopWindowInfo{}}
	desktopTransitions.sessions[session] = state
	desktopTransitions.order = append(desktopTransitions.order, session)
	return state
}

// DesktopOpenTransitions describes inputs whose resulting windows appeared
// but were never reported to the model. Completion checks treat them as open
// work. It reads the native window list once and sends no input.
func DesktopOpenTransitions(ctx context.Context, session string) []string {
	desktopTransitions.Lock()
	state := desktopTransitions.sessions[session]
	desktopTransitions.Unlock()
	if state == nil {
		return nil
	}
	state.mu.Lock()
	p, lister := state.pending, state.lister
	var snapshot desktopTransition
	if p != nil {
		snapshot = *p
	}
	state.mu.Unlock()
	if p == nil || snapshot.Acknowledged || lister == nil {
		return nil
	}
	current, _, ok := listDesktopWindows(ctx, lister)
	if !ok {
		return nil
	}
	appeared := desktopUnreportedWindows(snapshot, current)
	if len(appeared) == 0 {
		return nil
	}
	return []string{fmt.Sprintf("%s opened %s, which was never observed", desktopInputLabel(snapshot.Action, snapshot.Target), desktopWindowList(appeared))}
}

// desktopOpensWindow reports inputs whose effect commonly includes a window
// appearing, closing, navigating or retitling.
func desktopOpensWindow(action string, p ComputerParams) bool {
	switch action {
	case "launch_app", "double_click", "invoke", "hotkey", "click", "right_click":
		return true
	case "key":
		key := strings.ToLower(strings.TrimSpace(p.Key))
		return key == "enter" || key == "return" || key == "esc" || key == "escape"
	}
	return false
}

// desktopRequiresWindowChange reports inputs whose ordinary result is a
// window change: a launch, an opened item or menu, a shortcut or Enter. A
// plain click, invoke or Escape may change windows but usually does not, so
// an observation after it must not wait for a change or report one pending.
func desktopRequiresWindowChange(action string, p ComputerParams) bool {
	switch action {
	case "launch_app", "double_click", "right_click", "hotkey":
		return true
	case "key":
		key := strings.ToLower(strings.TrimSpace(p.Key))
		return key == "enter" || key == "return"
	}
	return false
}

// desktopStartsOpen reports inputs that start a new open, launch or
// navigation. They wait for an earlier unresolved transition before deciding.
func desktopStartsOpen(action string, p ComputerParams) bool {
	switch action {
	case "launch_app", "double_click", "hotkey", "focus":
		return true
	case "key":
		key := strings.ToLower(strings.TrimSpace(p.Key))
		return key == "enter" || key == "return"
	}
	return false
}

// desktopSettledRead reports observations that should see the settled result
// of a just-sent input.
func desktopSettledRead(action string) bool {
	switch action {
	case "screenshot", "capture_window", "capture_region", "windows", "observe", "ocr", "find", "inspect":
		return true
	}
	return false
}

// desktopWindowRead reports observations whose purpose includes noticing a
// window change, so they wait for an expected change instead of only for
// stability.
func desktopWindowRead(action string) bool {
	switch action {
	case "screenshot", "windows":
		return true
	}
	return false
}

var desktopTransientClasses = map[string]bool{
	"tooltips_class32": true, "Shell_TrayWnd": true, "Shell_SecondaryTrayWnd": true, "Progman": true, "WorkerW": true,
	"NotifyIconOverflowWindow": true, "TaskListThumbnailWnd": true,
}

// desktopRelevantWindow filters windows that a model could act on: no Atlas
// overlay windows, no shell chrome, and either a title, a dialog or a menu.
func desktopRelevantWindow(w desktopWindowInfo) bool {
	if w.ProcessID != 0 && int(w.ProcessID) == os.Getpid() {
		return false
	}
	if desktopTransientClasses[w.WindowClass] {
		return false
	}
	return strings.TrimSpace(w.Name) != "" || w.WindowClass == "#32770" || w.WindowClass == "#32768"
}

func listDesktopWindows(ctx context.Context, lister desktopWindowLister) (map[string]desktopWindowInfo, string, bool) {
	data, err := lister.ListWindows(ctx)
	if err != nil {
		return nil, "", false
	}
	var windows []desktopWindowInfo
	if json.Unmarshal(data, &windows) != nil {
		return nil, "", false
	}
	out := make(map[string]desktopWindowInfo, len(windows))
	parts := make([]string, 0, len(windows))
	for _, w := range windows {
		if w.ProcessID != 0 && int(w.ProcessID) == os.Getpid() {
			continue
		}
		out[w.ID] = w
		if desktopRelevantWindow(w) {
			parts = append(parts, fmt.Sprintf("%s|%s|%t|%t|%.0f,%.0f,%.0f,%.0f", w.ID, w.Name, w.Foreground, w.Minimized, w.X, w.Y, w.Width, w.Height))
		}
	}
	slices.Sort(parts)
	return out, strings.Join(parts, "\n"), true
}

func desktopAppeared(baseline, current map[string]desktopWindowInfo) []desktopWindowInfo {
	var out []desktopWindowInfo
	for id, w := range current {
		if _, existed := baseline[id]; !existed && desktopRelevantWindow(w) {
			out = append(out, w)
		}
	}
	slices.SortFunc(out, func(a, b desktopWindowInfo) int { return strings.Compare(a.ID, b.ID) })
	return out
}

func desktopWindowList(windows []desktopWindowInfo) string {
	parts := make([]string, 0, len(windows))
	for _, w := range windows {
		label := w.Name
		if label == "" {
			label = w.WindowClass
		}
		parts = append(parts, fmt.Sprintf("%q (window_id %s, %s)", label, w.ID, w.ProcessName))
	}
	return strings.Join(parts, ", ")
}

func desktopInputLabel(action, target string) string {
	if target == "" {
		return "the earlier " + action
	}
	return "the earlier " + action + " on window " + target
}

func desktopWindowIDs(windows []desktopWindowInfo) []string {
	ids := make([]string, 0, len(windows))
	for _, w := range windows {
		ids = append(ids, w.ID)
	}
	return ids
}

// desktopSettleReport is attached to observations that waited after input.
//
// After and InputAgeMS always describe the latest input. TransitionAfter
// names an earlier input whose window transition is still tracked. State is
// changed (a window linked to the input appeared, closed or was retitled),
// stable, unstable or pending (a required change did not come). A changed
// state is window-level evidence only, never proof that the step succeeded.
type desktopSettleReport struct {
	After           string   `json:"after"`
	TransitionAfter string   `json:"transition_after,omitempty"`
	State           string   `json:"state"`
	WaitReason      string   `json:"wait_reason,omitempty"`
	WaitedMS        int64    `json:"waited_ms"`
	InputAgeMS      int64    `json:"input_age_ms"`
	ObservedAt      string   `json:"observed_at,omitempty"`
	NewWindowIDs    []string `json:"new_window_ids,omitempty"`
	RelatedIDs      []string `json:"related_window_ids,omitempty"`
	ClosedIDs       []string `json:"closed_window_ids,omitempty"`
	UnrelatedIDs    []string `json:"unrelated_window_ids,omitempty"`
	TaskOpenedIDs   []string `json:"task_opened_window_ids,omitempty"`
	Note            string   `json:"note,omitempty"`
	correlated      bool
	transitionAt    time.Time
	appearedDetail  []desktopWindowInfo
	// settles records that delivering this report completes the transition.
	settles bool
}

type desktopTransitionTracker struct {
	state  *desktopTransitionState
	lister desktopWindowLister
	timing desktopSettleTiming
}

func (s *computerToolState) transitionTracker(ctx context.Context, controlID string) *desktopTransitionTracker {
	lister, ok := s.backend.(desktopWindowLister)
	if !ok {
		return nil
	}
	return &desktopTransitionTracker{
		state:  desktopTransitionsFor(controlID),
		lister: lister,
		timing: s.settleTiming.withDefaults(),
	}
}

func sleepContext(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// beforeInput refuses a mutation while a window that appeared after an
// earlier input is unknown to the model, and for a new open waits a bounded
// time for an earlier unresolved open to resolve first. It returns the
// baseline for this input when it may change windows.
func (t *desktopTransitionTracker) beforeInput(ctx context.Context, action string, p ComputerParams, delivery *desktopSettleDelivery) (*fantasy.ToolResponse, map[string]desktopWindowInfo) {
	t.state.mu.Lock()
	pending := t.state.pending
	if pending != nil && time.Since(pending.At) > t.timing.Track {
		t.state.pending, pending = nil, nil
	}
	var snapshot desktopTransition
	if pending != nil {
		snapshot = *pending
	}
	t.state.mu.Unlock()

	target := p.Automation.WindowID
	if pending != nil && !snapshot.Acknowledged && !snapshot.Settled {
		wait := time.Duration(0)
		if desktopStartsOpen(action, p) {
			wait = t.timing.OpenWait
			if snapshot.Action == "launch_app" || action == "launch_app" {
				wait = t.timing.LaunchWait
			}
		}
		deadline := snapshot.At.Add(wait)
		if action == "launch_app" && snapshot.Action == "launch_app" {
			// The earlier launch already reported pending: give it one more
			// bounded window before a second instance is started.
			deadline = time.Now().Add(wait)
		}
		for {
			current, _, ok := listDesktopWindows(ctx, t.lister)
			if !ok {
				break
			}
			appeared := desktopUnreportedWindows(snapshot, current)
			if len(appeared) > 0 {
				addressed := target != "" && slices.ContainsFunc(appeared, func(w desktopWindowInfo) bool { return w.ID == target })
				t.state.mu.Lock()
				if t.state.pending != nil && t.state.pending.At.Equal(snapshot.At) {
					t.state.pending.Appeared = appeared
					if addressed {
						t.state.pending.ReportedIDs = append(t.state.pending.ReportedIDs, target)
						if slices.ContainsFunc(desktopTransitionAppeared(snapshot, current), func(w desktopWindowInfo) bool { return w.ID == target }) {
							t.state.pending.Acknowledged = true
						}
					}
					for _, w := range appeared {
						t.state.taskOpened[w.ID] = w
					}
				}
				t.state.mu.Unlock()
				if addressed {
					break
				}
				resp := delayedTransitionResponse(snapshot, appeared, action)
				delivery.tracker = t
				delivery.refusal = true
				delivery.report = &desktopSettleReport{transitionAt: snapshot.At, NewWindowIDs: desktopWindowIDs(appeared), correlated: len(desktopTransitionAppeared(snapshot, current)) > 0}
				return &resp, nil
			}
			if time.Now().After(deadline) || sleepContext(ctx, t.timing.Poll) != nil {
				break
			}
		}
		if err := ctx.Err(); err != nil {
			resp := fantasy.NewTextErrorResponse("canceled: waiting for an earlier input to settle was canceled; no input was sent")
			return &resp, nil
		}
	}
	if !desktopOpensWindow(action, p) {
		return nil, nil
	}
	baseline, _, ok := listDesktopWindows(ctx, t.lister)
	if !ok {
		return nil, nil
	}
	return nil, baseline
}

func delayedTransitionResponse(earlier desktopTransition, appeared []desktopWindowInfo, action string) fantasy.ToolResponse {
	ids := desktopWindowIDs(appeared)
	message := fmt.Sprintf("delayed_transition: %s completed late: %s appeared after it and has not been observed. This %s was not sent. Inspect the new window first (observe or capture_window with its window_id); if the earlier input already produced the intended result, do not repeat the open/launch. The new window is now known, so a deliberate retry of this call is allowed.",
		desktopInputLabel(earlier.Action, earlier.Target), desktopWindowList(appeared), action)
	resp := fantasy.NewTextErrorResponse(message)
	data, _ := json.Marshal(map[string]any{"tool_contract": computer.ContractError{
		Code: "delayed_transition", Message: message, InputSent: false, Effect: "none", FreshObservation: true,
		NextStep: "Observe window " + strings.Join(ids, ", ") + " before any further input.",
		Example:  fmt.Sprintf(`{"action":"observe","observation":"auto","automation":{"window_id":%q}}`, ids[0]),
	}})
	resp.Metadata = string(data)
	return resp
}

// afterInput records a dispatched input. A successful input that may change
// windows becomes the pending transition; a later input that reached the
// same still-foreground target settles an earlier one.
func (t *desktopTransitionTracker) afterInput(action string, p ComputerParams, baseline map[string]desktopWindowInfo, resp fantasy.ToolResponse, sent bool) {
	if !sent {
		return
	}
	t.state.mu.Lock()
	defer t.state.mu.Unlock()
	now := time.Now()
	t.state.lastInput = now
	t.state.lastAction = action
	target := p.Automation.WindowID
	if pending := t.state.pending; pending != nil && target != "" {
		if slices.ContainsFunc(pending.Appeared, func(w desktopWindowInfo) bool { return w.ID == target }) {
			pending.ReportedIDs = append(pending.ReportedIDs, target)
		}
		if pending.Target == target && !resp.IsError && !desktopOpensWindow(action, p) {
			// The keyboard/pointer foreground check passed on the same
			// target: no window took the foreground in between.
			pending.Settled = true
		}
	}
	if baseline == nil || resp.IsError {
		return
	}
	t.state.pending = &desktopTransition{Action: action, Target: target, At: now, Baseline: baseline, ExpectedApplication: p.Automation.Name, Required: desktopRequiresWindowChange(action, p)}
	t.state.lister = t.lister
}

// settle waits before an observation that follows a recent input. Waits are
// conditional: they end as soon as the window list changes and stays stable,
// and every bound is reported. Recipes use the same readiness checks. Nothing
// it learns is committed to the tracker here: acknowledge does that once the
// observation is actually delivered, so a canceled or abandoned observation
// can neither mark windows task-opened nor settle a transition.
func (t *desktopTransitionTracker) settle(ctx context.Context, action string) *desktopSettleReport {
	t.state.mu.Lock()
	pending := t.state.pending
	if pending != nil && time.Since(pending.At) > t.timing.Track {
		t.state.pending, pending = nil, nil
	}
	lastInput, lastAction := t.state.lastInput, t.state.lastAction
	var snapshot desktopTransition
	if pending != nil {
		snapshot = *pending
	}
	taskOpened := make(map[string]desktopWindowInfo, len(t.state.taskOpened))
	for id, w := range t.state.taskOpened {
		taskOpened[id] = w
	}
	t.state.mu.Unlock()
	if lastInput.IsZero() {
		return nil
	}
	age := time.Since(lastInput)
	recent := age < t.timing.ReadSettle
	unresolved := pending != nil && !snapshot.Acknowledged
	if !recent && !unresolved {
		return nil
	}
	started := time.Now()
	report := &desktopSettleReport{State: "stable", After: lastAction, transitionAt: snapshot.At, WaitReason: "input_settle"}
	// A later input that did not start its own transition supersedes the
	// tracked one: the model already acted on the state after it, so this
	// observation neither waits for nor reports the earlier transition as
	// pending. A window it opens late is still caught before the next input.
	superseded := pending != nil && lastInput.After(snapshot.At)
	if pending != nil && (superseded || report.After == "") {
		report.TransitionAfter = snapshot.Action
	}
	if report.After == "" {
		report.After = snapshot.Action
	}
	if age < t.timing.MinSettle {
		if sleepContext(ctx, t.timing.MinSettle-age) != nil {
			return nil
		}
	}
	expectChange := pending != nil && snapshot.Required && !superseded && !snapshot.Settled && !snapshot.Acknowledged && desktopWindowRead(action)
	wait := t.timing.ReadSettle
	if expectChange {
		wait = t.timing.OpenWait
		report.WaitReason = "expected_window_change"
		if snapshot.Action == "launch_app" {
			wait = t.timing.LaunchWait
			report.WaitReason = "launch"
		}
	}
	deadline := lastInput.Add(wait)
	if pending != nil && expectChange {
		deadline = snapshot.At.Add(wait)
	}
	previous := ""
	var current map[string]desktopWindowInfo
	changed := false
	for polls := 1; ; polls++ {
		list, signature, ok := listDesktopWindows(ctx, t.lister)
		if !ok {
			return nil
		}
		current = list
		if pending != nil && desktopTransitionChanged(snapshot, list) {
			changed = true
		}
		stable := previous != "" && signature == previous
		previous = signature
		if stable && (!expectChange || changed) {
			break
		}
		if time.Now().After(deadline) && polls >= 2 {
			if expectChange && !changed {
				report.State = "pending"
			} else if !stable {
				report.State = "unstable"
				report.Note = "The window list was still changing when this observation was taken; observe once more before acting on it."
			}
			break
		}
		if sleepContext(ctx, t.timing.Poll) != nil {
			return nil
		}
	}
	if changed && report.State == "stable" {
		report.State = "changed"
	}
	report.WaitedMS = time.Since(started).Milliseconds()
	report.InputAgeMS = time.Since(lastInput).Milliseconds()
	report.ObservedAt = time.Now().UTC().Format(time.RFC3339Nano)
	if pending != nil {
		appeared := desktopUnreportedWindows(snapshot, current)
		related := desktopTransitionAppeared(snapshot, current)
		report.correlated = len(related) > 0
		report.appearedDetail = appeared
		report.NewWindowIDs = desktopWindowIDs(appeared)
		report.RelatedIDs = desktopWindowIDs(related)
		report.ClosedIDs = desktopWindowIDs(desktopTransitionClosed(snapshot, current))
		for _, w := range appeared {
			if !slices.Contains(report.RelatedIDs, w.ID) {
				report.UnrelatedIDs = append(report.UnrelatedIDs, w.ID)
			}
		}
		report.settles = len(appeared) == 0 && report.State != "pending" && changed
		label := snapshot.Action
		switch {
		case report.State == "pending" && len(report.UnrelatedIDs) > 0:
			report.Note = fmt.Sprintf("No window change linked to %s was observed within %d ms. The new windows %s are not linked to it (another process, not foreground, not owned by the target); do not treat them as its result. Observe again before starting another open or launch for the same purpose.", label, wait.Milliseconds(), strings.Join(report.UnrelatedIDs, ", "))
		case report.State == "pending":
			report.Note = fmt.Sprintf("No window change linked to %s was observed within %d ms; the result may still be on its way. Observe again before starting another open or launch for the same purpose.", label, wait.Milliseconds())
		case report.State == "changed":
			report.Note = "Window-level change only; verify the intended result before treating the step as done."
		}
	}
	for id, w := range taskOpened {
		if _, open := current[id]; open {
			report.TaskOpenedIDs = append(report.TaskOpenedIDs, w.ID)
		}
	}
	for _, w := range report.appearedDetail {
		if _, known := taskOpened[w.ID]; !known {
			report.TaskOpenedIDs = append(report.TaskOpenedIDs, w.ID)
		}
	}
	// Ownership of windows that no longer exist can be dropped at once; it
	// grants nothing, and the window it described is gone.
	t.state.mu.Lock()
	for id := range t.state.taskOpened {
		if _, open := current[id]; !open {
			delete(t.state.taskOpened, id)
		}
	}
	t.state.mu.Unlock()
	slices.Sort(report.TaskOpenedIDs)
	if report.TransitionAfter == "" && pending == nil && report.State == "stable" && report.WaitedMS == 0 {
		return nil
	}
	return report
}

// desktopTransitionChanged ignores unrelated focus, geometry and title changes.
// An open completes when a related new window appears, a related window
// closes (a dismissed menu or dialog), or the source itself navigates, closes
// or changes its title.
func desktopTransitionChanged(p desktopTransition, current map[string]desktopWindowInfo) bool {
	if len(desktopTransitionAppeared(p, current)) > 0 || len(desktopTransitionClosed(p, current)) > 0 {
		return true
	}
	source, hasSource := p.Baseline[p.Target]
	if hasSource {
		now, exists := current[p.Target]
		return !exists || now.Name != source.Name
	}
	// Coordinate inputs have no explicit target; correlate to the previous
	// foreground window rather than an arbitrary changed top-level window.
	for id, w := range p.Baseline {
		if w.Foreground && desktopRelevantWindow(w) {
			now, exists := current[id]
			if !exists || now.Name != w.Name {
				return true
			}
		}
	}
	return false
}

// desktopTransitionProcesses lists the processes an input can be linked to:
// the explicit target's process, or for a coordinate input without a target
// the foreground process before and after it. A desktop right-click makes
// the shell foreground, and its context menu belongs to that same process
// without ever becoming foreground itself.
func desktopTransitionProcesses(p desktopTransition, current map[string]desktopWindowInfo) []int64 {
	var out []int64
	add := func(pid int64) {
		if pid != 0 && pid != int64(os.Getpid()) && !slices.Contains(out, pid) {
			out = append(out, pid)
		}
	}
	if source, ok := p.Baseline[p.Target]; ok {
		add(int64(source.ProcessID))
	}
	if p.Target == "" {
		for _, windows := range []map[string]desktopWindowInfo{p.Baseline, current} {
			for _, w := range windows {
				if w.Foreground {
					add(int64(w.ProcessID))
				}
			}
		}
	}
	return out
}

// desktopTransitionRelated decides whether a window that appeared or closed
// belongs to this input. Unrelated background windows can neither complete
// nor acknowledge an input whose actual result is still pending.
func desktopTransitionRelated(p desktopTransition, w desktopWindowInfo, processes []int64) bool {
	if p.Action == "launch_app" && p.ExpectedApplication != "" {
		return desktopApplicationWindowMatches(w, p.ExpectedApplication)
	}
	return w.Foreground || p.Target != "" && (w.Owner == p.Target || w.ID == p.Target) || slices.Contains(processes, int64(w.ProcessID))
}

// desktopTransitionAppeared uses the same correlation for detection, input
// guards, reporting and completion.
func desktopTransitionAppeared(p desktopTransition, current map[string]desktopWindowInfo) []desktopWindowInfo {
	processes := desktopTransitionProcesses(p, current)
	return slices.DeleteFunc(desktopAppeared(p.Baseline, current), func(w desktopWindowInfo) bool {
		return !desktopTransitionRelated(p, w, processes)
	})
}

// desktopTransitionClosed lists related windows present before the input
// that are gone now: the closed target, a dismissed dialog or a menu that
// closed after its item was chosen.
func desktopTransitionClosed(p desktopTransition, current map[string]desktopWindowInfo) []desktopWindowInfo {
	if p.Action == "launch_app" {
		return nil
	}
	processes := desktopTransitionProcesses(p, current)
	var out []desktopWindowInfo
	for id, w := range p.Baseline {
		if _, open := current[id]; open || !desktopRelevantWindow(w) {
			continue
		}
		if desktopTransitionRelated(p, w, processes) {
			out = append(out, w)
		}
	}
	slices.SortFunc(out, func(a, b desktopWindowInfo) int { return strings.Compare(a.ID, b.ID) })
	return out
}

// desktopUnreportedWindows keeps possible opens visible even when focus moved
// before polling. Reporting an unrelated window does not settle the pending
// input, so a later correlated window still requires observation.
func desktopUnreportedWindows(p desktopTransition, current map[string]desktopWindowInfo) []desktopWindowInfo {
	return slices.DeleteFunc(desktopAppeared(p.Baseline, current), func(w desktopWindowInfo) bool { return slices.Contains(p.ReportedIDs, w.ID) })
}

type desktopSettleDelivery struct {
	tracker *desktopTransitionTracker
	report  *desktopSettleReport
	refusal bool
}

// acknowledge runs only on the successful return path after the outer timeout
// selection. An abandoned worker may annotate its own result but never records
// that its observation was delivered to the caller.
func (d *desktopSettleDelivery) acknowledge() {
	if d == nil || d.tracker == nil || d.report == nil {
		return
	}
	t, report := d.tracker, d.report
	t.state.mu.Lock()
	defer t.state.mu.Unlock()
	p := t.state.pending
	if p == nil || !p.At.Equal(report.transitionAt) {
		return
	}
	for _, id := range report.NewWindowIDs {
		if !slices.Contains(p.ReportedIDs, id) {
			p.ReportedIDs = append(p.ReportedIDs, id)
		}
	}
	if len(report.appearedDetail) > 0 {
		p.Appeared = report.appearedDetail
		for _, w := range report.appearedDetail {
			t.state.taskOpened[w.ID] = w
		}
	}
	if report.correlated {
		p.Acknowledged = true
	}
	if report.settles {
		p.Settled = true
	}
}

// annotateSettle exposes the settle report without disturbing the result
// shape: JSON objects gain a leading input_settle field, text gains a line.
func annotateSettle(resp fantasy.ToolResponse, report *desktopSettleReport) fantasy.ToolResponse {
	if report == nil || resp.IsError || resp.StopTurn {
		return resp
	}
	data, err := json.Marshal(report)
	if err != nil {
		return resp
	}
	// Images carry a text caption rather than a JSON envelope. Preserve the
	// same native evidence in metadata so the target guard can consume it.
	meta := map[string]json.RawMessage{}
	if resp.Metadata != "" {
		if json.Unmarshal([]byte(resp.Metadata), &meta) != nil || meta == nil {
			meta = map[string]json.RawMessage{"native_metadata": json.RawMessage(strconvJSON(resp.Metadata))}
		}
	}
	meta["input_settle"] = data
	resp = fantasy.WithResponseMetadata(resp, meta)
	content := strings.TrimLeft(resp.Content, " \t\r\n")
	if strings.HasPrefix(content, "{") && json.Valid([]byte(content)) {
		rest := strings.TrimLeft(content[1:], " \t\r\n")
		separator := ","
		if strings.HasPrefix(rest, "}") {
			separator = ""
		}
		resp.Content = `{"input_settle":` + string(data) + separator + rest
		return resp
	}
	line := fmt.Sprintf("Input settle: %s after %s (waited %d ms, input age %d ms)", report.State, report.After, report.WaitedMS, report.InputAgeMS)
	if report.TransitionAfter != "" {
		line += "; tracking the earlier " + report.TransitionAfter
	}
	if len(report.appearedDetail) > 0 {
		line += "; new windows: " + desktopWindowList(report.appearedDetail)
		if len(report.UnrelatedIDs) > 0 {
			line += " (not linked to this input: " + strings.Join(report.UnrelatedIDs, ", ") + ")"
		}
	}
	if len(report.ClosedIDs) > 0 {
		line += "; closed windows: " + strings.Join(report.ClosedIDs, ", ")
	}
	if report.Note != "" {
		line += ". " + report.Note
	}
	if resp.Content == "" {
		resp.Content = line
	} else {
		resp.Content += "\n" + line
	}
	return resp
}
