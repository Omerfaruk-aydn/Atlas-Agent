package nodes

import (
	"context"
	"encoding/json"

	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
)

// RemoteToolRequest addresses a built-in tool on the supported computer.
type RemoteToolRequest struct {
	OperationID string          `json:"operation_id"`
	Tool        string          `json:"tool"`
	Input       json.RawMessage `json:"input"`
}

// RemoteToolReply preserves native images, metadata and stop semantics.
type RemoteToolReply struct {
	Response  fantasy.ToolResponse `json:"response"`
	Operation *Operation           `json:"operation,omitempty"`
}

// RemoteToolExecutor runs only the host's built-ins, with a live grant check
// before every child of a workflow. Validate must send no input.
type RemoteToolExecutor interface {
	Validate(RemoteToolRequest) (bool, error)
	Run(context.Context, Grant, RemoteToolRequest, func(Need) error) (fantasy.ToolResponse, error)
	Close(string)
}

// RemoteTool invokes a built-in on the connected host, never this process.
func (c *Client) RemoteTool(ctx context.Context, request RemoteToolRequest) (RemoteToolReply, error) {
	var reply RemoteToolReply
	err := c.call(ctx, "POST", "/v1/support/tool", jsonBody(request), nil, &reply)
	return reply, err
}
