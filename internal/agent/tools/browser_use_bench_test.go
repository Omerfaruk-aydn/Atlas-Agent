package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/browser"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/stretchr/testify/require"
)

// legacyFormatSnapshot is the element formatter the browser tool used
// before change-focused observations: every state-changing action resent
// up to 100 elements in this form. The benchmark applies it to the same
// page states to reconstruct the baseline's text output.
func legacyFormatSnapshot(elements []browser.SnapshotElement, dialogs []browser.DialogInfo) string {
	var b strings.Builder
	if len(dialogs) > 0 {
		fmt.Fprintf(&b, "%d pending dialog(s) block the page -- resolve with the dialog action first:\n", len(dialogs))
		for _, d := range dialogs {
			fmt.Fprintf(&b, "- %s: %q\n", d.Type, d.Message)
		}
		b.WriteString("\n")
	}
	if len(elements) == 0 {
		b.WriteString("No interactive elements found.")
		return b.String()
	}
	for _, el := range elements {
		fmt.Fprintf(&b, "[%s] %s", el.Ref, el.Role)
		if el.Name != "" {
			fmt.Fprintf(&b, " %q", el.Name)
		}
		if el.Value != "" {
			fmt.Fprintf(&b, " value=%q", el.Value)
		}
		fmt.Fprintf(&b, " (%s)\n", el.Tag)
	}
	return b.String()
}

// benchResult is one measured run. Every count comes from real tool
// responses or the session ledger, except where a field says it is
// reconstructed. No language model runs in this benchmark, so there is no
// model call count, model latency or model success rate here.
type benchResult struct {
	Page   string `json:"page"`
	Policy string `json:"policy"`
	Run    int    `json:"run"`
	// Success and FieldsCorrect come from the page's own model state.
	Success       bool `json:"success"`
	FieldsCorrect int  `json:"fields_correct"`
	// FalseClaims counts claims the policy's output made ("the field holds
	// the value") that the page model contradicted after a 600 ms settle.
	FalseClaims int `json:"false_success_claims"`
	Claims      int `json:"claims_checked"`
	// ScriptedCalls are the top-level browser/pipeline calls the script
	// made (what a model would have issued); ToolOperations are browser
	// calls the ledger recorded, including pipeline children.
	ScriptedCalls  int `json:"scripted_tool_calls"`
	ToolOperations int `json:"tool_operations"`
	Screenshots    int `json:"screenshots"`
	// Output a model would read: text in bytes and Unicode characters, and
	// image payload bytes, kept apart. No token estimate is made.
	OutputTextBytes   int  `json:"output_text_bytes"`
	OutputTextRunes   int  `json:"output_text_runes"`
	OutputImageBytes  int  `json:"output_image_bytes"`
	TextReconstructed bool `json:"output_text_reconstructed"`
	// ToolMS is wall time of the policy's own calls. Both policies exclude
	// the same measurement work: oracle reads, their settle pauses and (for
	// the reconstruction) the snapshots that rebuild legacy output.
	ToolMS     int64  `json:"tool_ms"`
	Recoveries int    `json:"recovery_attempts"`
	ModelCalls string `json:"model_calls"`
}

var benchTargets = []struct{ label, name, hint, value, model string }{
	{"Width", "Width", "", "390", "w"},
	{"Height", "Height", "", "844", "h"},
	{"Corner radius", "", "Corner radius", "12", "radius"},
	{"Fill color", "Fill color", "", "F7F3EA", "fill"},
	{"Opacity", "Opacity", "", "60", "opacity"},
}

// benchOracle reads the fixture's own model, the ground truth for every
// claim. It is measurement, not policy.
func benchOracle(t *testing.T, sess browser.Session) (map[string]string, bool) {
	t.Helper()
	raw, err := sess.Eval(`JSON.stringify({w: String(model.w), h: String(model.h), radius: String(model.radius), fill: model.fill.replace('#','').toUpperCase(), opacity: String(model.opacity), exportOpen: !!document.getElementById('export'), toast: !!document.getElementById('toast'), layers: !!document.querySelector('[aria-label=Layers]')})`)
	require.NoError(t, err)
	var text string
	require.NoError(t, json.Unmarshal([]byte(raw), &text))
	var state map[string]any
	require.NoError(t, json.Unmarshal([]byte(text), &state))
	out := map[string]string{}
	for k, v := range state {
		out[k] = fmt.Sprint(v)
	}
	ok := out["exportOpen"] == "false" && out["toast"] == "false" && out["layers"] == "true"
	return out, ok
}

