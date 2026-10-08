package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/permission"
	"github.com/stretchr/testify/require"
)

const acceptanceNote = "1234 * 56 = 69104\n69104 / 8 = 8638\n8638 + 362 = 9000"

// acceptanceWorkflow is one durable flow of 23 nodes across Calculator,
// Notepad, its owned Save dialog and Explorer. Every node does distinct work:
// a branch decides whether the calculator must be cleared, three dependent
// calculations are checked, the note is written, saved through the dialog
// (file name written and read back, the dialog proven closed and the editor
// verified again), extended and saved in place, the editor closed, the file
// renamed with its hidden extension checked, the old name proven gone, the
// file reopened and its whole content verified, and exactly the windows this
// task opened closed. Selectors are named/role selectors, as durable runs
// require; no window handle appears in the plan.
const acceptanceWorkflow = `{"timeout_ms":300000,"desktop":{"mode":"flow","flow":{"run_id":"acceptance","nodes":[
 {"id":"calc","kind":"resolve","application":"Hesap Makinesi","next":"fresh"},
 {"id":"fresh","kind":"branch","window_ref":"calc","checkpoint":{"role":"ControlType.Text","condition":"text","expected":"Ekran değeri 0"},"then":"mul","else":"clear"},
 {"id":"clear","kind":"operation","window_ref":"calc","focus":true,"input":{"action":"key","key":"escape"},"checkpoint":{"role":"ControlType.Text","condition":"text","expected":"Ekran değeri 0","wait_ms":2000},"next":"mul"},
 {"id":"mul","kind":"operation","window_ref":"calc","focus":true,"input":{"action":"type","text":"1234*56="},"checkpoint":{"role":"ControlType.Text","condition":"text","expected":"Ekran değeri 69.104","wait_ms":3000},"next":"div"},
 {"id":"div","kind":"operation","window_ref":"calc","input":{"action":"type","text":"/8="},"checkpoint":{"role":"ControlType.Text","condition":"text","expected":"Ekran değeri 8.638","wait_ms":3000},"next":"add"},
 {"id":"add","kind":"operation","window_ref":"calc","input":{"action":"type","text":"+362="},"checkpoint":{"role":"ControlType.Text","condition":"text","expected":"Ekran değeri 9.000","wait_ms":3000},"next":"editor"},
 {"id":"editor","kind":"prepare","application":"Not Defteri","next":"write"},
 {"id":"write","kind":"operation","window_ref":"editor","focus":true,"input":{"action":"type","text":"1234 * 56 = 69104\n69104 / 8 = 8638\n8638 + 362 = 9000"},"checkpoint":{"role":"ControlType.Document","name":"Metin düzenleyicisi","condition":"document_text","expected":"1234 * 56 = 69104\n69104 / 8 = 8638\n8638 + 362 = 9000"},"next":"save"},
 {"id":"save","kind":"operation","window_ref":"editor","input":{"action":"hotkey","modifiers":"ctrl","key":"s"},"checkpoint":{"role":"ControlType.Window","name":"Adsız - Not Defteri","condition":"visible"},"next":"dialog"},
 {"id":"dialog","kind":"resolve","title":"Farklı Kaydet","owner_ref":"editor","wait_ms":5000,"next":"filename"},
 {"id":"filename","kind":"operation","window_ref":"dialog","input":{"action":"set_value","automation":{"name":"Dosya adı:","role":"ControlType.Edit","text":"hesap.txt"}},"checkpoint":{"name":"Dosya adı:","role":"ControlType.Edit","condition":"value","expected":"hesap.txt"},"next":"submit"},
 {"id":"submit","kind":"operation","window_ref":"dialog","input":{"action":"key","key":"enter"},"closes_window":true,"checkpoint_ref":"editor","checkpoint":{"role":"ControlType.Window","name":"hesap - Not Defteri","condition":"visible"},"next":"append"},
 {"id":"append","kind":"operation","window_ref":"editor","input":{"action":"type","text":"\nKontrol: 9000"},"checkpoint":{"role":"ControlType.Document","name":"Metin düzenleyicisi","condition":"document_text","expected":"1234 * 56 = 69104\n69104 / 8 = 8638\n8638 + 362 = 9000\nKontrol: 9000"},"next":"save_again"},
 {"id":"save_again","kind":"operation","window_ref":"editor","input":{"action":"hotkey","modifiers":"ctrl","key":"s"},"checkpoint":{"role":"ControlType.Window","name":"hesap - Not Defteri","condition":"visible"},"next":"close_editor"},
 {"id":"close_editor","kind":"close","window_ref":"editor","next":"folder"},
 {"id":"folder","kind":"resolve","application":"File Explorer","title":"Atlas-Test","next":"rename"},
 {"id":"rename","kind":"rename","window_ref":"folder","rename":{"old_name":"hesap","new_name":"sonuc","expected_full_name":"sonuc.txt"},"next":"old_gone"},
 {"id":"old_gone","kind":"verify","window_ref":"folder","checkpoint":{"name":"hesap","role":"ControlType.ListItem","condition":"hidden"},"next":"open"},
 {"id":"open","kind":"operation","window_ref":"folder","focus":true,"input":{"action":"invoke","automation":{"name":"sonuc","role":"ControlType.ListItem"}},"checkpoint":{"name":"sonuc","role":"ControlType.ListItem","condition":"visible"},"next":"reopened"},
 {"id":"reopened","kind":"resolve","application":"Not Defteri","title":"sonuc - Not Defteri","wait_ms":5000,"next":"content"},
 {"id":"content","kind":"verify","window_ref":"reopened","checkpoint":{"role":"ControlType.Document","name":"Metin düzenleyicisi","condition":"document_text","expected":"1234 * 56 = 69104\n69104 / 8 = 8638\n8638 + 362 = 9000\nKontrol: 9000"},"next":"close_reopened"},
 {"id":"close_reopened","kind":"close","window_ref":"reopened","next":"close_calc"},
 {"id":"close_calc","kind":"close","window_ref":"calc"}]}}}`

