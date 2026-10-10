package dialog

import (
	"context"
	"errors"
	"image"
	"strings"
	"testing"

	tea "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ui/v2"
	uv "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ultraviolet"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/i18n"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/integrations"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/localapps"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/common"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/styles"
	"github.com/stretchr/testify/require"
)

func testCommon(lang string) *common.Common {
	sty := styles.AtlasPantera()
	sty.Locale = i18n.New(lang)
	return &common.Common{Styles: &sty}
}

type stubAdapter struct {
	items []integrations.Item
	did   []integrations.Action
}

func (a *stubAdapter) Items(context.Context, integrations.Fingerprinter) []integrations.Item {
	return a.items
}

func (a *stubAdapter) Check(context.Context, integrations.Item) integrations.CheckResult {
	return integrations.CheckResult{Status: integrations.StatusReady, Detail: i18n.M("Credentials accepted by the provider.")}
}

func (a *stubAdapter) Do(_ context.Context, _ integrations.Item, act integrations.Action) (i18n.Message, error) {
	a.did = append(a.did, act)
	if act == integrations.ActionRemove {
		return i18n.Message{}, errors.New("save failed")
	}
	return i18n.M("Disabled in %s. Credentials were kept.", "/cfg/atlas.json"), nil
}

func newIntegrationsDialog(t *testing.T, lang string) (*Integrations, *stubAdapter, *integrations.Service) {
	t.Helper()
	a := &stubAdapter{items: []integrations.Item{
		{ID: "provider:openai", Name: "OpenAI", Kind: integrations.KindProvider, Status: integrations.StatusUnchecked, Source: integrations.Source{Origin: "atlas.json", Path: "/cfg/atlas.json", Editable: true}, Actions: []integrations.Action{integrations.ActionTest, integrations.ActionConfigure, integrations.ActionDisable, integrations.ActionRemove}, Capabilities: []i18n.Message{i18n.M("reasoning")}},
		{ID: "provider:groq", Name: "Groq", Kind: integrations.KindProvider, Status: integrations.StatusNotConfigured, Actions: []integrations.Action{integrations.ActionConfigure}},
		{ID: "tool:git", Name: "Git", Kind: integrations.KindTool, Status: integrations.StatusMissingDependency, LastError: i18n.M("Not found on PATH."), Live: true, Actions: []integrations.Action{integrations.ActionTest}},
		{ID: "mcp:files", Name: "files", Kind: integrations.KindMCP, Status: integrations.StatusNeedsLogin, Live: true, Actions: []integrations.Action{integrations.ActionLogin}},
	}}
	svc := integrations.New(integrations.Options{})
	svc.Add(a)
	d := NewIntegrations(testCommon(lang), svc)
	d.HandleMsg(IntegrationsLoaded{Items: svc.Snapshot(t.Context(), integrations.Filter{})})
	return d, a, svc
}

func keyPress(s string) tea.KeyPressMsg {
	switch s {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	}
	r := []rune(s)[0]
	return tea.KeyPressMsg{Code: r, Text: s}
}

func TestIntegrationsFiltersSearchAndCatalog(t *testing.T) {
	t.Parallel()
	d, _, _ := newIntegrationsDialog(t, "en")
	require.Len(t, d.visible(), 3, "catalog entries are hidden by default")
	d.HandleMsg(keyPress("a"))
	require.Len(t, d.visible(), 4)
	d.HandleMsg(keyPress("a"))

	d.HandleMsg(keyPress("s")) // Needs attention.
	ids := []string{}
	for _, it := range d.visible() {
		ids = append(ids, it.ID)
	}
	require.ElementsMatch(t, []string{"tool:git", "mcp:files"}, ids)
	d.HandleMsg(keyPress("s"))
	d.HandleMsg(keyPress("s"))
	d.HandleMsg(keyPress("s"))
	d.HandleMsg(keyPress("s"))
	require.Equal(t, 0, d.statusIdx)

	d.HandleMsg(keyPress("f")) // Model providers.
	require.Len(t, d.visible(), 1)
	d.kindFilter = 0

	d.HandleMsg(keyPress("/"))
	require.True(t, d.searching)
	for _, r := range "reason" {
		d.HandleMsg(keyPress(string(r)))
	}
	require.Len(t, d.visible(), 1)
	require.Equal(t, "provider:openai", d.visible()[0].ID)
	d.HandleMsg(keyPress("esc"))
	require.False(t, d.searching)
	require.Len(t, d.visible(), 3)
}

func TestIntegrationsSearchMatchesTranslatedText(t *testing.T) {
	t.Parallel()
	d, _, _ := newIntegrationsDialog(t, "tr")
	d.search.SetValue("akıl")
	require.Len(t, d.visible(), 1, "Turkish capability text is searchable")
}

