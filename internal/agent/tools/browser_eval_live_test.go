package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/stretchr/testify/require"
)

// The browser evaluation set: reproducible scenarios on local fixtures, each
// run by a scripted policy (the calls a correct agent would make, including
// the mistakes seen in session 14b5c83a). It measures tool behavior: task
// completion from the fixture's own state, false success claims, calls,
// pipeline inner steps, errors, recoveries, screenshots, output size and
// tool time. It does not measure a model or a provider; model waiting time
// and tokens are not available here and are reported as not measured.
// ATLAS_BROWSER_EVAL_OUT=path writes the results as JSON.

type evalResult struct {
	Scenario      string `json:"scenario"`
	Category      string `json:"category"`
	Completed     bool   `json:"completed"`
	FalseSuccess  int    `json:"false_success_claims"`
	Calls         int    `json:"top_level_calls"`
	PipelineSteps int    `json:"pipeline_inner_steps"`
	Errors        int    `json:"error_results"`
	Recoveries    int    `json:"recoveries"`
	Screenshots   int    `json:"screenshots"`
	OutputBytes   int    `json:"output_text_bytes"`
	ImageBytes    int    `json:"output_image_bytes"`
	ToolMS        int64  `json:"tool_ms"`
	TotalMS       int64  `json:"total_ms"`
	ModelWait     string `json:"model_wait"`
	Tokens        string `json:"tokens"`
	Note          string `json:"note,omitempty"`
}

type evalRun struct {
	t        *testing.T
	l        *liveTool
	pipeline fantasy.AgentTool
	result   evalResult
	started  time.Time
}

func (e *evalRun) record(resp fantasy.ToolResponse, elapsed time.Duration) {
	e.result.Calls++
	e.result.ToolMS += elapsed.Milliseconds()
	e.result.OutputBytes += len(resp.Content)
	if resp.Type == "image" {
		e.result.Screenshots++
		e.result.ImageBytes += len(resp.Data)
	}
	if resp.IsError {
		e.result.Errors++
	}
}

func (e *evalRun) call(input string) fantasy.ToolResponse {
	e.t.Helper()
	started := time.Now()
	resp := e.l.run(input)
	e.record(resp, time.Since(started))
	return resp
}

// expectRefusal is a call the policy makes knowing it must be refused
// before input; a success would be a false claim of safety.
func (e *evalRun) expectRefusal(input, code string) {
	e.t.Helper()
	resp := e.call(input)
	if !resp.IsError || !strings.Contains(resp.Content, code) {
		e.result.FalseSuccess++
		e.t.Errorf("%s: expected %s refusal, got %s", e.result.Scenario, code, resp.Content)
	}
}

func (e *evalRun) pipe(input string) []PipelineResult {
	e.t.Helper()
	started := time.Now()
	resp, err := e.pipeline.Run(e.l.ctx, fantasy.ToolCall{ID: fmt.Sprintf("eval-%d", time.Now().UnixNano()), Name: "tool_pipeline", Input: input})
	require.NoError(e.t, err)
	e.record(resp, time.Since(started))
	var results []PipelineResult
	_ = json.Unmarshal([]byte(resp.Content), &results)
	for _, r := range results {
		if r.Status != StepSkipped {
			e.result.PipelineSteps += max(1, r.Attempts)
		}
		if r.Attempts > 1 {
			e.result.Recoveries++
		}
	}
	return results
}

func (e *evalRun) state() map[string]any {
	e.t.Helper()
	raw, err := e.l.session().Eval("JSON.stringify(window.model||null)")
	require.NoError(e.t, err)
	var text string
	require.NoError(e.t, json.Unmarshal([]byte(raw), &text))
	var m map[string]any
	_ = json.Unmarshal([]byte(text), &m)
	return m
}

type evalScenario struct {
	name, category, page string
	run                  func(e *evalRun) bool
}

