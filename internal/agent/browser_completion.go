package agent

import (
	"context"
	"encoding/json"
	"strings"
	"unicode"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agent/tools"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/interaction"
)

// summaryResumePrefix starts the prompt Run re-queues after auto-compaction.
// The original prompt follows in backticks.
const summaryResumePrefix = "The previous session was interrupted because it got too long, the initial user request was: `"

// summaryResumePrompt wraps a prompt for the turn after auto-compaction. A
// completion-check prompt keeps its marker in front, so the resumed turn is
// still recognized as continuing the contract.
func summaryResumePrompt(prompt string) string {
	wrapped := summaryResumePrefix + prompt + "`"
	if strings.HasPrefix(prompt, tools.BrowserContinuationMarker) {
		return tools.BrowserContinuationMarker + " " + wrapped
	}
	return wrapped
}

// summaryResumeAfterTurn preserves participation in the browser contract
// across compaction. Later steps may only edit or observe and need not plan
// the same task again; unrelated prompts do not acquire this marker.
func summaryResumeAfterTurn(prompt string, result *fantasy.AgentResult) string {
	if result != nil && turnWorkedOnBrowserTask(result.Steps) && !strings.HasPrefix(prompt, tools.BrowserContinuationMarker) {
		prompt = tools.BrowserContinuationMarker + " " + prompt
	}
	return summaryResumePrompt(prompt)
}

// unwrapSummaryResume returns the original prompt of a turn re-queued after
// auto-compaction, or the prompt itself.
func unwrapSummaryResume(prompt string) string {
	inner := strings.TrimPrefix(prompt, tools.BrowserContinuationMarker+" ")
	if rest, ok := strings.CutPrefix(inner, summaryResumePrefix); ok {
		return strings.TrimSuffix(rest, "`")
	}
	return prompt
}

// continueDirectives are short replies that only tell the agent to carry
// on with work it was already given. They carry no new direction, so the
// active contract stays the task.
var continueDirectives = []string{
	"devam", "devam et", "devam edin", "devam ediniz", "devam eder misin", "devam etsene", "devam et lütfen", "lütfen devam et",
	"evet devam", "evet devam et", "tamam devam", "tamam devam et", "kaldığın yerden devam et", "kaldığın yerden devam",
	"sürdür", "bitir", "tamamla", "işe devam", "devam edelim", "hadi devam",
	"continue", "please continue", "continue please", "go on", "keep going", "carry on", "resume", "proceed", "go ahead",
	"yes continue", "ok continue", "continue from where you left off",
}

// IsContinueDirective reports whether a user prompt only asks to continue
// already authorized work.
func IsContinueDirective(prompt string) bool {
	normalized := strings.Join(strings.FieldsFunc(strings.ToLower(prompt), func(r rune) bool {
		return unicode.IsSpace(r) || (unicode.IsPunct(r) && r != '\'' && r != '’')
	}), " ")
	if normalized == "" || len([]rune(normalized)) > 60 {
		return false
	}
	for _, directive := range continueDirectives {
		if normalized == directive {
			return true
		}
	}
	return false
}

// browserCompletionPrompt decides whether a turn that just ended left the
// active browser task with open, workable requirements. It only continues a
// turn that ended on its own: never after cancellation, a handoff or
// permission stop, the step limit or loop detection, never for a sub-agent,
// and never ahead of a prompt the user already queued. A turn the user
// started continues when it worked on the contract itself (task_plan or
// task_update) or when the user only said to continue ("devam et",
// "continue"), including after auto-compaction re-queued that prompt. A new
// user direction that did not touch the old task is the user's choice, not
// an unfinished task. The task bounds how many continuations may happen
// without progress, so this cannot loop. Every reason for not continuing
// while work is open is recorded on the task, so cancellation, waiting for
// the user and missing progress stay distinguishable.
func (a *sessionAgent) browserCompletionPrompt(ctx context.Context, sessionID, prompt string, result *fantasy.AgentResult, queued int) tools.BrowserContinuation {
	if a.isSubAgent || a.workingDir == "" || result == nil {
		return tools.BrowserContinuation{}
	}
	record := func(reason, detail string) tools.BrowserContinuation {
		tools.RecordBrowserTaskStop(a.workingDir, sessionID, reason, detail)
		return tools.BrowserContinuation{StopReason: reason}
	}
	if ctx.Err() != nil {
		return record(tools.BrowserStopUserCanceled, "")
	}
	if queued > 0 {
		return tools.BrowserContinuation{}
	}
	steps := result.Steps
	if len(steps) == 0 {
		return tools.BrowserContinuation{}
	}
	if maxStepsReached(steps, a.maxStepsPerTurn.Get()) {
		return record(tools.BrowserStopStepLimit, "the turn reached its step limit")
	}
	if hasRepeatedToolCalls(steps, loopDetectionWindowSize, loopDetectionMaxRepeats) {
		return record(tools.BrowserStopRepeatedCalls, "the same tool call repeated; change strategy")
	}
	for _, tr := range steps[len(steps)-1].Content.ToolResults() {
		if tr.StopTurn {
			return record(tools.BrowserStopWaitingForUser, "a tool stopped the turn for the user (handoff, permission or question)")
		}
	}
	original := unwrapSummaryResume(prompt)
	continuing := strings.HasPrefix(prompt, tools.BrowserContinuationMarker) || strings.HasPrefix(original, tools.BrowserContinuationMarker) || IsContinueDirective(original)
	if !continuing && !turnWorkedOnBrowserTask(steps) {
		return tools.BrowserContinuation{}
	}
	controlID := engineering.GetScope(ctx, sessionID).SessionID
	if state := interaction.Default.Snapshot(controlID); state.Paused || state.Canceled {
		if state.Canceled {
			return record(tools.BrowserStopUserCanceled, "")
		}
		return record(tools.BrowserStopWaitingForUser, "control is paused for the user")
	}
	return tools.BrowserTaskContinue(a.workingDir, sessionID)
}

// turnWorkedOnBrowserTask reports whether the turn planned or updated the
// browser completion contract.
func turnWorkedOnBrowserTask(steps []fantasy.StepResult) bool {
	for _, step := range steps {
		for _, call := range step.Content.ToolCalls() {
			if call.ToolName != tools.BrowserToolName {
				continue
			}
			var input struct {
				Action string `json:"action"`
			}
			if json.Unmarshal([]byte(call.Input), &input) == nil && (input.Action == "task_plan" || input.Action == "task_update") {
				return true
			}
		}
	}
	return false
}
