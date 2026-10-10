package dialog

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ansi"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-style/v2"
	tea "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ui/v2"
	uv "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ultraviolet"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-widgets/v2/help"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-widgets/v2/key"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-widgets/v2/textinput"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/localapps"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/common"
)

// AppsID identifies the /app dialog.
const AppsID = "apps"

// ActionOpenApps opens /app, optionally on one app.
type ActionOpenApps struct{ AppID string }

// ActionAppsCreate asks the assistant to build a new local app.
type ActionAppsCreate struct{ Request string }

// ActionAppsUpdate asks the assistant to change an existing app.
type ActionAppsUpdate struct{ ID, Request string }

// ActionAppsOpenURL opens a running app in the default browser.
type ActionAppsOpenURL struct{ URL string }

// AppsTick refreshes /app. The UI routes it to the dialog even when another
// dialog is in front.
type AppsTick struct{}

// AppsLoaded carries what the current view needs, read off the UI loop.
type AppsLoaded struct {
	Apps    []localapps.App
	Err     error
	Detail  *localapps.App
	Journal *localapps.Journal
	Backups []localapps.BackupInfo
	Log     []string
	Runtime *localapps.RuntimeStatus
}

// AppsDone reports a finished operation.
type AppsDone struct {
	ID     string
	Op     string
	Detail string
	Err    error
}

type appsView int

const (
	appsList appsView = iota
	appsDetail
	appsLog
	appsBackups
	appsConfirm
	appsInput
)

// Operation identifiers.
const (
	appOpNew        = "new"
	appOpRegister   = "register"
	appOpOpen       = "open"
	appOpStart      = "start"
	appOpStop       = "stop"
	appOpRestart    = "restart"
	appOpVerify     = "verify"
	appOpLog        = "log"
	appOpBackup     = "backup"
	appOpRestore    = "restore"
	appOpUpdate     = "update"
	appOpRollback   = "rollback"
	appOpUnregister = "unregister"
	appOpDelData    = "delete_data"
	appOpDelFiles   = "delete_files"
	appOpCancel     = "cancel"
)

type appsInputKind int

const (
	appsInputCreate appsInputKind = iota
	appsInputUpdate
	appsInputRegister
)

// Apps is the /app dialog: the local apps Atlas manages.
type Apps struct {
	com   *common.Common
	mgr   *localapps.Manager
	help  help.Model
	input textinput.Model
	view  appsView

	apps     []localapps.App
	loadErr  error
	loaded   bool
	detail   *localapps.App
	journal  *localapps.Journal
	backups  []localapps.BackupInfo
	log      []string
	runtime  *localapps.RuntimeStatus
	selected int
	opSel    int
	backSel  int
	scroll   int
	appID    string
	inputFor appsInputKind
	confirm  *integrationConfirm
	backTo   appsView
	notice   string
	detailOf string
	noticeOK bool
	// cancels holds the cancel functions of operations this dialog
	// started, by app id.
	cancels map[string]context.CancelFunc
	keyMap  struct {
		Up, Down, Enter, Back, Cancel key.Binding
	}
}

var _ Dialog = (*Apps)(nil)

// NewApps creates the /app dialog. mgr may be nil in client mode.
func NewApps(com *common.Common, mgr *localapps.Manager, appID string) *Apps {
	d := &Apps{com: com, mgr: mgr, cancels: map[string]context.CancelFunc{}}
	d.help = help.New()
	d.help.Styles = com.Styles.DialogHelpStyles()
	d.input = textinput.New()
	d.input.SetVirtualCursor(false)
	d.input.SetStyles(com.Styles.TextInput)
	d.keyMap.Up = key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑", com.Text("previous")))
	d.keyMap.Down = key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓", com.Text("next")))
	d.keyMap.Enter = key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", com.Text("choose")))
	d.keyMap.Back = key.NewBinding(key.WithKeys("esc", "alt+esc"), key.WithHelp("esc", com.Text("back")))
	d.keyMap.Cancel = key.NewBinding(key.WithKeys("c"), key.WithHelp("c", com.Text("cancel operation")))
	if appID != "" {
		d.appID, d.view = appID, appsDetail
	}
	return d
}

// ID implements Dialog.
func (d *Apps) ID() string { return AppsID }

// Init loads the first snapshot and starts the refresh tick.
func (d *Apps) Init() tea.Cmd { return tea.Batch(d.load(), d.tick()) }

