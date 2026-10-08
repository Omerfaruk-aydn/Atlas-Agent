package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/computer"
	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/stretchr/testify/require"
)

// calcNotepad is a scripted desktop with the calculator of session 6c6446a0
// (window 21, display "Ekran değeri 9.000") and a Notepad field (window 11).
type calcNotepad struct {
	t         *testing.T
	display   string
	field     string
	mutations int
	reads     map[string]int
	// foreground is the window a focus child last brought forward.
	foreground string
	// failWrite makes the next set_value report an error after applying it.
	failWrite bool
	// failAsserts makes every assert report an unavailable read.
	failAsserts bool
}

func newCalcNotepad(t *testing.T) *calcNotepad {
	return &calcNotepad{t: t, display: "Ekran değeri 9.000", field: "", reads: map[string]int{}, foreground: "21"}
}

func (d *calcNotepad) invoke(ctx context.Context, c fantasy.ToolCall) (fantasy.ToolResponse, error) {
	var p ComputerParams
	require.NoError(d.t, json.Unmarshal([]byte(c.Input), &p))
	reply := func(v any) (fantasy.ToolResponse, error) {
		data, err := json.Marshal(v)
		require.NoError(d.t, err)
		return fantasy.NewTextResponse(string(data)), nil
	}
	switch p.Action {
	case "file_check":
		return runDesktopFileCheck(ctx, *p.File), nil
	case "windows":
		return reply(map[string]any{"result": []map[string]any{
			{"window_id": "21", "name": "Hesap Makinesi", "process_id": 30, "process_name": "ApplicationFrameHost.exe", "class_name": "ApplicationFrameWindow", "foreground": d.foreground == "21"},
			{"window_id": "11", "name": "Untitled - Notepad", "process_id": 17, "process_name": "Notepad.exe", "class_name": "Notepad", "foreground": d.foreground == "11"},
		}})
	case "focus":
		d.foreground = p.Automation.WindowID
		return fantasy.NewTextResponse(`{"result":{"focused":true}}`), nil
	case "find":
		d.reads[p.Automation.WindowID]++
		var e desktopElement
		if p.Automation.WindowID == "21" {
			e = desktopElement{ID: "display", Name: d.display, Role: "ControlType.Text", Enabled: true, Width: 300, Height: 60}
		} else {
			e = desktopElement{ID: "fresh", Name: "Field", Role: "ControlType.Edit", Enabled: true, ValueAvailable: true, Value: d.field, Width: 30, Height: 20}
		}
		return reply(map[string]any{"result": map[string]any{"matches": []desktopElement{e}, "truncated": false}, "screen_origin": computer.Point{}})
	case "set_value":
		require.Equal(d.t, "11", p.Automation.WindowID, "input goes only to the bound Notepad window")
		d.mutations++
		d.field = p.Automation.Text
		if d.failWrite {
			d.failWrite = false
			return fantasy.NewTextErrorResponse("provider disconnected after input"), nil
		}
		return fantasy.NewTextResponse(`{"result":{"action_sent":true}}`), nil
	case "assert":
		if d.failAsserts {
			return fantasy.NewTextErrorResponse("read unavailable"), nil
		}
		actual := d.field
		if p.Automation.WindowID == "21" {
			actual = d.display
		}
		return reply(map[string]any{"window_id": p.Automation.WindowID, "passed": actual == p.Automation.Expected, "actual": actual})
	}
	d.t.Fatalf("Unexpected action %s", p.Action)
	return fantasy.ToolResponse{}, nil
}