func acceptanceFixture(t *testing.T, permissions *recordingPermissions) (*simDesktop, *simTool, context.Context) {
	t.Helper()
	d, sim, ctx, _ := longWorkflowFixture(t, permissions)
	return d, sim, context.WithValue(ctx, desktopFlowStoreKey{}, t.TempDir())
}

func countInputs(inputs []string, want string) int {
	n := 0
	for _, input := range inputs {
		if input == want {
			n++
		}
	}
	return n
}

// The whole 23-node workflow runs in one top-level call against the real
// decoder, flow executor and computer tool. Every child passes the permission
// check, no screenshot is taken, the journal holds every completed node and
// the report stays inside the output limit with its evidence intact.
func TestLongWorkflowAcceptanceRunsTwentyThreeNodes(t *testing.T) {
	t.Parallel()
	permissions := &recordingPermissions{Service: permission.NewPermissionService(t.TempDir(), true, nil)}
	d, sim, ctx := acceptanceFixture(t, permissions)
	before, requestsBefore := len(sim.calls), len(permissions.requests)

	started := time.Now()
	resp := sim.pipeline(t, ctx, acceptanceWorkflow)
	elapsed := time.Since(started)
	require.False(t, resp.IsError, resp.Content)
	require.NotEqual(t, "image", resp.Type)
	report := flowReportOf(t, resp.Content)
	require.Len(t, report.Flow.Nodes, 23)
	executed := 0
	for id, state := range nodeStates(report) {
		if id == "clear" {
			require.Equal(t, flowNodeSkipped, state, "the calculator was already clear")
			continue
		}
		executed++
		require.Contains(t, []string{flowNodeBound, flowNodeEvaluated, flowNodeVerified}, state, id)
	}
	require.Equal(t, 22, executed)
	require.Len(t, report.Flow.Steps, 22, "per-node evidence was not shortened")
	require.LessOrEqual(t, len(resp.Content), 64*1024)

	d.mu.Lock()
	folder := d.desktopDir + `\Atlas-Test`
	require.Equal(t, acceptanceNote+"\nKontrol: 9000", d.files[folder+`\sonuc.txt`])
	require.NotContains(t, d.files, folder+`\hesap.txt`)
	var titles []string
	for _, w := range d.windows {
		titles = append(titles, w.info.Name)
	}
	require.ElementsMatch(t, []string{"Rapor.docx - Word", "Atlas-Test"}, titles, "only the task's windows were closed")
	require.NotContains(t, d.inputs, "focus:1001", "the user document was never focused")
	d.mu.Unlock()

	children := sim.calls[before:]
	inputs := 0
	for _, c := range children {
		if desktopMutation(c.Action) && c.Action != "focus" {
			inputs++
		}
	}
	require.Equal(t, len(children), len(permissions.requests)-requestsBefore, "every child passed the permission check")
	require.LessOrEqual(t, len(children), maxDesktopFlowChildCalls)

	journal := flowJournal(t, ctx, "acceptance")
	require.Len(t, journal.Completed, 22)
	require.Empty(t, journal.Pending)
	require.Empty(t, journal.Next)
	dir := ctx.Value(desktopFlowStoreKey{}).(string)
	stat, err := os.Stat(filepath.Join(dir, "desktop-runs", engineering.Hash(desktopFlowSession(ctx)+"\x00acceptance")+".json"))
	require.NoError(t, err)
	require.Less(t, stat.Size(), int64(64*1024), "the journal stays inside its read bound")
	require.Less(t, elapsed, 300*time.Second)
	t.Logf("acceptance: 1 top-level call, 23 workflow nodes (22 executed, 1 skipped branch), %d child calls (%d input actions), 0 images, journal %d bytes, report %d bytes, %s", len(children), inputs, stat.Size(), len(resp.Content), elapsed.Round(time.Millisecond))
}

