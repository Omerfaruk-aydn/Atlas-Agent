	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/message"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/vault"
	"github.com/stretchr/testify/require"
)

// A password the user typed into chat and a tool then refused is removed
// from every later model request and from the stored tool arguments.
func TestRegisteredSecretNeverReachesLaterRequestsOrStoredArgs(t *testing.T) {
	t.Parallel()
	session := "redact-agent-" + t.Name()
	secret := "Synth-Chat-Secret-8842"
	require.NoError(t, vault.Register(session, secret))

	input, _ := json.Marshal(map[string]any{"action": "set_field", "ref": "e4", "value": secret})
	msgs := []fantasy.Message{
		{Role: fantasy.MessageRoleUser, Content: []fantasy.MessagePart{fantasy.TextPart{Text: "my password is " + secret}}},
		{Role: fantasy.MessageRoleAssistant, Content: []fantasy.MessagePart{fantasy.ToolCallPart{ToolCallID: "c1", ToolName: "browser", Input: string(input)}}},
		{Role: fantasy.MessageRoleTool, Content: []fantasy.MessagePart{
			fantasy.ToolResultPart{ToolCallID: "c1", Output: fantasy.ToolResultOutputContentText{Text: "password_field: refused " + secret}},
			fantasy.ToolResultPart{ToolCallID: "c2", Output: fantasy.ToolResultOutputContentError{Error: errors.New("echo " + secret)}},
		}},
	}
	out := redactRegisteredSecrets(session, msgs)
	data, err := json.Marshal(out)
	require.NoError(t, err)
	require.NotContains(t, string(data), secret)
	call := out[1].Content[0].(fantasy.ToolCallPart)
	require.True(t, json.Valid([]byte(call.Input)), "redacted arguments stay valid JSON")
	require.Contains(t, call.Input, "[REDACTED]")

	// Other sessions are untouched when they registered nothing.
	other := []fantasy.Message{{Role: fantasy.MessageRoleUser, Content: []fantasy.MessagePart{fantasy.TextPart{Text: "hello"}}}}
	require.Equal(t, other, redactRegisteredSecrets("clean-"+t.Name(), other))

	stored := &message.Message{ID: "m1", SessionID: session, Role: message.Assistant}
	stored.AddToolCall(message.ToolCall{ID: "c1", Name: "browser", Input: string(input), Finished: true})
	require.True(t, redactStoredToolCall(stored, "c1"))
	raw, err := json.Marshal(stored.Parts)
	require.NoError(t, err)
	require.NotContains(t, string(raw), secret, "the persisted tool call is rewritten")
	require.False(t, redactStoredToolCall(stored, "c1"), "nothing left to change")
}