func (d *Apps) tick() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return AppsTick{} })
}

// load reads the registry, and for the open app its manifest, journal,
// backups and log, in a command.
func (d *Apps) load() tea.Cmd {
	mgr := d.mgr
	if mgr == nil {
		return nil
	}
	id, view := d.appID, d.view
	if view == appsConfirm || view == appsInput {
		view = d.backTo
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		out := AppsLoaded{}
		out.Apps, out.Err = mgr.List(ctx)
		if id == "" || view == appsList {
			if len(out.Apps) == 0 {
				rt := mgr.CheckRuntime(ctx, localapps.SupportedRuntimes()[0].Kind)
				out.Runtime = &rt
			}
			return out
		}
		if app, err := mgr.Get(ctx, id); err == nil {
			out.Detail = &app
		}
		if j, ok := mgr.Journal(id); ok {
			out.Journal = &j
		}
		switch view {
		case appsBackups:
			out.Backups, _ = mgr.Backups(id)
		case appsLog:
			out.Log = mgr.TailLog(id, 200)
		}
		return out
	}
}

// HandleMsg implements Dialog.
func (d *Apps) HandleMsg(msg tea.Msg) Action {
	switch msg := msg.(type) {
	case AppsTick:
		return ActionCmd{Cmd: tea.Batch(d.load(), d.tick())}
	case AppsLoaded:
		d.apps, d.loadErr, d.loaded = msg.Apps, msg.Err, true
		d.detail, d.journal, d.runtime = msg.Detail, msg.Journal, msg.Runtime
		if msg.Backups != nil || d.view != appsBackups {
			d.backups = msg.Backups
		}
		if msg.Log != nil || d.view != appsLog {
			d.log = msg.Log
		}
		return nil
	case AppsDone:
		delete(d.cancels, msg.ID)
		d.done(msg)
		return ActionCmd{Cmd: d.load()}
	case tea.PasteMsg:
		if d.view == appsInput {
			var cmd tea.Cmd
			d.input, cmd = d.input.Update(msg)
			return ActionCmd{Cmd: cmd}
		}
	case tea.KeyPressMsg:
		if d.mgr == nil {
			if key.Matches(msg, d.keyMap.Back) {
				return ActionClose{}
			}
			return nil
		}
		switch d.view {
		case appsConfirm:
			return d.handleConfirm(msg)
		case appsInput:
			return d.handleInput(msg)
		case appsDetail:
			return d.handleDetail(msg)
		case appsLog:
			return d.handleScroll(msg)
		case appsBackups:
			return d.handleBackups(msg)
		}
		return d.handleList(msg)
	}
	return nil
}

func (d *Apps) setNotice(text string, ok bool, detail string) {
	d.notice, d.noticeOK, d.detailOf = text, ok, detail
}

// done turns an operation result into a localized notice. The manager's
// own sentence follows as detail: it names the paths involved.
func (d *Apps) done(msg AppsDone) {
	if msg.Err != nil {
		headline := d.errorHeadline(msg.Err)
		if msg.Op == appOpVerify && errors.Is(msg.Err, localapps.ErrInvalidState) {
			headline = d.com.Text("Verification failed. The failed checks are listed above.")
		}
		d.setNotice(headline, false, appErrorDetail(msg.Err))
		return
	}
	var text string
	switch msg.Op {
	case appOpStart, appOpRestart:
		text = d.com.Text("The app is running.")
	case appOpStop:
		text = d.com.Text("The app was stopped.")
	case appOpVerify:
		text = d.com.Text("Verification finished.")
	case appOpBackup:
		text = d.com.Text("Backup created.")
	case appOpRestore:
		text = d.com.Text("Data restored from the backup.")
	case appOpRollback:
		text = d.com.Text("The update was rolled back.")
	case appOpUnregister:
		text = d.com.Text("Removed from the list; files and backups were kept.")
		d.view, d.appID = appsList, ""
	case appOpDelData:
		text = d.com.Text("The app's data was deleted; a backup was kept.")
	case appOpDelFiles:
		text = d.com.Text("The app's files were deleted; a data backup was kept.")
		d.view, d.appID = appsList, ""
	case appOpRegister:
		text = d.com.Text("The app was added to the list. Verify it before relying on it.")
	default:
		text = d.com.Text("Done.")
	}
	d.setNotice(text, true, msg.Detail)
}

