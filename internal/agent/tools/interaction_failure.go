package tools

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/computer"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/interaction"
)

// interactionAcquireResponse stops the turn on a real user handoff. It
// never grants control or retries an operation on the user's behalf.
func interactionAcquireResponse(err error) fantasy.ToolResponse {
	var paused *interaction.PausedError
	if !errors.As(err, &paused) {
		return fantasy.NewTextErrorResponse(err.Error())
	}
	content := "Automation is waiting for the user. Complete the manual step, then run /resume-use in this conversation to return browser and desktop control and continue. Alternatively, open /interactions and press r, then ask Atlas to continue. Do not retry browser/computer actions or resume yourself."
	if paused.OtherSession {
		content = fmt.Sprintf("Automation is waiting for the user in conversation %s. Switch to that conversation and run /resume-use after completing the manual step. Do not retry actions or resume another conversation yourself.", paused.SessionID)
	}
	response := fantasy.NewTextResponse(content)
	response.StopTurn = true
	return fantasy.WithResponseMetadata(response, struct {
		Status    string `json:"status"`
		SessionID string `json:"paused_session_id"`
		Reason    string `json:"reason"`
	}{"waiting_for_user", paused.SessionID, paused.Reason})
}

func withInteractionFailure(response fantasy.ToolResponse) fantasy.ToolResponse {
	if !response.IsError {
		return response
	}
	code, recovery := interaction.Failure(response.Content)
	return fantasy.WithResponseMetadata(response, struct {
		Code              string `json:"error_code"`
		Recovery          string `json:"recovery"`
		VerifyBeforeRetry bool   `json:"verify_before_retry"`
	}{code, recovery, true})
}

// maxHandoffStep bounds the persisted description of the user's step.
const maxHandoffStep = 240

// handoffStep validates the description of exactly what is left to the
// user. A handoff without one leaves the user guessing and lets an ordinary
// login screen be abandoned without saying why, so it is refused.
func handoffStep(text string) (string, *computer.ContractError) {
	step := strings.Join(strings.Fields(text), " ")
	if step == "" {
		return "", computer.NewContractError("handoff_step_required", "text", "handoff requires text naming the exact step only the user can do (for example: approve the sign-in prompt on your phone). Ordinary login forms, account choosers, consent screens and visible CAPTCHA checkboxes are not handoffs; continue them yourself. Nothing was paused", "Continue the task, or call handoff again with text describing the one remaining manual step. Never include passwords or codes.", `{"action":"handoff","text":"Approve the Google sign-in prompt on your phone"}`)
	}
	if utf8.RuneCountInString(step) > maxHandoffStep {
		step = string([]rune(step)[:maxHandoffStep])
	}
	return step, nil
}

// handoffResponse stops the turn and tells the user precisely what to do.
func handoffResponse(step, where string) fantasy.ToolResponse {
	response := fantasy.NewTextResponse(fmt.Sprintf("Automation paused. Left to the user %s: %s. After that step, the user runs /resume-use in this conversation to return control and continue (or opens /interactions and presses r, then asks Atlas to continue). Until then, do not retry browser/computer actions or resume yourself.", where, step))
	response.StopTurn = true
	return response
}
