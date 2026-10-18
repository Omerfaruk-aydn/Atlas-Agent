package engineering

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/outcome"
)

// WorkState is the task's working memory. It keeps what the user asked for
// separate from what the agent proposed or assumed, why the working method
// changed, which hypotheses were tested and how, which solution candidates
// exist and how they compared, and what is still open. Every claim of
// progress points at observed operations in the same execution state;
// nothing here is accepted on the model's word alone.
type WorkState struct {
	Turns       []UserTurn       `json:"turns,omitempty"`
	Items       []WorkItem       `json:"items,omitempty"`
	Methods     []MethodDecision `json:"methods,omitempty"`
	Hypotheses  []Hypothesis     `json:"hypotheses,omitempty"`
	Experiments []Experiment     `json:"experiments,omitempty"`
	Candidates  []Candidate      `json:"candidates,omitempty"`
	Suite       *CandidateSuite  `json:"suite,omitempty"`
	Selection   string           `json:"selection,omitempty"`
	Attempts    []FailedAttempt  `json:"attempts,omitempty"`
	Questions   []OpenQuestion   `json:"questions,omitempty"`
	NextChecks  []string         `json:"next_checks,omitempty"`
	Budget      WorkBudget       `json:"budget"`
	Strategy    StrategyState    `json:"strategy"`
	Phase       string           `json:"phase,omitempty"`
	Waiting     string           `json:"waiting,omitempty"`
	Seq         int              `json:"seq"`
}

// UserTurn is a bounded copy of a user message as received. User-origin
// contract items must cite one; the agent cannot invent a user request.
type UserTurn struct {
	Seq         int    `json:"seq"`
	Kind        string `json:"kind"`
	Text        string `json:"text"`
	At          int64  `json:"at"`
	Fingerprint string `json:"fingerprint,omitempty"`
	Truncated   bool   `json:"truncated,omitempty"`
}

// Contract item origins.
const (
	OriginUser       = "user"
	OriginProposal   = "agent_proposal"
	OriginAssumption = "assumption"
)

// Contract item statuses. Implemented, verified and delivered are distinct:
// a change is not a verification and a verification is not a delivery.
const (
	ItemOpen        = "open"
	ItemImplemented = "implemented"
	ItemVerified    = "verified"
	ItemDelivered   = "delivered"
	ItemSuperseded  = "superseded"
	ItemDeclined    = "declined"
)

// WorkItem is one element of the task contract.
type WorkItem struct {
	ID           string        `json:"id"`
	Kind         string        `json:"kind"`
	Text         string        `json:"text"`
	Origin       string        `json:"origin"`
	SourceTurns  []int         `json:"source_turns,omitempty"`
	Status       string        `json:"status"`
	Evidence     []EvidenceRef `json:"evidence,omitempty"`
	SupersededBy string        `json:"superseded_by,omitempty"`
	UpdatedAt    int64         `json:"updated_at"`
}

// EvidenceRef binds a claim to an observed operation and the source
// generation it was observed at.
type EvidenceRef struct {
	OperationID string `json:"operation_id"`
	Tool        string `json:"tool"`
	Generation  uint64 `json:"generation"`
	Passed      bool   `json:"passed"`
	ObservedAt  int64  `json:"observed_at"`
}

// MethodDecision records a choice of working method and the evidence that
// prompted it.
type MethodDecision struct {
	ID       string   `json:"id"`
	Method   string   `json:"method"`
	Reason   string   `json:"reason"`
	Evidence []string `json:"evidence,omitempty"`
	Trigger  string   `json:"trigger"`
	At       int64    `json:"at"`
}

type Hypothesis struct {
	ID        string `json:"id"`
	Statement string `json:"statement"`
	Status    string `json:"status"`
	Limited   bool   `json:"limited,omitempty"`
	At        int64  `json:"at"`
}

// Experiment is a bounded test of a hypothesis. Its result must come from
// operations observed after it was registered.
type Experiment struct {
	ID             string        `json:"id"`
	HypothesisID   string        `json:"hypothesis_id"`
	Question       string        `json:"question"`
	Prediction     string        `json:"prediction"`
	Representative bool          `json:"representative"`
	Limits         string        `json:"limits,omitempty"`
	Outcome        string        `json:"outcome"`
	Observation    string        `json:"observation,omitempty"`
	Evidence       []EvidenceRef `json:"evidence,omitempty"`
	At             int64         `json:"at"`
}

// Candidate is one distinct solution approach.
type Candidate struct {
	BranchID    string                     `json:"branch_id,omitempty"`
	ID          string                     `json:"id"`
	Approach    string                     `json:"approach"`
	Axis        string                     `json:"axis"`
	WorkspaceID string                     `json:"workspace_id,omitempty"`
	TaskID      string                     `json:"task_id,omitempty"`
	Status      string                     `json:"status"`
	Results     map[string]CandidateResult `json:"results,omitempty"`
	At          int64                      `json:"at"`
}

type CandidateResult struct {
	OperationID string `json:"operation_id"`
	Passed      bool   `json:"passed"`
	Generation  uint64 `json:"generation"`
}

// CandidateSuite is the set of discriminating checks every candidate is
// judged by. It is fixed once the first result is recorded; a revision
// after that clears every result and is recorded with its reason.
type CandidateSuite struct {
	Checks    []string `json:"checks"`
	Rationale string   `json:"rationale"`
	LockedAt  int64    `json:"locked_at,omitempty"`
	Revisions []string `json:"revisions,omitempty"`
}

type FailedAttempt struct {
	What       string `json:"what"`
	ErrorClass string `json:"error_class"`
	Evidence   string `json:"evidence,omitempty"`
	Lesson     string `json:"lesson,omitempty"`
	Source     string `json:"source"`
	At         int64  `json:"at"`
}