// calcToNotepadFlow reads the calculator result, which Explorer and Turkish
// calculators show as "9.000", and writes it into Notepad as plain 9000.
func calcToNotepadFlow() DesktopWorkflowParams {
	return DesktopWorkflowParams{Mode: "flow", Flow: &DesktopFlowParams{Nodes: []DesktopFlowNode{
		{ID: "calc", Kind: "resolve", Application: "Hesap Makinesi", Next: "read"},
		{
			ID: "read", Kind: "verify", WindowRef: "calc", Next: "pad",
			Checkpoint: computer.AutomationRequest{Role: "ControlType.Text", Condition: "text"},
			Capture:    &DesktopFlowCapture{Name: "result", Pattern: `^Ekran değeri (.+)$`, Number: &DesktopNumberFormat{Thousands: ".", Decimal: ","}},
		},
		{ID: "pad", Kind: "resolve", Application: "Notepad", Next: "write"},
		{
			ID: "write", Kind: "operation", WindowRef: "pad", Focus: true,
			Input:      ComputerParams{Action: "set_value", Automation: computer.AutomationRequest{Name: "Field", Text: "Sonuç: {{result}}"}},
			Checkpoint: computer.AutomationRequest{Name: "Field", Condition: "value", Expected: "Sonuç: {{result}}"},
		},
	}}}
}

func TestDesktopFlowCaptureCarriesReadValueIntoLaterInput(t *testing.T) {
	t.Parallel()
	d := newCalcNotepad(t)
	r, err := runDesktopWorkflow(t.Context(), calcToNotepadFlow(), fantasy.ToolCall{}, d.invoke)
	require.NoError(t, err)
	require.False(t, r.IsError, r.Content)
	require.Equal(t, "Sonuç: 9000", d.field, "the localized display value arrives as a plain number")
	require.Equal(t, 1, d.mutations)
	require.Contains(t, r.Content, `"captured":{"result":"9000"}`)
}

// A readback that does not match the capture stops the flow before the
// input that would have used it.
func TestDesktopFlowCaptureMismatchStopsBeforeInput(t *testing.T) {
	t.Parallel()
	for name, display := range map[string]string{
		"pattern": "Sıfıra bölünemez",
		"number":  "Ekran değeri 9,0,0",
		"empty":   "Ekran değeri ",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			d := newCalcNotepad(t)
			d.display = display
			r, err := runDesktopWorkflow(t.Context(), calcToNotepadFlow(), fantasy.ToolCall{}, d.invoke)
			require.NoError(t, err)
			require.True(t, r.IsError)
			require.Contains(t, r.Content, "capture_")
			require.Contains(t, r.Content, "later input was not sent")
			require.Zero(t, d.mutations)
		})
	}
}