// errorHeadline explains an operation error in the reader's language.
func (d *Apps) errorHeadline(err error) string {
	switch {
	case errors.Is(err, context.Canceled):
		return d.com.Text("The operation was canceled.")
	case errors.Is(err, localapps.ErrBusy):
		return d.com.Text("Another operation is running for this app.")
	case errors.Is(err, localapps.ErrRuntimeMissing):
		return d.com.Text("The runtime this app needs is not available.")
	case errors.Is(err, localapps.ErrPortInUse):
		return d.com.Text("Another program uses the app's port. Atlas does not stop programs it did not start.")
	case errors.Is(err, localapps.ErrEarlyExit):
		return d.com.Text("The app exited while starting. Read its log.")
	case errors.Is(err, localapps.ErrHealthTimeout):
		return d.com.Text("The app did not answer its health check in time.")
	case errors.Is(err, localapps.ErrNotOwned):
		return d.com.Text("This process was not started by Atlas, so Atlas will not stop it.")
	case errors.Is(err, localapps.ErrInvalidBackup), errors.Is(err, localapps.ErrIncompatible):
		return d.com.Text("The backup cannot be used; nothing was changed.")
	case errors.Is(err, localapps.ErrUnsafePath):
		return d.com.Text("Atlas refused an unsafe path; nothing was deleted.")
	case errors.Is(err, localapps.ErrNotFound):
		return d.com.Text("The app is no longer registered.")
	}
	return d.com.Text("The operation failed.")
}

func appErrorDetail(err error) string {
	var op *localapps.OpError
	if errors.As(err, &op) {
		if op.Remedy != "" {
			return op.Msg + " " + op.Remedy
		}
		return op.Msg
	}
	return err.Error()
}

func (d *Apps) handleList(msg tea.KeyPressMsg) Action {
	rows := len(d.apps) + 2
	switch {
	case key.Matches(msg, d.keyMap.Back):
		return ActionClose{}
	case key.Matches(msg, d.keyMap.Up):
		d.selected = max(0, d.selected-1)
	case key.Matches(msg, d.keyMap.Down):
		d.selected = min(rows-1, d.selected+1)
	case key.Matches(msg, d.keyMap.Enter):
		d.notice = ""
		switch {
		case d.selected == 0:
			d.openInput(appsInputCreate, "", d.com.Text("Describe the app: what it is for, what it stores, the main screens"))
		case d.selected == 1:
			d.openInput(appsInputRegister, "", d.com.Text("Absolute path of a folder with atlas-app.json"))
		case d.selected-2 < len(d.apps):
			d.appID, d.opSel, d.view = d.apps[d.selected-2].ID, 0, appsDetail
			return ActionCmd{Cmd: d.load()}
		}
	}
	return nil
}

func (d *Apps) openInput(kind appsInputKind, value, placeholder string) {
	d.backTo = d.view
	d.inputFor = kind
	d.input.SetValue(value)
	d.input.Placeholder = placeholder
	d.input.Focus()
	d.view = appsInput
}

func (d *Apps) handleInput(msg tea.KeyPressMsg) Action {
	switch msg.String() {
	case "esc", "alt+esc":
		d.input.Blur()
		d.view = d.backTo
		return nil
	case "enter":
		value := strings.TrimSpace(d.input.Value())
		if value == "" {
			return nil
		}
		d.input.Blur()
		d.view = d.backTo
		switch d.inputFor {
		case appsInputCreate:
			return ActionAppsCreate{Request: value}
		case appsInputUpdate:
			return ActionAppsUpdate{ID: d.appID, Request: value}
		case appsInputRegister:
			if !filepath.IsAbs(value) {
				d.setNotice(d.com.Text("Use an absolute folder path."), false, "")
				return nil
			}
			return ActionCmd{Cmd: d.run(appOpRegister, "", func(ctx context.Context) (string, error) {
				rec, err := d.mgr.Register(ctx, value)
				return rec.Root, err
			})}
		}
	}
	var cmd tea.Cmd
	d.input, cmd = d.input.Update(msg)
	return ActionCmd{Cmd: cmd}
}