type OpenQuestion struct {
	ID     string   `json:"id"`
	Text   string   `json:"text"`
	Blocks []string `json:"blocks,omitempty"`
	Status string   `json:"status"`
	Answer string   `json:"answer,omitempty"`
	At     int64    `json:"at"`
}

// Work budget tiers. A tier changes how much structure, research and
// alternative exploration is offered; it never changes permissions.
const (
	TierDirect   = "direct"
	TierStandard = "standard"
	TierDeep     = "deep"
)

type WorkBudget struct {
	Tier     string   `json:"tier"`
	Reasons  []string `json:"reasons,omitempty"`
	Override string   `json:"override,omitempty"`
}

type StrategyState struct {
	Signals   int    `json:"signals,omitempty"`
	LastClass string `json:"last_class,omitempty"`
	LastAt    int64  `json:"last_at,omitempty"`
}

// Bounds keep the working memory small enough to be shown every turn.
const (
	maxWorkTurns       = 24
	maxWorkTurnText    = 24 * 1024
	maxWorkItems       = 48
	maxWorkMethods     = 24
	maxWorkHypotheses  = 12
	maxWorkExperiments = 24
	maxWorkCandidates  = 6
	maxWorkAttempts    = 24
	maxWorkQuestions   = 12
	maxWorkText        = 1024
)

var (
	workMethods       = []string{"research", "direct", "prototype", "experiment", "review", "candidates", "delegate", "ask_user"}
	workItemKinds     = []string{"behavior", "deliverable", "constraint", "uncertainty"}
	candidateAxes     = []string{"algorithm", "architecture", "hypothesis", "configuration", "dependency"}
	checkTools        = []string{"verify", "test_run", "lint_run", "ui_verify", "scenario", "api_probe"}
	continuationRegex = regexp.MustCompile(`(?i)^\s*(devam( et| edelim| ediyoruz)?|devam|continue|go on|keep going|proceed|ok(ay)?|tamam|evet|yes|go|sürdür|bitir)\W*\s*$`)
)

func (w *WorkState) next(prefix string) string {
	w.Seq++
	return prefix + strconv.Itoa(w.Seq)
}

func boundedWork(s string) error {
	if strings.TrimSpace(s) == "" || len(s) > maxWorkText {
		return fmt.Errorf("text must be 1-%d bytes", maxWorkText)
	}
	return nil
}

// RecordUserTurn stores a user message as received and classifies it. A
// short continuation ("devam et") never becomes a new requirement.
func (w *WorkState) RecordUserTurn(text string, now time.Time) UserTurn {
	fingerprint := Hash(text)
	truncated := len(text) > maxWorkTurnText
	kind := "follow_up"
	switch {
	case continuationRegex.MatchString(text):
		kind = "continuation"
	case len(w.Turns) == 0:
		kind = "request"
	}
	if len(text) > maxWorkTurnText {
		text = strings.ToValidUTF8(text[:maxWorkTurnText], "") + " [truncated]"
	}
	seq := 1
	if n := len(w.Turns); n > 0 {
		seq = w.Turns[n-1].Seq + 1
	}
	turn := UserTurn{Seq: seq, Kind: kind, Text: text, At: now.UnixMilli(), Fingerprint: fingerprint, Truncated: truncated}
	w.Turns = append(w.Turns, turn)
	if len(w.Turns) > maxWorkTurns {
		// The first request is kept: it anchors the contract.
		w.Turns = append(w.Turns[:1:1], w.Turns[len(w.Turns)-maxWorkTurns+1:]...)
	}
	if w.Phase == "waiting" {
		w.Phase, w.Waiting = "", ""
	}
	return turn
}

func (w *WorkState) turn(seq int) (UserTurn, bool) {
	for _, t := range w.Turns {
		if t.Seq == seq {
			return t, true
		}
	}
	return UserTurn{}, false
}

func (w *WorkState) item(id string) *WorkItem {
	for i := range w.Items {
		if w.Items[i].ID == id {
			return &w.Items[i]
		}
	}
	return nil
}

// AddItem adds a contract item. User-origin items must cite a recorded user
// turn that is not a bare continuation.
func (w *WorkState) AddItem(kind, text, origin string, turns []int, now time.Time) (WorkItem, error) {
	if !slices.Contains(workItemKinds, kind) {
		return WorkItem{}, fmt.Errorf("kind must be one of %s", strings.Join(workItemKinds, ", "))
	}
	if err := boundedWork(text); err != nil {
		return WorkItem{}, err
	}
	switch origin {
	case OriginUser:
		if len(turns) == 0 {
			return WorkItem{}, errors.New("a user requirement must cite source_turns from the recorded user turns; record agent ideas as agent_proposal and inferences as assumption")
		}
		for _, seq := range turns {
			t, ok := w.turn(seq)
			if !ok {
				return WorkItem{}, fmt.Errorf("user turn %d is not recorded", seq)
			}
			if t.Kind == "continuation" {
				return WorkItem{}, fmt.Errorf("user turn %d only says to continue; it adds no requirement", seq)
			}
		}
	case OriginProposal, OriginAssumption:
		turns = nil
	default:
		return WorkItem{}, errors.New("origin must be user, agent_proposal or assumption")
	}
	if len(w.Items) >= maxWorkItems {
		return WorkItem{}, errors.New("contract item limit reached; supersede or decline obsolete items first")
	}
	item := WorkItem{ID: w.next("R"), Kind: kind, Text: text, Origin: origin, SourceTurns: turns, Status: ItemOpen, UpdatedAt: now.UnixMilli()}
	w.Items = append(w.Items, item)
	return item, nil
}

