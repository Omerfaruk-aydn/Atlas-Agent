package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agent/notify"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agent/tools"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/pubsub"
)

// A goal is the session driving itself. The user states what they want
// reached; the agent then keeps taking turns on its own until it is
// reached, instead of the user typing "carry on" twenty times.
//
// Two things make that safe enough to ship. The agent cannot decide on
// its own that it is finished -- it says so, and a second model checks
// the claim against the goal before the run stops, because a model that
// grades its own homework stops early. And every run is bounded: by the
// turn budget the agent sets for itself when it starts, by a ceiling it
// cannot exceed whatever it asks for, and by the session cost cap that
// already guards ordinary runs.
//
// The goal text itself lives in the system prompt (see sessionAgent.Run),
// not in the conversation, so summarizing the history away cannot take
// it with it. That matters most exactly here: an autonomous run is the
// kind that gets long enough to be compacted.

// GoalToolName is the tool the agent uses to plan and end a goal run.
const GoalToolName = "goal"

// goalHardCeiling bounds a goal run no matter what budget the agent asks
// for. The agent sizes its own budget because only it knows how big the
// work is, but "the agent decides" cannot mean "without limit" when each
// turn spends the user's money.
const goalHardCeiling = 100

// goalDefaultBudget applies until the agent sets one of its own. Small
// on purpose: an agent that never got round to planning should stop and
// say so rather than run on quietly.
const goalDefaultBudget = 10

// goalRun is one goal's progress through a session.
type goalRun struct {
	mu sync.Mutex

	goal    string
	budget  int
	used    int
	claimed bool // the agent says it is finished; awaiting the check
	done    bool
	stopped bool
}

func (g *goalRun) snapshot() (goal string, budget, used int, claimed, done, stopped bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.goal, g.budget, g.used, g.claimed, g.done, g.stopped
}

// goalRunFor returns the session's run, and false when there is none.
//
// The nil check is not defensive noise: a coordinator assembled field by
// field rather than through its constructor -- which tests do -- has no
// map here, and a session with no goal is the overwhelmingly common
// case. Neither should end a turn in a panic.
func (c *coordinator) goalRunFor(sessionID string) (*goalRun, bool) {
	if c == nil || c.goalRuns == nil {
		return nil, false
	}
	return c.goalRuns.Get(sessionID)
}

// StartGoal records what the session is working towards and arms the
// autonomous run. It does not take the first turn itself: the caller
// sends a prompt as usual, and every turn after that one comes from
// advanceGoal.
func (c *coordinator) StartGoal(ctx context.Context, sessionID, goal string) error {
	goal = strings.TrimSpace(goal)
	if goal == "" {
		return errors.New("a goal needs saying in words")
	}
	if c.goalRuns == nil {
		return errors.New("this coordinator cannot run goals")
	}
	if err := c.sessions.SetGoal(ctx, sessionID, goal); err != nil {
		return err
	}
	c.goalRuns.Set(sessionID, &goalRun{goal: goal, budget: goalDefaultBudget})
	return nil
}

// ClearGoal ends the run and forgets the goal. Safe to call when none is
// set, which is what the stop path and the UI's "clear" both do.
func (c *coordinator) ClearGoal(ctx context.Context, sessionID string) error {
	if run, ok := c.goalRunFor(sessionID); ok {
		run.mu.Lock()
		run.stopped = true
		run.mu.Unlock()
		c.goalRuns.Del(sessionID)
	}
	return c.sessions.SetGoal(ctx, sessionID, "")
}

// GoalStatus reports the active goal and how far through its budget the
// run is, for the UI to show. ok is false when no run is active.
func (c *coordinator) GoalStatus(sessionID string) (goal string, used, budget int, ok bool) {
	run, found := c.resumeGoalRun(context.Background(), sessionID)
	if !found {
		return "", 0, 0, false
	}
	goal, budget, used, _, done, stopped := run.snapshot()
	if done || stopped {
		return "", 0, 0, false
	}
	return goal, used, budget, true
}

// stopGoal ends the run, clears the stored goal so a later turn does not
// find it in the system prompt, and tells the user why it stopped.
func (c *coordinator) stopGoal(ctx context.Context, sessionID, reason string) {
	if err := c.ClearGoal(ctx, sessionID); err != nil {
		slog.Warn("Failed to clear the goal after the run ended", "session_id", sessionID, "error", err)
	}
	slog.Info("Goal run ended", "session_id", sessionID, "reason", reason)
	if c.notify != nil {
		c.notify.Publish(pubsub.CreatedEvent, notify.Notification{
			SessionID: sessionID,
			Message:   reason,
		})
	}
}