// ops lists the operations offered for the open app.
func (d *Apps) ops() []string {
	app := d.detail
	if app == nil {
		return nil
	}
	var out []string
	if app.Busy != "" {
		if d.cancels[app.ID] != nil {
			out = append(out, appOpCancel)
		}
		return append(out, appOpLog)
	}
	if app.Running {
		out = append(out, appOpOpen, appOpStop, appOpRestart)
	} else if app.Manifest != nil {
		out = append(out, appOpStart)
	}
	if app.Manifest != nil {
		out = append(out, appOpVerify)
	}
	out = append(out, appOpLog)
	if app.Manifest != nil {
		out = append(out, appOpUpdate, appOpBackup, appOpRestore)
	}
	if app.Update != nil {
		out = append(out, appOpRollback)
	}
	return append(out, appOpUnregister, appOpDelData, appOpDelFiles)
}

func appOpLabel(op string) string {
	switch op {
	case appOpOpen:
		return "Open in browser"
	case appOpStart:
		return "Start"
	case appOpStop:
		return "Stop"
	case appOpRestart:
		return "Restart"
	case appOpVerify:
		return "Verify now (build, tests, start, data round trip)"
	case appOpLog:
		return "Show log"
	case appOpBackup:
		return "Back up now"
	case appOpRestore:
		return "Restore data from a backup…"
	case appOpUpdate:
		return "Change the app with the assistant…"
	case appOpRollback:
		return "Roll back the last update"
	case appOpUnregister:
		return "Remove from the list (keep files and data)"
	case appOpDelData:
		return "Delete the app's data…"
	case appOpDelFiles:
		return "Delete the app's files…"
	case appOpCancel:
		return "Cancel the running operation"
	}
	return op
}

func (d *Apps) handleDetail(msg tea.KeyPressMsg) Action {
	ops := d.ops()
	switch {
	case key.Matches(msg, d.keyMap.Back):
		d.view, d.notice = appsList, ""
		return ActionCmd{Cmd: d.load()}
	case key.Matches(msg, d.keyMap.Up):
		d.opSel = max(0, d.opSel-1)
	case key.Matches(msg, d.keyMap.Down):
		d.opSel = min(len(ops)-1, d.opSel+1)
	case key.Matches(msg, d.keyMap.Cancel):
		if c := d.cancels[d.appID]; c != nil {
			c()
		}
	case key.Matches(msg, d.keyMap.Enter):
		if d.opSel >= 0 && d.opSel < len(ops) && d.detail != nil {
			return d.runOp(*d.detail, ops[d.opSel])
		}
	}
	return nil
}

func (d *Apps) runOp(app localapps.App, op string) Action {
	d.notice = ""
	mgr, id := d.mgr, app.ID
	switch op {
	case appOpCancel:
		if c := d.cancels[id]; c != nil {
			c()
		}
		return nil
	case appOpOpen:
		return ActionAppsOpenURL{URL: app.URL}
	case appOpLog:
		d.view, d.scroll = appsLog, 1<<30
		return ActionCmd{Cmd: d.load()}
	case appOpRestore:
		d.view, d.backSel = appsBackups, 0
		return ActionCmd{Cmd: d.load()}
	case appOpUpdate:
		d.openInput(appsInputUpdate, "", d.com.Text("What should change?"))
		return nil
	case appOpStart:
		return ActionCmd{Cmd: d.run(op, id, func(ctx context.Context) (string, error) {
			res, err := mgr.Start(ctx, id, localapps.StartOptions{})
			return res.URL, err
		})}
	case appOpStop:
		return ActionCmd{Cmd: d.run(op, id, func(ctx context.Context) (string, error) { return mgr.Stop(ctx, id) })}
	case appOpRestart:
		return ActionCmd{Cmd: d.run(op, id, func(ctx context.Context) (string, error) {
			if _, err := mgr.Stop(ctx, id); err != nil {
				return "", err
			}
			res, err := mgr.Start(ctx, id, localapps.StartOptions{})
			return res.URL, err
		})}
	case appOpVerify:
		return ActionCmd{Cmd: d.run(op, id, func(ctx context.Context) (string, error) {
			v, err := mgr.Verify(ctx, id)
			if err != nil {
				return "", err
			}
			if !v.Passed {
				return "", &localapps.OpError{Code: localapps.ErrInvalidState, Msg: v.Error}
			}
			return "", nil
		})}
	case appOpBackup:
		return ActionCmd{Cmd: d.run(op, id, func(ctx context.Context) (string, error) {
			b, err := mgr.Backup(ctx, id, "manual", true)
			return b.Path, err
		})}
	case appOpRollback:
		d.ask(fmt.Sprintf(d.com.Text("Roll %s back to the files and data saved when the last update began? Changes made since then are replaced; the current state is backed up first."), app.Name), func() Action {
			return ActionCmd{Cmd: d.run(op, id, func(ctx context.Context) (string, error) { return mgr.Rollback(ctx, id) })}
		})
	case appOpUnregister:
		d.ask(fmt.Sprintf(d.com.Text("Remove %s from the app list? Atlas stops it if it started it. Files stay in %s, data and backups are kept."), app.Name, app.Root), func() Action {
			return ActionCmd{Cmd: d.run(op, id, func(ctx context.Context) (string, error) { return mgr.Unregister(ctx, id) })}
		})
	case appOpDelData:
		d.ask(fmt.Sprintf(d.com.Text("Delete all data of %s? The app is stopped, a backup is made first, then its data folders are emptied. The files stay and the app keeps working with empty data."), app.Name), func() Action {
			return ActionCmd{Cmd: d.run(op, id, func(ctx context.Context) (string, error) { return mgr.DeleteData(ctx, id) })}
		})
	case appOpDelFiles:
		d.ask(fmt.Sprintf(d.com.Text("Delete the folder %s? The app is stopped, a data backup is kept, then the folder is deleted and the app leaves the list."), app.Root), func() Action {
			return ActionCmd{Cmd: d.run(op, id, func(ctx context.Context) (string, error) { return mgr.DeleteFiles(ctx, id) })}
		})
	}
	return nil
}