// Supersede replaces an item after a user correction. A user requirement
// can only be superseded by a newer user turn.
func (w *WorkState) Supersede(id, replacement string, turn int, now time.Time) error {
	old, repl := w.item(id), w.item(replacement)
	if old == nil || repl == nil || old == repl {
		return errors.New("superseding requires two existing, different items")
	}
	if old.Origin == OriginUser {
		t, ok := w.turn(turn)
		if !ok || t.Kind == "continuation" || slices.Contains(old.SourceTurns, turn) || turn < slices.Max(old.SourceTurns) {
			return errors.New("a user requirement can only be superseded by a later recorded user correction")
		}
	}
	old.Status, old.SupersededBy, old.UpdatedAt = ItemSuperseded, replacement, now.UnixMilli()
	return nil
}

// Decline marks an agent proposal or assumption as not pursued. User
// requirements are never declined by the agent.
func (w *WorkState) Decline(id string, now time.Time) error {
	item := w.item(id)
	if item == nil {
		return errors.New("unknown item")
	}
	if item.Origin == OriginUser {
		return errors.New("the agent cannot decline a user requirement; ask the user or supersede it with their later correction")
	}
	item.Status, item.UpdatedAt = ItemDeclined, now.UnixMilli()
	return nil
}

// findWorkOperation accepts the runtime operation ID or the tool call ID the
// model saw when it made the call.
func findWorkOperation(st State, ref string) (Operation, bool) {
	if ref == "" {
		return Operation{}, false
	}
	for i := len(st.Operations) - 1; i >= 0; i-- {
		if op := st.Operations[i]; op.ID == ref || op.CallID == ref {
			return op, true
		}
	}
	return Operation{}, false
}

// LatestCheck returns the most recent passing check operation observed at
// the current source generation.
func LatestCheck(st State) (Operation, bool) {
	for i := len(st.Operations) - 1; i >= 0; i-- {
		op := st.Operations[i]
		if verificationOperation(st, op) {
			return op, true
		}
	}
	return Operation{}, false
}

// verificationOperation requires an explicit check or native assertion.
// Merely reading a page, listing windows or completing a shell command
// cannot verify an arbitrary contract requirement.
func verificationOperation(st State, op Operation) bool {
	if op.Status != "completed" || !op.OutcomeObserved || op.Generation != st.Generation {
		return false
	}
	if slices.Contains(checkTools, op.Tool) {
		return true
	}
	for _, check := range st.Checks {
		if check.RunID == op.CallID && check.Passed && check.Evidence != "" {
			return true
		}
	}
	if op.Tool == "browser" || op.Tool == "computer" || op.Tool == "tool_pipeline" {
		for _, e := range st.Outcomes.Evidence {
			if e.Source == op.ID && e.Kind == outcome.KindOutput && e.StaleBy == "" {
				return true
			}
		}
	}
	return false
}

// RecentOperations lists finished operations newest first, for citing.
func RecentOperations(st State, n int) []map[string]any {
	var out []map[string]any
	for i := len(st.Operations) - 1; i >= 0 && len(out) < n; i-- {
		op := st.Operations[i]
		if op.Status == "running" {
			continue
		}
		out = append(out, map[string]any{"id": op.ID, "call_id": op.CallID, "tool": op.Tool, "status": op.Status, "observed": op.OutcomeObserved, "generation": op.Generation})
	}
	return out
}

// observed resolves operation IDs against the execution state. Only
// finished operations with an observed machine outcome count as evidence.
func observed(st State, ids []string) ([]EvidenceRef, error) {
	if len(ids) == 0 || len(ids) > 8 {
		return nil, errors.New("1-8 operation_ids from workflow status are required")
	}
	var refs []EvidenceRef
	for _, id := range ids {
		op, ok := findWorkOperation(st, id)
		if !ok {
			return nil, fmt.Errorf("operation %s is not in the retained execution state; use an id or call_id from recent_operations", id)
		}
		if op.Status != "completed" && op.Status != "failed" || !op.OutcomeObserved {
			return nil, fmt.Errorf("operation %s has no observed outcome (status %s)", id, op.Status)
		}
		refs = append(refs, EvidenceRef{OperationID: op.ID, Tool: op.Tool, Generation: op.Generation, Passed: op.Status == "completed", ObservedAt: op.FinishedAt})
	}
	return refs, nil
}

// Advance moves a contract item forward on observed evidence. Implemented
// needs a completed operation; verified needs a passing check observed at
// the current source generation.
func (w *WorkState) Advance(st State, id, status string, ops []string, now time.Time) error {
	item := w.item(id)
	if item == nil {
		return errors.New("unknown item")
	}
	if item.Status == ItemSuperseded || item.Status == ItemDeclined {
		return fmt.Errorf("item %s is %s", id, item.Status)
	}
	refs, err := observed(st, ops)
	if err != nil {
		return err
	}
	switch status {
	case ItemImplemented:
		if !slices.ContainsFunc(refs, func(r EvidenceRef) bool { return r.Passed }) {
			return errors.New("implemented requires at least one completed operation")
		}
	case ItemVerified:
		ok := slices.ContainsFunc(refs, func(r EvidenceRef) bool {
			op, _ := findWorkOperation(st, r.OperationID)
			return verificationOperation(st, op)
		})
		if !ok {
			return fmt.Errorf("verified requires a passing check (%s) observed at the current source generation %d; rerun the check after the last change", strings.Join(checkTools, ", "), st.Generation)
		}
		for _, r := range refs {
			if !r.Passed && r.Generation == st.Generation {
				return fmt.Errorf("operation %s failed at the current generation; a failing check cannot verify", r.OperationID)
			}
		}
	default:
		return errors.New("status must be implemented or verified; delivery is recorded by the runtime when the answer is delivered")
	}
	item.Status, item.Evidence, item.UpdatedAt = status, append(item.Evidence[:0:0], refs...), now.UnixMilli()
	return nil
}

