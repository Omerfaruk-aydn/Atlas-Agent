package agent

import (
	"context"
	"unicode/utf8"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agent/prompt"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agentstate"
)

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