// run executes an operation off the UI loop with a cancelable context.
func (d *Apps) run(op, id string, fn func(ctx context.Context) (string, error)) tea.Cmd {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	if id != "" {
		d.cancels[id] = cancel
	}
	return tea.Batch(
		func() tea.Msg {
			defer cancel()
			detail, err := fn(ctx)
			return AppsDone{ID: id, Op: op, Detail: detail, Err: err}
		},
		func() tea.Msg {
			time.Sleep(100 * time.Millisecond)
			return AppsTick{}
		},
	)
}

func (d *Apps) ask(text string, run func() Action) {
	d.backTo = d.view
	d.confirm = &integrationConfirm{text: text, run: run}
	d.view = appsConfirm
}

func (d *Apps) handleConfirm(msg tea.KeyPressMsg) Action {
	switch msg.String() {
	case "y", "Y":
		c := d.confirm
		d.confirm, d.view = nil, d.backTo
		if c != nil && c.run != nil {
			return c.run()
		}
	case "n", "N", "esc", "alt+esc":
		d.confirm, d.view = nil, d.backTo
	}
	return nil
}

func (d *Apps) handleScroll(msg tea.KeyPressMsg) Action {
	switch {
	case key.Matches(msg, d.keyMap.Back):
		d.view = appsDetail
	case key.Matches(msg, d.keyMap.Up):
		d.scroll = max(0, d.scroll-1)
	case key.Matches(msg, d.keyMap.Down):
		d.scroll++
	case msg.String() == "pgdown":
		d.scroll += 10
	case msg.String() == "pgup":
		d.scroll = max(0, d.scroll-10)
	}
	return nil
}

func (d *Apps) handleBackups(msg tea.KeyPressMsg) Action {
	switch {
	case key.Matches(msg, d.keyMap.Back):
		d.view = appsDetail
	case key.Matches(msg, d.keyMap.Up):
		d.backSel = max(0, d.backSel-1)
	case key.Matches(msg, d.keyMap.Down):
		d.backSel = min(len(d.backups)-1, d.backSel+1)
	case key.Matches(msg, d.keyMap.Enter):
		if d.backSel < 0 || d.backSel >= len(d.backups) || d.detail == nil {
			return nil
		}
		b, mgr, id, name := d.backups[d.backSel], d.mgr, d.detail.ID, d.detail.Name
		d.ask(fmt.Sprintf(d.com.Text("Replace the data of %s with the backup from %s? The current data is backed up first. The app's files are not changed."), name, d.com.Styles.RelativeTime(b.Meta.CreatedAt)), func() Action {
			return ActionCmd{Cmd: d.run(appOpRestore, id, func(ctx context.Context) (string, error) {
				return mgr.Restore(ctx, id, b.ID, localapps.RestoreOptions{})
			})}
		})
		d.backTo = appsDetail
	}
	return nil
}

func stateLabel(s localapps.State) string {
	switch s {
	case localapps.StateDesigning:
		return "planned"
	case localapps.StateScaffolded:
		return "not verified"
	case localapps.StateVerifying:
		return "verifying"
	case localapps.StateVerified:
		return "verified, acceptance pending"
	case localapps.StateReady:
		return "ready"
	case localapps.StateFailed:
		return "verification failed"
	case localapps.StateUpdating:
		return "update in progress"
	case localapps.StateUpdateFailed:
		return "update failed"
	}
	return string(s)
}

