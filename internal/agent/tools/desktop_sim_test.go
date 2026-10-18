package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/computer"
	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm/schema"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/interaction"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/permission"
	"github.com/stretchr/testify/require"
)

// simDesktop is a scripted Windows desktop for end-to-end workflow tests. It
// answers the same native shapes as the PowerShell automation backend:
// window lists, UIA inspect/find/assert, focus, launch, select, set_value,
// invoke and keyboard input. Calculator computes with a Turkish display,
// Notepad saves through an owned "Farklı Kaydet" dialog, and Explorer hides
// the .txt extension in its item labels while the shell view keeps the real
// file-system name.
type simDesktop struct {
	fakeComputerBackend
	mu         sync.Mutex
	windows    []*simWindow
	foreground string
	nextID     int
	files      map[string]string
	desktopDir string
	// onDisk mirrors every saved, renamed or removed file to desktopDir on
	// the real file system, so file_check nodes read what the apps wrote.
	onDisk   bool
	inputs   []string
	launches int
}

type simWindow struct {
	info desktopWindowInfo
	kind string
	// Calculator.
	acc   int
	op    byte
	entry string
	shown int
	// Notepad.
	doc       string
	file      string
	dirty     bool
	selectAll bool
	// Save dialog and Explorer.
	folder   string
	fileName string
	owner    *simWindow
	selected string
	editing  bool
	editor   string
}

func newSimDesktop() *simDesktop {
	d := &simDesktop{files: map[string]string{}, desktopDir: `C:\Users\Test\OneDrive\Masaüstü`, nextID: 1000}
	d.size = computer.Size{Width: 1920, Height: 1080}
	d.screenshot = nil
	return d
}

func (d *simDesktop) open(kind, title string, pid uint32, process, class string) *simWindow {
	d.nextID++
	w := &simWindow{kind: kind, info: desktopWindowInfo{ID: strconv.Itoa(d.nextID), Name: title, ProcessID: pid, ProcessName: process, WindowClass: class, X: 200, Y: 100, Width: 900, Height: 700}}
	d.windows = append(d.windows, w)
	d.foreground = w.info.ID
	return w
}

func (d *simDesktop) window(id string) *simWindow {
	for _, w := range d.windows {
		if w.info.ID == id {
			return w
		}
	}
	return nil
}

func (d *simDesktop) close(w *simWindow) {
	d.windows = slices.DeleteFunc(d.windows, func(c *simWindow) bool { return c == w })
	if d.foreground == w.info.ID {
		d.foreground = ""
		if w.owner != nil && d.window(w.owner.info.ID) != nil {
			d.foreground = w.owner.info.ID
		} else if len(d.windows) > 0 {
			d.foreground = d.windows[len(d.windows)-1].info.ID
		}
	}
}

func (d *simDesktop) list() []desktopWindowInfo {
	out := make([]desktopWindowInfo, 0, len(d.windows))
	for _, w := range d.windows {
		info := w.info
		info.Foreground = info.ID == d.foreground
		if w.owner != nil {
			info.Owner = w.owner.info.ID
		}
		out = append(out, info)
	}
	return out
}

func (d *simDesktop) ListWindows(context.Context) (json.RawMessage, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return json.Marshal(d.list())
}

func (d *simDesktop) ForegroundWindow() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.foreground
}

func (d *simDesktop) Origin() computer.Point { return computer.Point{} }

func formatTR(n int) string {
	digits := strconv.Itoa(n)
	var b strings.Builder
	for i, r := range digits {
		if i > 0 && (len(digits)-i)%3 == 0 {
			b.WriteByte('.')
		}
		b.WriteRune(r)
	}
	return b.String()
}

func explorerDisplay(leaf string) string {
	if ext := path.Ext(leaf); ext == ".txt" {
		return strings.TrimSuffix(leaf, ext)
	}
	return leaf
}

