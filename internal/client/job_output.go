package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/shell"
)

func (c *Client) BackgroundJobOutput(ctx context.Context, wid, id string) (shell.BackgroundOutput, error) {
	rsp, err := c.get(ctx, fmt.Sprintf("/workspaces/%s/jobs/%s/output", url.PathEscape(wid), url.PathEscape(id)), url.Values{"client_id": {c.clientID}}, nil)
	if err != nil {
		return shell.BackgroundOutput{}, err
	}
	defer rsp.Body.Close()
	if rsp.StatusCode != http.StatusOK {
		return shell.BackgroundOutput{}, fmt.Errorf("job output: status %d", rsp.StatusCode)
	}
	var out shell.BackgroundOutput
	err = json.NewDecoder(rsp.Body).Decode(&out)
	return out, err
}
