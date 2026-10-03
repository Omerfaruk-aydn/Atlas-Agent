package agent

import (
	"context"
	"fmt"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/session"
)

// prepareDirectives consumes durable user instructions at a model boundary.
// Claiming before message creation makes uncertain deliveries inspectable.
func (a *sessionAgent) prepareDirectives(ctx context.Context, call SessionAgentCall, first bool) ([]fantasy.Message, error) {
	if a.engineering == nil {
		return nil, nil
	}
	scope := engineering.GetScope(ctx, call.SessionID)
	_, board, err := a.engineering.ReadControlBoard(ctx, scope.SessionID)
	if err != nil {
		return nil, err
	}
	if len(board.Directives) == 0 {
		return nil, nil
	}
	if call.QueueContinuation {
		state, err := a.engineering.Read(ctx, scope.SessionID)
		if err != nil {
			return nil, err
		}
		if state.Paused {
			return nil, fmt.Errorf("workflow queue is paused; no instruction was delivered")
		}
	}
	sess, err := a.sessions.Get(ctx, scope.SessionID)
	if err != nil {
		return nil, err
	}
	completed := map[string]bool{}
	for _, task := range sess.Todos {
		completed[task.ID] = task.Status == session.TodoStatusCompleted
	}
	var messages []fantasy.Message
	for range 4 {
		directive, err := a.engineering.ClaimDirective(ctx, scope.SessionID, scope.TaskID, first, completed)
		if err != nil {
			return nil, err
		}
		if directive == nil {
			break
		}
		prompt := fmt.Sprintf("User workflow instruction %s (target task: %s):\n%s", directive.ID, directive.TaskID, directive.Text)
		if directive.File != "" {
			prompt += fmt.Sprintf("\nUser review location: %s:%d. Reinspect current source before applying this feedback.", directive.File, directive.Line)
		}
		userMessage, err := a.createUserMessage(ctx, SessionAgentCall{SessionID: call.SessionID, Prompt: prompt})
		if err != nil {
			return nil, err
		}
		if err := a.engineering.ReceiveDirective(ctx, scope.SessionID, directive.ID); err != nil {
			return nil, err
		}
		messages = append(messages, userMessage.ToAIMessage()...)
		if directive.Mode == "next" {
			first = false
		}
	}
	return messages, nil
}

func (a *sessionAgent) durableQueueReady(ctx context.Context, call SessionAgentCall) (bool, error) {
	if a.engineering == nil {
		return false, nil
	}
	scope := engineering.GetScope(ctx, call.SessionID)
	_, board, err := a.engineering.ReadControlBoard(ctx, scope.SessionID)
	if err != nil {
		return false, err
	}
	if len(board.Directives) == 0 {
		return false, nil
	}
	st, err := a.engineering.Read(ctx, scope.SessionID)
	if err != nil {
		return false, err
	}
	if st.Paused || a.engineering.Check(ctx, scope.SessionID, scope.TaskID) != nil {
		return false, nil
	}
	sess, err := a.sessions.Get(ctx, scope.SessionID)
	if err != nil {
		return false, err
	}
	completed := map[string]bool{}
	for _, task := range sess.Todos {
		completed[task.ID] = task.Status == session.TodoStatusCompleted
	}
	for _, directive := range board.Directives {
		if directive.Status != "queued" || directive.TaskID != scope.TaskID {
			continue
		}
		ready := true
		for _, dep := range directive.DependsOn {
			if !completed[dep] {
				ready = false
			}
		}
		if ready {
			return true, nil
		}
	}
	return false, nil
}