func (d *Apps) stateStyle(app localapps.App) (string, lipgloss.Style) {
	t := d.com.Styles
	switch {
	case app.ManifestError != "", app.State == localapps.StateFailed, app.State == localapps.StateUpdateFailed:
		return "✗", t.Dialog.OAuth.ErrorText
	case app.Busy != "":
		return "…", t.Dialog.TitleAccent
	case app.Running:
		return "●", t.Dialog.OAuth.Success
	case app.State == localapps.StateReady:
		return "○", t.Dialog.OAuth.Success
	}
	return "○", t.Dialog.SecondaryText.Padding(0)
}

// Draw implements Dialog.
func (d *Apps) Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	t := d.com.Styles
	width := max(0, min(100, area.Dx()-t.Dialog.View.GetHorizontalBorderSize()))
	inner := max(10, width-t.Dialog.View.GetHorizontalFrameSize())
	text := inner - 1
	rc := NewRenderContext(t, width)
	rc.Title = d.com.Text("Local apps")
	rc.Help = renderDialogHelp(t, &d.help, d, inner)
	height := max(4, area.Dy()-t.Dialog.View.GetVerticalFrameSize()-5)

	var cur *tea.Cursor
	if d.view == appsInput {
		d.input.SetWidth(dialogInputTextWidth(t, d.input, inner))
		rc.AddPart(t.Dialog.InputPrompt.Render(d.input.View()))
		cur = InputCursor(t, d.input.Cursor())
		height--
	}
	var lines []string
	focus := -1
	switch {
	case d.mgr == nil:
		lines = wrapLines(t.Dialog.PrimaryText.Padding(0), d.com.Text("Local apps are only available when Atlas runs locally."), text)
	case d.view == appsConfirm && d.confirm != nil:
		lines = wrapLines(t.Dialog.PrimaryText.Padding(0), d.confirm.text, text)
		lines = append(lines, "", t.Dialog.SecondaryText.Padding(0).Render(d.com.Text("y confirm · n cancel")))
	case d.view == appsInput:
		lines = d.inputLines(text)
	case d.view == appsDetail:
		lines, focus = d.detailLines(text)
	case d.view == appsLog:
		lines = d.logLines(text)
	case d.view == appsBackups:
		lines, focus = d.backupLines(text)
	default:
		lines, focus = d.listLines(text)
	}
	if focus < 0 {
		d.scroll = min(d.scroll, max(0, len(lines)-height))
		lines = window(lines, d.scroll, height)
	} else {
		lines = windowAround(lines, focus, height)
	}
	for i, l := range lines {
		lines[i] = " " + ansi.Truncate(l, text, "…")
	}
	rc.AddPart(strings.Join(lines, "\n"))
	DrawCenterCursor(scr, area, rc.Render(), cur)
	return cur
}

func (d *Apps) noticeLines(width int) []string {
	if d.notice == "" {
		return nil
	}
	t := d.com.Styles
	style := t.Dialog.OAuth.ErrorText
	if d.noticeOK {
		style = t.Dialog.OAuth.Success
	}
	lines := wrapLines(style, d.notice, width)
	if d.detailOf != "" {
		lines = append(lines, wrapLines(t.Dialog.SecondaryText.Padding(0), d.detailOf, width)...)
	}
	return lines
}

func (d *Apps) inputLines(width int) []string {
	sec := d.com.Styles.Dialog.SecondaryText.Padding(0)
	var hint string
	switch d.inputFor {
	case appsInputCreate:
		hint = d.com.Text("The assistant plans the app, writes it, verifies it with real data and starts it on 127.0.0.1. You can follow and answer in the chat. Supported: Python 3.9+ web apps with SQLite (standard library only).")
	case appsInputUpdate:
		hint = d.com.Text("The assistant backs up the files and data, makes the change, verifies it against a copy of the real data and only then accepts it.")
	case appsInputRegister:
		hint = d.com.Text("Only folders with a valid atlas-app.json for a supported runtime can be added. Nothing is copied or changed.")
	}
	return append(wrapLines(sec, hint, width), "", sec.Render(d.com.Text("enter confirm · esc cancel")))
}