func TestIntegrationsDestructiveActionNeedsConfirmation(t *testing.T) {
	t.Parallel()
	d, a, _ := newIntegrationsDialog(t, "en")
	d.HandleMsg(keyPress("enter"))
	require.Equal(t, integrationsDetail, d.view)
	require.Equal(t, "provider:openai", d.detailID)

	// Actions: test, configure, disable, remove.
	d.HandleMsg(keyPress("down"))
	act := d.HandleMsg(keyPress("enter"))
	require.Equal(t, ActionOpenDialog{DialogID: ModelsID}, act)

	d.HandleMsg(keyPress("down"))
	require.Nil(t, d.HandleMsg(keyPress("enter")))
	require.Equal(t, integrationsConfirm, d.view)
	require.Contains(t, d.confirm.text, "/cfg/atlas.json")
	d.HandleMsg(keyPress("n"))
	require.Equal(t, integrationsDetail, d.view)
	require.Empty(t, a.did, "declining changes nothing")

	d.HandleMsg(keyPress("enter"))
	act = d.HandleMsg(keyPress("y"))
	cmd, ok := act.(ActionCmd)
	require.True(t, ok)
	done := cmd.Cmd().(IntegrationsDone)
	require.NoError(t, done.Err)
	d.HandleMsg(done)
	require.True(t, d.noticeOK)
	require.Contains(t, d.notice, "Credentials were kept")

	// A failed save is reported as a failure.
	d.HandleMsg(keyPress("down"))
	d.HandleMsg(keyPress("enter"))
	cmd = d.HandleMsg(keyPress("y")).(ActionCmd)
	done = cmd.Cmd().(IntegrationsDone)
	require.Error(t, done.Err)
	d.HandleMsg(done)
	require.False(t, d.noticeOK)
	require.Contains(t, d.notice, "save failed")
}

func TestIntegrationsMCPLoginUsesExistingFlow(t *testing.T) {
	t.Parallel()
	d, _, _ := newIntegrationsDialog(t, "en")
	d.detailID, d.view = "mcp:files", integrationsDetail
	require.Equal(t, ActionIntegrationsMCPLogin{Name: "files"}, d.HandleMsg(keyPress("enter")))
}

func TestIntegrationsDrawsEveryLocaleAndSize(t *testing.T) {
	t.Parallel()
	for _, lang := range i18n.Languages() {
		d, _, _ := newIntegrationsDialog(t, lang.Code)
		for _, view := range []integrationsView{integrationsList, integrationsDetail} {
			d.view, d.detailID = view, "provider:openai"
			for _, size := range []image.Point{{X: 120, Y: 40}, {X: 60, Y: 16}, {X: 20, Y: 6}, {X: 0, Y: 0}} {
				scr := uv.NewScreenBuffer(size.X, size.Y)
				require.NotPanics(t, func() { d.Draw(scr, image.Rectangle{Max: size}) })
				if size.X == 120 && view == integrationsList {
					require.Contains(t, scr.String(), i18n.Text(lang.Code, "Integrations"))
				}
			}
		}
	}
	d := NewIntegrations(testCommon("en"), nil)
	scr := uv.NewScreenBuffer(100, 20)
	d.Draw(scr, image.Rectangle{Max: image.Point{X: 100, Y: 20}})
	require.Contains(t, scr.String(), "only available when Atlas runs locally")
}

func TestIntegrationAndAppLabelsAreTranslated(t *testing.T) {
	t.Parallel()
	var labels []string
	for _, k := range append(integrations.Kinds(), "") {
		labels = append(labels, kindLabel(k))
	}
	for _, s := range []integrations.Status{integrations.StatusNotConfigured, integrations.StatusUnchecked, integrations.StatusChecking, integrations.StatusReady, integrations.StatusNeedsLogin, integrations.StatusMissingDependency, integrations.StatusConnectionError, integrations.StatusLimited, integrations.StatusDisabled} {
		labels = append(labels, statusLabel(s))
	}
	for _, f := range integrationStatusFilters {
		labels = append(labels, statusFilterLabel(f))
	}
	for _, a := range []integrations.Action{integrations.ActionTest, integrations.ActionConfigure, integrations.ActionLogin, integrations.ActionEnable, integrations.ActionDisable, integrations.ActionForgetCredentials, integrations.ActionRemove, integrations.ActionOpenApp, integrationsActionCancelCheck} {
		labels = append(labels, actionLabel(a))
	}
	for _, a := range []integrations.AuthKind{integrations.AuthNone, integrations.AuthAPIKey, integrations.AuthOAuth, integrations.AuthHeaders, integrations.AuthEnv} {
		labels = append(labels, authLabelOf(a))
	}
	for _, op := range []string{appOpOpen, appOpStart, appOpStop, appOpRestart, appOpVerify, appOpLog, appOpBackup, appOpRestore, appOpUpdate, appOpRollback, appOpUnregister, appOpDelData, appOpDelFiles, appOpCancel} {
		labels = append(labels, appOpLabel(op))
	}
	for _, s := range []localapps.State{localapps.StateDesigning, localapps.StateScaffolded, localapps.StateVerifying, localapps.StateVerified, localapps.StateReady, localapps.StateFailed, localapps.StateUpdating, localapps.StateUpdateFailed} {
		labels = append(labels, stateLabel(s))
	}
	labels = append(labels, "Detail:", "Error:", "Next step:", "Last check:", "Before restart:", "Note:", "Can do:", "Source:", "Authentication:", "Check cost:",
		"Folder:", "Technology:", "Data:", "Last verification:", "Acceptance checks pending:", "Last error:")
	for _, l := range labels {
		for _, lang := range i18n.Languages() {
			require.True(t, i18n.Has(lang.Code, l), "missing %s translation for %q", lang.Code, l)
		}
	}
}

