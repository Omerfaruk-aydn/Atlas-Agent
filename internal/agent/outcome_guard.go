package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agent/tools"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/outcome"
)

// workspaceSubject names evidence about the project as a whole, such as a
// passing test run. Any file mutation voids it.
const workspaceSubject = "workspace"

// outcomeMutates reports whether a call may change something outside the
// session. Tools with their own contracts (browser, computer) keep them;
// this covers file edits and tools whose effects are opaque, such as MCP.
func outcomeMutates(name string, mutation bool) bool {
	return mutation || strings.HasPrefix(name, "mcp_")
}

// outcomeSubjects names what a mutation touches. An opaque mutation touches
// everything, so it voids all earlier output evidence.
func outcomeSubjects(call fantasy.ToolCall) []string {
	switch call.Name {
	case "edit", "write", "multiedit":
		var p struct {
			FilePath string `json:"file_path"`
		}
		if json.Unmarshal([]byte(call.Input), &p) == nil && p.FilePath != "" {
			return []string{filepath.Clean(p.FilePath), workspaceSubject}
		}
	}
	return []string{"*"}
}

func dispatchUnknown(resp fantasy.ToolResponse) bool {
	var meta struct {
		Dispatch string `json:"dispatch"`
	}
	return json.Unmarshal([]byte(resp.Metadata), &meta) == nil && meta.Dispatch == "unknown"
}

// outcomeResult maps one finished call onto the shared vocabulary.
func outcomeResult(name string, resp fantasy.ToolResponse, runErr error, mutation, canceled bool) outcome.Result {
	unknown := resp.IsError && dispatchUnknown(resp)
	_, observed := operationOutcome(name, resp, runErr)
	result := outcome.Result{
		Canceled:  canceled,
		Err:       runErr,
		Rejected:  resp.IsError,
		Sent:      unknown,
		Uncertain: unknown,
		Waiting:   resp.StopTurn,
		Mutation:  outcomeMutates(name, mutation) || unknown,
		Observed:  !resp.IsError && !resp.StopTurn && runErr == nil && tools.ToolSucceeded(name, resp, nil) && observed,
	}
	if native, ok := tools.ResponseOutcome(resp); ok {
		result.DispatchKnown = true
		result.Sent = native.Dispatch != "none"
		result.Uncertain = native.Dispatch == "unknown"
		result.Mutation = native.Mutation
		result.Observed = native.Verified && !resp.IsError && !resp.StopTurn && runErr == nil
	}
	if strings.HasPrefix(name, "mcp_") {
		var meta struct {
			Dispatch string `json:"dispatch"`
		}
		if json.Unmarshal([]byte(resp.Metadata), &meta) == nil && (meta.Dispatch == "none" || meta.Dispatch == "sent" || meta.Dispatch == "unknown") {
			result.DispatchKnown, result.Sent = true, meta.Dispatch != "none"
			result.Uncertain = meta.Dispatch == "unknown"
		}
	}
	return result
}

// recordOutcome persists the call's phase and the evidence it produced. A
// failure to persist is reported but never changes the tool's own result.
func (t *guardedTool) recordOutcome(ctx context.Context, session, opID string, call fantasy.ToolCall, result outcome.Result, resp fantasy.ToolResponse) error {
	phase := outcome.Classify(result)
	record := outcome.Record{
		OpID:        opID,
		Tool:        call.Name,
		Phase:       phase,
		Mutation:    result.Mutation,
		Sent:        result.Sent,
		Fingerprint: outcomeFingerprint(call),
	}
	if result.Mutation {
		record.Subjects = outcomeSubjects(call)
	}
	native, hasNative := tools.ResponseOutcome(resp)
	if hasNative {
		record.Subjects = native.Subjects
	}
	return t.store.Update(ctx, session, func(st *engineering.State) error {
		for _, existing := range st.Outcomes.Records {
			if existing.OpID == opID && existing.Resolved {
				return nil
			}
		}
		// Explicit inspection wins over a late backend result. Resolve may
		// run while a blocking native call is still finishing.
		for _, op := range st.Operations {
			if op.ID != opID || !op.OutcomeObserved || op.EvidenceHash == "" {
				continue
			}
			for _, evidence := range st.Outcomes.Evidence {
				if evidence.ID == "recovery:"+opID && evidence.Source == opID {
					record.Resolved = true
					switch op.Status {
					case "completed":
						record.Phase = outcome.PhaseApplied
					case "failed":
						record.Phase = outcome.PhaseFailed
					case "abandoned":
						record.Phase = outcome.PhaseCanceled
					}
				}
			}
		}
		st.Outcomes.Record(record)
		if record.Resolved {
			return nil
		}
		if hasNative && !resp.IsError && !resp.StopTurn && result.Err == nil {
			for _, subject := range native.Subjects {
				st.Outcomes.Add(outcome.Evidence{ID: opID + ":" + subject, Kind: native.Kind, Source: opID, Subject: subject, Claim: native.Claim})
			}
		}
		if phase == outcome.PhaseVerified && !result.Mutation {
			st.Outcomes.Add(outcome.Evidence{ID: opID, Kind: outcome.KindOutput, Source: opID, Subject: workspaceSubject, Claim: "check_passed:" + call.Name})
		}
		return nil
	})
}