func (d *Apps) listLines(width int) ([]string, int) {
	t := d.com.Styles
	sec := t.Dialog.SecondaryText.Padding(0)
	lines := d.noticeLines(width)
	if d.loadErr != nil {
		lines = append(lines, wrapLines(t.Dialog.OAuth.ErrorText, d.com.Text("The app registry could not be read:")+" "+d.loadErr.Error(), width)...)
	}
	if !d.loaded {
		return append(lines, sec.Render(d.com.Text("Loading…"))), -1
	}
	if len(d.apps) == 0 {
		lines = append(lines, wrapLines(sec, d.com.Text("No local apps yet. Atlas builds small web apps that run on this computer and keep their data in SQLite."), width)...)
		if rt := d.runtime; rt != nil {
			if rt.Available {
				lines = append(lines, t.Dialog.OAuth.Success.Render(fmt.Sprintf(d.com.Text("Python %s found: apps can be created."), rt.Version)))
			} else {
				lines = append(lines, wrapLines(t.Dialog.OAuth.ErrorText, d.com.Text("Python 3.9 or newer is needed to create apps.")+" "+rt.Remedy, width)...)
			}
		}
	}
	lines = append(lines, "")
	rows := []string{d.com.Text("New app…"), d.com.Text("Add an existing app folder…")}
	for _, app := range d.apps {
		icon, style := d.stateStyle(app)
		status := d.com.Text(stateLabel(app.State))
		switch {
		case app.Busy != "":
			status = d.com.Text("working…")
		case app.Running:
			status = d.com.Text("running") + " · " + app.URL
		case app.ManifestError != "":
			status = d.com.Text("manifest problem")
		}
		rows = append(rows, style.Render(icon+" ")+app.Name+" "+style.Render(status))
	}
	d.selected = min(max(0, d.selected), len(rows)-1)
	focus := len(lines)
	for i, r := range rows {
		if i == d.selected {
			focus = len(lines)
			lines = append(lines, t.Dialog.SelectedItem.Width(width).Render(stripStyles(r)))
		} else {
			lines = append(lines, t.Dialog.NormalItem.Width(width).Render(r))
		}
		if i == 1 && len(d.apps) > 0 {
			lines = append(lines, "")
		}
	}
	return lines, focus
}

func (d *Apps) detailLines(width int) ([]string, int) {
	t := d.com.Styles
	sec := t.Dialog.SecondaryText.Padding(0)
	prim := t.Dialog.PrimaryText.Padding(0)
	app := d.detail
	if app == nil {
		if !d.loaded {
			return []string{sec.Render(d.com.Text("Loading…"))}, -1
		}
		return append(d.noticeLines(width), sec.Render(d.com.Text("The app is no longer registered."))), -1
	}
	icon, style := d.stateStyle(*app)
	lines := []string{t.Dialog.TitleAccent.Render(app.Name) + sec.Render("  "+app.ID)}
	status := d.com.Text(stateLabel(app.State))
	if app.Running {
		status = d.com.Text("running") + " · " + app.URL
		if app.Owner == "previous_session" {
			status += " · " + d.com.Text("started by an earlier Atlas run")
		}
	}
	lines = append(lines, style.Render(icon+" "+status))
	if app.Description != "" {
		lines = append(lines, wrapLines(sec, app.Description, width)...)
	}
	lines = append(lines, "")
	add := func(label, value string) {
		if value != "" {
			lines = append(lines, wrapLines(prim, d.com.Text(label)+" "+value, width)...)
		}
	}
	add("Folder:", app.Root)
	add("Technology:", app.Tech)
	if app.Manifest != nil {
		add("Data:", strings.Join(app.Manifest.DataDirs, ", "))
	}
	if v := app.LastVerification; v != nil {
		result := d.com.Text("passed")
		if !v.Passed {
			result = d.com.Text("failed")
		}
		add("Last verification:", result+" ("+t.RelativeTime(v.At)+")")
		for _, c := range v.Checks {
			mark, cs := "✓", t.Dialog.OAuth.Success
			if !c.Passed {
				mark, cs = "✗", t.Dialog.OAuth.ErrorText
			}
			line := cs.Render(mark+" ") + c.Name
			if c.Detail != "" {
				line += sec.Render(" — " + c.Detail)
			}
			lines = append(lines, "  "+line)
		}
	} else {
		add("Last verification:", d.com.Text("never"))
	}
	if app.SourceChanged {
		lines = append(lines, wrapLines(t.Dialog.TitleAccent, d.com.Text("Files changed after the last verification; verify again."), width)...)
	}
	if len(app.PendingAcceptance) > 0 {
		add("Acceptance checks pending:", strings.Join(app.PendingAcceptance, "; "))
	}
	add("Last error:", app.LastError)
	for _, diag := range app.Diagnostics {
		lines = append(lines, wrapLines(t.Dialog.OAuth.ErrorText, diag, width)...)
	}
	if j := d.journal; j != nil && (app.Busy != "" || !j.Finished.IsZero() && time.Since(j.Finished) < 10*time.Minute) {
		lines = append(lines, "", prim.Render(fmt.Sprintf(d.com.Text("Operation: %s"), j.Op)))
		for _, s := range j.Stages {
			mark, cs := "…", t.Dialog.TitleAccent
			switch s.State {
			case localapps.StagePassed:
				mark, cs = "✓", t.Dialog.OAuth.Success
			case localapps.StageFailed:
				mark, cs = "✗", t.Dialog.OAuth.ErrorText
			}
			line := cs.Render(mark+" ") + string(s.Name)
			if s.Detail != "" {
				line += sec.Render(" — " + s.Detail)
			}
			lines = append(lines, "  "+line)
		}
	}
	if n := d.noticeLines(width); len(n) > 0 {
		lines = append(lines, "")
		lines = append(lines, n...)
	}
	ops := d.ops()
	lines = append(lines, "")
	d.opSel = min(max(0, d.opSel), len(ops)-1)
	focus := len(lines)
	for i, op := range ops {
		label := d.com.Text(appOpLabel(op))
		if i == d.opSel {
			focus = len(lines)
			lines = append(lines, t.Dialog.SelectedItem.Width(width).Render(label))
		} else {
			lines = append(lines, t.Dialog.NormalItem.Width(width).Render(label))
		}
	}
	return lines, focus
}

