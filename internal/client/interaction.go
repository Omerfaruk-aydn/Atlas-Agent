package client

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/proto"
)

// Do sends a request to the server's /v1 API and returns the raw
// response, which the caller must close. path is relative to /v1. It
// exists for adapters, such as the web gateway, that relay calls whose
// payloads they do not need to decode.
func (c *Client) Do(ctx context.Context, method, path string, query url.Values, body io.Reader, headers http.Header) (*http.Response, error) {
	return c.sendReq(ctx, method, path, query, body, headers)
}

// GetPermissionMode returns the workspace's permission mode.
func (c *Client) GetPermissionMode(ctx context.Context, id string) (string, error) {
	rsp, err := c.get(ctx, fmt.Sprintf("/workspaces/%s/permissions/mode", id), nil, nil)
	if err != nil {
		return "", fmt.Errorf("failed to get permission mode: %w", err)
	}
	defer rsp.Body.Close()
	if err := checkStatus(rsp); err != nil {
		return "", fmt.Errorf("failed to get permission mode: %w", err)
	}
	var out proto.PermissionModeRequest
	if err := json.NewDecoder(rsp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("failed to decode permission mode: %w", err)
	}
	return out.Mode, nil
}

// SetPermissionMode sets the workspace's permission mode.
func (c *Client) SetPermissionMode(ctx context.Context, id, mode string) error {
	rsp, err := c.post(ctx, fmt.Sprintf("/workspaces/%s/permissions/mode", id), nil,
		jsonBody(proto.PermissionModeRequest{Mode: mode}),
		http.Header{"Content-Type": []string{"application/json"}})
	if err != nil {
		return fmt.Errorf("failed to set permission mode: %w", err)
	}
	defer rsp.Body.Close()
	if err := checkStatus(rsp); err != nil {
		return fmt.Errorf("failed to set permission mode: %w", err)
	}
	return nil
}