func findBenchRef(elements []browser.SnapshotElement, name, hint string) string {
	for _, el := range elements {
		if (name != "" && el.Name == name) || (hint != "" && el.Hint == hint) {
			return el.Ref
		}
	}
	return ""
}

// benchMeter accumulates what a policy's calls returned and the time spent
// on measurement, which is subtracted from the wall time.
type benchMeter struct {
	result    *benchResult
	measuring time.Duration
}

func (m *benchMeter) response(resp fantasy.ToolResponse) {
	m.result.ScriptedCalls++
	m.text(resp.Content)
	if resp.Type == "image" {
		m.result.Screenshots++
		m.result.OutputImageBytes += len(resp.Data)
	}
}

func (m *benchMeter) text(s string) {
	m.result.OutputTextBytes += len(s)
	m.result.OutputTextRunes += utf8.RuneCountInString(s)
}

func (m *benchMeter) measure(f func()) {
	begin := time.Now()
	f()
	m.measuring += time.Since(begin)
}

// claim checks one claimed value against the oracle after a settle pause,
// as measurement.
func (m *benchMeter) claim(t *testing.T, sess browser.Session, model, claimed string) {
	m.measure(func() {
		time.Sleep(600 * time.Millisecond)
		state, _ := benchOracle(t, sess)
		m.result.Claims++
		if state[model] != claimed {
			m.result.FalseClaims++
		}
	})
}

// TestBrowserUseFigmaFixtureBaselineVersusWorkflow runs the same property
// panel task on the local design fixture three times per policy and page:
//
//   - scripted_legacy_reconstruction replays the step-by-step pattern of the
//     regression session (type, Enter, screenshots, values read from the
//     element list) on the current build; its text output is rebuilt with
//     the legacy formatter. It is not the old binary.
//   - scripted_workflow uses the contract (task_plan, one pipeline of
//     set_field and scoped clicks, one screenshot, task_update).
//
// It measures tool behavior under scripted policies. It does not measure a
// model, a provider, real Figma, or the old release.
func TestBrowserUseFigmaFixtureBaselineVersusWorkflow(t *testing.T) {
	executable := liveBrowserExecutable(t)
	server := httptest.NewServer(http.FileServer(http.Dir("../../browser/testdata/browseruse")))
	defer server.Close()
	manager := browser.GetManager(browser.Options{ExecutablePath: executable, Headless: true, UserDataDir: t.TempDir(), OverlayDisabled: true, ActionTimeout: 20 * time.Second})
	root := t.TempDir()
	tool := newBrowserTool(&mockPermissionService{}, root, manager, browserDescription(false))
	pipeline := NewToolPipeline(func(ctx context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
		return tool.Run(ctx, call)
	})
	var results []benchResult
	for _, page := range []string{"design.html", "design.html?dense=1"} {
		url := server.URL + "/" + page
		for run := 1; run <= 3; run++ {
			base := runBaselinePolicy(t, manager, tool, url, run)
			flow := runWorkflowPolicy(t, manager, tool, pipeline, url, run, root)
			base.Page, flow.Page = page, page
			results = append(results, base, flow)
		}
	}
	for _, r := range results {
		t.Logf("%-20s %-31s run=%d success=%v fields=%d/5 false_claims=%d/%d scripted_calls=%d tool_ops=%d screenshots=%d text_bytes=%d text_runes=%d image_bytes=%d reconstructed=%v tool_ms=%d recoveries=%d",
			r.Page, r.Policy, r.Run, r.Success, r.FieldsCorrect, r.FalseClaims, r.Claims, r.ScriptedCalls, r.ToolOperations, r.Screenshots, r.OutputTextBytes, r.OutputTextRunes, r.OutputImageBytes, r.TextReconstructed, r.ToolMS, r.Recoveries)
	}
	if out := os.Getenv("ATLAS_BROWSER_BENCH_OUT"); out != "" {
		data, _ := json.MarshalIndent(results, "", "  ")
		require.NoError(t, os.WriteFile(out, data, 0o644))
	}
	// Only correctness is asserted; the comparison numbers are reported, not
	// tuned toward a target.
	for _, r := range results {
		if r.Policy == "scripted_workflow" {
			require.True(t, r.Success, "%+v", r)
			require.Zero(t, r.FalseClaims, "%+v", r)
			require.Equal(t, len(benchTargets), r.Claims)
		}
	}
}

