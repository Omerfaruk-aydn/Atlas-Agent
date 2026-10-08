package agent

import (
	"context"
	"strings"
	"unicode/utf8"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agent/prompt"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agentstate"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/message"
)

// maxIntentAttachmentBytes bounds how much pasted text is read to choose
// task guidance; the request's intent is stated near its start.
const maxIntentAttachmentBytes = 16 * 1024

// promptIntentText is the text task guidance is chosen from: the typed
// prompt plus pasted text attachments. A request pasted as an attachment
// with an empty message otherwise selected no guidance at all.
func promptIntentText(prompt string, attachments []message.Attachment) string {
	var b strings.Builder
	b.WriteString(prompt)
	for _, a := range attachments {
		if !a.IsText() || len(a.Content) == 0 {
			continue
		}
		content := a.Content
		if len(content) > maxIntentAttachmentBytes {
			content = content[:maxIntentAttachmentBytes]
		}
		b.WriteByte('\n')
		b.WriteString(strings.ToValidUTF8(string(content), ""))
	}
	return b.String()
}

type promptRecipe struct {
	Version string   `json:"version"`
	Layers  []string `json:"layers"`
}

type (
	promptSnapshotKey struct{}
	promptSnapshot    struct{ content string }
)

// preparePromptContext preserves task guidance for short continuation turns
// without storing the user's text or duplicating the conversation ledger.
func (c *coordinator) preparePromptContext(ctx context.Context, id, text string) (context.Context, error) {
	role := ""
	if c.cfg != nil {
		if mode, ok := c.sessionMode(); ok {
			role = mode.Name
			ctx = prompt.WithRoleSkills(ctx, mode.PreferredSkills)
		}
	}
	ids := prompt.SelectProtocols(text, role)
	taskIDs := prompt.SelectProtocols(text, "")
	if state := c.stateStore(); state != nil {
		err := agentstate.Update[promptRecipe](ctx, state, "prompt_recipe/"+id, "active", func(saved *promptRecipe) error {
			if len(taskIDs) == 0 && utf8.RuneCountInString(text) <= 160 && saved.Version == prompt.ProtocolVersion {
				ids = append(ids, saved.Layers...)
			}
			ordered, err := prompt.CanonicalProtocols(ids)
			if err != nil {
				return err
			}
			ids = ordered
			*saved = promptRecipe{Version: prompt.ProtocolVersion, Layers: ids}
			return nil
		})
		if err != nil {
			return ctx, err
		}
	}
	ctx = context.WithValue(ctx, promptSnapshotKey{}, &promptSnapshot{})
	return prompt.WithProtocols(ctx, ids), nil
}

func frozenSystemPrompt(ctx context.Context) string {
	if snapshot, ok := ctx.Value(promptSnapshotKey{}).(*promptSnapshot); ok {
		return snapshot.content
	}
	return ""
}
