package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agent/tools"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
)

// WorkInput carries the fields of one work action. Only the fields that
// action documents are read.
type WorkInput struct {
	BranchID       string   `json:"branch_id,omitempty" description:"candidate: optional patch branch created with workflow branch fork/propose. Cannot be combined with workspace_id."`
	Kind           string   `json:"kind,omitempty" description:"contract_add: behavior, deliverable, constraint or uncertainty."`
	Text           string   `json:"text,omitempty" description:"contract_add/question: the requirement or question in one sentence."`
	Origin         string   `json:"origin,omitempty" description:"contract_add: user (must cite source_turns), agent_proposal or assumption."`
	SourceTurns    []int    `json:"source_turns,omitempty" description:"contract_add with origin user: recorded user turn numbers (seq) that state it."`
	ItemID         string   `json:"item_id,omitempty" description:"contract_advance/supersede/decline: contract item ID."`
	Status         string   `json:"status,omitempty" description:"contract_advance: implemented or verified."`
	OperationIDs   []string `json:"operation_ids,omitempty" description:"Observed operations (id or call_id from recent_operations). verified may omit it to bind the latest passing check at the current source generation."`
	Replacement    string   `json:"replacement,omitempty" description:"contract_supersede: the item that replaces item_id."`
	Turn           int      `json:"turn,omitempty" description:"contract_supersede/answer: the user turn (seq) that changed or answered it."`
	Method         string   `json:"method,omitempty" description:"method: research, direct, prototype, experiment, review, candidates, delegate or ask_user."`
	Reason         string   `json:"reason,omitempty" description:"method/budget: why, in one or two sentences."`
	Evidence       []string `json:"evidence,omitempty" description:"method/budget: references that prompted the decision: operation id/call_id, H/E/Q IDs or turn:N."`
	Statement      string   `json:"statement,omitempty" description:"hypothesis: a falsifiable statement."`
	HypothesisID   string   `json:"hypothesis_id,omitempty" description:"experiment: the hypothesis tested."`
	Question       string   `json:"question,omitempty" description:"experiment: what the experiment asks."`
	Prediction     string   `json:"prediction,omitempty" description:"experiment: the observation that would refute the hypothesis."`
	Representative *bool    `json:"representative,omitempty" description:"experiment: true when it exercises the real code path; false requires limits."`
	Limits         string   `json:"limits,omitempty" description:"experiment: what a non-representative fixture does not exercise."`
	ExperimentID   string   `json:"experiment_id,omitempty" description:"experiment_result: experiment ID."`
	Outcome        string   `json:"outcome,omitempty" description:"experiment_result: supported, refuted or inconclusive."`
	Observation    string   `json:"observation,omitempty" description:"experiment_result: what was observed."`
	Approach       string   `json:"approach,omitempty" description:"candidate: the approach, distinct in substance from existing candidates."`
	Axis           string   `json:"axis,omitempty" description:"candidate: algorithm, architecture, hypothesis, configuration or dependency."`
	WorkspaceID    string   `json:"workspace_id,omitempty" description:"candidate: isolated workspace holding this candidate."`
	TaskID         string   `json:"task_id,omitempty" description:"candidate: todo ID whose isolated runs measure this candidate."`
	CandidateID    string   `json:"candidate_id,omitempty" description:"candidate_result/select: candidate ID."`
	Checks         []string `json:"checks,omitempty" description:"suite: names of verify checks on which the candidates are expected to differ."`
	Rationale      string   `json:"rationale,omitempty" description:"suite: why these checks discriminate between the candidates."`
	TieBreak       string   `json:"tie_break,omitempty" description:"select: required when several candidates passed the suite."`
	What           string   `json:"what,omitempty" description:"attempt: what was tried."`
	ErrorClass     string   `json:"error_class,omitempty" description:"attempt: short class of the failure."`
	Lesson         string   `json:"lesson,omitempty" description:"attempt: what not to repeat."`
	Blocks         []string `json:"blocks,omitempty" description:"question: contract item IDs blocked by it."`
	QuestionID     string   `json:"question_id,omitempty" description:"answer: question ID."`
	Answer         string   `json:"answer,omitempty" description:"answer: the answer, with turn or operation_ids as its source."`
	Phase          string   `json:"phase,omitempty" description:"phase: orient, investigate, implement, verify, deliver or waiting."`
	Waiting        string   `json:"waiting,omitempty" description:"phase waiting: what is awaited."`
	NextChecks     []string `json:"next_checks,omitempty" description:"phase: up to 8 checks still to run."`
	Tier           string   `json:"tier,omitempty" description:"budget: standard or deep. Raises the budget only, with evidence."`
}