func benchCall(t *testing.T, ctx context.Context, tool fantasy.AgentTool, input string) fantasy.ToolResponse {
	t.Helper()
	resp, err := tool.Run(ctx, fantasy.ToolCall{ID: fmt.Sprintf("bench-%d", time.Now().UnixNano()), Name: BrowserToolName, Input: input})
	require.NoError(t, err)
	return resp
}

func runBaselinePolicy(t *testing.T, manager *browser.Manager, tool fantasy.AgentTool, url string, run int) benchResult {
	t.Helper()
	id := fmt.Sprintf("bench-baseline-%d-%d", run, len(url))
	defer manager.Close(id)
	ctx := context.WithValue(t.Context(), SessionIDContextKey, id)
	result := benchResult{Policy: "scripted_legacy_reconstruction", Run: run, TextReconstructed: true, ModelCalls: "not_measured"}
	m := &benchMeter{result: &result}
	started := time.Now()
	sess := func() browser.Session { s, err := manager.Session(id); require.NoError(t, err); return s }
	// The legacy tool answered each state-changing call with the element
	// list; rebuilding it is measurement, its size is the policy's output.
	legacy := func(msg string) {
		m.measure(func() {
			elements, err := sess().Snapshot(false)
			require.NoError(t, err)
			if len(elements) > 100 {
				elements = elements[:100]
			}
			m.text(msg + "\n\nInteractive elements now:\n" + legacyFormatSnapshot(elements, sess().PendingDialogs()))
		})
	}
	call := func(input, msg string) fantasy.ToolResponse {
		resp := benchCall(t, ctx, tool, input)
		m.result.ScriptedCalls++
		legacy(msg)
		return resp
	}
	screenshot := func() {
		resp := benchCall(t, ctx, tool, `{"action":"screenshot"}`)
		require.False(t, resp.IsError, resp.Content)
		m.response(resp)
	}
	call(fmt.Sprintf(`{"action":"navigate","url":%q}`, url), "Navigated to "+url)
	screenshot()
	for i, target := range benchTargets {
		var ref string
		m.measure(func() {
			elements, err := sess().Snapshot(false)
			require.NoError(t, err)
			ref = findBenchRef(elements, target.name, target.hint)
		})
		require.NotEmpty(t, ref, target.label)
		call(fmt.Sprintf(`{"action":"type","ref":%q,"text":%q}`, ref, target.value), "Typed into ref "+ref)
		call(`{"action":"key","key":"enter"}`, "Sent key enter")
		// The claim is the value the legacy output showed right after Enter:
		// the element list taken then, whatever the field held.
		var reported string
		m.measure(func() {
			listed, err := sess().Snapshot(false)
			require.NoError(t, err)
			for _, el := range listed {
				if (target.name != "" && el.Name == target.name) || (target.hint != "" && el.Hint == target.hint) {
					reported = el.Value
				}
			}
		})
		if strings.EqualFold(reported, target.value) {
			m.claim(t, sess(), target.model, target.value)
		}
		if i%2 == 1 {
			screenshot()
		}
	}
	var elements []browser.SnapshotElement
	m.measure(func() {
		var err error
		elements, err = sess().Snapshot(false)
		require.NoError(t, err)
	})
	for _, el := range elements {
		if el.Name == "Close" && strings.Contains(el.Context, "Export") {
			call(fmt.Sprintf(`{"action":"click","ref":%q}`, el.Ref), "Clicked ref "+el.Ref)
		}
	}
	if ref := findBenchRef(elements, "Dismiss", ""); ref != "" {
		call(fmt.Sprintf(`{"action":"click","ref":%q}`, ref), "Clicked ref "+ref)
	}
	screenshot()
	result.ToolMS = (time.Since(started) - m.measuring).Milliseconds()
	state, panelsOK := benchOracle(t, sess())
	for _, target := range benchTargets {
		if state[target.model] == target.value {
			result.FieldsCorrect++
		}
	}
	result.Success = panelsOK && result.FieldsCorrect == len(benchTargets)
	metrics := BrowserSessionMetrics(id)
	result.ToolOperations, result.Recoveries = metrics.Calls, metrics.Recoveries
	return result
}

