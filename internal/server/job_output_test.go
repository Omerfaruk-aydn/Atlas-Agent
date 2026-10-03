package server

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/shell"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestJobOutputRequiresAttachedClientAndProjectOwnership(t *testing.T) {
	t.Parallel()
	h := newE2EHarness(t)
	h.workspace.Cfg = config.NewTestStore(&config.Config{Options: &config.Options{}}).Scoped(h.workspace.Path)
	manager := shell.GetBackgroundShellManager()
	job, err := manager.Start(t.Context(), h.workspace.Path, nil, "printf atlas-workflow-output", "workflow output test")
	require.NoError(t, err)
	t.Cleanup(func() { _ = manager.Kill(job.ID); _ = manager.Remove(job.ID) })
	clientID := uuid.NewString()
	url := h.httpSrv.URL + "/v1/workspaces/" + h.workspace.ID + "/jobs/" + job.ID + "/output?client_id=" + clientID
	rsp, err := workflowTestRequest(t, http.MethodGet, url, nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusForbidden, rsp.StatusCode)
	rsp.Body.Close()
	require.NoError(t, h.backend.AttachClient(h.workspace.ID, clientID))
	require.Eventually(t, job.IsDone, 3*time.Second, 10*time.Millisecond)
	rsp, err = workflowTestRequest(t, http.MethodGet, url, nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, rsp.StatusCode)
	var out shell.BackgroundOutput
	require.NoError(t, json.NewDecoder(rsp.Body).Decode(&out))
	rsp.Body.Close()
	require.True(t, out.Done)
	require.Contains(t, out.Stdout, "atlas-workflow-output")
	h.workspace.Cfg = h.workspace.Cfg.Scoped(t.TempDir())
	rsp, err = workflowTestRequest(t, http.MethodGet, url, nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusNotFound, rsp.StatusCode)
	rsp.Body.Close()
}