// workInput returns the work action's fields. They belong under "work", but
// models often put them next to work_action instead; those were silently
// dropped, so contract_add failed on its kind and contract_advance on an
// unknown item even with valid values. A field set under "work" wins over
// the same field at the top level.
func workInput(p WorkflowParams, raw string) WorkInput {
	var in WorkInput
	if raw != "" {
		_ = json.Unmarshal([]byte(raw), &in)
	}
	if p.Work == nil {
		return in
	}
	nested, err := json.Marshal(p.Work)
	if err != nil {
		return *p.Work
	}
	if err := json.Unmarshal(nested, &in); err != nil {
		return *p.Work
	}
	return in
}

const workActions = "status, contract_add, contract_advance, contract_supersede, contract_decline, method, hypothesis, experiment, experiment_result, suite, candidate, candidate_result, select, attempt, question, answer, phase, budget"

func (c *coordinator) workWorkflow(ctx context.Context, p WorkflowParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
	id := engineering.GetScope(ctx, tools.GetSessionFromContext(ctx)).SessionID
	in := workInput(p, call.Input)
	if p.WorkAction == "status" || p.WorkAction == "" {
		st, err := c.engineering.Read(ctx, id)
		if err != nil {
			return fantasy.ToolResponse{}, err
		}
		work := engineering.WorkState{}
		if st.Work != nil {
			work = *st.Work
		}
		return workResponse(map[string]any{"work": work, "source_generation": st.Generation, "recent_operations": engineering.RecentOperations(st, 12)})
	}
	var result any
	now := time.Now()
	_, err := c.engineering.UpdateWork(ctx, id, func(w *engineering.WorkState, st engineering.State) error {
		var err error
		switch p.WorkAction {
		case "contract_add":
			result, err = w.AddItem(in.Kind, in.Text, in.Origin, in.SourceTurns, now)
		case "contract_advance":
			ops := in.OperationIDs
			if len(ops) == 0 && in.Status == engineering.ItemVerified {
				latest, ok := engineering.LatestCheck(st)
				if !ok {
					return errors.New("no passing check observed at the current source generation; run the relevant check first")
				}
				ops = []string{latest.ID}
			}
			if err = w.Advance(st, in.ItemID, in.Status, ops, now); err == nil {
				result = map[string]any{"item_id": in.ItemID, "status": in.Status, "bound_operations": ops}
			}
		case "contract_supersede":
			err = w.Supersede(in.ItemID, in.Replacement, in.Turn, now)
		case "contract_decline":
			err = w.Decline(in.ItemID, now)
		case "method":
			result, err = w.AddMethod(st, in.Method, in.Reason, in.Evidence, "agent", now)
		case "hypothesis":
			result, err = w.AddHypothesis(in.Statement, now)
		case "experiment":
			if in.Representative == nil {
				return errors.New("representative is required: true when the experiment exercises the real path")
			}
			result, err = w.AddExperiment(in.HypothesisID, in.Question, in.Prediction, *in.Representative, in.Limits, now)
		case "experiment_result":
			err = w.RecordExperiment(st, in.ExperimentID, in.Outcome, in.Observation, in.OperationIDs)
		case "suite":
			err = w.SetSuite(in.Checks, in.Rationale, now)
		case "candidate":
			if in.BranchID != "" {
				if in.WorkspaceID != "" {
					return errors.New("choose workspace_id or branch_id, not both")
				}
				found := false
				for _, b := range st.Branches {
					if b.ID == in.BranchID && (b.Status == "draft" || b.Status == "proposed") && (in.TaskID == "" || b.TaskID == in.TaskID) {
						found = true
						in.TaskID = b.TaskID
					}
				}
				if !found {
					return errors.New("candidate patch branch is missing, submitted or belongs to another task")
				}
			}
			result, err = w.AddCandidate(st, in.Approach, in.Axis, in.WorkspaceID, in.TaskID, now)
			if err == nil && in.BranchID != "" {
				w.Candidates[len(w.Candidates)-1].BranchID = in.BranchID
				result = w.Candidates[len(w.Candidates)-1]
			}
		case "candidate_result":
			if len(in.OperationIDs) != 1 {
				return errors.New("candidate_result takes exactly one verify operation in operation_ids")
			}
			err = w.RecordCandidateResult(st, in.CandidateID, in.OperationIDs[0], now)
		case "select":
			var basis string
			if basis, err = w.Select(in.CandidateID, in.TieBreak, now); err == nil {
				result = map[string]string{"selected": in.CandidateID, "basis": basis, "next": "integrate the selection, then verify the contract at the new source generation"}
			}
		case "attempt":
			err = w.RecordAttempt(in.What, in.ErrorClass, firstOf(in.OperationIDs), in.Lesson, "agent", now)
		case "question":
			result, err = w.AddQuestion(in.Text, in.Blocks, now)
		case "answer":
			err = w.AnswerQuestion(st, in.QuestionID, in.Answer, in.Turn, in.OperationIDs)
		case "phase":
			if !engineering.ValidPhase(in.Phase) || len(in.NextChecks) > 8 || len(in.Waiting) > 512 {
				return errors.New("phase must be orient, investigate, implement, verify, deliver or waiting, with at most 8 next_checks")
			}
			w.Phase, w.Waiting, w.NextChecks = in.Phase, in.Waiting, in.NextChecks
		case "budget":
			err = raiseBudget(w, st, in, now)
		default:
			return fmt.Errorf("unknown work_action; use one of: %s", workActions)
		}
		return err
	})
	if err != nil {
		if ctx.Err() != nil {
			return fantasy.ToolResponse{}, ctx.Err()
		}
		return fantasy.NewTextErrorResponse("work " + p.WorkAction + " rejected, nothing was recorded: " + err.Error()), nil
	}
	st, err := c.engineering.Read(ctx, id)
	if err != nil {
		return fantasy.ToolResponse{}, err
	}
	return workResponse(map[string]any{"recorded": p.WorkAction, "result": result, "view": st.Work.View(st)})
}