// Placeholders and captures are checked for the whole plan before any child
// call, with the node and field of the mistake.
func TestDesktopFlowCaptureValidationBeforeDispatch(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		change func(p *DesktopWorkflowParams)
		field  string
	}{
		"placeholder selects target": {func(p *DesktopWorkflowParams) {
			p.Flow.Nodes[3].Input.Automation.Name = "{{result}}"
			p.Flow.Nodes[3].Checkpoint.Name = "{{result}}"
		}, "desktop.flow.nodes[3]"},
		"unknown capture": {func(p *DesktopWorkflowParams) {
			p.Flow.Nodes[3].Input.Automation.Text = "{{total}}"
		}, "desktop.flow.nodes[3].input.automation.text"},
		"malformed placeholder": {func(p *DesktopWorkflowParams) {
			p.Flow.Nodes[3].Input.Automation.Text = "{{ Result }}"
		}, "desktop.flow.nodes[3].input.automation.text"},
		"capture used before it is read": {func(p *DesktopWorkflowParams) {
			p.Flow.Nodes[1].Checkpoint.Expected, p.Flow.Nodes[1].Capture = "Ekran değeri 9.000", nil
			p.Flow.Nodes = append(p.Flow.Nodes, DesktopFlowNode{ID: "late", Kind: "verify", WindowRef: "calc", Checkpoint: computer.AutomationRequest{Role: "ControlType.Text", Condition: "text"}, Capture: &DesktopFlowCapture{Name: "result"}})
			p.Flow.Nodes[3].Next = "late"
		}, "desktop.flow.nodes[3].checkpoint.expected"},
		"capture on resolve": {func(p *DesktopWorkflowParams) {
			p.Flow.Nodes[0].Capture = &DesktopFlowCapture{Name: "x"}
		}, "desktop.flow.nodes[0].capture"},
		"text capture by name": {func(p *DesktopWorkflowParams) {
			p.Flow.Nodes[1].Checkpoint.Name = "Ekran değeri 9.000"
		}, "desktop.flow.nodes[1].checkpoint.name"},
		"pattern without group": {func(p *DesktopWorkflowParams) {
			p.Flow.Nodes[1].Capture.Pattern = "^Ekran"
		}, "desktop.flow.nodes[1].capture"},
		"ambiguous number format": {func(p *DesktopWorkflowParams) {
			p.Flow.Nodes[1].Capture.Number = &DesktopNumberFormat{Thousands: ",", Decimal: ","}
		}, "desktop.flow.nodes[1].capture"},
		"operation capture without expected": {func(p *DesktopWorkflowParams) {
			p.Flow.Nodes[3].Checkpoint.Expected = ""
			p.Flow.Nodes[3].Capture = &DesktopFlowCapture{Name: "written"}
		}, "desktop.flow.nodes[3].checkpoint.expected"},
		"duplicate capture": {func(p *DesktopWorkflowParams) {
			p.Flow.Nodes = append(p.Flow.Nodes, DesktopFlowNode{ID: "again", Kind: "verify", WindowRef: "calc", Checkpoint: computer.AutomationRequest{Role: "ControlType.Text", Condition: "text"}, Capture: &DesktopFlowCapture{Name: "result"}})
			p.Flow.Nodes[3].Next = "again"
		}, "desktop.flow.nodes[4].capture.name"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			p := calcToNotepadFlow()
			tc.change(&p)
			r, err := runDesktopWorkflow(t.Context(), p, fantasy.ToolCall{}, func(context.Context, fantasy.ToolCall) (fantasy.ToolResponse, error) {
				t.Fatal("Invalid plan dispatched a child")
				return fantasy.ToolResponse{}, nil
			})
			require.NoError(t, err)
			contract := contractOf(t, r)
			require.Equal(t, "invalid_flow", contract.Code)
			require.True(t, strings.HasPrefix(contract.Field, tc.field), "field %q, want prefix %q: %s", contract.Field, tc.field, contract.Message)
			require.Contains(t, contract.Message, "nothing was sent")
		})
	}
}

// A capture made before an interruption is kept in the journal, so a resume
// reconciles the later write with the same value instead of re-reading the
// display or typing again.
func TestDesktopFlowDurableResumeKeepsCaptures(t *testing.T) {
	t.Parallel()
	ctx := context.WithValue(t.Context(), SessionIDContextKey, "session")
	store := engineering.NewStore(t.TempDir())
	p := calcToNotepadFlow()
	p.Flow.RunID = "calc-to-pad"
	d := newCalcNotepad(t)
	// The write is applied but neither its result nor its checkpoint can be
	// read, so the effect is uncertain and the intent stays pending.
	d.failWrite, d.failAsserts = true, true
	run := func() fantasy.ToolResponse {
		data, err := json.Marshal(PipelineParams{Desktop: &p})
		require.NoError(t, err)
		r, err := NewToolPipeline(d.invoke, store).Run(ctx, fantasy.ToolCall{Input: string(data)})
		require.NoError(t, err)
		return r
	}
	r := run()
	require.True(t, r.IsError, r.Content)
	require.Equal(t, 1, d.mutations)
	files, err := filepath.Glob(filepath.Join(store.Dir(), "desktop-runs", "*.json"))
	require.NoError(t, err)
	require.Len(t, files, 1)
	saved, err := os.ReadFile(files[0])
	require.NoError(t, err)
	require.Contains(t, string(saved), `"captures":{"result":"9000"}`)
	require.Contains(t, string(saved), `"pending":"write"`)

	// The display changed meanwhile; the completed read is not repeated.
	d.display, d.failAsserts = "Ekran değeri 1", false
	calcReads := d.reads["21"]
	p.Flow.Resume = true
	r = run()
	require.False(t, r.IsError, r.Content)
	require.Equal(t, 1, d.mutations, "the applied write is reconciled, not replayed")
	require.Contains(t, r.Content, "replacement_already_verified")
	require.Equal(t, calcReads, d.reads["21"], "the captured value comes from the journal")
	require.Equal(t, "Sonuç: 9000", d.field)
}

