package agent

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agent/notify"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/pubsub"
)

// advisorTimeout bounds how long one advisor pass may run. It reviews a
// turn that has already finished, so nothing is waiting on it but the next
// prompt's note; a hung advisor model must not accumulate goroutines
// forever across a long session.
const advisorTimeout = 45 * time.Second

// advisorMaxReviewChars truncates the prompt and response handed to the
// advisor. Reviewing is meant to be a quick second look, not a full replay
// of the turn's context -- and an unbounded transcript would make the
// advisor's own cost comparable to the turn it is reviewing.
const advisorMaxReviewChars = 4000

// escalateTimeout bounds one escalation pass. Longer than
// advisorTimeout: unlike a quick flag-or-not review, an escalation pass
// is explicitly asked to dig in (and, with tools, may inspect the code),
// so it reasonably needs more room -- but it must still not accumulate
// goroutines forever across a long session.
const escalateTimeout = 90 * time.Second

const escalateSystemPrompt = `You are a second reviewer, escalated in because a first pass already flagged this turn as ` +
	`worth a closer look. You are given the user's request, the agent's final response, and the first reviewer's note, ` +
	`and you have read-only tools (glob, grep, ls, view) to inspect the resulting code yourself.

Reply with a short diagnosis: confirm or correct the first reviewer's concern against the actual code, and if you can, ` +
	`describe a concrete fix -- specific enough that another agent could act on it next turn. A few sentences is enough; ` +
	`this is a note for the next turn to read, not a report.`

const advisorSystemPrompt = `You are a second reviewer watching another AI agent's coding work. You are given the ` +
	`user's request and the agent's final response for one completed turn, and you have read-only tools ` +
	`(glob, grep, ls, view) to inspect the resulting code yourself.

Reply with exactly one line, starting with one of these prefixes:
NONE: -- nothing worth flagging.
NIT: <note> -- a minor observation, not worth interrupting anyone over.
CONCERN: <note> -- something worth a human's attention soon.
BLOCKER: <note> -- something that should be addressed before the work continues.

Keep <note> to one or two sentences. Do not use tools unless the response actually claims something ` +
	`about the code that is worth checking against it.`

// advisorSeverityRank orders severities for the notify-threshold
// comparison. NONE is deliberately absent: it never reaches this check,
// since runAdvisorPass returns before queuing or notifying on NONE.
var advisorSeverityRank = map[string]int{"NIT": 1, "CONCERN": 2, "BLOCKER": 3}

// advisorShouldNotify reports whether severity meets or exceeds threshold
// (an unrecognized or empty threshold falls back to CONCERN, the
// advisor's original, pre-configurable behavior).
func advisorShouldNotify(severity, threshold string) bool {
	if _, ok := advisorSeverityRank[threshold]; !ok {
		threshold = "CONCERN"
	}
	return advisorSeverityRank[severity] >= advisorSeverityRank[threshold]
}

// shouldRunAdvisorPass reports whether the turn that just finished for
// sessionID falls on the configured review cadence (config.Advisor's
// EveryNTurns), advancing that session's turn counter as a side effect.
// Every call counts a turn even when it declines to review, so "every 3rd
// turn" means turns 3, 6, 9, ... rather than resetting whenever a review
// is skipped.
func (a *sessionAgent) shouldRunAdvisorPass(sessionID string) bool {
	interval := a.advisorEveryNTurns.Get()
	if interval <= 0 {
		interval = 1
	}

	count, _ := a.advisorTurnCounts.Get(sessionID)
	count++
	a.advisorTurnCounts.Set(sessionID, count)

	return count%interval == 0
}

// injectAdvisorNote pops any note the advisor left for sessionID (from the
// turn that just finished) and prepends it to prompt. Read-and-clear: a
// note is delivered to the very next prompt only, not repeated.
func (a *sessionAgent) injectAdvisorNote(sessionID, prompt string) string {
	note, ok := a.advisorNotes.Take(sessionID)
	if !ok || note == "" {
		return prompt
	}
	if prompt == "" {
		return note
	}
	return note + "\n\n" + prompt
}

