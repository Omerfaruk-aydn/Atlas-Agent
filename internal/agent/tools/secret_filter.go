package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/vault"
)

type secretFilteredTool struct{ fantasy.AgentTool }

// cliBridgeOutputContextKey permits only the internal bridge consumer to
// evaluate a nested bash result before model-facing redaction.
type cliBridgeOutputContextKey struct{}

func WithSecretFilter(tool fantasy.AgentTool) fantasy.AgentTool { return &secretFilteredTool{tool} }
func (t *secretFilteredTool) Run(ctx context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
	r, err := t.AgentTool.Run(ctx, call)
	if raw, _ := ctx.Value(cliBridgeOutputContextKey{}).(bool); raw && t.AgentTool.Info().Name == BashToolName {
		return r, err
	}
	id := GetSessionFromContext(ctx)
	if t.AgentTool.Info().Name == CLIBridgeToolName {
		r.Content = redactCLIBridgePayload(id, r.Content)
		r.Metadata = redactCLIBridgePayload(id, r.Metadata)
	} else {
		r.Content = redactToolPayload(id, r.Content)
		r.Metadata = redactToolPayload(id, r.Metadata)
	}
	if err != nil {
		err = errors.New(vault.Redact(id, err.Error()))
	}
	if vault.Sensitive("") && len(r.Data) > 0 {
		stop := r.StopTurn
		r = fantasy.NewTextErrorResponse("Media observation is disabled in a credential-bearing session to prevent visual secret exposure. Use redacted text observations.")
		r.StopTurn = stop
	}
	return r, err
// vision without any external solver API.
func isCaptchaChallengeMedia(r fantasy.ToolResponse) bool {
	return r.Type == "image" && strings.Contains(r.Metadata, `"captcha_challenge":true`)
}

// redactToolPayload preserves JSON booleans, numbers, and structural keys.
// String values remain protected, including nested output and escaped text.
func redactToolPayload(session, payload string) string {
	trimmed := strings.TrimSpace(payload)
	if (!strings.HasPrefix(trimmed, "{") && !strings.HasPrefix(trimmed, "[")) || !json.Valid([]byte(payload)) {
		return vault.Redact(session, payload)
	}
	decoder := json.NewDecoder(strings.NewReader(payload))
	decoder.UseNumber()
	var value any
	if decoder.Decode(&value) != nil {
		return vault.Redact(session, payload)
	}
	value = redactJSONValue(session, value)
	encoded, err := json.Marshal(value)
	if err != nil {
		return vault.Redact(session, payload)
	}
	return string(encoded)
}

func redactJSONValue(session string, value any) any {
	switch value := value.(type) {
	case string:
		return vault.Redact(session, value)
	case map[string]any:
		for key, item := range value {
			value[key] = redactJSONValue(session, item)
		}
		return value
	case []any:
		for i, item := range value {
			value[i] = redactJSONValue(session, item)
		}
		return value
	default:
		return value
	}
}
