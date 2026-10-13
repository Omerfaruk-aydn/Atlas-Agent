package tools

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/activity"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/permission"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/question"
)

const QuestionToolName = "question"

//go:embed question.md
var questionDescription string

// QuestionParams defines the parameters for the question tool.
type QuestionParams struct {
	Questions          []QuestionItem `json:"questions" description:"List of questions to present. Single item = no tabs, multiple = tabbed form."`
	ConfirmTitle       string         `json:"confirm_title,omitempty" description:"Title for the confirmation tab. Required for multi-question batches."`
	ConfirmDescription string         `json:"confirm_description,omitempty" description:"Description for the confirmation tab. Required for multi-question batches."`
}

// UnmarshalJSON handles models that double-serialize the questions field as a
// JSON string instead of a native array.
func (p *QuestionParams) UnmarshalJSON(data []byte) error {
	type Alias QuestionParams
	aux := &struct {
		Questions json.RawMessage `json:"questions"`
		*Alias
	}{
		Alias: (*Alias)(p),
	}
	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}
	if len(aux.Questions) == 0 {
		return nil
	}
	// Try array first.
	if err := json.Unmarshal(aux.Questions, &p.Questions); err != nil {
		// Fall back to string-encoded JSON array.
		var s string
		if err2 := json.Unmarshal(aux.Questions, &s); err2 != nil {
			return err
		}
		if err2 := json.Unmarshal([]byte(strings.TrimSpace(s)), &p.Questions); err2 != nil {
			return fmt.Errorf("questions must be an array: %w", err2)
		}
	}
	return nil
}

// QuestionItem is a single question from the tool input.
type QuestionItem struct {
	Label       string           `json:"label,omitempty" description:"Short tab header label (3 words max)."`
	Purpose     string           `json:"purpose,omitempty" description:"Question intent: information for a missing fact or user preference, confirmation for approval to act or continue. Required in YOLO; independent of question type. Never label an approval request as information."`
	Type        string           `json:"type" description:"The type of question: yes_no, single_choice, multi_choice, or free_text"`
	Question    string           `json:"question" description:"The question text"`
	Description string           `json:"description" description:"Required markdown description shown below the question"`
	Choices     []QuestionChoice `json:"choices,omitempty" description:"List of choices"`
	Options     []QuestionChoice `json:"options,omitempty"` // alias for Choices
}

// GetChoices returns choices, preferring the Choices field over Options.
func (q QuestionItem) GetChoices() []QuestionChoice {
	if len(q.Choices) > 0 {
		return q.Choices
	}
	return q.Options
}

// QuestionChoice represents a selectable option.
type QuestionChoice struct {
	ID          string `json:"id" description:"Unique identifier for this choice"`
	Label       string `json:"label" description:"Display text for this choice"`
	Description string `json:"description,omitempty" description:"Optional description for this choice"`
}

// permissionModer reports the live permission mode; YOLO is ModeBypass.
type permissionModer interface {
	Mode() permission.PermissionMode
}

// yoloConfirmationRefusal prevents approval prompts regardless of format.
const yoloConfirmationRefusal = "yolo_confirmation_not_needed: YOLO mode is on, so the user has already approved tool use and continuing the requested work. No question was shown. Do not ask for permission or confirmation; continue the task. If you need information only the user has, ask with purpose=information and name the missing fact or preference. Any question type, including yes_no, may gather information; changing the type of an approval request does not make it an information request."

// NewQuestionTool creates a new question tool. YOLO requires an explicit
// purpose and refuses confirmation requests regardless of their format.
func NewQuestionTool(svc question.Service, modes ...permissionModer) fantasy.AgentTool {
	return fantasy.NewAgentTool(
		QuestionToolName,
		questionDescription,
		func(ctx context.Context, params QuestionParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			sessionID := GetSessionFromContext(ctx)

			if len(params.Questions) == 0 {
				return fantasy.NewTextErrorResponse("at least one question is required"), nil
			}
			yolo := len(modes) > 0 && modes[0] != nil && modes[0].Mode() == permission.ModeBypass
			for i, item := range params.Questions {
				switch item.Purpose {
				case "confirmation":
					if yolo {
						return fantasy.NewTextErrorResponse(yoloConfirmationRefusal), nil
					}
				case "information":
				case "":
					if yolo {
						return fantasy.NewTextErrorResponse("question_purpose_required: No question was shown. In YOLO, each question must declare purpose=information or purpose=confirmation. Continue pre-approved work without asking for confirmation. Only ask with purpose=information when a fact or preference is missing; yes_no is allowed for factual questions."), nil
					}
				default:
					return fantasy.NewTextErrorResponse(fmt.Sprintf("question %d: invalid purpose (must be information or confirmation)", i+1)), nil
				}
			}
			if len(params.Questions) > question.MaxQuestions {
				return fantasy.NewTextErrorResponse(fmt.Sprintf("exceeds maximum of %d questions per batch (got %d). Split into multiple batches and tell the user there will be follow-up questions", question.MaxQuestions, len(params.Questions))), nil
			}

			questions := make([]question.Question, len(params.Questions))
			for i, item := range params.Questions {
				qType := question.Type(item.Type)
				if qType != question.TypeYesNo && qType != question.TypeSingleChoice && qType != question.TypeMultiChoice && qType != question.TypeFreeText {
					label := item.Label
					if label == "" {
						label = item.Question
					}
					return fantasy.NewTextErrorResponse(fmt.Sprintf("question %d [%s]: invalid type %q (must be yes_no, single_choice, multi_choice, or free_text)", i+1, label, item.Type)), nil
				}
				if qType == question.TypeFreeText && asksForSecret(item.Label+" "+item.Question+" "+item.Description) {
					return fantasy.NewTextErrorResponse("secret_question_refused: No question was shown. Passwords and one-time codes are never collected through chat or questions, because answers are stored in the conversation and sent to the model. For a sign-in password call browser credential_request on the password field; the user types it on their paired phone and you receive only a handle."), nil
				}
				questions[i] = question.Question{
					Type:        qType,
					Label:       item.Label,
					Text:        item.Question,
					Description: item.Description,
					Choices:     convertChoices(item.GetChoices()),
				}
			}

			req := question.Request{
				SessionID:          sessionID,
				ToolCallID:         call.ID,
				Questions:          questions,
				ConfirmTitle:       params.ConfirmTitle,
				ConfirmDescription: params.ConfirmDescription,
			}

			req, err := question.Prepare(req)
			if err != nil {
				return fantasy.NewTextErrorResponse(err.Error()), nil
			}
			// The run keeps its control surfaces while it waits. The island
			// is another view of this same pending request and answers it
			// by ID through the service, exactly like the terminal form.
			var pending *activity.PendingPrompt
			askCtx := question.WithPendingHook(ctx, func(registered question.Request) {
				pending = activity.AwaitPrompt(ctx, questionPrompt(registered), func(r activity.PromptResponse) bool {
					return svc.AnswerRequest(registered.ID, questionAnswers(r))
				})
			})
			answers, err := svc.Ask(askCtx, req)
			outcome := activity.OutcomeAnswered
			if err != nil {
				outcome = activity.OutcomeCancelled
			}
			pending.Done(outcome)
			if err != nil {
				if errors.Is(err, question.ErrCancelled) {
					resp := fantasy.NewTextErrorResponse("User cancelled this question")
					resp.StopTurn = true
					return resp, nil
				}
				return fantasy.NewTextErrorResponse(err.Error()), nil
			}

			return formatAnswers(answers, questions)
		},
	)
}