// raiseBudget lets the agent raise the work tier on recorded evidence. It
// cannot lower the tier or override the user's setting.
func raiseBudget(w *engineering.WorkState, st engineering.State, in WorkInput, now time.Time) error {
	if w.Budget.Override != "" {
		return errors.New("the user fixed the work budget in settings; it cannot be changed by the agent")
	}
	if in.Tier != engineering.TierStandard && in.Tier != engineering.TierDeep {
		return errors.New("tier must be standard or deep")
	}
	rank := map[string]int{engineering.TierDirect: 1, engineering.TierStandard: 2, engineering.TierDeep: 3}
	if rank[in.Tier] <= rank[w.Budget.Tier] {
		return errors.New("the budget can only be raised")
	}
	if len(in.Evidence) == 0 {
		return errors.New("raising the budget needs evidence: operations, refuted hypotheses or questions that show the task is harder than classified")
	}
	if _, err := w.AddMethod(st, "research", "budget raised to "+in.Tier+": "+in.Reason, in.Evidence, "agent", now); err != nil {
		return err
	}
	w.Budget.Tier = in.Tier
	w.Budget.Reasons = append(w.Budget.Reasons, "raised by agent: "+in.Reason)
	return nil
}

func firstOf(s []string) string {
	if len(s) == 0 {
		return ""
	}
	return s[0]
}

func workResponse(v any) (fantasy.ToolResponse, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return fantasy.ToolResponse{}, err
	}
	return fantasy.NewTextResponse(string(data)), nil
}