func evalScenarios() []evalScenario {
	num := func(m map[string]any, key string) float64 { v, _ := m[key].(float64); return v }
	return []evalScenario{
		{"form_fill_and_readback", "forms", "/form.html", func(e *evalRun) bool {
			e.call(`{"action":"set_field","advanced":{"label":"Email","value":"ada@example.com"}}`)
			e.call(`{"action":"set_field","advanced":{"label":"Age","value":"36"}}`)
			r := e.call(`{"action":"read_field","advanced":{"label":"Email"}}`)
			return strings.Contains(r.Content, `"accepted_value":"ada@example.com"`)
		}},
		{"top_level_value_alias", "contract", "/panelwait.html", func(e *evalRun) bool {
			e.call(`{"action":"observe","advanced":{"mode":"full"}}`)
			f := e.call(`{"action":"find","advanced":{"role":"textbox","name":"Right padding"}}`)
			var found struct{ Matches []struct{ Ref string } }
			_ = json.Unmarshal([]byte(f.Content), &found)
			if len(found.Matches) != 1 {
				return false
			}
			e.call(fmt.Sprintf(`{"action":"set_field","ref":%q,"value":"24","advanced":{"commit":"enter"}}`, found.Matches[0].Ref))
			return num(e.state(), "padR") == 24
		}},
		{"missing_value_refused", "contract", "/panelwait.html", func(e *evalRun) bool {
			e.expectRefusal(`{"action":"set_field","advanced":{"label":"Right padding","commit":"enter"}}`, "missing_value")
			return num(e.state(), "padR") == 69
		}},
		{"conflicting_values_refused", "contract", "/panelwait.html", func(e *evalRun) bool {
			e.expectRefusal(`{"action":"set_field","value":"24","advanced":{"label":"Right padding","value":"25"}}`, "conflicting_fields")
			return num(e.state(), "padR") == 69
		}},
		{"explicit_empty_clears", "contract", "/async.html", func(e *evalRun) bool {
			r := e.call(`{"action":"set_field","advanced":{"label":"Fast","value":""}}`)
			read := e.call(`{"action":"read_field","advanced":{"label":"Fast"}}`)
			return !r.IsError && strings.Contains(read.Content, `"accepted_value":""`)
		}},
		{"read_field_advanced_ref", "contract", "/panelwait.html", func(e *evalRun) bool {
			f := e.call(`{"action":"find","advanced":{"role":"textbox","name":"Left padding"}}`)
			var found struct{ Matches []struct{ Ref string } }
			_ = json.Unmarshal([]byte(f.Content), &found)
			if len(found.Matches) != 1 {
				return false
			}
			r := e.call(fmt.Sprintf(`{"action":"read_field","advanced":{"ref":%q}}`, found.Matches[0].Ref))
			return strings.Contains(r.Content, `"accepted_value":"69"`)
		}},
		{"wrapping_label_field", "targeting", "/panelwait.html", func(e *evalRun) bool {
			e.call(`{"action":"set_field","advanced":{"label":"Right padding","within":"Auto layout","value":"16","commit":"enter"}}`)
			return num(e.state(), "padR") == 16
		}},
		{"within_reported_context", "targeting", "/panelwait.html", func(e *evalRun) bool {
			r := e.call(`{"action":"semantic_click","advanced":{"role":"button","name":"Text","within":"region \"Right sidebar\" > toolbar \"Editor\""}}`)
			return !r.IsError
		}},
		{"within_missing_fails_fast", "recovery", "/panelwait.html", func(e *evalRun) bool {
			started := time.Now()
			e.expectRefusal(`{"action":"semantic_click","advanced":{"role":"button","name":"Text","within":"Inspector"}}`, "within_not_found")
			return time.Since(started) < 5*time.Second
		}},
		{"absent_target_fails_fast", "recovery", "/panelwait.html", func(e *evalRun) bool {
			started := time.Now()
			e.expectRefusal(`{"action":"semantic_click","advanced":{"role":"button","name":"Collapse chat"}}`, "target_not_found")
			return time.Since(started) < 5*time.Second
		}},
		{"covered_control_not_clicked", "focus", "/panelwait.html", func(e *evalRun) bool {
			f := e.call(`{"action":"find","advanced":{"role":"button","name":"Main menu"}}`)
			var found struct{ Matches []struct{ Ref string } }
			_ = json.Unmarshal([]byte(f.Content), &found)
			if len(found.Matches) != 1 {
				return false
			}
			e.expectRefusal(fmt.Sprintf(`{"action":"click","ref":%q}`, found.Matches[0].Ref), "div#shield")
			return num(e.state(), "menu") == 0
		}},
		{"reused_row_ref_refused", "stale", "/evalmisc.html", func(e *evalRun) bool {
			f := e.call(`{"action":"find","advanced":{"role":"row","name":"Title"}}`)
			var found struct{ Matches []struct{ Ref string } }
			_ = json.Unmarshal([]byte(f.Content), &found)
			if len(found.Matches) != 1 {
				return false
			}
			e.call(`{"action":"semantic_click","advanced":{"role":"button","name":"Scroll layers"}}`)
			e.expectRefusal(fmt.Sprintf(`{"action":"click","ref":%q}`, found.Matches[0].Ref), "stale_ref")
			return e.state()["selected"] == nil
		}},
		{"rerendered_ref_gone", "stale", "/figmalike.html", func(e *evalRun) bool {
			f := e.call(`{"action":"find","advanced":{"role":"row","name":"06 · Oturumlar"}}`)
			var found struct{ Matches []struct{ Ref string } }
			_ = json.Unmarshal([]byte(f.Content), &found)
			if len(found.Matches) != 1 {
				return false
			}
			_, _ = e.l.session().Eval("rerenderRows()")
			e.expectRefusal(fmt.Sprintf(`{"action":"click","ref":%q}`, found.Matches[0].Ref), "stale_ref")
			return e.state()["selected"] == "1:2"
		}},
		{"shadow_dom_field", "targeting", "/frames.html", func(e *evalRun) bool {
			e.call(`{"action":"set_field","advanced":{"label":"Nickname","within":"Shadow settings","value":"Atlas"}}`)
			e.call(`{"action":"semantic_click","advanced":{"role":"button","name":"Apply","within":"Shadow settings"}}`)
			raw, err := e.l.session().Eval(`document.querySelector('settings-panel').shadowRoot.getElementById('status').textContent`)
			return err == nil && strings.Contains(raw, "Applied Atlas")
		}},
		{"popup_and_return", "tabs", "/popup.html", func(e *evalRun) bool {
			e.call(`{"action":"semantic_click","advanced":{"role":"button","name":"Sign in with Provider"}}`)
			p := e.call(`{"action":"popup_wait"}`)
			return !p.IsError
		}},
		{"confirm_dialog_handled", "dialogs", "/evalmisc.html", func(e *evalRun) bool {
			e.call(`{"action":"semantic_click","advanced":{"role":"row","name":"Header"}}`)
			e.call(`{"action":"semantic_click","advanced":{"role":"button","name":"Delete layer"}}`)
			d := e.call(`{"action":"dialog_wait"}`)
			if d.IsError {
				return false
			}
			e.call(`{"action":"dialog_handle","advanced":{"accept":true,"expected":"Delete the selected layer?"}}`)
			deleted, _ := e.state()["deleted"].([]any)
			return len(deleted) == 1 && deleted[0] == "Header"
		}},
		{"busy_page_waits_for_condition", "async", "/busy.html", func(e *evalRun) bool {
			r := e.call(`{"action":"wait_for","advanced":{"condition":"text","selector":"#status","expected":"Ready","timeout_ms":5000}}`)
			return !r.IsError
		}},
		{"late_element_retry_after_observe", "async", "/evalmisc.html", func(e *evalRun) bool {
			res := e.pipe(`{"browser":{"steps":[{"id":"load","action":"semantic_click","target":{"role":"button","name":"Load results"}},{"id":"open","action":"semantic_click","target":{"role":"button","name":"Result"},"retry_on":["target_not_found"]}]}}`)
			return len(res) == 2 && res[1].Status == StepSucceeded && num(e.state(), "result") == 1
		}},
		{"pipeline_when_skips_branch", "pipeline", "/evalmisc.html", func(e *evalRun) bool {
			res := e.pipe(`{"steps":[{"id":"found","tool":"browser","arguments":{"action":"find","advanced":{"role":"button","name":"Result"}}},{"id":"open","tool":"browser","arguments":{"action":"semantic_click","advanced":{"role":"button","name":"Result"}},"when":{"source":"found#/count","equals":"1"}},{"id":"load","tool":"browser","arguments":{"action":"semantic_click","advanced":{"role":"button","name":"Load results"}},"when":{"source":"found#/count","equals":"0"}}]}`)
			return len(res) == 3 && res[1].Status == StepSkipped && res[2].Status == StepSucceeded
		}},
		{"pipeline_partial_failure_resume", "pipeline", "/panelwait.html", func(e *evalRun) bool {
			res := e.pipe(`{"browser":{"steps":[{"id":"l","action":"set_field","target":{"label":"Left padding","value":"8","commit":"enter"}},{"id":"bad","action":"semantic_click","target":{"role":"button","name":"Missing"}},{"id":"r","action":"set_field","target":{"label":"Right padding","value":"8","commit":"enter"}}]}}`)
			stopped := false
			for _, r := range res {
				if r.ID == "bad" && strings.Contains(r.Resume, "Already applied, do not repeat: l") {
					stopped = true
				}
			}
			m := e.state()
			return stopped && num(m, "padL") == 8 && num(m, "padR") == 69
		}},
		{"pipeline_form_batch", "pipeline", "/panelwait.html", func(e *evalRun) bool {
			res := e.pipe(`{"browser":{"steps":[{"id":"l","action":"set_field","target":{"label":"Left padding","value":"24","commit":"enter"}},{"id":"r","action":"set_field","target":{"label":"Right padding","value":"24","commit":"enter"}},{"id":"check","action":"read_field","target":{"label":"Right padding"}}]}}`)
			m := e.state()
			return len(res) == 3 && num(m, "padL") == 24 && num(m, "padR") == 24
		}},
		{"canvas_text_new_layer", "canvas", "/canvastext.html", func(e *evalRun) bool {
			e.call(`{"action":"semantic_click","advanced":{"role":"button","name":"Text"}}`)
			grant := e.l.viewport()
			e.result.Calls++
			e.l.canvasClick(grant, 200, 100, 1)
			e.result.Calls++
			e.call(`{"action":"insert_text","text":"Bu telefonu eşleştir"}`)
			e.call(`{"action":"key","key":"escape"}`)
			return e.l.canvasText().Texts[0].Text == "Bu telefonu eşleştir"
		}},
		{"canvas_stale_editor_refused", "canvas", "/canvastext.html", func(e *evalRun) bool {
			e.call(`{"action":"semantic_click","advanced":{"role":"button","name":"Text"}}`)
			grant := e.l.viewport()
			e.result.Calls++
			e.l.canvasClick(grant, 200, 100, 1)
			e.result.Calls++
			e.call(`{"action":"insert_text","text":"ATLAS"}`)
			e.call(`{"action":"key","key":"escape"}`)
			e.expectRefusal(`{"action":"key","key":"t"}`, "key_would_type")
			e.expectRefusal(`{"action":"insert_text","text":"Remote Control"}`, "text_edit_not_open")
			m := e.l.canvasText()
			return len(m.Texts) == 1 && m.Texts[0].Text == "ATLAS"
		}},
		{"canvas_append_requires_mode", "canvas", "/canvastext.html?overlay", func(e *evalRun) bool {
			e.call(`{"action":"semantic_click","advanced":{"role":"button","name":"Text"}}`)
			grant := e.l.viewport()
			e.result.Calls++
			e.l.canvasClick(grant, 200, 100, 1)
			e.result.Calls++
			e.call(`{"action":"insert_text","text":"ATLAS"}`)
			e.call(`{"action":"key","key":"escape"}`)
			e.l.canvasClick(grant, 205, 105, 2)
			e.result.Calls++
			e.expectRefusal(`{"action":"insert_text","text":" Remote"}`, "text_editor_not_empty")
			e.call(`{"action":"insert_text","text":" Remote","advanced":{"mode":"append"}}`)
			return e.l.canvasText().Texts[0].Text == "ATLAS Remote"
		}},
		{"canvas_text_pipeline", "canvas", "/canvastext.html", func(e *evalRun) bool {
			res := e.pipe(`{"browser":{"steps":[{"id":"tool","action":"semantic_click","target":{"role":"button","name":"Text"}},{"id":"shot","action":"screenshot"},{"id":"place","action":"pointer_click","target":{"x":200,"y":136},"bindings":{"advanced.viewport_id":"shot#/viewport_id"}},{"id":"type","action":"insert_text","text":"Nasıl çalışır?"},{"id":"done","action":"key","key":"escape"}]}}`)
			m := e.l.canvasText()
			return len(res) == 5 && len(m.Texts) == 1 && m.Texts[0].Text == "Nasıl çalışır?"
		}},
		{"figma_like_value_named_field", "design_panel", "/figmalike.html", func(e *evalRun) bool {
			r := e.call(`{"action":"set_field","advanced":{"role":"combobox","name":"Font size: 12 px","value":"14","commit":"enter"}}`)
			fs, _ := e.l.figma().layer("1:2")
			return strings.Contains(r.Content, `"verification":"verified"`) && fs == 14
		}},
		{"figma_like_page_rename", "design_panel", "/figmalike.html", func(e *evalRun) bool {
			e.call(`{"action":"semantic_click","advanced":{"role":"button","name":"Add new page"}}`)
			r := e.call(`{"action":"set_field","advanced":{"role":"textbox","name":"Page name","value":"01 — Eşleştirme","commit":"enter"}}`)
			return strings.Contains(r.Content, `"inline_rename"`) || strings.Contains(r.Content, `"verification":"verified"`)
		}},
		{"selection_following_panel", "design_panel", "/panel.html", func(e *evalRun) bool {
			r := e.call(`{"action":"set_field","advanced":{"label":"Width","value":"390","commit":"enter"}}`)
			return strings.Contains(r.Content, `"accepted_value":"390"`)
		}},
		{"repeated_failure_blocked", "recovery", "/panelwait.html", func(e *evalRun) bool {
			for range 2 {
				e.call(`{"action":"click","selector":"#nope"}`)
			}
			third := e.call(`{"action":"click","selector":"#nope"}`)
			return third.IsError && strings.Contains(third.Content, "repeated_failure")
		}},
		{"virtualized_list_collect", "lists", "/list.html", func(e *evalRun) bool {
			r := e.call(`{"action":"collect","advanced":{"row_selector":".row","scope":"#viewport","max_items":250}}`)
			return !r.IsError && strings.Contains(r.Content, `"complete"`)
		}},
		{"iframe_field", "targeting", "/fieldframes.html", func(e *evalRun) bool {
			fr := e.call(`{"action":"frames"}`)
			return !fr.IsError
		}},
	}
}

func TestBrowserEvaluationSet(t *testing.T) {
	executable := liveBrowserExecutable(t)
	_ = executable
	var results []evalResult
	for _, sc := range evalScenarios() {
		t.Run(sc.name, func(t *testing.T) {
			l := newLiveTool(t)
			e := &evalRun{t: t, l: l, started: time.Now(), result: evalResult{Scenario: sc.name, Category: sc.category, ModelWait: "not_measured", Tokens: "not_measured"}}
			e.pipeline = NewToolPipeline(func(ctx context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
				return l.tool.Run(l.ctx, call)
			})
			e.call(fmt.Sprintf(`{"action":"navigate","url":%q}`, l.base+sc.page))
			e.result.Completed = sc.run(e)
			e.result.TotalMS = time.Since(e.started).Milliseconds()
			results = append(results, e.result)
			require.True(t, e.result.Completed, "%+v", e.result)
			require.Zero(t, e.result.FalseSuccess)
		})
	}
	if out := os.Getenv("ATLAS_BROWSER_EVAL_OUT"); out != "" {
		data, _ := json.MarshalIndent(results, "", "  ")
		require.NoError(t, os.WriteFile(out, data, 0o644))
	}
}