// runAdvisorPass reviews one finished turn and, if the advisor flags
// anything above NONE, queues a note for the session's next prompt (see
// injectAdvisorNote) and, for CONCERN/BLOCKER, publishes a
// notify.TypeAdvisorNote notification so it surfaces before then too.
//
// Errors are logged and otherwise swallowed: this runs in the background
// after the turn it reviews has already completed, so there is no request
// left for an advisor failure to fail.
func (a *sessionAgent) runAdvisorPass(ctx context.Context, sessionID, userPrompt, assistantText string) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("Advisor pass panicked", "session_id", sessionID, "panic", r)
		}
	}()

	ctx, cancel := context.WithTimeout(ctx, advisorTimeout)
	defer cancel()

	advisorModel := a.advisorModel.Get().v
	agent := fantasy.NewAgent(
		advisorModel.Model,
		fantasy.WithSystemPrompt(advisorSystemPrompt),
		fantasy.WithTools(a.advisorTools.Copy()...),
		fantasy.WithUserAgent(userAgent),
	)

	review := "User asked:\n" + truncateForAdvisor(userPrompt) +
		"\n\nAgent responded:\n" + truncateForAdvisor(assistantText)

	result, err := agent.Stream(ctx, fantasy.AgentStreamCall{Prompt: review})
	if err != nil {
		slog.Warn("Advisor pass failed", "session_id", sessionID, "error", err)
		return
	}
	if result == nil {
		return
	}

	severity, note := parseAdvisorReply(result.Response.Content.Text())
	if severity == "" || severity == "NONE" {
		return
	}

	label := "Advisor"
	if a.escalateModel.Get().v != nil && shouldEscalate(severity, a.escalateThreshold.Get()) {
		if escalated, ok := a.runEscalationPass(ctx, sessionID, userPrompt, assistantText, severity, note); ok {
			note = escalated
			label = "Escalated review"
		}
	}

	a.advisorNotes.Set(sessionID, label+" ("+strings.ToLower(severity)+"): "+note)

	if !advisorShouldNotify(severity, a.advisorNotifyThreshold.Get()) {
		// Below the configured floor: queued for the next prompt above,
		// but not worth interrupting the session over.
		slog.Info("Advisor left a note", "session_id", sessionID, "severity", severity, "note", note)
		return
	}

	slog.Warn("Advisor flagged the last turn", "session_id", sessionID, "severity", severity, "note", note)
	if a.notify != nil {
		a.notify.Publish(pubsub.CreatedEvent, notify.Notification{
			SessionID: sessionID,
			Type:      notify.TypeAdvisorNote,
			Message:   note,
		})
	}
}

// shouldEscalate reports whether severity meets threshold for
// Advisor.AutoEscalate (an unrecognized or empty threshold defaults to
// BLOCKER, escalation's own original, most conservative floor -- unlike
// advisorShouldNotify's CONCERN default, since escalation costs a
// second model call).
func shouldEscalate(severity, threshold string) bool {
	if _, ok := advisorSeverityRank[threshold]; !ok {
		threshold = "BLOCKER"
	}
	return advisorSeverityRank[severity] >= advisorSeverityRank[threshold]
}

// runEscalationPass asks a's escalate model to take a closer look at a
// turn the advisor already flagged, and returns its diagnosis (ok=true)
// in place of the advisor's own one-liner. A failure or empty reply
// returns ok=false, leaving the caller to keep the advisor's original
// note rather than lose it -- an escalation is meant to add depth, not
// risk replacing a real note with nothing.
//
// Like runAdvisorPass, this runs after the reviewed turn has already
// finished and errors are logged and swallowed: there is no live request
// for a failed escalation to fail.
func (a *sessionAgent) runEscalationPass(ctx context.Context, sessionID, userPrompt, assistantText, severity, advisorNote string) (note string, ok bool) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("Escalation pass panicked", "session_id", sessionID, "panic", r)
		}
	}()

	ctx, cancel := context.WithTimeout(ctx, escalateTimeout)
	defer cancel()

	escalateModel := a.escalateModel.Get().v
	agent := fantasy.NewAgent(
		escalateModel.Model,
		fantasy.WithSystemPrompt(escalateSystemPrompt),
		fantasy.WithTools(a.escalateTools.Copy()...),
		fantasy.WithUserAgent(userAgent),
	)

	review := "User asked:\n" + truncateForAdvisor(userPrompt) +
		"\n\nAgent responded:\n" + truncateForAdvisor(assistantText) +
		"\n\nFirst reviewer (" + strings.ToLower(severity) + "): " + advisorNote

	result, err := agent.Stream(ctx, fantasy.AgentStreamCall{Prompt: review})
	if err != nil {
		slog.Warn("Escalation pass failed", "session_id", sessionID, "error", err)
		return "", false
	}
	if result == nil {
		return "", false
	}

	escalated := strings.TrimSpace(result.Response.Content.Text())
	if escalated == "" {
		return "", false
	}
	return escalated, true
}

// parseAdvisorReply extracts the severity prefix and note from the
// advisor's one-line reply. An unrecognized or empty reply is treated as
// NONE rather than guessed at: a reviewer that did not follow the format
// is not one to trust with a stronger signal.
func parseAdvisorReply(reply string) (severity, note string) {
	reply = strings.TrimSpace(reply)
	for _, prefix := range []string{"NONE:", "NIT:", "CONCERN:", "BLOCKER:"} {
		if rest, ok := strings.CutPrefix(reply, prefix); ok {
			return strings.TrimSuffix(prefix, ":"), strings.TrimSpace(rest)
		}
	}
	return "", ""
}

func truncateForAdvisor(s string) string {
	s = strings.TrimSpace(s)
	if len(s) <= advisorMaxReviewChars {
		return s
	}
	return s[:advisorMaxReviewChars] + "… (truncated)"
}
