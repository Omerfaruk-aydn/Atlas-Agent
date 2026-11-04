package tools

import (
	"context"
	_ "embed"
	"encoding/json"
	"path/filepath"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/connections"
	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/mailbox"
)

//go:embed mailbox.md
var mailboxDescription string

const MailboxToolName = "mailbox"

type mailboxParams struct {
	Limit int `json:"limit,omitempty" description:"How many recent messages to read, from 1 to 8"`
}

// NewMailboxTool reads the mailbox saved in the encrypted connection store.
// The secret is never included in the tool result.
func NewMailboxTool(dataDir string) fantasy.AgentTool {
	return fantasy.NewAgentTool(MailboxToolName, mailboxDescription,
		func(ctx context.Context, params mailboxParams, _ fantasy.ToolCall) (fantasy.ToolResponse, error) {
			store := connections.SecretStore{Dir: filepath.Join(dataDir, "connections", "secrets")}
			address, err := store.Get(connections.MailboxAddress)
			if err != nil || strings.TrimSpace(address) == "" {
				return fantasy.NewTextErrorResponse("Mailbox is not saved. The owner adds it in the dot's workspace settings. Do not ask them to paste a password in the chat."), nil
			}
			secret, err := store.Get(connections.MailboxSecret)
			if err != nil || secret == "" {
				return fantasy.NewTextErrorResponse("Mailbox is not saved. The owner adds it in the dot's workspace settings. Do not ask them to paste a password in the chat."), nil
			}
			messages, err := mailbox.Recent(ctx, address, secret, params.Limit)
			if err != nil {
				return fantasy.NewTextErrorResponse(err.Error()), nil
			}
			payload := map[string]any{"address": address, "messages": messages}
			raw, err := json.Marshal(payload)
			if err != nil {
				return fantasy.NewTextErrorResponse("could not read the mailbox"), nil
			}
			return fantasy.NewTextResponse(string(raw)), nil
		})
}