// A journal whose captures do not belong to completed capture nodes is
// refused before any input.
func TestDesktopFlowJournalCapturesMustMatchCompletedNodes(t *testing.T) {
	t.Parallel()
	f := *calcToNotepadFlow().Flow
	base := desktopFlowState{Version: 1, Next: "pad", Attempts: map[string]int{}, Completed: []desktopFlowRecord{{ID: "calc", Next: "read"}, {ID: "read", Next: "pad"}}}
	ok := base
	ok.Captures = map[string]string{"result": "9000"}
	require.NoError(t, validateDesktopFlowState(f, ok))
	missing := base
	require.ErrorContains(t, validateDesktopFlowState(f, missing), "no stored value")
	foreign := base
	foreign.Captures = map[string]string{"result": "9000", "other": "1"}
	require.ErrorContains(t, validateDesktopFlowState(f, foreign), "does not belong")
	early := base
	early.Completed, early.Next = base.Completed[:1], "read"
	early.Captures = map[string]string{"result": "9000"}
	require.ErrorContains(t, validateDesktopFlowState(f, early), "does not belong")
}

func TestParseDisplayedNumber(t *testing.T) {
	t.Parallel()
	tr := DesktopNumberFormat{Thousands: ".", Decimal: ","}
	en := DesktopNumberFormat{Thousands: ",", Decimal: "."}
	for _, tc := range []struct {
		in     string
		format DesktopNumberFormat
		want   string
	}{
		{"69.104", tr, "69104"},
		{"8.638", tr, "8638"},
		{"8,5", tr, "8.5"},
		{"-1.234.567,25", tr, "-1234567.25"},
		{"\u202a9.000\u202c", tr, "9000"},
		{"8.638", en, "8.638"},
		{"69,104", en, "69104"},
		{"\u22125", en, "-5"},
		{"1 234,5", DesktopNumberFormat{Thousands: " ", Decimal: ","}, "1234.5"},
		{"1\u00a0234,5", DesktopNumberFormat{Thousands: " ", Decimal: ","}, "1234.5"},
		{"42", DesktopNumberFormat{Decimal: "."}, "42"},
	} {
		got, err := parseDisplayedNumber(tc.in, tc.format)
		require.NoError(t, err, tc.in)
		require.Equal(t, tc.want, got, tc.in)
	}
	for _, bad := range []string{"", "9,0,0", "12.34.5", "1.2345", "abc", "9.000,", "1234.567"} {
		_, err := parseDisplayedNumber(bad, tr)
		require.Error(t, err, bad)
	}
}