// MarkDelivered records delivery of verified items whose verification is
// still current. Unverified or stale items stay as they are.
func (w *WorkState) MarkDelivered(generation uint64, now time.Time) {
	for i := range w.Items {
		item := &w.Items[i]
		if item.Status == ItemVerified && verificationCurrent(*item, generation) {
			item.Status, item.UpdatedAt = ItemDelivered, now.UnixMilli()
		}
	}
}

func verificationCurrent(item WorkItem, generation uint64) bool {
	return slices.ContainsFunc(item.Evidence, func(r EvidenceRef) bool { return r.Passed && r.Generation == generation })
}

// EffectiveStatus reports stale verification: a later change invalidates
// an earlier check without rewriting history.
func EffectiveStatus(item WorkItem, generation uint64) string {
	if (item.Status == ItemVerified || item.Status == ItemDelivered) && !verificationCurrent(item, generation) {
		return item.Status + "_stale"
	}
	return item.Status
}

// AddMethod records a method decision. Every change after the first must
// cite evidence recorded since the previous decision.
func (w *WorkState) AddMethod(st State, method, reason string, evidence []string, trigger string, now time.Time) (MethodDecision, error) {
	if !slices.Contains(workMethods, method) {
		return MethodDecision{}, fmt.Errorf("method must be one of %s", strings.Join(workMethods, ", "))
	}
	if err := boundedWork(reason); err != nil {
		return MethodDecision{}, err
	}
	if len(evidence) > 8 {
		return MethodDecision{}, errors.New("at most 8 evidence references")
	}
	var since int64
	if n := len(w.Methods); n > 0 {
		since = w.Methods[n-1].At
		if len(evidence) == 0 {
			return MethodDecision{}, errors.New("a method change must cite the new evidence that prompted it (operation, hypothesis, experiment, question or turn:N IDs)")
		}
	}
	fresh := len(w.Methods) == 0
	for _, ref := range evidence {
		at, ok := w.referenceTime(st, ref)
		if !ok {
			return MethodDecision{}, fmt.Errorf("unknown evidence reference %q", ref)
		}
		if at >= since {
			fresh = true
		}
	}
	if !fresh {
		return MethodDecision{}, errors.New("the cited evidence predates the previous method decision; repeating a method change needs new evidence")
	}
	d := MethodDecision{ID: w.next("M"), Method: method, Reason: reason, Evidence: evidence, Trigger: trigger, At: now.UnixMilli()}
	w.Methods = append(w.Methods, d)
	if len(w.Methods) > maxWorkMethods {
		w.Methods = w.Methods[len(w.Methods)-maxWorkMethods:]
	}
	return d, nil
}

// referenceTime resolves an evidence reference to the time it was recorded.
func (w *WorkState) referenceTime(st State, ref string) (int64, bool) {
	if seq, ok := strings.CutPrefix(ref, "turn:"); ok {
		n, err := strconv.Atoi(seq)
		if t, found := w.turn(n); err == nil && found {
			return t.At, true
		}
		return 0, false
	}
	if op, ok := findWorkOperation(st, ref); ok {
		return max(op.FinishedAt, op.StartedAt), true
	}
	for _, h := range w.Hypotheses {
		if h.ID == ref {
			return h.At, true
		}
	}
	for _, e := range w.Experiments {
		if e.ID == ref {
			return e.At, true
		}
	}
	for _, q := range w.Questions {
		if q.ID == ref {
			return q.At, true
		}
	}
	for _, a := range w.Attempts {
		if a.Evidence == ref || "attempt:"+strconv.FormatInt(a.At, 10) == ref {
			return a.At, true
		}
	}
	return 0, false
}

func (w *WorkState) AddHypothesis(statement string, now time.Time) (Hypothesis, error) {
	if err := boundedWork(statement); err != nil {
		return Hypothesis{}, err
	}
	open := 0
	for _, h := range w.Hypotheses {
		if h.Status == "open" {
			open++
		}
	}
	if len(w.Hypotheses) >= maxWorkHypotheses {
		return Hypothesis{}, errors.New("hypothesis limit reached")
	}
	h := Hypothesis{ID: w.next("H"), Statement: statement, Status: "open", At: now.UnixMilli()}
	w.Hypotheses = append(w.Hypotheses, h)
	return h, nil
}

func (w *WorkState) hypothesis(id string) *Hypothesis {
	for i := range w.Hypotheses {
		if w.Hypotheses[i].ID == id {
			return &w.Hypotheses[i]
		}
	}
	return nil
}

// AddExperiment registers an experiment before it runs. A fixture that does
// not exercise the real path must state its limits.
func (w *WorkState) AddExperiment(hypothesis, question, prediction string, representative bool, limits string, now time.Time) (Experiment, error) {
	if w.hypothesis(hypothesis) == nil {
		return Experiment{}, errors.New("unknown hypothesis")
	}
	for _, s := range []string{question, prediction} {
		if err := boundedWork(s); err != nil {
			return Experiment{}, err
		}
	}
	if !representative && strings.TrimSpace(limits) == "" {
		return Experiment{}, errors.New("a non-representative experiment must state its limits: which part of the real path it does not exercise")
	}
	if len(limits) > maxWorkText {
		return Experiment{}, errors.New("limits too long")
	}
	if len(w.Experiments) >= maxWorkExperiments {
		return Experiment{}, errors.New("experiment limit reached")
	}
	e := Experiment{ID: w.next("E"), HypothesisID: hypothesis, Question: question, Prediction: prediction, Representative: representative, Limits: limits, Outcome: "pending", At: now.UnixMilli()}
	w.Experiments = append(w.Experiments, e)
	return e, nil
}