// resumeGoalRun returns the session's run, rebuilding it from the stored
// goal when there is none in memory.
//
// The goal itself lives in the database and the run tracking it does
// not, so restarting Atlas left a session whose goal was still on
// record, still in the system prompt, and still reported by /goal --
// with nothing left to drive it. Everything said a run was under way
// and no turn ever came. Reading the goal back here means the run picks
// up where it was rather than quietly not existing.
//
// What does not survive is the turn budget: a resumed run starts its
// count again. Persisting that too would be more faithful, but a
// restart is also the moment a user is most likely to have changed
// their mind, and beginning again is the harmless direction to be wrong
// in -- the ceiling and the cost cap still apply.
func (c *coordinator) resumeGoalRun(ctx context.Context, sessionID string) (*goalRun, bool) {
	if run, ok := c.goalRunFor(sessionID); ok {
		return run, true
	}
	if c.goalRuns == nil || c.sessions == nil {
		return nil, false
	}

	sess, err := c.sessions.Get(ctx, sessionID)
	if err != nil {
		return nil, false
	}
	goal := strings.TrimSpace(sess.Goal)
	if goal == "" {
		return nil, false
	}

	slog.Info("Resuming a goal run from the stored goal", "session_id", sessionID)
	run := &goalRun{goal: goal, budget: goalDefaultBudget}
	c.goalRuns.Set(sessionID, run)
	return run, true
}

// advanceGoal decides, at the end of a turn, whether the session takes
// another one. It is the whole loop.
//
// It returns immediately when there is nothing to drive, and otherwise
// starts the next turn in the background: the turn that just finished
// has to be allowed to complete and render before the next one begins,
// and the caller is the one completing it.
func (c *coordinator) advanceGoal(ctx context.Context, sessionID string, runErr error) {
	run, ok := c.resumeGoalRun(ctx, sessionID)
	if !ok {
		return
	}

	// A failed turn stops the run rather than being retried into the
	// same wall repeatedly. Cancellation is the user saying stop, and
	// the budget error is the cost cap doing its job; neither is worth
	// a notification saying something went wrong.
	if runErr != nil {
		switch {
		case errors.Is(runErr, context.Canceled):
			c.stopGoal(ctx, sessionID, "Goal run stopped.")
		case errors.Is(runErr, ErrSessionBudgetExceeded):
			c.stopGoal(ctx, sessionID, "Goal run stopped: this session reached its cost limit.")
		default:
			c.stopGoal(ctx, sessionID, "Goal run stopped after an error: "+runErr.Error())
		}
		return
	}

	run.mu.Lock()
	if run.stopped || run.done {
		run.mu.Unlock()
		return
	}
	run.used++
	goal, budget, used, claimed := run.goal, run.budget, run.used, run.claimed
	run.mu.Unlock()

	if claimed {
		// The agent says it is finished. Check the claim before taking
		// its word for it, and if the check disagrees, hand back what
		// is missing rather than a bare "no".
		verdict, reason := c.judgeGoal(ctx, sessionID, goal)
		if verdict {
			c.stopGoal(ctx, sessionID, "Goal reached: "+goal)
			return
		}
		run.mu.Lock()
		run.claimed = false
		run.mu.Unlock()
		c.nextGoalTurn(ctx, sessionID,
			"You called goal(action:\"done\"), but the goal is not met yet.\n\n"+
				reason+"\n\nKeep working on it.")
		return
	}

	if used >= budget {
		c.stopGoal(ctx, sessionID, fmt.Sprintf(
			"Goal run stopped after %d turns without reaching: %s", used, goal))
		return
	}

	c.nextGoalTurn(ctx, sessionID, fmt.Sprintf(
		"Continue working towards the goal. Turn %d of %d.\n\n"+
			"Take the next concrete step. Do not summarize what you have already "+
			"done or ask whether to proceed. When the goal is genuinely met, call "+
			"goal(action:\"done\").", used+1, budget))
}

// nextGoalTurn takes the following turn. The context is detached: the
// one belonging to the finished turn is about to be cancelled by its own
// caller, and this turn is not part of it.
func (c *coordinator) nextGoalTurn(ctx context.Context, sessionID, prompt string) {
	go func() {
		if _, err := c.Run(context.WithoutCancel(ctx), sessionID, prompt); err != nil {
			slog.Warn("Goal turn failed", "session_id", sessionID, "error", err)
		}
	}()
}

// judgeGoal asks a second model whether the goal is actually met, and
// returns its verdict along with what it says is still missing.
//
// A model asked to reach a goal and to say when it has reached one is
// being asked to mark its own work, and it marks generously. The check
// is a different model looking only at the goal and the transcript, with
// no stake in being finished.
//
// A judge that cannot run is not allowed to hold the session hostage:
// with no model configured, or on an error, the agent's own claim
// stands.
func (c *coordinator) judgeGoal(ctx context.Context, sessionID, goal string) (bool, string) {
	goalJudge := c.goalJudge.Get().v
	if goalJudge == nil {
		return true, ""
	}

	agent := fantasy.NewAgent(
		goalJudge.Model,
		fantasy.WithSystemPrompt(goalJudgePrompt),
		fantasy.WithUserAgent(userAgent),
	)

	transcript, err := c.recentTranscript(ctx, sessionID)
	if err != nil {
		slog.Warn("Could not read the session to check the goal; accepting the agent's claim",
			"session_id", sessionID, "error", err)
		return true, ""
	}

	result, err := agent.Stream(ctx, fantasy.AgentStreamCall{
		Prompt: "GOAL:\n" + goal + "\n\nWHAT THE AGENT DID:\n" + transcript,
	})
	if err != nil || result == nil {
		slog.Warn("Goal check failed; accepting the agent's claim", "session_id", sessionID, "error", err)
		return true, ""
	}

	reply := strings.TrimSpace(result.Response.Content.Text())
	if strings.HasPrefix(strings.ToUpper(reply), "MET") {
		return true, ""
	}
	reason := strings.TrimSpace(strings.TrimPrefix(reply, "NOT MET"))
	reason = strings.TrimSpace(strings.TrimPrefix(reason, ":"))
	if reason == "" {
		reason = "The check did not say what is missing."
	}
	return false, reason
}