func runWorkflowPolicy(t *testing.T, manager *browser.Manager, tool, pipeline fantasy.AgentTool, url string, run int, root string) benchResult {
	t.Helper()
	id := fmt.Sprintf("bench-workflow-%d-%d", run, len(url))
	defer manager.Close(id)
	ctx := context.WithValue(t.Context(), SessionIDContextKey, id)
	result := benchResult{Policy: "scripted_workflow", Run: run, ModelCalls: "not_measured"}
	m := &benchMeter{result: &result}
	started := time.Now()
	sess := func() browser.Session { s, err := manager.Session(id); require.NoError(t, err); return s }
	call := func(input string) fantasy.ToolResponse {
		resp := benchCall(t, ctx, tool, input)
		m.response(resp)
		require.False(t, resp.IsError, resp.Content)
		return resp
	}
	call(fmt.Sprintf(`{"action":"navigate","url":%q}`, url))
	call(`{"action":"task_plan","task":{"task_id":"panel","goal":"Set the frame properties and tidy the panels","requirements":[{"id":"props","text":"W 390, H 844, radius 12, fill F7F3EA, opacity 60","verify":"structural"},{"id":"panels","text":"Export dialog closed, notification dismissed, Layers kept","verify":"structural"},{"id":"look","text":"Result looks right","verify":"visual"}]}}`)
	var steps []string
	for i, target := range benchTargets {
		key := "label"
		if target.name != "" {
			key = "name"
		}
		steps = append(steps, fmt.Sprintf(`{"id":"f%d","action":"set_field","target":{%q:%q,"value":%q}}`, i, key, target.label, target.value))
	}
	steps = append(steps,
		`{"id":"export","action":"semantic_click","target":{"role":"button","name":"Close","within":"Export"}}`,
		`{"id":"toast","action":"semantic_click","target":{"role":"button","name":"Dismiss","within":"Notification"}}`,
		`{"id":"panels","action":"wait_for","target":{"selector":"#export, #toast","condition":"count","expected":"0"}}`,
		`{"id":"layers","action":"assert","target":{"selector":"[aria-label=Layers]","condition":"count","expected":"1"}}`)
	resp, err := pipeline.Run(ctx, fantasy.ToolCall{ID: id + "-wf", Input: `{"browser":{"steps":[` + strings.Join(steps, ",") + `]},"return":["f0","f1","f2","f3","f4","layers"]}`})
	require.NoError(t, err)
	m.response(resp)
	require.False(t, resp.IsError, resp.Content)
	var children []PipelineResult
	require.NoError(t, json.Unmarshal([]byte(resp.Content), &children))
	var evidence []string
	for i, child := range children {
		var object struct {
			Op       BrowserOp `json:"op"`
			Status   string    `json:"status"`
			Accepted string    `json:"accepted_value"`
		}
		require.NoError(t, json.Unmarshal([]byte(child.Content), &object), child.Content)
		evidence = append(evidence, object.Op.ID)
		// Each set_field result that reports the value as held is a claim,
		// checked against the page model like the reconstruction's claims.
		if i < len(benchTargets) && (object.Status == browser.FieldApplied || object.Status == browser.FieldNormalized) {
			m.claim(t, sess(), benchTargets[i].model, strings.ToUpper(strings.TrimPrefix(object.Accepted, "#")))
		}
	}
	shot := call(`{"action":"screenshot"}`)
	shotOp := opOf(t, shot.Content).ID
	quoted := func(ids []string) string { data, _ := json.Marshal(ids); return string(data) }
	call(fmt.Sprintf(`{"action":"task_update","task":{"id":"props","status":"verified","evidence":%s}}`, quoted(evidence[:5])))
	call(fmt.Sprintf(`{"action":"task_update","task":{"id":"panels","status":"verified","evidence":%s}}`, quoted(evidence[5:])))
	call(fmt.Sprintf(`{"action":"task_update","task":{"id":"look","status":"verified","evidence":[%q]}}`, shotOp))
	result.ToolMS = (time.Since(started) - m.measuring).Milliseconds()
	state, panelsOK := benchOracle(t, sess())
	for _, target := range benchTargets {
		if state[target.model] == target.value {
			result.FieldsCorrect++
		}
	}
	result.Success = panelsOK && result.FieldsCorrect == len(benchTargets)
	metrics := BrowserSessionMetrics(id)
	result.ToolOperations, result.Recoveries = metrics.Calls, metrics.Recoveries
	_, open := BrowserTaskContinuation(root, id)
	require.False(t, open, "every requirement is verified, so the turn may end")
	return result
}