// RecordExperiment binds an outcome to operations observed after the
// experiment was registered, and updates its hypothesis.
func (w *WorkState) RecordExperiment(st State, id, outcome, observation string, ops []string) error {
	var e *Experiment
	for i := range w.Experiments {
		if w.Experiments[i].ID == id {
			e = &w.Experiments[i]
		}
	}
	if e == nil {
		return errors.New("unknown experiment")
	}
	if e.Outcome != "pending" {
		return errors.New("experiment already has an outcome; register a new experiment for new evidence")
	}
	if outcome != "supported" && outcome != "refuted" && outcome != "inconclusive" {
		return errors.New("outcome must be supported, refuted or inconclusive")
	}
	if err := boundedWork(observation); err != nil {
		return err
	}
	refs, err := observed(st, ops)
	if err != nil {
		return err
	}
	for _, r := range refs {
		if r.ObservedAt < e.At {
			return fmt.Errorf("operation %s finished before the experiment was registered; an earlier observation is not this experiment's result", r.OperationID)
		}
	}
	e.Outcome, e.Observation, e.Evidence = outcome, observation, refs
	h := w.hypothesis(e.HypothesisID)
	if outcome != "inconclusive" {
		h.Status, h.Limited = outcome, !e.Representative
	}
	return nil
}

func normalizedWords(s string) map[string]bool {
	out := map[string]bool{}
	for _, f := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return (r < 'a' || r > 'z') && (r < '0' || r > '9') && r <= 127
	}) {
		if len(f) > 2 {
			out[f] = true
		}
	}
	return out
}

func similarity(a, b string) float64 {
	wa, wb := normalizedWords(a), normalizedWords(b)
	if len(wa) == 0 || len(wb) == 0 {
		return 0
	}
	inter := 0
	for w := range wa {
		if wb[w] {
			inter++
		}
	}
	return float64(inter) / float64(len(wa)+len(wb)-inter)
}

// CandidateLimit is the number of candidates a budget tier allows.
func CandidateLimit(tier string) int {
	switch tier {
	case TierDirect:
		return 0
	case TierDeep:
		return maxWorkCandidates
	default:
		return 3
	}
}