func (d *Apps) logLines(width int) []string {
	sec := d.com.Styles.Dialog.SecondaryText.Padding(0)
	if len(d.log) == 0 {
		return []string{sec.Render(d.com.Text("The log is empty."))}
	}
	var lines []string
	for _, l := range d.log {
		lines = append(lines, ansi.Truncate(ansi.Strip(l), width, "…"))
	}
	return lines
}

func (d *Apps) backupLines(width int) ([]string, int) {
	t := d.com.Styles
	sec := t.Dialog.SecondaryText.Padding(0)
	if len(d.backups) == 0 {
		return []string{sec.Render(d.com.Text("No backups yet."))}, -1
	}
	lines := wrapLines(sec, d.com.Text("Choose a backup. Only the data is restored; the current data is backed up first."), width)
	lines = append(lines, "")
	d.backSel = min(max(0, d.backSel), len(d.backups)-1)
	focus := len(lines)
	for i, b := range d.backups {
		label := t.RelativeTime(b.Meta.CreatedAt) + " · " + b.Meta.Reason + " · " + humanSize(b.Size)
		if i == d.backSel {
			focus = len(lines)
			lines = append(lines, t.Dialog.SelectedItem.Width(width).Render(label))
		} else {
			lines = append(lines, t.Dialog.NormalItem.Width(width).Render(label))
		}
	}
	return lines, focus
}

func humanSize(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}

// ShortHelp implements help.KeyMap.
func (d *Apps) ShortHelp() []key.Binding {
	switch d.view {
	case appsConfirm, appsInput:
		return []key.Binding{d.keyMap.Back}
	case appsLog:
		return []key.Binding{d.keyMap.Up, d.keyMap.Down, d.keyMap.Back}
	case appsDetail:
		if d.cancels[d.appID] != nil {
			return []key.Binding{d.keyMap.Up, d.keyMap.Down, d.keyMap.Enter, d.keyMap.Cancel, d.keyMap.Back}
		}
		return []key.Binding{d.keyMap.Up, d.keyMap.Down, d.keyMap.Enter, d.keyMap.Back}
	case appsBackups:
		return []key.Binding{d.keyMap.Up, d.keyMap.Down, d.keyMap.Enter, d.keyMap.Back}
	}
	return []key.Binding{d.keyMap.Up, d.keyMap.Down, d.keyMap.Enter, CloseKey}
}

// FullHelp implements help.KeyMap.
func (d *Apps) FullHelp() [][]key.Binding { return [][]key.Binding{d.ShortHelp()} }