// questionPrompt is the island's read-only view of a registered request.
func questionPrompt(req question.Request) activity.Prompt {
	p := activity.Prompt{Kind: activity.KindQuestion, ID: req.ID, ConfirmTitle: req.ConfirmTitle, ConfirmDescription: req.ConfirmDescription}
	for _, q := range req.Questions {
		item := activity.PromptQuestion{ID: q.ID, Type: string(q.Type), Label: q.Label, Text: q.Text, Description: q.Description}
		for _, c := range q.Choices {
			item.Choices = append(item.Choices, activity.PromptChoice{ID: c.ID, Label: c.Label, Description: c.Description})
		}
		p.Questions = append(p.Questions, item)
	}
	return p
}

// questionAnswers converts a validated island response to service answers.
func questionAnswers(r activity.PromptResponse) []question.Answer {
	answers := make([]question.Answer, len(r.Answers))
	for i, a := range r.Answers {
		answers[i] = question.Answer{QuestionID: a.QuestionID, SelectedIDs: append([]string(nil), a.Selected...), FillInText: a.Text, Yes: a.Yes}
	}
	return answers
}

func convertChoices(in []QuestionChoice) []question.Choice {
	out := make([]question.Choice, len(in))
	for i, c := range in {
		out[i] = question.Choice{ID: c.ID, Label: c.Label, Description: c.Description}
	}
	return out
}

// formatAnswers converts answers into a tool response string for the LLM.
func formatAnswers(answers []question.Answer, questions []question.Question) (fantasy.ToolResponse, error) {
	var b strings.Builder
	for i, answer := range answers {
		if i > 0 {
			b.WriteString("\n\n")
		}
		if i < len(questions) {
			fmt.Fprintf(&b, "Q%d: %s\n", i+1, questions[i].Text)
		}
		formatted, _ := formatAnswer(&answer, question.Type(""))
		b.WriteString(formatted.Content)
	}
	return fantasy.NewTextResponse(b.String()), nil
}

// formatAnswer formats a single answer for the LLM.
func formatAnswer(answer *question.Answer, _ question.Type) (fantasy.ToolResponse, error) {
	var b strings.Builder

	switch {
	case answer.Yes != nil:
		if *answer.Yes {
			b.WriteString("User answered: yes")
		} else {
			b.WriteString("User answered: no")
		}
	case len(answer.SelectedIDs) > 0 || answer.FillInText != "":
		// Selections and free-text can coexist for multi-choice;
		// report both so the model sees the complete answer.
		var parts []string
		if len(answer.SelectedIDs) > 0 {
			data, _ := json.Marshal(answer.SelectedIDs)
			parts = append(parts, fmt.Sprintf("User selected: %s", string(data)))
		}
		if answer.FillInText != "" {
			parts = append(parts, fmt.Sprintf("User provided: %s", answer.FillInText))
		}
		b.WriteString(strings.Join(parts, "\n"))
	default:
		b.WriteString("User skipped this question")
	}

	if len(answer.Notes) > 0 {
		b.WriteString("\n\nNotes:")
		for key, note := range answer.Notes {
			if key == "_question" {
				fmt.Fprintf(&b, "\n- %s", note)
			} else {
				fmt.Fprintf(&b, "\n- [%s]: %s", key, note)
			}
		}
	}

	return fantasy.NewTextResponse(b.String()), nil
}

// secretQuestion matches requests for values that must never be typed
// into a chat answer. Short tokens are matched as whole words.
var secretQuestion = regexp.MustCompile(`(?i)(password|passwort|passcode|şifre|sifre|parola|contraseña|mot de passe|one-time code|doğrulama kodu|\botp\b|\b2fa code\b|\bpin (code|kodu)\b)`)

// asksForSecret reports a question or step that requests a secret value.
func asksForSecret(text string) bool { return secretQuestion.MatchString(text) }