func newAppsDialog(t *testing.T) *Apps {
	t.Helper()
	mgr := localapps.NewManager(localapps.Options{StateDir: t.TempDir(), AppsHome: t.TempDir()})
	d := NewApps(testCommon("en"), mgr, "")
	d.HandleMsg(d.load()().(AppsLoaded))
	return d
}

func TestAppsCreateAndRegisterInputs(t *testing.T) {
	t.Parallel()
	d := newAppsDialog(t)
	require.True(t, d.loaded)
	require.Empty(t, d.apps)

	d.HandleMsg(keyPress("enter")) // New app…
	require.Equal(t, appsInput, d.view)
	for _, r := range "A tracker for my records" {
		d.HandleMsg(keyPress(string(r)))
	}
	act := d.HandleMsg(keyPress("enter"))
	require.Equal(t, ActionAppsCreate{Request: "A tracker for my records"}, act)
	require.Equal(t, appsList, d.view)

	d.HandleMsg(keyPress("down"))
	d.HandleMsg(keyPress("enter")) // Add an existing folder…
	for _, r := range "relative" {
		d.HandleMsg(keyPress(string(r)))
	}
	require.Nil(t, d.HandleMsg(keyPress("enter")))
	require.False(t, d.noticeOK)
	require.Contains(t, d.notice, "absolute")
}

func TestAppsDeleteActionsAreSeparateAndConfirmed(t *testing.T) {
	t.Parallel()
	d := newAppsDialog(t)
	app := localapps.App{Record: localapps.Record{ID: "notes", Name: "Notes", Root: "/apps/notes", State: localapps.StateReady}, Manifest: &localapps.Manifest{}}
	d.detail, d.appID, d.view = &app, "notes", appsDetail
	ops := d.ops()
	require.Contains(t, ops, appOpUnregister)
	require.Contains(t, ops, appOpDelData)
	require.Contains(t, ops, appOpDelFiles)
	require.NotContains(t, ops, appOpRollback, "nothing to roll back")
	require.NotContains(t, ops, appOpStop, "not running")

	for _, op := range []string{appOpUnregister, appOpDelData, appOpDelFiles} {
		require.Nil(t, d.runOp(app, op))
		require.Equal(t, appsConfirm, d.view, op)
		d.HandleMsg(keyPress("n"))
		require.Equal(t, appsDetail, d.view)
	}
	d.runOp(app, appOpDelFiles)
	require.Contains(t, d.confirm.text, "/apps/notes")
	d.HandleMsg(keyPress("esc"))

	// A busy app offers only cancel (when this dialog started it) and log.
	app.Busy = "verify"
	d.detail = &app
	require.Equal(t, []string{appOpLog}, d.ops())
}

func TestAppsErrorsAreExplained(t *testing.T) {
	t.Parallel()
	d := newAppsDialog(t)
	d.HandleMsg(AppsDone{ID: "x", Op: appOpStart, Err: &localapps.OpError{Code: localapps.ErrPortInUse, Msg: "port 18100 is in use", Remedy: "Stop the other program."}})
	require.False(t, d.noticeOK)
	require.Contains(t, d.notice, "does not stop programs it did not start")
	require.Contains(t, d.detailOf, "18100")
	require.True(t, strings.HasSuffix(d.detailOf, "Stop the other program."))

	d.HandleMsg(AppsDone{ID: "x", Op: appOpUnregister, Detail: "Removed."})
	require.True(t, d.noticeOK)
	require.Equal(t, appsList, d.view)
}

func TestAppsDrawsEverySize(t *testing.T) {
	t.Parallel()
	d := newAppsDialog(t)
	app := localapps.App{Record: localapps.Record{ID: "notes", Name: "Notes", Root: "/apps/notes", State: localapps.StateReady, LastVerification: &localapps.Verification{Passed: true, Checks: []localapps.Check{{Name: "build", Passed: true}}}}, Running: true, URL: "http://127.0.0.1:18100"}
	for _, view := range []appsView{appsList, appsDetail, appsLog, appsBackups} {
		d.view, d.detail, d.apps = view, &app, []localapps.App{app}
		for _, size := range []image.Point{{X: 120, Y: 40}, {X: 40, Y: 10}, {X: 0, Y: 0}} {
			scr := uv.NewScreenBuffer(size.X, size.Y)
			require.NotPanics(t, func() { d.Draw(scr, image.Rectangle{Max: size}) })
		}
	}
}
