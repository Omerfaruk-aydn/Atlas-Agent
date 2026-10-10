package localapps_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/integrations"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/localapps"
	"github.com/stretchr/testify/require"
)

// realRuntime wires the integrations service with only the tool and
// dependency adapters, so the test uses the same runtime answer as Atlas.
func realRuntime(t *testing.T, apps *localapps.Manager) *integrations.Service {
	t.Helper()
	svc := integrations.New(integrations.Options{})
	prober := integrations.NewProber()
	tools := integrations.DefaultTools(nil)
	svc.Add(&integrations.ToolAdapter{Specs: tools, Prober: prober})
	svc.Add(&integrations.DependencyAdapter{Runtimes: integrations.Runtimes(), Tools: tools, Prober: prober})
	svc.Add(&integrations.AppAdapter{Apps: apps})
	apps.SetRuntimeChecker(svc)
	return svc
}

func trackerSpec() localapps.Spec {
	return localapps.Spec{
		Purpose:  "Track work requests and their status",
		Flows:    []string{"Add a request", "Mark a request done", "See totals"},
		Language: "tr",
		Entities: []localapps.Entity{{
			Name:  "requests",
			Label: "Talepler",
			Fields: []localapps.Field{
				{Name: "title", Label: "Başlık", Type: "text", Required: true},
				{Name: "hours", Label: "Saat", Type: "number"},
				{Name: "due", Label: "Teslim", Type: "date"},
				{Name: "status", Label: "Durum", Type: "choice", Choices: []string{"açık", "kapalı"}},
				{Name: "urgent", Label: "Acil", Type: "boolean"},
			},
		}},
		Acceptance: []string{"A request can be added and is listed", "Records survive a restart"},
	}
}

func postJSON(t *testing.T, url string, body any) (int, map[string]any) {
	t.Helper()
	data, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, url, bytes.NewReader(data))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	return resp.StatusCode, out
}

