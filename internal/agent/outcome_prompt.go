package agent

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agent/tools"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/outcome"
)

const runtimeOutcomeMarker = "<atlas_runtime_outcomes>"

// withRuntimeOutcomes replaces the runtime state on every model request.
// Static prompts remain cacheable and stale states never accumulate.
func withRuntimeOutcomes(messages []fantasy.Message, block string) []fantasy.Message {
	result := make([]fantasy.Message, 0, len(messages)+1)
	for _, msg := range messages {
		if msg.Role == fantasy.MessageRoleSystem && len(msg.Content) == 1 {
			if text, ok := msg.Content[0].(fantasy.TextPart); ok && strings.HasPrefix(text.Text, runtimeOutcomeMarker+"\n") {
				continue
			}
		}
		result = append(result, msg)
	}
	if block != "" {
		result = append(result, fantasy.NewSystemMessage(runtimeOutcomeMarker+"\n"+block+"\n</atlas_runtime_outcomes>"))
	}
	return result
}

func (a *sessionAgent) runtimeOutcomeBlock(ctx context.Context, sessionID string) (string, error) {
	if a.isSubAgent || a.engineering == nil {
		return "", nil
	}
	reqs, err := sharedTaskRequirements(ctx, a.engineering, a.sessions, sessionID)
	if err != nil {
		return "", fmt.Errorf("read runtime outcomes: %w", err)
	}
	snapshots, err := tools.BrowserTaskRequirementsChecked(a.workingDir, sessionID)
	if err != nil {
		return "", fmt.Errorf("read browser requirements: %w", err)
	}
	for _, req := range snapshots {
		phase := outcome.PhasePlanned
		switch req.Status {
		case tools.ReqVerified:
			phase = outcome.PhaseVerified
		case tools.ReqInProgress:
			phase = outcome.PhaseApplied
		case tools.ReqBlocked:
			phase = outcome.PhaseBlocked
		}
		if req.Verify == "delivery" && phase == outcome.PhaseVerified {
			phase = outcome.PhaseDelivered
		}
		reqs = append(reqs, outcome.Requirement{ID: "browser:" + req.TaskID + ":" + req.ID, Description: req.Text, Phase: phase, Reason: req.Reason})
	}
	return outcome.Summarize(reqs).Block(), nil
}

// syncBrowserEvidence bridges persisted task provenance into the common
// ledger. Browser's live authority stays private to its target tracker.
func syncBrowserEvidence(ctx context.Context, store *engineering.Store, root, sessionID string) error {
	if store == nil {
		return nil
	}
	var evidence []outcome.Evidence
	snapshots, err := tools.BrowserTaskRequirementsChecked(root, sessionID)
	if err != nil {
		return err
	}
	for _, req := range snapshots {
		if req.Status != tools.ReqVerified {
			continue
		}
		for _, item := range req.Provenance {
			kind := outcome.KindOutput
			if item.Kind == "observation" {
				kind = outcome.KindObservation
			}
			if item.Kind == "delivery" {
				kind = outcome.KindDelivery
			}
			subject := item.Subject
			if subject == "" {
				subject = "browser:" + req.TaskID + ":" + req.ID
			}
			evidence = append(evidence, outcome.Evidence{ID: "browser:" + req.TaskID + ":" + req.ID + ":" + item.Op, Kind: kind, Source: item.Source + "#" + item.Op, Subject: subject, Claim: item.Claim, ObservedAt: item.At.UnixMilli()})
		}
	}
	if len(evidence) == 0 {
		return nil
	}
	return store.Update(ctx, engineering.GetScope(ctx, sessionID).SessionID, func(st *engineering.State) error {
		for _, item := range evidence {
			if !slices.ContainsFunc(st.Outcomes.Evidence, func(existing outcome.Evidence) bool { return existing.ID == item.ID }) {
				st.Outcomes.Add(item)
			}
		}
		return nil
	})
}

// resetSharedContinuation accepts a new user instruction as a new budget, but
// never resets bounds for generated continuations or compaction resumes.
func resetSharedContinuation(ctx context.Context, store *engineering.Store, sessionID, prompt string) error {
	if store == nil || IsContinueDirective(prompt) || strings.HasPrefix(prompt, sharedContinuationMarker) || strings.HasPrefix(prompt, tools.BrowserContinuationMarker) || strings.HasPrefix(prompt, summaryResumePrefix) {
		return nil
	}
	return store.Update(ctx, engineering.GetScope(ctx, sessionID).SessionID, func(st *engineering.State) error {
		st.Outcomes.Continuation = outcome.Continuation{}
		return nil
	})
}