// AddCandidate registers a solution candidate that differs in substance
// from the existing ones.
func (w *WorkState) AddCandidate(st State, approach, axis, workspace, task string, now time.Time) (Candidate, error) {
	if !slices.Contains(candidateAxes, axis) {
		return Candidate{}, fmt.Errorf("axis must be one of %s", strings.Join(candidateAxes, ", "))
	}
	if err := boundedWork(approach); err != nil {
		return Candidate{}, err
	}
	if limit := CandidateLimit(w.Budget.Tier); len(w.Candidates) >= limit {
		return Candidate{}, fmt.Errorf("the %s work budget allows %d candidates; escalate with evidence (workflow work budget) if the problem needs more", w.Budget.Tier, limit)
	}
	for _, c := range w.Candidates {
		if c.Axis == axis && similarity(c.Approach, approach) >= 0.6 {
			return Candidate{}, fmt.Errorf("candidate is a restatement of %s; describe a different algorithm, architecture or failure hypothesis", c.ID)
		}
	}
	if workspace != "" {
		found := false
		for _, ws := range st.Workspaces {
			if ws.ID == workspace {
				if task != "" && task != ws.TaskID {
					return Candidate{}, errors.New("candidate task must match the managed workspace owner")
				}
				found, task = true, cmpOr(task, ws.TaskID)
			}
		}
		if !found {
			return Candidate{}, errors.New("unknown workspace; create it with worktree or workflow dispatch isolate")
		}
	}
	c := Candidate{ID: w.next("C"), Approach: approach, Axis: axis, WorkspaceID: workspace, TaskID: task, Status: "proposed", At: now.UnixMilli()}
	w.Candidates = append(w.Candidates, c)
	return c, nil
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// SetSuite registers the discriminating checks. After the first result a
// revision clears all results and must give its reason.
func (w *WorkState) SetSuite(checks []string, rationale string, now time.Time) error {
	if len(checks) == 0 || len(checks) > 12 {
		return errors.New("1-12 named checks are required")
	}
	seen := map[string]bool{}
	for _, c := range checks {
		if strings.TrimSpace(c) == "" || len(c) > 128 || seen[c] {
			return errors.New("check names must be unique, non-empty and at most 128 bytes")
		}
		seen[c] = true
	}
	if err := boundedWork(rationale); err != nil {
		return err
	}
	if w.Suite != nil && w.Suite.LockedAt != 0 {
		var removed []string
		for _, c := range w.Suite.Checks {
			if !seen[c] {
				removed = append(removed, c)
			}
		}
		revision := fmt.Sprintf("%d: revised after results (%s); removed: %s", now.UnixMilli(), rationale, strings.Join(removed, ", "))
		w.Suite = &CandidateSuite{Checks: checks, Rationale: rationale, Revisions: append(w.Suite.Revisions, revision)}
		for i := range w.Candidates {
			w.Candidates[i].Results = nil
			if w.Candidates[i].Status == "evaluated" || w.Candidates[i].Status == "selected" {
				w.Candidates[i].Status = "built"
			}
		}
		w.Selection = ""
		return nil
	}
	revisions := []string(nil)
	if w.Suite != nil {
		revisions = w.Suite.Revisions
	}
	w.Suite = &CandidateSuite{Checks: checks, Rationale: rationale, Revisions: revisions}
	return nil
}

// RecordCandidateResult binds the verification checks of an observed verify
// operation to a candidate. Check names must match the registered suite.
func (w *WorkState) RecordCandidateResult(st State, id, operation string, now time.Time) error {
	if w.Suite == nil {
		return errors.New("register the discriminating suite before recording results")
	}
	var c *Candidate
	for i := range w.Candidates {
		if w.Candidates[i].ID == id {
			c = &w.Candidates[i]
		}
	}
	if c == nil {
		return errors.New("unknown candidate")
	}
	refs, err := observed(st, []string{operation})
	if err != nil {
		return err
	}
	op, _ := findWorkOperation(st, operation)
	if op.Tool != "verify" {
		return errors.New("candidate results come from a verify run whose check names match the suite")
	}
	if c.TaskID != "" && op.TaskID != c.TaskID {
		return fmt.Errorf("verify run belongs to task %q, not to the candidate's task %q", op.TaskID, c.TaskID)
	}
	if c.TaskID == "" {
		// In a shared checkout each candidate must be measured on its own
		// source generation; one run cannot stand for several candidates.
		for _, other := range w.Candidates {
			if other.ID == c.ID || other.TaskID != "" {
				continue
			}
			for _, r := range other.Results {
				if r.Generation == refs[0].Generation {
					return fmt.Errorf("candidate %s was measured on the same source generation; apply this candidate before measuring it", other.ID)
				}
			}
		}
	}
	results := map[string]CandidateResult{}
	for _, check := range st.Checks {
		if check.RunID == op.CallID && slices.Contains(w.Suite.Checks, check.Name) {
			results[check.Name] = CandidateResult{OperationID: op.ID, Passed: check.Passed, Generation: op.Generation}
		}
	}
	if len(results) == 0 {
		return errors.New("the verify run contains none of the suite's checks; use the registered check names")
	}
	if c.Results == nil {
		c.Results = map[string]CandidateResult{}
	}
	// Checks from different versions of a candidate cannot collectively
	// prove that one version passed the suite.
	for _, previous := range c.Results {
		if previous.Generation != op.Generation {
			c.Results = map[string]CandidateResult{}
			break
		}
	}
	for name, r := range results {
		c.Results[name] = r
	}
	c.Status = "evaluated"
	if w.Suite.LockedAt == 0 {
		w.Suite.LockedAt = now.UnixMilli()
	}
	return nil
}

// CandidateScore counts passed suite checks; complete is true when every
// suite check has a result.
func (w *WorkState) CandidateScore(c Candidate) (passed int, complete bool) {
	if w.Suite == nil {
		return 0, false
	}
	complete = true
	for _, name := range w.Suite.Checks {
		r, ok := c.Results[name]
		if !ok {
			complete = false
			continue
		}
		if r.Passed {
			passed++
		}
	}
	return passed, complete
}

// Select chooses a candidate from measured results, not from the model's
// opinion. Only a candidate that passed the complete suite can be selected;
// a choice between several such candidates needs a stated tie-break.
func (w *WorkState) Select(id, tieBreak string, now time.Time) (string, error) {
	if w.Suite == nil {
		return "", errors.New("no discriminating suite registered")
	}
	var winners []string
	best, bestID := -1, ""
	for _, c := range w.Candidates {
		passed, complete := w.CandidateScore(c)
		if complete && passed == len(w.Suite.Checks) {
			winners = append(winners, c.ID)
		}
		if passed > best {
			best, bestID = passed, c.ID
		}
	}
	if !slices.Contains(winners, id) {
		if len(winners) == 0 {
			return "", fmt.Errorf("no candidate passed the complete suite (best: %s with %d/%d); add a candidate or an experiment instead of selecting", bestID, max(best, 0), len(w.Suite.Checks))
		}
		return "", fmt.Errorf("%s did not pass the complete suite; passing candidates: %s", id, strings.Join(winners, ", "))
	}
	basis := "only candidate that passed the complete suite"
	if len(winners) > 1 {
		if err := boundedWork(tieBreak); err != nil {
			return "", errors.New("several candidates passed the suite; give a tie_break grounded in a user requirement or a measured difference")
		}
		basis = "tie-break between " + strings.Join(winners, ", ") + ": " + tieBreak
	}
	w.Selection = id + ": " + basis
	for i := range w.Candidates {
		switch {
		case w.Candidates[i].ID == id:
			w.Candidates[i].Status = "selected"
		case w.Candidates[i].Status == "selected":
			w.Candidates[i].Status = "evaluated"
		}
	}
	return basis, nil
}

// RecordAttempt keeps a failed approach and its error class so it is not
// silently retried.
func (w *WorkState) RecordAttempt(what, class, evidence, lesson, source string, now time.Time) error {
	if err := boundedWork(what); err != nil {
		return err
	}
	if len(class) > 128 || len(evidence) > 128 || len(lesson) > maxWorkText {
		return errors.New("attempt fields exceed bounds")
	}
	w.Attempts = append(w.Attempts, FailedAttempt{What: what, ErrorClass: class, Evidence: evidence, Lesson: lesson, Source: source, At: now.UnixMilli()})
	if len(w.Attempts) > maxWorkAttempts {
		w.Attempts = w.Attempts[len(w.Attempts)-maxWorkAttempts:]
	}
	return nil
}

func (w *WorkState) AddQuestion(text string, blocks []string, now time.Time) (OpenQuestion, error) {
	if err := boundedWork(text); err != nil {
		return OpenQuestion{}, err
	}
	for _, id := range blocks {
		if w.item(id) == nil {
			return OpenQuestion{}, fmt.Errorf("unknown item %s", id)
		}
	}
	open := 0
	for _, q := range w.Questions {
		if q.Status == "open" {
			open++
		}
	}
	if open >= maxWorkQuestions {
		return OpenQuestion{}, errors.New("too many open questions; resolve some first")
	}
	q := OpenQuestion{ID: w.next("Q"), Text: text, Blocks: blocks, Status: "open", At: now.UnixMilli()}
	w.Questions = append(w.Questions, q)
	if len(w.Questions) > 2*maxWorkQuestions {
		w.Questions = slices.DeleteFunc(w.Questions, func(q OpenQuestion) bool { return q.Status != "open" })
	}
	return q, nil
}

// AnswerQuestion resolves a question from a recorded user turn or observed
// operations; the agent cannot answer its own open question by assertion.
func (w *WorkState) AnswerQuestion(st State, id, answer string, turn int, ops []string) error {
	var q *OpenQuestion
	for i := range w.Questions {
		if w.Questions[i].ID == id {
			q = &w.Questions[i]
		}
	}
	if q == nil || q.Status != "open" {
		return errors.New("unknown or already answered question")
	}
	if err := boundedWork(answer); err != nil {
		return err
	}
	if turn != 0 {
		if _, ok := w.turn(turn); !ok {
			return fmt.Errorf("user turn %d is not recorded", turn)
		}
		answer = fmt.Sprintf("%s (user turn %d)", answer, turn)
	} else {
		if _, err := observed(st, ops); err != nil {
			return fmt.Errorf("an answer needs a user turn or observed operations: %w", err)
		}
		answer = fmt.Sprintf("%s (observed: %s)", answer, strings.Join(ops, ", "))
	}
	q.Status, q.Answer = "answered", answer
	return nil
}

// ClassifyBudget derives the work tier from observable signals. The prompt
// gives the starting point; recorded failures, refuted hypotheses and
// strategy signals can only raise it. A user override wins.
func ClassifyBudget(prompt string, w *WorkState, profile string) WorkBudget {
	if w != nil && w.Budget.Override != "" {
		return WorkBudget{Tier: w.Budget.Override, Override: w.Budget.Override, Reasons: []string{"user override"}}
	}
	text := strings.ToLower(prompt)
	words := len(strings.Fields(text))
	tier := TierStandard
	var reasons []string
	deepWords := []string{"architecture", "mimari", "refactor", "migrat", "intermittent", "flaky", "race", "deadlock", "sporadic", "ara sıra", "rastgele", "kök neden", "root cause", "redesign", "yeniden tasarla", "kapsamlı", "comprehensive", "performance", "performans"} //nolint:misspell // Turkish intent term.
	changeWords := []string{"fix", "düzelt", "implement", "add", "ekle", "change", "değiştir", "write", "yaz", "create", "oluştur", "build", "refactor", "update", "güncelle", "remove", "kaldır", "sil", "migrat"}
	for _, word := range deepWords {
		if strings.Contains(text, word) {
			tier = TierDeep
			reasons = append(reasons, "request mentions "+strconv.Quote(word))
			break
		}
	}
	if tier != TierDeep && words > 0 && words <= 25 && !slices.ContainsFunc(changeWords, func(w string) bool { return strings.Contains(text, w) }) {
		tier = TierDirect
		reasons = append(reasons, fmt.Sprintf("short request (%d words) without a change verb", words))
	}
	if tier == TierStandard {
		reasons = append(reasons, "change request; profile "+cmpOr(profile, "feature"))
		if words > 200 {
			tier = TierDeep
			reasons = append(reasons, fmt.Sprintf("long specification (%d words)", words))
		}
	}
	if w != nil {
		refuted := 0
		for _, h := range w.Hypotheses {
			if h.Status == "refuted" {
				refuted++
			}
		}
		escalate := func(reason string) {
			reasons = append(reasons, reason)
			if tier == TierDirect {
				tier = TierStandard
			} else {
				tier = TierDeep
			}
		}
		if refuted > 0 {
			escalate(fmt.Sprintf("%d refuted hypotheses", refuted))
		}
		if len(w.Attempts) >= 2 {
			escalate(fmt.Sprintf("%d failed attempts recorded", len(w.Attempts)))
		}
		if w.Strategy.Signals > 0 {
			escalate(fmt.Sprintf("%d strategy signals observed", w.Strategy.Signals))
		}
		// A tier never drops while the same task is in progress.
		if rank(w.Budget.Tier) > rank(tier) {
			tier = w.Budget.Tier
			reasons = append(reasons, "kept from earlier in this task")
		}
	}
	return WorkBudget{Tier: tier, Reasons: reasons}
}

func rank(tier string) int {
	switch tier {
	case TierDirect:
		return 1
	case TierStandard:
		return 2
	case TierDeep:
		return 3
	}
	return 0
}

// ValidTier reports whether s names a work budget tier.
func ValidTier(s string) bool { return rank(s) > 0 }

// ValidPhase reports whether s names a work phase.
func ValidPhase(s string) bool {
	return slices.Contains([]string{"orient", "investigate", "implement", "verify", "deliver", "waiting"}, s)
}

// WorkView is the compact projection shown to the model every turn and to
// the user in the workflow panel.
type WorkView struct {
	Budget      WorkBudget        `json:"budget"`
	Phase       string            `json:"phase,omitempty"`
	Waiting     string            `json:"waiting,omitempty"`
	Generation  uint64            `json:"source_generation"`
	Requested   []WorkItemView    `json:"requested,omitempty"`
	Proposed    []WorkItemView    `json:"proposed,omitempty"`
	Assumptions []WorkItemView    `json:"assumptions,omitempty"`
	LastTurn    *UserTurn         `json:"last_user_turn,omitempty"`
	Method      *MethodDecision   `json:"method,omitempty"`
	Hypotheses  []Hypothesis      `json:"hypotheses,omitempty"`
	Experiments []Experiment      `json:"pending_experiments,omitempty"`
	Candidates  []CandidateView   `json:"candidates,omitempty"`
	Suite       *CandidateSuite   `json:"suite,omitempty"`
	Selection   string            `json:"selection,omitempty"`
	Attempts    []FailedAttempt   `json:"recent_failed_attempts,omitempty"`
	Questions   []OpenQuestion    `json:"open_questions,omitempty"`
	NextChecks  []string          `json:"next_checks,omitempty"`
	Omitted     map[string]int    `json:"omitted,omitempty"`
	Recovered   *StateRecovery    `json:"state_recovered,omitempty"`
	Counts      map[string]int    `json:"status_counts,omitempty"`
	Extra       map[string]string `json:"notes,omitempty"`
}

type WorkItemView struct {
	ID     string `json:"id"`
	Kind   string `json:"kind"`
	Text   string `json:"text"`
	Status string `json:"status"`
	Turns  []int  `json:"turns,omitempty"`
}

type CandidateView struct {
	ID       string `json:"id"`
	Approach string `json:"approach"`
	Axis     string `json:"axis"`
	Status   string `json:"status"`
	Passed   int    `json:"passed"`
	Complete bool   `json:"complete"`
	Isolated bool   `json:"isolated"`
}

// View projects the state. Finished items are counted, not listed, so the
// view stays small; the full records remain readable through the tool.
func (w *WorkState) View(st State) WorkView {
	v := WorkView{Budget: w.Budget, Phase: w.Phase, Waiting: w.Waiting, Generation: st.Generation, NextChecks: w.NextChecks, Recovered: st.Recovered, Suite: w.Suite, Selection: w.Selection, Counts: map[string]int{}, Omitted: map[string]int{}}
	if n := len(w.Turns); n > 0 {
		last := w.Turns[n-1]
		if len(last.Text) > 280 {
			last.Text = strings.ToValidUTF8(last.Text[:280], "") + " [see workflow work status]"
		}
		v.LastTurn = &last
	}
	for _, item := range w.Items {
		status := EffectiveStatus(item, st.Generation)
		v.Counts[status]++
		if status == ItemDelivered || status == ItemSuperseded || status == ItemDeclined {
			v.Omitted["finished_items"]++
			continue
		}
		view := WorkItemView{ID: item.ID, Kind: item.Kind, Text: item.Text, Status: status, Turns: item.SourceTurns}
		switch item.Origin {
		case OriginUser:
			v.Requested = append(v.Requested, view)
		case OriginProposal:
			v.Proposed = append(v.Proposed, view)
		default:
			v.Assumptions = append(v.Assumptions, view)
		}
	}
	if n := len(w.Methods); n > 0 {
		m := w.Methods[n-1]
		v.Method = &m
	}
	for _, h := range w.Hypotheses {
		if h.Status == "open" || len(v.Hypotheses) < 4 {
			v.Hypotheses = append(v.Hypotheses, h)
		}
	}
	for _, e := range w.Experiments {
		if e.Outcome == "pending" {
			v.Experiments = append(v.Experiments, e)
		}
	}
	for _, c := range w.Candidates {
		passed, complete := w.CandidateScore(c)
		v.Candidates = append(v.Candidates, CandidateView{ID: c.ID, Approach: c.Approach, Axis: c.Axis, Status: c.Status, Passed: passed, Complete: complete, Isolated: c.WorkspaceID != "" || c.BranchID != ""})
	}
	start := max(0, len(w.Attempts)-3)
	v.Attempts = w.Attempts[start:]
	if start > 0 {
		v.Omitted["older_attempts"] = start
	}
	for _, q := range w.Questions {
		if q.Status == "open" {
			v.Questions = append(v.Questions, q)
		}
	}
	return v
}

// validate enforces the bounds on every write, including state decoded
// from disk, so a damaged or hand-edited record cannot grow without limit.
func (w *WorkState) validate() error {
	if len(w.Turns) > maxWorkTurns || len(w.Items) > maxWorkItems || len(w.Methods) > maxWorkMethods ||
		len(w.Hypotheses) > maxWorkHypotheses || len(w.Experiments) > maxWorkExperiments ||
		len(w.Candidates) > maxWorkCandidates || len(w.Attempts) > maxWorkAttempts ||
		len(w.Questions) > 2*maxWorkQuestions || len(w.NextChecks) > 8 {
		return errors.New("work state collection limit exceeded")
	}
	if w.Budget.Tier != "" && !ValidTier(w.Budget.Tier) || w.Budget.Override != "" && !ValidTier(w.Budget.Override) {
		return errors.New("invalid work budget tier")
	}
	if w.Phase != "" && !ValidPhase(w.Phase) {
		return errors.New("invalid work phase")
	}
	return nil
}

// UpdateWork applies fn to the session's work state under the state lock.
// The execution state passed to fn is the same snapshot the write is based
// on, so evidence checks and the update are atomic.
func (s *Store) UpdateWork(ctx context.Context, id string, fn func(w *WorkState, st State) error) (WorkState, error) {
	var out WorkState
	err := s.Update(ctx, id, func(st *State) error {
		if st.Work == nil {
			st.Work = &WorkState{}
		}
		if err := fn(st.Work, *st); err != nil {
			return err
		}
		out = *st.Work
		return nil
	})
	return out, err
}