func listTitles(t *testing.T, base string) []string {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, base+"api/requests", nil)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	var body struct {
		Items []map[string]any `json:"items"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	var titles []string
	for _, it := range body.Items {
		titles = append(titles, fmt.Sprint(it["title"]))
	}
	return titles
}

// TestLocalAppLifecycleEndToEnd builds a record tracker in a temporary
// folder with the real Python runtime and walks the whole lifecycle.
func TestLocalAppLifecycleEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("starts real processes")
	}
	state := t.TempDir()
	home := t.TempDir()
	apps := localapps.NewManager(localapps.Options{StateDir: state, AppsHome: home})
	svc := realRuntime(t, apps)
	t.Cleanup(func() {
		apps.StopAll(context.Background())
		svc.Shutdown()
	})
	ctx := t.Context()
	if rt := apps.CheckRuntime(ctx, localapps.RuntimePython); !rt.Available {
		t.Skipf("python runtime unavailable: %s", rt.Problem)
	}

	rec, err := apps.Plan(ctx, localapps.PlanInput{Name: "İş Talepleri", Description: "Ekip talepleri", Spec: trackerSpec()})
	require.NoError(t, err)
	require.Equal(t, localapps.StateDesigning, rec.State)
	require.Contains(t, rec.ID, "is-talepleri-")

	res, err := apps.Scaffold(ctx, rec.ID)
	require.NoError(t, err)
	require.FileExists(t, filepath.Join(res.Root, localapps.ManifestFile))
	require.FileExists(t, filepath.Join(res.Root, "server.py"))

	v, err := apps.Verify(ctx, rec.ID)
	require.NoError(t, err)
	require.True(t, v.Passed, "verification failed: %s %+v", v.Error, v.Checks)
	for _, c := range v.Checks {
		t.Logf("check %s passed=%v: %s", c.Name, c.Passed, c.Detail)
	}
	app, err := apps.Get(ctx, rec.ID)
	require.NoError(t, err)
	require.Equal(t, localapps.StateVerified, app.State, "acceptance is still pending")
	require.Len(t, app.PendingAcceptance, 2)
	_, err = os.Stat(filepath.Join(res.Root, "data", "app.db"))
	require.True(t, errors.Is(err, os.ErrNotExist), "verification must not create the real database")

	// Start, add a record through the real API.
	started, err := apps.Start(ctx, rec.ID, localapps.StartOptions{})
	require.NoError(t, err)
	require.Equal(t, "started", started.Outcome)
	again, err := apps.Start(ctx, rec.ID, localapps.StartOptions{})
	require.NoError(t, err)
	require.Equal(t, "already_running", again.Outcome)
	code, created := postJSON(t, started.URL+"api/requests", map[string]any{"title": "Yazıcı arızası", "hours": 1.5, "status": "açık", "urgent": true})
	require.Equal(t, http.StatusCreated, code, created)
	require.Equal(t, []string{"Yazıcı arızası"}, listTitles(t, started.URL))

	// Liveness is visible immediately; readiness requires a health check.
	item, ok := svc.Get(ctx, "app:"+rec.ID)
	require.True(t, ok)
	require.Equal(t, integrations.StatusUnchecked, item.Status)
	item, err = svc.Check(ctx, "app:"+rec.ID)
	require.NoError(t, err)
	require.Equal(t, integrations.StatusReady, item.Status)
	require.Contains(t, item.Detail.String(), started.URL)

	// Stop and start: the record persists.
	out, err := apps.Stop(ctx, rec.ID)
	require.NoError(t, err)
	require.Equal(t, "stopped", out)
	app, _ = apps.Get(ctx, rec.ID)
	require.False(t, app.Running)
	started, err = apps.Start(ctx, rec.ID, localapps.StartOptions{})
	require.NoError(t, err)
	require.Equal(t, []string{"Yazıcı arızası"}, listTitles(t, started.URL))

	// Backup while running, then change data and restore.
	backup, err := apps.Backup(ctx, rec.ID, "manual", false)
	require.NoError(t, err)
	require.Equal(t, 1, backup.Meta.SchemaVersions["data/app.db"])
	code, _ = postJSON(t, started.URL+"api/requests", map[string]any{"title": "Sonradan eklenen"})
	require.Equal(t, http.StatusCreated, code)
	require.Len(t, listTitles(t, started.URL), 2)
	msg, err := apps.Restore(ctx, rec.ID, backup.ID, localapps.RestoreOptions{})
	require.NoError(t, err, msg)
	app, _ = apps.Get(ctx, rec.ID)
	require.True(t, app.Running, "a running app is started again after restore")
	require.Equal(t, []string{"Yazıcı arızası"}, listTitles(t, app.URL))

	// Acceptance evidence makes it ready.
	_, err = apps.RecordAcceptance(ctx, rec.ID, 1, true, "http", "POST then GET listed the record")
	require.NoError(t, err)
	r, err := apps.RecordAcceptance(ctx, rec.ID, 2, true, "http", "record listed after stop/start")
	require.NoError(t, err)
	require.Equal(t, localapps.StateReady, r.State)

	// Editing source invalidates the old acceptance and cached health result.
	serverPath := filepath.Join(res.Root, "server.py")
	original, err := os.ReadFile(serverPath)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(serverPath, append(original, []byte("\n# changed source\n")...), 0o644))
	item, ok = svc.Get(ctx, "app:"+rec.ID)
	require.True(t, ok)
	require.Equal(t, integrations.StatusUnchecked, item.Status)
	item, err = svc.Check(ctx, "app:"+rec.ID)
	require.NoError(t, err)
	require.Equal(t, integrations.StatusUnchecked, item.Status)
	require.NoError(t, os.WriteFile(serverPath, original, 0o644))
	item, err = svc.Check(ctx, "app:"+rec.ID)
	require.NoError(t, err)
	require.Equal(t, integrations.StatusReady, item.Status)

	// A fresh manager (Atlas restart) re-derives the running state.
	fresh := localapps.NewManager(localapps.Options{StateDir: state, AppsHome: home})
	realRuntime(t, fresh)
	app, err = fresh.Get(ctx, rec.ID)
	require.NoError(t, err)
	require.True(t, app.Running)
	require.Equal(t, "previous_session", app.Owner)
	out, err = fresh.Stop(ctx, rec.ID)
	require.NoError(t, err)
	require.Equal(t, "stopped", out)

	// Delete data keeps the registration and a backup.
	msg, err = apps.DeleteData(ctx, rec.ID)
	require.NoError(t, err)
	require.Contains(t, msg, "backup")
	list, _ := apps.Backups(rec.ID)
	require.GreaterOrEqual(t, len(list), 3)

	// Unregister keeps the files.
	msg, err = apps.Unregister(ctx, rec.ID)
	require.NoError(t, err)
	require.Contains(t, msg, res.Root)
	require.FileExists(t, filepath.Join(res.Root, "server.py"))
	_, err = apps.Get(ctx, rec.ID)
	require.ErrorIs(t, err, localapps.ErrNotFound)
}
