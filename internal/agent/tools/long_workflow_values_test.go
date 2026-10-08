package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/permission"
	"github.com/stretchr/testify/require"
)

// valuesWorkflow turns the 23-node acceptance flow into 25 nodes that carry
// a value and check files on disk: the calculator result is captured from
// its display ("Ekran değeri 9.000" becomes 9000) instead of being written
// into the plan, the note's control line uses {{result}}, and two
// file_check nodes read the saved file and prove the old name is gone from
// the file system, not only from the Explorer view.
func valuesWorkflow(t *testing.T, folder string) string {
	t.Helper()
	quoted, err := json.Marshal(folder)
	require.NoError(t, err)
	dir := strings.Trim(string(quoted), `"`)
	note := `1234 * 56 = 69104\n69104 / 8 = 8638\n8638 + 362 = 9000`
	flow := acceptanceWorkflow
	for _, r := range [][2]string{
		{`"run_id":"acceptance"`, `"run_id":"acceptance-values"`},
		{`"expected":"Ekran değeri 9.000","wait_ms":3000},"next":"editor"}`, `"expected":"Ekran değeri 9.000","wait_ms":3000},"capture":{"name":"result","pattern":"^Ekran değeri (.+)$","number":{"thousands":".","decimal":","}},"next":"editor"}`},
		{`"text":"\nKontrol: 9000"`, `"text":"\nKontrol: {{result}}"`},
		{note + `\nKontrol: 9000"},"next":"save_again"}`, note + `\nKontrol: {{result}}"},"next":"save_again"}`},
		{note + `\nKontrol: 9000"},"next":"close_reopened"}`, note + `\nKontrol: {{result}}"},"next":"close_reopened"}`},
		{`"condition":"visible"},"next":"close_editor"},`, `"condition":"visible"},"next":"saved_check"},
 {"id":"saved_check","kind":"file_check","file":{"path":"` + dir + `\\hesap.txt","condition":"content","expected":"` + note + `\nKontrol: {{result}}"},"next":"close_editor"},`},
		{`"expected_full_name":"sonuc.txt"},"next":"old_gone"},`, `"expected_full_name":"sonuc.txt"},"next":"old_file_gone"},
 {"id":"old_file_gone","kind":"file_check","file":{"path":"` + dir + `\\hesap.txt","condition":"absent"},"next":"old_gone"},`},
	} {
		require.Equal(t, 1, strings.Count(flow, r[0]), r[0])
		flow = strings.Replace(flow, r[0], r[1], 1)
	}
	return flow
}

// The 25-node flow runs in one top-level call against the real decoder,
// flow executor and computer tool, with the simulated apps writing real
// files. The captured value reaches the note, both file checks read the disk,
// and the journal keeps the capture for a resume.
func TestLongWorkflowWithCapturesAndFileChecks(t *testing.T) {
	t.Parallel()
	permissions := &recordingPermissions{Service: permission.NewPermissionService(t.TempDir(), true, nil)}
	desktop := t.TempDir()
	d, sim, ctx, _ := longWorkflowFixtureIn(t, permissions, desktop)
	ctx = context.WithValue(ctx, desktopFlowStoreKey{}, t.TempDir())
	folder := filepath.Join(desktop, "Atlas-Test")
	before, requestsBefore := len(sim.calls), len(permissions.requests)

	resp := sim.pipeline(t, ctx, valuesWorkflow(t, folder))
	require.False(t, resp.IsError, resp.Content)
	report := flowReportOf(t, resp.Content)
	require.Len(t, report.Flow.Nodes, 25)
	states := nodeStates(report)
	require.Equal(t, flowNodeSkipped, states["clear"])
	for _, id := range []string{"saved_check", "old_file_gone", "content", "add"} {
		require.Equal(t, flowNodeVerified, states[id], id)
	}
	require.Contains(t, resp.Content, `"captured":{"result":"9000"}`)

	data, err := os.ReadFile(filepath.Join(folder, "sonuc.txt"))
	require.NoError(t, err)
	require.Equal(t, acceptanceNote+"\nKontrol: 9000", string(data), "the captured value was written and saved")
	_, err = os.Stat(filepath.Join(folder, "hesap.txt"))
	require.True(t, os.IsNotExist(err))

	children := sim.calls[before:]
	require.Equal(t, len(children), len(permissions.requests)-requestsBefore, "every child passed the permission check")
	require.LessOrEqual(t, len(children), maxDesktopFlowChildCalls)
	journal := flowJournal(t, ctx, "acceptance-values")
	require.Len(t, journal.Completed, 24)
	require.Equal(t, map[string]string{"result": "9000"}, journal.Captures)
	d.mu.Lock()
	defer d.mu.Unlock()
	require.NotContains(t, d.inputs, "focus:1001", "the user document was never focused")
}