const goalJudgePrompt = `You check whether a goal has actually been reached. You are not doing the work and you are not helping with it; another agent has claimed it is finished and your job is to say whether that is true.

Answer in one of two forms and nothing else:

MET
NOT MET: <what is still missing, in one or two sentences>

Judge only against the goal as written. Work that is close, partly done, or done for some cases but not others is NOT MET. Say what remains, specifically enough to act on. Do not praise, do not summarize what was done, do not suggest improvements beyond the goal.`

// goalToolDescription is deliberately blunt about the one thing the
// agent gets wrong on its own: declaring victory to end the turn.
const goalToolDescription = `Plan and end a goal run -- the mode where you keep taking turns on your own until the session's goal is reached.

- action "budget": say how many turns you expect the goal to take, in turns. Call this once, early, after you understand the work. Sizing it honestly is what stops the run being cut off mid-way or running long after it should have stopped.
- action "done": the goal is reached. Another model checks this against the goal before the run ends, so claiming it early does not finish the run; it costs a turn and hands you back what is still missing.

Do not call "done" because a turn went well, because you have made good progress, or because you are unsure what to do next. Call it when the thing the goal asks for is true.`

// GoalParams is the goal tool's input.
type GoalParams struct {
	Action string `json:"action"`
	Turns  int    `json:"turns,omitempty"`
}

// goalTool lets the agent size its own run and end it.
func (c *coordinator) goalTool() fantasy.AgentTool {
	return fantasy.NewAgentTool(
		GoalToolName,
		goalToolDescription,
		func(ctx context.Context, params GoalParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			sessionID := tools.GetSessionFromContext(ctx)
			if sessionID == "" {
				return fantasy.ToolResponse{}, errors.New("session id missing from context")
			}
			run, ok := c.goalRunFor(sessionID)
			if !ok {
				return fantasy.NewTextErrorResponse("no goal is set for this session"), nil
			}

			switch strings.ToLower(strings.TrimSpace(params.Action)) {
			case "budget":
				if params.Turns <= 0 {
					return fantasy.NewTextErrorResponse("turns must be a positive number"), nil
				}
				turns := min(params.Turns, goalHardCeiling)
				run.mu.Lock()
				run.budget = turns
				used := run.used
				run.mu.Unlock()
				if turns < params.Turns {
					return fantasy.NewTextResponse(fmt.Sprintf(
						"Budget set to %d turns (%d used). Asked for %d, capped at the ceiling.",
						turns, used, params.Turns)), nil
				}
				return fantasy.NewTextResponse(fmt.Sprintf(
					"Budget set to %d turns (%d used).", turns, used)), nil

			case "done":
				run.mu.Lock()
				run.claimed = true
				run.mu.Unlock()
				return fantasy.NewTextResponse(
					"Noted. The claim is checked against the goal when this turn ends; " +
						"if anything is missing you will be told what."), nil

			default:
				return fantasy.NewTextErrorResponse(`action must be "budget" or "done"`), nil
			}
		},
	)
}

// goalTranscriptTurns is how many of the most recent messages the check
// reads. Enough to see what the last stretch of work actually produced,
// short enough that checking a goal costs a fraction of pursuing it.
const goalTranscriptTurns = 12

// goalTranscriptBudget caps the transcript handed to the check, so one
// enormous tool result cannot turn a cheap verification into an
// expensive one.
const goalTranscriptBudget = 12000

// recentTranscript renders the tail of a session for the goal check:
// who said what, most recent last, trimmed to a budget.
func (c *coordinator) recentTranscript(ctx context.Context, sessionID string) (string, error) {
	msgs, err := c.messages.List(ctx, sessionID)
	if err != nil {
		return "", err
	}
	if len(msgs) > goalTranscriptTurns {
		msgs = msgs[len(msgs)-goalTranscriptTurns:]
	}

	var b strings.Builder
	for _, m := range msgs {
		text := strings.TrimSpace(m.Content().Text)
		if text == "" {
			continue
		}
		b.WriteString(strings.ToUpper(string(m.Role)))
		b.WriteString(":\n")
		b.WriteString(text)
		b.WriteString("\n\n")
	}

	out := b.String()
	if len(out) > goalTranscriptBudget {
		// Keep the end: what happened most recently is what decides
		// whether the goal is met now.
		out = "[…earlier turns omitted…]\n\n" + out[len(out)-goalTranscriptBudget:]
	}
	return out, nil
}
