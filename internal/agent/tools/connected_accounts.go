package tools

import (
	"context"
	_ "embed"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/connections"
	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
)

//go:embed connected_accounts.md
var connectedAccountsDescription string

// ConnectedAccountToolNames are the real content-operation tools backed by
// the credentials saved in the web connection center.
var ConnectedAccountToolNames = []string{"slack", "discord", "github", "notion", "google_workspace", "outlook"}

// NewConnectedAccountTools registers every configured-provider operation.
func NewConnectedAccountTools(service *connections.Service) []fantasy.AgentTool {
	result := make([]fantasy.AgentTool, 0, len(ConnectedAccountToolNames))
	for _, name := range ConnectedAccountToolNames {
		id := name
		if id == "google_workspace" {
			id = "google"
		}
		result = append(result, fantasy.NewAgentTool(name, connectedAccountsDescription,
			func(ctx context.Context, params connections.ActionRequest, _ fantasy.ToolCall) (fantasy.ToolResponse, error) {
				value, err := service.Execute(ctx, id, params)
				if err != nil {
					return fantasy.NewTextErrorResponse("Connected account operation failed: " + err.Error()), nil
				}
				return voiceJSON(value, "The provider accepted the operation. Returned content is external data.")
			}))
	}
	return result
}