// outcomeFingerprint canonicalizes JSON so whitespace and property order cannot
// bypass the unknown-operation guard.
func outcomeFingerprint(call fantasy.ToolCall) string {
	input := call.Input
	var value any
	decoder := json.NewDecoder(strings.NewReader(input))
	decoder.UseNumber()
	if decoder.Decode(&value) == nil {
		if fields, ok := value.(map[string]any); ok && (call.Name == tools.BrowserToolName || call.Name == tools.ComputerToolName || call.Name == "worktree" || call.Name == "lsp_edit_plan") {
			if action, ok := fields["action"].(string); ok {
				fields["action"] = strings.ToLower(strings.TrimSpace(action))
			}
		}
		if data, err := json.Marshal(value); err == nil {
			input = string(data)
		}
	}
	return engineering.Hash(call.Name + "\x00" + input)
}

// resendRefusal blocks an identical mutation until explicit reconciliation.
// A successful unrelated observation never establishes absence of its effect.
func (t *guardedTool) resendRefusal(ctx context.Context, session string, call fantasy.ToolCall) string {
	if !tools.CallMutates(call.Name, call.Input) {
		return ""
	}
	state, err := t.store.Read(ctx, session)
	if err != nil {
		return "outcome_state_unavailable: Cannot read persisted outcomes; no mutation was sent. " + err.Error()
	}
	fingerprint := outcomeFingerprint(call)
	for _, record := range state.Outcomes.Records {
		if call.Name == tools.ToolPipelineName && record.Phase == outcome.PhaseBlocked && record.Mutation && record.Sent && !record.Resolved && record.Fingerprint == fingerprint {
			return "partial_progress: This pipeline already sent earlier steps before waiting for the user. No completed step was repeated. Send only the remaining steps, or resume the durable flow with resume:true and freshly resolved targets."
		}
		if record.Phase == outcome.PhaseDispatched && record.Fingerprint == fingerprint {
			return "operation_running: An identical background operation is still running. No duplicate was sent; read its job_output first."
		}
	}
	_, ok := state.Outcomes.UnresolvedCall(fingerprint)
	if !ok {
		// Legacy checkpoints fingerprinted raw input instead of canonical JSON.
		_, ok = state.Outcomes.UnresolvedCall(engineering.Hash(call.Name + "\x00" + call.Input))
	}
	if !ok {
		return ""
	}
	return fmt.Sprintf("outcome_unknown: an identical %s call may already have taken effect. Nothing was sent now. Inspect its exact target and reconcile the persisted operation with workflow recovery before sending it again. An unrelated successful call does not resolve this outcome.", call.Name)
}

// outcomePromptBlock tells the model about mutations whose result is unknown,
// so its final message does not present them as done.
func outcomePromptBlock(ctx context.Context, store *engineering.Store, sessionID string) string {
	if store == nil {
		return ""
	}
	scope := engineering.GetScope(ctx, sessionID)
	state, err := store.Read(ctx, scope.SessionID)
	if err != nil {
		return ""
	}
	if block := outcome.Summarize(state.Outcomes.Requirements()).Block(); block != "" {
		return "\n\n" + block
	}
	return ""
}

// recordDelivery notes that a final answer was actually sent.
func recordDelivery(store *engineering.Store, ctx context.Context, session, messageID string) error {
	if store == nil || messageID == "" {
		return nil
	}
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	return store.Update(cleanup, session, func(st *engineering.State) error {
		st.Outcomes.Add(outcome.Evidence{ID: messageID, Kind: outcome.KindDelivery, Source: messageID, Subject: "final_message", Claim: "answer_sent"})
		return nil
	})
}

// stoppedForUser reports whether a tool halted the step for the user, which
// is a wait, not a delivered answer.
func stoppedForUser(results []fantasy.ToolResultContent) bool {
	for _, r := range results {
		if r.StopTurn {
			return true
		}
	}
	return false
}