func (d *simDesktop) folderItems(folder string) []string {
	var out []string
	for full := range d.files {
		dir, leaf, _ := strings.Cut(reverse(full), `\`)
		if reverse(leaf) == folder {
			out = append(out, reverse(dir))
		}
	}
	slices.Sort(out)
	return out
}

func reverse(s string) string {
	r := []rune(s)
	slices.Reverse(r)
	return string(r)
}

// elements describes a window's UIA tree, the root first as the native
// traversal does.
func (d *simDesktop) elements(w *simWindow) []desktopElement {
	pid := int(w.info.ProcessID)
	focused := w.info.ID == d.foreground
	root := desktopElement{ID: w.info.ID + ":root", Name: w.info.Name, Role: "ControlType.Window", ProcessID: pid, Enabled: true, X: w.info.X, Y: w.info.Y, Width: w.info.Width, Height: w.info.Height}
	out := []desktopElement{root}
	switch w.kind {
	case "calculator":
		out = append(out, desktopElement{ID: w.info.ID + ":result", Name: "Ekran değeri " + formatTR(w.shown), Role: "ControlType.Text", AutomationID: "CalculatorResults", ProcessID: pid, Enabled: true, X: 300, Y: 200, Width: 400, Height: 60})
	case "notepad":
		out = append(out, desktopElement{ID: w.info.ID + ":doc", Name: "Metin düzenleyicisi", Role: "ControlType.Document", ProcessID: pid, Enabled: true, Patterns: []string{"Text"}, Text: w.doc, TextAvailable: true, KeyboardFocused: focused, X: 220, Y: 160, Width: 860, Height: 600})
	case "savedialog":
		out = append(out,
			desktopElement{ID: w.info.ID + ":name", Name: "Dosya adı:", Role: "ControlType.Edit", ProcessID: pid, Enabled: true, Patterns: []string{"Value"}, Value: w.fileName, ValueAvailable: true, KeyboardFocused: focused, X: 400, Y: 600, Width: 400, Height: 24},
			desktopElement{ID: w.info.ID + ":save", Name: "Kaydet", Role: "ControlType.Button", ProcessID: pid, Enabled: true, Patterns: []string{"Invoke"}, X: 820, Y: 640, Width: 80, Height: 24})
	case "prompt":
		out = append(out, desktopElement{ID: w.info.ID + ":save", Name: "Kaydet", Role: "ControlType.Button", ProcessID: pid, Enabled: true, Patterns: []string{"Invoke"}, X: 500, Y: 400, Width: 80, Height: 24})
	case "explorer":
		for i, leaf := range d.folderItems(w.folder) {
			item := desktopElement{ID: w.info.ID + ":item:" + leaf, Name: explorerDisplay(leaf), Role: "ControlType.ListItem", ProcessID: pid, Enabled: true, Patterns: []string{"Invoke", "SelectionItem"}, X: 300, Y: float64(200 + 30*i), Width: 400, Height: 30}
			item.KeyboardFocused = focused && !w.editing && w.selected == leaf
			out = append(out, item)
			if w.editing && w.selected == leaf {
				out = append(out, desktopElement{ID: w.info.ID + ":editor", Name: w.editor, Role: "ControlType.Edit", ProcessID: pid, Enabled: true, Patterns: []string{"Value"}, Value: w.editor, ValueAvailable: true, KeyboardFocused: focused, X: 310, Y: float64(202 + 30*i), Width: 200, Height: 25})
			}
		}
	}
	return out
}

func (d *simDesktop) focused(w *simWindow) *desktopElement {
	for _, e := range d.elements(w) {
		if e.KeyboardFocused {
			return &e
		}
	}
	return nil
}

func matchesSelector(e desktopElement, q computer.AutomationRequest) bool {
	role, _ := computer.NormalizeRole(q.Role)
	return (q.ElementID == "" || e.ID == q.ElementID) && (q.Name == "" || e.Name == q.Name) && (role == "" || e.Role == role)
}

func result(v any) (json.RawMessage, error) { return json.Marshal(v) }

func (d *simDesktop) Automation(_ context.Context, q computer.AutomationRequest) (json.RawMessage, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	switch q.Action {
	case "windows":
		return result(d.list())
	case "launch_app":
		d.launches++
		d.inputs = append(d.inputs, "launch_app:"+q.Name)
		switch strings.ToLower(q.Name) {
		case "hesap makinesi", "calculator":
			d.open("calculator", "Hesap Makinesi", 900, "ApplicationFrameHost.exe", "ApplicationFrameWindow")
			return result(map[string]any{"launch_requested": true, "application": "Hesap Makinesi", "verify_required": true})
		case "not defteri", "notepad":
			w := d.open("notepad", "Adsız - Not Defteri", uint32(5000+d.nextID), "notepad.exe", "Notepad")
			_ = w
			return result(map[string]any{"launch_requested": true, "application": "Not Defteri", "verify_required": true})
		}
		return nil, fmt.Errorf("target_missing: application is not installed")
	}
	w := d.window(q.WindowID)
	if w == nil {
		return nil, fmt.Errorf("target_missing: window disappeared; inspect windows again")
	}
	if q.Action == "focus" {
		d.foreground = w.info.ID
		d.inputs = append(d.inputs, "focus:"+w.info.ID)
		return result(map[string]any{"focused": true, "window_id": w.info.ID})
	}
	elements := d.elements(w)
	if q.Action == "inspect" {
		o := map[string]any{"window_id": w.info.ID, "truncated": false, "scanned_count": len(elements), "elements": elements, "focused_element": d.focused(w)}
		if q.MaxElements > 0 && q.MaxElements < len(elements) {
			o["elements"], o["truncated"] = elements[:q.MaxElements], true
		}
		if w.kind == "explorer" {
			o["explorer_location"] = map[string]any{"window_id": w.info.ID, "process_id": w.info.ProcessID, "path": w.folder, "source": "shell_current_folder", "verified": true}
		}
		return result(o)
	}
	matches := []desktopElement{}
	for _, e := range elements {
		if matchesSelector(e, q) {
			matches = append(matches, e)
		}
	}
	if q.Action == "find" {
		return result(map[string]any{"count": len(matches), "truncated": false, "scanned_count": len(elements), "matches": matches})
	}
	if q.Action == "assert" && q.Condition == "hidden" {
		return result(map[string]any{"passed": len(matches) == 0, "actual": len(matches) == 0, "condition": "hidden", "window_id": w.info.ID})
	}
	if q.Action == "assert" && len(matches) == 0 {
		return result(map[string]any{"passed": false, "reason": "target_missing"})
	}
	if len(matches) != 1 {
		return nil, fmt.Errorf("Target ambiguous")
	}
	e := matches[0]
	switch q.Action {
	case "assert":
		var actual any
		passed := false
		switch q.Condition {
		case "visible":
			actual, passed = !e.Offscreen, !e.Offscreen
		case "enabled":
			actual, passed = e.Enabled, e.Enabled
		case "keyboard_focused":
			actual, passed = e.KeyboardFocused, e.KeyboardFocused
		case "text":
			actual, passed = e.Name, e.Name == q.Expected
		case "value":
			actual, passed = e.Value, e.Value == q.Expected
		case "document_text":
			actual, passed = e.Text, e.Text == q.Expected
		case "file_name":
			leaf, ok := strings.CutPrefix(e.ID, w.info.ID+":item:")
			if w.kind != "explorer" || !ok {
				return nil, fmt.Errorf("File name unavailable")
			}
			return result(map[string]any{"passed": leaf == q.Expected, "actual": leaf, "actual_truncated": false, "condition": "file_name", "window_id": w.info.ID, "folder_path": w.folder, "source": "shell_view_item"})
		default:
			return nil, fmt.Errorf("Unsupported assertion condition")
		}
		return result(map[string]any{"passed": passed, "actual": actual, "actual_truncated": false, "condition": q.Condition, "window_id": w.info.ID})
	case "select":
		leaf, ok := strings.CutPrefix(e.ID, w.info.ID+":item:")
		if !ok {
			return nil, fmt.Errorf("Pattern unavailable: SelectionItem")
		}
		w.selected, w.editing = leaf, false
		d.inputs = append(d.inputs, "select:"+leaf)
		return result(map[string]any{"selected": true, "selection_count": 1, "keyboard_focused": true, "element_id": e.ID, "window_id": w.info.ID})
	case "set_value":
		if w.kind != "savedialog" || e.Role != "ControlType.Edit" {
			return nil, fmt.Errorf("Pattern unavailable: Value")
		}
		w.fileName = q.Text
		d.inputs = append(d.inputs, "set_value:"+q.Text)
		return result(map[string]any{"element_id": e.ID, "value_verified": true, "keyboard_focused": true, "window_id": w.info.ID})
	case "invoke":
		d.inputs = append(d.inputs, "invoke:"+e.Name)
		if leaf, ok := strings.CutPrefix(e.ID, w.info.ID+":item:"); ok {
			full := w.folder + `\` + leaf
			n := d.open("notepad", explorerDisplay(leaf)+" - Not Defteri", uint32(5000+d.nextID), "notepad.exe", "Notepad")
			n.doc, n.file = d.files[full], full
			return result(map[string]any{"action_sent": true, "verify_required": true})
		}
		if e.Name == "Kaydet" && w.kind == "savedialog" {
			d.saveDialog(w)
			return result(map[string]any{"action_sent": true, "verify_required": true})
		}
		return nil, fmt.Errorf("Pattern unavailable: Invoke")
	}
	return nil, fmt.Errorf("unsupported simulated action %s", q.Action)
}

// put stores a file the simulated apps wrote, on disk too when onDisk.
func (d *simDesktop) put(full, content string) {
	d.files[full] = content
	if d.onDisk {
		_ = os.MkdirAll(filepath.Dir(full), 0o755)
		_ = os.WriteFile(full, []byte(content), 0o644)
	}
}

// drop removes a file the simulated apps renamed away.
func (d *simDesktop) drop(full string) {
	delete(d.files, full)
	if d.onDisk {
		_ = os.Remove(full)
	}
}

func (d *simDesktop) saveDialog(dialog *simWindow) {
	name := dialog.fileName
	if path.Ext(name) == "" {
		name += ".txt"
	}
	full := dialog.folder + `\` + name
	editor := dialog.owner
	d.put(full, editor.doc)
	editor.file, editor.dirty = full, false
	editor.info.Name = explorerDisplay(name) + " - Not Defteri"
	d.close(dialog)
}

func (d *simDesktop) TypeText(text string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.inputs = append(d.inputs, "type:"+text)
	w := d.window(d.foreground)
	if w == nil {
		return nil
	}
	switch w.kind {
	case "calculator":
		for i := 0; i < len(text); i++ {
			w.calculate(text[i])
		}
	case "notepad":
		if w.selectAll {
			w.doc, w.selectAll = "", false
		}
		w.doc += text
		w.dirty = true
	case "explorer":
		if w.editing {
			w.editor = text
		}
	case "savedialog":
		w.fileName = text
	}
	return nil
}

func (w *simWindow) calculate(c byte) {
	apply := func() {
		value, _ := strconv.Atoi(w.entry)
		switch w.op {
		case 0:
			if w.entry != "" {
				w.acc = value
			}
		case '+':
			w.acc += value
		case '-':
			w.acc -= value
		case '*':
			w.acc *= value
		case '/':
			if value != 0 {
				w.acc /= value
			}
		}
		w.entry = ""
	}
	switch {
	case c >= '0' && c <= '9':
		w.entry += string(c)
		w.shown, _ = strconv.Atoi(w.entry)
	case c == '+' || c == '-' || c == '*' || c == '/':
		if w.entry != "" {
			apply()
		}
		w.op = c
		w.shown = w.acc
	case c == '=':
		apply()
		w.op = 0
		w.shown = w.acc
	}
}

func (d *simDesktop) KeyPress(key string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.inputs = append(d.inputs, "key:"+key)
	w := d.window(d.foreground)
	if w == nil {
		return nil
	}
	switch {
	case w.kind == "calculator" && key == "enter":
		w.calculate('=')
	case w.kind == "savedialog" && key == "enter":
		d.saveDialog(w)
	case w.kind == "explorer" && key == "f2" && w.selected != "":
		w.editing, w.editor = true, explorerDisplay(w.selected)
	case w.kind == "explorer" && key == "enter" && w.editing:
		old := w.folder + `\` + w.selected
		leaf := w.editor
		if ext := path.Ext(w.selected); ext == ".txt" {
			leaf += ext
		}
		d.put(w.folder+`\`+leaf, d.files[old])
		d.drop(old)
		w.selected, w.editing = leaf, false
	}
	return nil
}

func (d *simDesktop) Hotkey(modifiers []string, key string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	chord := strings.Join(modifiers, "+") + "+" + key
	d.inputs = append(d.inputs, "hotkey:"+chord)
	w := d.window(d.foreground)
	if w == nil {
		return nil
	}
	switch chord {
	case "alt+f4":
		if w.kind == "notepad" && w.dirty {
			prompt := d.open("prompt", "Not Defteri", w.info.ProcessID, "notepad.exe", "#32770")
			prompt.owner = w
			return nil
		}
		d.close(w)
	case "ctrl+a":
		w.selectAll = w.kind == "notepad"
	case "ctrl+s":
		if w.kind != "notepad" {
			return nil
		}
		if w.file != "" {
			d.put(w.file, w.doc)
			w.dirty = false
			return nil
		}
		dialog := d.open("savedialog", "Farklı Kaydet", w.info.ProcessID, "notepad.exe", "#32770")
		dialog.owner, dialog.folder, dialog.fileName = w, d.desktopDir+`\Atlas-Test`, "*.txt"
	}
	return nil
}

// simTool runs the complete computer tool against the simulated desktop
// with short settle bounds, and dispatches pipeline children the way the
// coordinator does: schema validation against the advertised computer
// schema, then the tool's own permission, guard and lease checks.
type simTool struct {
	state   *computerToolState
	desktop *simDesktop
	tool    fantasy.AgentTool
	spec    schema.Schema
	calls   []ComputerParams
	mu      sync.Mutex
}

func newSimTool(t *testing.T, d *simDesktop, permissions permission.Service) *simTool {
	t.Helper()
	if permissions == nil {
		permissions = permission.NewPermissionService(t.TempDir(), true, nil)
	}
	state := &computerToolState{permissions: permissions, workingDir: t.TempDir(), backend: d, enabled: func() bool { return true }, actionTimeout: 10 * time.Second, settleTiming: desktopSettleTiming{MinSettle: 5 * time.Millisecond, Poll: 5 * time.Millisecond, ReadSettle: 30 * time.Millisecond, OpenWait: 60 * time.Millisecond, LaunchWait: 100 * time.Millisecond, Track: 10 * time.Second}}
	state.controller = interaction.New()
	tool := newComputerToolWithState(state, "computer")
	data, err := json.Marshal(fantasy.PrepareFunctionTool(tool).InputSchema)
	require.NoError(t, err)
	var spec schema.Schema
	require.NoError(t, json.Unmarshal(data, &spec))
	return &simTool{desktop: d, tool: tool, spec: spec, state: state}
}

func TestIndependentSimulatedDesktopsDoNotShareLease(t *testing.T) {
	t.Parallel()
	first := newSimTool(t, newSimDesktop(), nil)
	second := newSimTool(t, newSimDesktop(), nil)
	ctx := simContext(t)
	release, err := first.state.interactions().Acquire(ctx, "first", "desktop")
	require.NoError(t, err)
	defer release()
	bounded, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	response, err := second.invoke(bounded, fantasy.ToolCall{ID: "windows", Name: ComputerToolName, Input: `{"action":"windows"}`})
	require.NoError(t, err)
	require.False(t, response.IsError, response.Content)
}

func (s *simTool) invoke(ctx context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
	if call.Name != ComputerToolName {
		return fantasy.NewTextErrorResponse("Tool is disabled or unavailable: " + call.Name), nil
	}
	var arguments any
	if err := json.Unmarshal([]byte(call.Input), &arguments); err != nil {
		return fantasy.NewTextErrorResponse("Invalid pipeline arguments"), nil
	}
	if err := schema.ValidateAgainstSchema(arguments, s.spec); err != nil {
		return SchemaViolationResponse(call, err), nil
	}
	var p ComputerParams
	_ = json.Unmarshal([]byte(call.Input), &p)
	s.mu.Lock()
	s.calls = append(s.calls, p)
	s.mu.Unlock()
	return s.tool.Run(ctx, call)
}

func (s *simTool) pipeline(t *testing.T, ctx context.Context, input string) fantasy.ToolResponse {
	t.Helper()
	resp, err := NewToolPipeline(s.invoke).Run(ctx, fantasy.ToolCall{ID: "pipeline", Name: ToolPipelineName, Input: input})
	require.NoError(t, err)
	return resp
}

func simContext(t *testing.T) context.Context {
	t.Helper()
	ctx := context.WithValue(t.Context(), SessionIDContextKey, "sim-"+t.Name())
	desktopTransitions.Lock()
	delete(desktopTransitions.sessions, desktopFlowSession(ctx))
	desktopTransitions.Unlock()
	return ctx
}
