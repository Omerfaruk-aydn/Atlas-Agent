package tools

import (
	"context"
	"errors"

	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/vault"
)

type secretFilteredTool struct{ fantasy.AgentTool }

func WithSecretFilter(tool fantasy.AgentTool) fantasy.AgentTool { return &secretFilteredTool{tool} }
func (t *secretFilteredTool) Run(ctx context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
	r, err := t.AgentTool.Run(ctx, call)
	id := GetSessionFromContext(ctx)
	r.Content = vault.Redact(id, r.Content)
	r.Metadata = vault.Redact(id, r.Metadata)
	if err != nil {
		err = errors.New(vault.Redact(id, err.Error()))
	}
	if vault.Sensitive("") && len(r.Data) > 0 {
		stop := r.StopTurn
		r = fantasy.NewTextErrorResponse("Media observation is disabled in a credential-bearing session to prevent visual secret exposure. Use redacted text observations.")
		r.StopTurn = stop
	}
	return r, err
}
