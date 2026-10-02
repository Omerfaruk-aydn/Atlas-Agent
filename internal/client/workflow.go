package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/proto"
)

func (c *Client) WorkflowSnapshot(ctx context.Context, wid, sid string) (proto.WorkflowSnapshot, error) {
	rsp, err := c.get(ctx, fmt.Sprintf("/workspaces/%s/sessions/%s/workflow", url.PathEscape(wid), url.PathEscape(sid)), url.Values{"client_id": {c.clientID}}, nil)
	if err != nil {
		return proto.WorkflowSnapshot{}, err
	}
	defer rsp.Body.Close()
	if rsp.StatusCode != http.StatusOK {
		return proto.WorkflowSnapshot{}, fmt.Errorf("workflow snapshot: status %d", rsp.StatusCode)
	}
	var snapshot proto.WorkflowSnapshot
	err = json.NewDecoder(rsp.Body).Decode(&snapshot)
	return snapshot, err
}

func (c *Client) WorkflowControl(ctx context.Context, wid, sid string, control proto.WorkflowControl) error {
	rsp, err := c.post(ctx, fmt.Sprintf("/workspaces/%s/sessions/%s/workflow", url.PathEscape(wid), url.PathEscape(sid)), url.Values{"client_id": {c.clientID}}, jsonBody(control), http.Header{"Content-Type": {"application/json"}})
	if err != nil {
		return err
	}
	defer rsp.Body.Close()
	if rsp.StatusCode != http.StatusOK {
		var body proto.Error
		_ = json.NewDecoder(rsp.Body).Decode(&body)
		return fmt.Errorf("workflow control: status %d: %s", rsp.StatusCode, body.Message)
	}
	return nil
}