// file_check reads the saved output from the file system: exists, absent,
// text content with optional line-ending normalization, and sha256.
func TestDesktopFileCheckConditions(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "sonuc.txt")
	content := "Sonuç: 9000\r\n"
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	sum := sha256.Sum256([]byte(content))
	digest := hex.EncodeToString(sum[:])
	for _, tc := range []struct {
		check  DesktopFileCheck
		passed bool
		code   string
	}{
		{DesktopFileCheck{Path: path, Condition: "exists"}, true, ""},
		{DesktopFileCheck{Path: path, Condition: "absent"}, false, "file_present"},
		{DesktopFileCheck{Path: filepath.Join(dir, "yok.txt"), Condition: "absent"}, true, ""},
		{DesktopFileCheck{Path: filepath.Join(dir, "yok.txt"), Condition: "exists"}, false, "file_missing"},
		{DesktopFileCheck{Path: path, Condition: "content", Expected: "Sonuç: 9000"}, true, ""},
		{DesktopFileCheck{Path: path, Condition: "content", Expected: "Sonuç: 9000", LineEndings: "exact"}, false, "file_mismatch"},
		{DesktopFileCheck{Path: path, Condition: "content", Expected: "Sonuç: 9000\r\n", LineEndings: "exact"}, true, ""},
		{DesktopFileCheck{Path: path, Condition: "content", Expected: "Sonuç: 8638"}, false, "file_mismatch"},
		{DesktopFileCheck{Path: path, Condition: "sha256", Expected: strings.ToUpper(digest)}, true, ""},
		{DesktopFileCheck{Path: path, Condition: "sha256", Expected: strings.Repeat("0", 64)}, false, "file_mismatch"},
		{DesktopFileCheck{Path: dir, Condition: "exists"}, false, "file_mismatch"},
	} {
		require.NoError(t, validateDesktopFileCheck(&tc.check))
		r := runDesktopFileCheck(t.Context(), tc.check)
		require.Equal(t, !tc.passed, r.IsError, "%+v: %s", tc.check, r.Content)
		if !tc.passed {
			require.True(t, strings.HasPrefix(r.Content, tc.code+":"), r.Content)
		}
	}
	r := runDesktopFileCheck(t.Context(), DesktopFileCheck{Path: path, Condition: "content", Expected: "Sonuç: 8638"})
	require.Contains(t, r.Content, `"first_difference":{"actual":"Sonuç: 9000","expected":"Sonuç: 8638","line":1}`)
	for _, bad := range []DesktopFileCheck{
		{Path: "sonuc.txt", Condition: "exists"},
		{Path: path + `\..\x`, Condition: "exists"},
		{Path: path, Condition: "exists", Expected: "x"},
		{Path: path, Condition: "sha256", Expected: "abc"},
		{Path: path, Condition: "size"},
		{Path: path, Condition: "content", LineEndings: "crlf"},
	} {
		require.Error(t, validateDesktopFileCheck(&bad), "%+v", bad)
	}
}

// A flow ends with a file_check that compares the saved file with the
// captured value; a mismatch is a failed node, not a completed flow.
func TestDesktopFlowFileCheckUsesCapture(t *testing.T) {
	t.Parallel()
	for _, saved := range []string{"Sonuç: 9000\n", "Sonuç: 8638\n"} {
		dir := t.TempDir()
		path := filepath.Join(dir, "sonuc.txt")
		require.NoError(t, os.WriteFile(path, []byte(saved), 0o644))
		p := calcToNotepadFlow()
		p.Flow.Nodes[3].Next = "saved"
		p.Flow.Nodes = append(p.Flow.Nodes, DesktopFlowNode{ID: "saved", Kind: "file_check", File: &DesktopFileCheck{Path: path, Condition: "content", Expected: "Sonuç: {{result}}"}})
		d := newCalcNotepad(t)
		r, err := runDesktopWorkflow(t.Context(), p, fantasy.ToolCall{}, d.invoke)
		require.NoError(t, err)
		match := strings.HasPrefix(saved, "Sonuç: 9000")
		require.Equal(t, !match, r.IsError, r.Content)
		if !match {
			require.True(t, strings.HasPrefix(r.Content, "file_mismatch:"), r.Content)
		}
	}
	p := calcToNotepadFlow()
	p.Flow.Nodes[3].Next = "saved"
	p.Flow.Nodes = append(p.Flow.Nodes, DesktopFlowNode{ID: "saved", Kind: "file_check", File: &DesktopFileCheck{Path: `C:\{{result}}.txt`, Condition: "exists"}})
	r, err := runDesktopWorkflow(t.Context(), p, fantasy.ToolCall{}, func(context.Context, fantasy.ToolCall) (fantasy.ToolResponse, error) {
		t.Fatal("Invalid plan dispatched a child")
		return fantasy.ToolResponse{}, nil
	})
	require.NoError(t, err)
	require.Equal(t, "desktop.flow.nodes[4].file.path", contractOf(t, r).Field)
}
