package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agent"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/db"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/session"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type workflowFixtureCoordinator struct {
	agent.Coordinator
	value engineering.WorkflowSnapshot
}

func workflowTestRequest(t *testing.T, method, url string, body []byte) (*http.Response, error) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), method, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/json")
	}
	return http.DefaultClient.Do(req)
}

type workflowCoreCoordinator struct {
	agent.Coordinator
	agent.WorkflowController
}

func (c *workflowCoreCoordinator) WorkflowSnapshot(ctx context.Context, id string) (engineering.WorkflowSnapshot, error) {
	return c.WorkflowController.WorkflowSnapshot(ctx, id)
}

func (c *workflowCoreCoordinator) WorkflowControl(ctx context.Context, id string, control engineering.WorkflowControl) error {
	return c.WorkflowController.WorkflowControl(ctx, id, control)
}

func TestWorkflowServerUsesRealPersistentControllerAndStrictControls(t *testing.T) {
	t.Parallel()
	h := newE2EHarness(t)
	dataDir := t.TempDir()
	conn, err := db.Connect(t.Context(), dataDir)
	require.NoError(t, err)
	defer db.Release(dataDir)
	sessions := session.NewService(db.New(conn), conn)
	sess, err := sessions.Create(t.Context(), "workflow")
	require.NoError(t, err)
	store := engineering.NewStore(dataDir)
	cfg := config.NewTestStore(&config.Config{Options: &config.Options{}}).Scoped(h.workspace.Path)
	core := agent.NewWorkflowController(cfg, sessions, store)
	h.workspace.AgentCoordinator = &workflowCoreCoordinator{WorkflowController: core}
	cid := uuid.NewString()
	require.NoError(t, h.backend.AttachClient(h.workspace.ID, cid))
	url := h.httpSrv.URL + "/v1/workspaces/" + h.workspace.ID + "/sessions/" + sess.ID + "/workflow?client_id=" + cid
	local, err := core.WorkflowSnapshot(t.Context(), sess.ID)
	require.NoError(t, err)
	rsp, err := workflowTestRequest(t, http.MethodGet, url, nil)
	require.NoError(t, err)
	var remote engineering.WorkflowSnapshot
	require.NoError(t, json.NewDecoder(rsp.Body).Decode(&remote))
	rsp.Body.Close()
	require.Equal(t, local, remote)
	valid, err := json.Marshal(engineering.WorkflowControl{Action: "pause", ExpectedRevision: local.Revision})
	require.NoError(t, err)
	for _, input := range [][]byte{append(append([]byte{}, valid...), []byte(`{}`)...), []byte(`{"action":"pause","action":"resume"}`), []byte(`{"Action":"pause"}`)} {
		rsp, err := workflowTestRequest(t, http.MethodPost, url, input)
		require.NoError(t, err)
		require.Equal(t, http.StatusBadRequest, rsp.StatusCode)
		rsp.Body.Close()
	}
	rsp, err = workflowTestRequest(t, http.MethodPost, url, valid)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, rsp.StatusCode)
	rsp.Body.Close()
	paused, err := core.WorkflowSnapshot(t.Context(), sess.ID)
	require.NoError(t, err)
	require.True(t, paused.Paused)
	require.NotEqual(t, local.Revision, paused.Revision)
	rsp, err = workflowTestRequest(t, http.MethodPost, url, valid)
	require.NoError(t, err)
	require.Equal(t, http.StatusConflict, rsp.StatusCode)
	rsp.Body.Close()
}

func (c *workflowFixtureCoordinator) WorkflowSnapshot(context.Context, string) (engineering.WorkflowSnapshot, error) {
	return c.value, nil
}

func (c *workflowFixtureCoordinator) WorkflowControl(context.Context, string, engineering.WorkflowControl) error {
	return nil
}

func TestWorkflowSnapshotClientServerParityAndUnauthorizedDenied(t *testing.T) {
	t.Parallel()
	h := newE2EHarness(t)
	value := engineering.WorkflowSnapshot{SessionID: "session", Revision: engineering.Hash("revision"), Tasks: []engineering.WorkflowTask{{ID: "task", Status: "pending"}}, Capabilities: map[string]string{"controls": "available"}}
	h.workspace.AgentCoordinator = &workflowFixtureCoordinator{value: value}
	url := h.httpSrv.URL + "/v1/workspaces/" + h.workspace.ID + "/sessions/session/workflow?client_id=" + uuid.NewString()
	rsp, err := workflowTestRequest(t, http.MethodGet, url, nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusForbidden, rsp.StatusCode)
	rsp.Body.Close()
	cid := uuid.NewString()
	require.NoError(t, h.backend.AttachClient(h.workspace.ID, cid))
	rsp, err = workflowTestRequest(t, http.MethodGet, h.httpSrv.URL+"/v1/workspaces/"+h.workspace.ID+"/sessions/session/workflow?client_id="+cid, nil)
	require.NoError(t, err)
	defer rsp.Body.Close()
	require.Equal(t, http.StatusOK, rsp.StatusCode)
	var remote engineering.WorkflowSnapshot
	require.NoError(t, json.NewDecoder(rsp.Body).Decode(&remote))
	require.Equal(t, value, remote)
}