// Failures in the middle stop every dependent node, and each stop is
// classified: a wrong result fails, a refused input is blocked before its
// effect, cancellation after input and an unknown dispatch are uncertain.
func TestLongWorkflowAcceptanceStopsDependentNodesMidway(t *testing.T) {
	t.Parallel()
	later := func(from string) []string {
		ids := []string{"calc", "fresh", "clear", "mul", "div", "add", "editor", "write", "save", "dialog", "filename", "submit", "append", "save_again", "close_editor", "folder", "rename", "old_gone", "open", "reopened", "content", "close_reopened", "close_calc"}
		return ids[slices.Index(ids, from)+1:]
	}
	requireStopped := func(t *testing.T, states map[string]string, at, state string) {
		t.Helper()
		require.Equal(t, state, states[at], at)
		for _, id := range later(at) {
			require.Equal(t, flowNodeNotRun, states[id], id)
		}
	}

	t.Run("wrong content", func(t *testing.T) {
		t.Parallel()
		d, sim, ctx := acceptanceFixture(t, nil)
		plan := strings.Replace(acceptanceWorkflow, `\nKontrol: 9000"},"next":"close_reopened"`, `\nKontrol: 9001"},"next":"close_reopened"`, 1)
		resp := sim.pipeline(t, ctx, plan)
		require.True(t, resp.IsError)
		requireStopped(t, nodeStates(flowReportOf(t, resp.Content)), "content", flowNodeFailed)
		d.mu.Lock()
		defer d.mu.Unlock()
		require.Len(t, d.windows, 4, "nothing was closed after the failed check")
	})
	t.Run("permission refused before input", func(t *testing.T) {
		t.Parallel()
		permissions := &recordingPermissions{Service: permission.NewPermissionService(t.TempDir(), true, nil)}
		permissions.deny = func(r permission.CreatePermissionRequest) bool { return r.Action == "invoke" }
		d, sim, ctx := acceptanceFixture(t, permissions)
		resp := sim.pipeline(t, ctx, acceptanceWorkflow)
		require.True(t, resp.StopTurn)
		requireStopped(t, nodeStates(flowReportOf(t, resp.Content)), "open", flowNodeBlocked)
		require.Contains(t, resp.Content, `"dispatch":"none"`)
		d.mu.Lock()
		require.False(t, slices.Contains(d.inputs, "invoke:sonuc"))
		d.mu.Unlock()

		// The user approves: resume continues at the blocked node with fresh
		// checks and repeats nothing that was already done.
		permissions.mu.Lock()
		permissions.deny = nil
		permissions.mu.Unlock()
		resp = sim.pipeline(t, ctx, strings.Replace(acceptanceWorkflow, `"run_id":"acceptance"`, `"run_id":"acceptance","resume":true`, 1))
		require.False(t, resp.IsError, resp.Content)
		d.mu.Lock()
		defer d.mu.Unlock()
		require.Equal(t, 1, countInputs(d.inputs, "type:"+acceptanceNote), "the note was typed once")
		require.Equal(t, 1, countInputs(d.inputs, "set_value:hesap.txt"))
		require.Equal(t, 1, countInputs(d.inputs, "key:f2"), "the rename was not repeated")
		require.Equal(t, 1, countInputs(d.inputs, "invoke:sonuc"))
		require.Equal(t, acceptanceNote+"\nKontrol: 9000", d.files[d.desktopDir+`\Atlas-Test\sonuc.txt`])
	})
	t.Run("cancel after input", func(t *testing.T) {
		t.Parallel()
		d, sim, ctx := acceptanceFixture(t, nil)
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()
		invoke := sim.invoke
		resp, err := NewToolPipeline(func(ctx context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			r, err := invoke(ctx, call)
			if strings.Contains(call.Input, `"action":"type"`) && strings.Contains(call.Input, `Kontrol: 9000"`) {
				cancel()
			}
			return r, err
		}).Run(ctx, fantasy.ToolCall{ID: "pipeline", Name: ToolPipelineName, Input: acceptanceWorkflow})
		require.True(t, resp.IsError || err != nil)
		requireStopped(t, nodeStates(flowReportOf(t, resp.Content)), "append", flowNodeUncertain)
		d.mu.Lock()
		defer d.mu.Unlock()
		require.Equal(t, "type:\nKontrol: 9000", d.inputs[len(d.inputs)-1], "no input after the cancel")
	})
	t.Run("unknown dispatch", func(t *testing.T) {
		t.Parallel()
		d, sim, ctx := acceptanceFixture(t, nil)
		invoke := sim.invoke
		unknown := func(ctx context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			var p ComputerParams
			_ = json.Unmarshal([]byte(call.Input), &p)
			if p.Action == "invoke" {
				return withToolOutcome(fantasy.NewTextErrorResponse(`computer action "invoke" timed out after 30s; the result may be unknown. Inspect before repeating input.`), ToolOutcome{Dispatch: "unknown", Mutation: true}), nil
			}
			return invoke(ctx, call)
		}
		resp, err := NewToolPipeline(unknown).Run(ctx, fantasy.ToolCall{ID: "pipeline", Name: ToolPipelineName, Input: acceptanceWorkflow})
		require.NoError(t, err)
		requireStopped(t, nodeStates(flowReportOf(t, resp.Content)), "open", flowNodeUncertain)
		require.Contains(t, flowReportOf(t, resp.Content).Flow.Resume, "may have taken effect")

		resp, err = NewToolPipeline(unknown).Run(ctx, fantasy.ToolCall{ID: "resume", Name: ToolPipelineName, Input: strings.Replace(acceptanceWorkflow, `"run_id":"acceptance"`, `"run_id":"acceptance","resume":true`, 1)})
		require.NoError(t, err)
		require.True(t, strings.HasPrefix(resp.Content, "uncertain_effect:"), resp.Content)
		d.mu.Lock()
		defer d.mu.Unlock()
		require.Equal(t, 3, len(d.windows), "nothing was reopened or closed after the unknown dispatch")
	})
}
