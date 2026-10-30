package model

import (
	"context"
	"strings"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/codereview"
	tea "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ui/v2"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/dialog"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/util"
)

// "/review [target] [low|medium|high|max] [--comment]" reviews code
// changes. The target is the uncommitted changes (default), a pull
// request number or URL, a range such as main..HEAD, or a ref.
//
// In the TUI the review runs through the main agent rather than a
// separate pipeline: the command resolves the target here, then sends a
// prompt that tells the agent to run the reviewer and verifier passes with
// its own agent tool. That keeps the review in the session transcript
// where the user can follow up on a finding. The same engine, with the
// same plan per effort level, runs headless as `atlas-agent review`.

const reviewCommand = "/review"

// reviewRequest is a parsed "/review" line.
type reviewRequest struct {
	spec    string
	effort  codereview.Effort
	comment bool
}

// parseReviewCommand reads a "/review ..." line. ok is false when the
// line is not the command; a non-nil err means it is the command with
// arguments that cannot be used.
func parseReviewCommand(content string) (req reviewRequest, ok bool, err error) {
	trimmed := strings.TrimSpace(content)
	if !strings.HasPrefix(trimmed, reviewCommand) {
		return req, false, nil
	}
	rest := trimmed[len(reviewCommand):]
	if rest != "" && !isSpace(rest[0]) {
		return req, false, nil
	}
	req.effort = codereview.EffortMedium
	fields := strings.Fields(rest)
	for i := 0; i < len(fields); i++ {
		switch field := fields[i]; {
		case field == "--comment":
			req.comment = true
		case field == "--effort" && i+1 < len(fields):
			i++
			if req.effort, err = codereview.ParseEffort(fields[i]); err != nil {
				return req, true, err
			}
		case strings.HasPrefix(field, "--effort="):
			if req.effort, err = codereview.ParseEffort(strings.TrimPrefix(field, "--effort=")); err != nil {
				return req, true, err
			}
		case isEffortWord(field):
			req.effort, _ = codereview.ParseEffort(field)
		case strings.HasPrefix(field, "-") && !strings.HasPrefix(field, "--") && len(field) > 1 && field[1] >= '0' && field[1] <= '9':
			// A "-1" style revision is not an option.
			req.spec = field
		case strings.HasPrefix(field, "-"):
			return req, true, errReviewOption(field)
		case req.spec == "":
			req.spec = field
		default:
			return req, true, errReviewOption("extra argument " + field)
		}
	}
	return req, true, nil
}

func isEffortWord(s string) bool {
	switch s {
	case "low", "medium", "high", "max":
		return true
	}
	return false
}

type errReviewOption string

func (e errReviewOption) Error() string {
	return "/review: unsupported option " + string(e) + " (use a target, low|medium|high|max, --comment)"
}

// startReview resolves the target and sends the review prompt to the
// agent. The git and gh lookups run off the UI goroutine.
func (m *UI) startReview(req reviewRequest) tea.Cmd {
	root := m.com.Workspace.WorkingDir()
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		if req.comment && !codereview.IsPRSpec(req.spec) {
			return util.NewErrorMsg(errReviewOption("--comment needs a pull request target"))
		}
		target, err := codereview.NewResolver().Resolve(ctx, root, req.spec)
		if err != nil {
			return util.NewErrorMsg(err)
		}
		if strings.TrimSpace(target.Diff) == "" {
			return util.NewInfoMsg(m.com.Text("Nothing to review: that scope has no changes."))
		}
		return sendMessageMsg{Content: codereview.AgentPrompt(target, req.effort, req.comment)}
	}
}

// interceptReviewCommand handles "/review ..." typed into the editor.
// Reports whether it consumed the prompt.
func (m *UI) interceptReviewCommand(content string) (tea.Cmd, bool) {
	req, ok, err := parseReviewCommand(content)
	if !ok {
		return nil, false
	}
	if err != nil {
		return util.ReportError(err), true
	}
	prevHeight := m.textarea.Height()
	m.textarea.SetValue("")
	heightCmd := m.handleTextareaHeightChange(prevHeight)
	return tea.Batch(heightCmd, m.startReview(req)), true
}

// handleReviewAction runs the default review (uncommitted changes, medium
// effort) when /review is picked from the command list.
func (m *UI) handleReviewAction(dialog.ActionReview) tea.Cmd {
	m.dialog.CloseDialog(dialog.CommandsID)
	return m.startReview(reviewRequest{effort: codereview.EffortMedium})
}
