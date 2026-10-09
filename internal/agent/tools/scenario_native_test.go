package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/android"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/devloop"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/scenarios"
	"github.com/stretchr/testify/require"
)

type nativeFixture struct {
	t                  *testing.T
	scenario           scenarios.Scenario
	value              string
	focused            bool
	deny               bool
	stop               bool
	wrongApp           bool
	background         bool
	passwordAfterClick bool
	duplicate          bool
	truncated          bool
	stale              bool
	inputs             int
	observed           int
	root               string
	mutate             bool
	imageBytes         []byte
}

func newNativeFixture(t *testing.T, target string) (*nativeFixture, ScenarioServices) {
	t.Helper()
	root := t.TempDir()
	_, err := engineering.Git(t.Context(), root, "init")
	require.NoError(t, err)
	img := image.NewRGBA(image.Rect(0, 0, 16, 16))
	img.Set(1, 1, color.RGBA{R: 255, A: 255})
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))
	f := &nativeFixture{t: t, value: "saved", root: root, imageBytes: buf.Bytes()}
	f.scenario = scenarios.Scenario{ID: "native-save", Version: 1, Target: target, TimeoutMS: 15000, Assertions: []scenarios.Assertion{{Kind: "value", Selector: "id:editor", Expected: "saved"}}}
	if target == "android" {
		f.scenario.Serial, f.scenario.Package = "emulator-5554", "com.example.app"
		f.scenario.Assertions[0].Selector = "com.example.app:id/editor"
	} else {
		f.scenario.WindowID = "123"
	}
	return f, ScenarioServices{Root: root, SessionID: "native-scenario", Store: engineering.NewStore(t.TempDir()), Invoke: f.invoke}
}

func (f *nativeFixture) invoke(_ context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
	var p struct {
		Action        string `json:"action"`
		Text          string `json:"text"`
		WindowID      string `json:"window_id"`
		ObservationID string `json:"observation_id"`
	}
	require.NoError(f.t, json.Unmarshal([]byte(call.Input), &p))
	if f.scenario.Target == "desktop" {
		require.Equal(f.t, IsolatedDesktopToolName, call.Name, "desktop scenarios must not use the real host computer tool")
	} else {
		require.Equal(f.t, AndroidToolName, call.Name)
	}
	if p.Action != "observe" && p.Action != "inspect" {
		require.Equal(f.t, "fresh-observation", p.ObservationID, "native input must preserve the observation guard through tool dispatch")
		if f.scenario.Target == "desktop" {
			require.Equal(f.t, f.scenario.WindowID, p.WindowID)
		}
		if f.deny {
			return fantasy.NewTextErrorResponse("permission_denied"), nil
		}
		if f.stop {
			resp := fantasy.NewTextResponse("user question")
			resp.StopTurn = true
			return resp, nil
		}
		f.inputs++
		if p.Action == "click" || p.Action == "tap" {
			f.focused = !f.stale
		}
		if p.Action == "type" {
			f.value = p.Text
		}
		return deviceResponse(map[string]any{"ok": true}), nil
	}
	if p.Action == "observe" {
		f.observed++
		if f.mutate {
			require.NoError(f.t, os.WriteFile(filepath.Join(f.root, "changed.txt"), []byte("changed"), 0o600))
		}
		resp := fantasy.NewImageResponse(f.imageBytes, "image/png")
		meta := map[string]any{"ok": true, "observation_id": "fresh-observation", "blank": false}
		if f.scenario.Target == "desktop" {
			meta["windows"] = []map[string]any{{"window_id": f.scenario.WindowID, "foreground": !f.background, "minimized": false}}
		}
		if f.scenario.Target == "android" {
			pkg := f.scenario.Package
			if f.wrongApp {
				pkg = "com.other.app"
			}
			elements := []android.Element{{ResourceID: "com.example.app:id/editor", Text: f.value, Enabled: true, Focused: f.focused, Password: f.passwordAfterClick && f.inputs > 0, Bounds: [4]int{0, 0, 16, 16}}}
			if f.duplicate {
				elements = append(elements, elements[0])
			}
			meta["serial"], meta["focus"], meta["elements"], meta["element_count"] = f.scenario.Serial, android.Focus{Package: pkg}, elements, len(elements)
		}
		data, err := json.Marshal(meta)
		require.NoError(f.t, err)
		resp.Content = string(data)
		return resp, nil
	}
	elements := []nativeElement{{ID: "editor", Name: "Editor", Value: f.value, ValuePresent: true, Enabled: true, Focused: f.focused, Password: f.passwordAfterClick && f.inputs > 0, Width: 16, Height: 16}}
	if f.duplicate {
		elements = append(elements, elements[0])
	}
	window := f.scenario.WindowID
	if f.wrongApp {
		window = "456"
	}
	return deviceResponse(map[string]any{"ok": true, "result": map[string]any{"window_id": window, "elements": elements, "truncated": f.truncated}}), nil
}

func TestNativeScenarioPersistsSourceBoundEvidenceThroughDeviceTools(t *testing.T) {
	for _, target := range []string{"desktop", "android"} {
		t.Run(target, func(t *testing.T) {
			f, services := newNativeFixture(t, target)
			f.value = "before"
			f.scenario.Steps = []scenarios.Step{{Action: "type", Selector: f.scenario.Assertions[0].Selector, Value: "saved"}}
			run, err := ExecuteScenario(t.Context(), services, f.scenario, fantasy.ToolCall{ID: "native-proof", Input: `{}`})
			require.NoError(t, err)
			require.True(t, run.Passed, "%+v", run)
			require.True(t, run.Observed)
			require.Equal(t, 2, f.inputs, "one focus click and one type; observations must not replay input")
			st, err := services.Store.Read(t.Context(), services.SessionID)
			require.NoError(t, err)
			require.Len(t, st.UIEvidence, 1)
			require.Equal(t, 16, st.UIEvidence[0].Width)
			require.NoError(t, engineering.ValidateUIArtifact(t.Context(), st.UIEvidence[0]))
			saved, _, err := ReadScenario(t.Context(), services, run.ID)
			require.NoError(t, err)
			require.Equal(t, target, saved.Run.Target)
			require.NoError(t, scenarios.Current(t.Context(), saved.Run, &scenarioBackend{ScenarioServices: services}))
		})
	}
}

func TestNativeScenarioFailsClosedOnDeniedStaleOrAmbiguousTargets(t *testing.T) {
	for _, target := range []string{"desktop", "android"} {
		for _, issue := range []string{"deny", "stop", "wrong-app", "duplicate", "no-focus", "background", "password-after-click"} {
			t.Run(target+"/"+issue, func(t *testing.T) {
				f, services := newNativeFixture(t, target)
				f.deny, f.stop, f.wrongApp, f.duplicate, f.stale = issue == "deny", issue == "stop", issue == "wrong-app", issue == "duplicate", issue == "no-focus"
				f.background = issue == "background"
				f.passwordAfterClick = issue == "password-after-click"
				if target == "android" && f.background {
					f.wrongApp = true
				}
				f.scenario.Steps = []scenarios.Step{{Action: "type", Selector: f.scenario.Assertions[0].Selector, Value: "saved"}}
				run, err := ExecuteScenario(t.Context(), services, f.scenario, fantasy.ToolCall{ID: "native-rejected", Input: `{}`})
				require.Error(t, err)
				require.False(t, run.Passed)
				require.LessOrEqual(t, f.inputs, 1, "no typing after failed focus or denied input")
			})
		}
	}
}

func TestNativeAppLoopRecordsFailedIterationThenSuccessfulFix(t *testing.T) {
	for _, target := range []string{"desktop", "android"} {
		t.Run(target, func(t *testing.T) {
			f, services := newNativeFixture(t, target)
			tool := NewAppLoopTool(services.Root, t.TempDir(), services.Store, nil, services.Invoke)
			ctx := context.WithValue(t.Context(), SessionIDContextKey, services.SessionID)
			call := func(id string, params AppLoopParams) fantasy.ToolResponse {
				data, err := json.Marshal(params)
				require.NoError(t, err)
				resp, err := tool.Run(ctx, fantasy.ToolCall{ID: id, Name: AppLoopToolName, Input: string(data)})
				require.NoError(t, err)
				return resp
			}
			resp := call("native-plan", AppLoopParams{Action: "plan", LoopID: "save", Criteria: []devloop.Criterion{{ID: "saved", Text: "The editor holds saved text", Verify: "scenario", Assertions: []int{1}}}})
			require.False(t, resp.IsError, resp.Content)
			f.value = "broken"
			resp = call("native-fails", AppLoopParams{Action: "check", LoopID: "save", Scenario: &f.scenario})
			require.True(t, resp.IsError, resp.Content)
			f.value = "saved"
			resp = call("native-fixed", AppLoopParams{Action: "check", LoopID: "save", Scenario: &f.scenario, ChangeNote: "fixed save handler"})
			require.False(t, resp.IsError, resp.Content)
			resp = call("native-report", AppLoopParams{Action: "report", LoopID: "save"})
			require.Contains(t, resp.Content, `"state":"completed"`)
			require.Contains(t, resp.Content, `"iterations":2`)
			require.Contains(t, resp.Content, "fixed save handler")
		})
	}
}

func TestNativeEvidenceCannotPassWithMissingImageOrIncompleteTree(t *testing.T) {
	for _, issue := range []string{"image", "truncated"} {
		t.Run(issue, func(t *testing.T) {
			f, services := newNativeFixture(t, "desktop")
			if issue == "image" {
				f.imageBytes = nil
			} else {
				f.truncated = true
			}
			run, err := ExecuteScenario(t.Context(), services, f.scenario, fantasy.ToolCall{ID: "bad-evidence", Input: `{}`})
			require.Error(t, err)
			require.False(t, run.Passed)
			st, err := services.Store.Read(t.Context(), services.SessionID)
			require.NoError(t, err)
			require.Empty(t, st.UIEvidence)
		})
	}
}

func TestNativeAppLoopRejectsSourceChangesDespitePassingAssertions(t *testing.T) {
	f, services := newNativeFixture(t, "android")
	f.mutate = true
	tool := NewAppLoopTool(services.Root, t.TempDir(), services.Store, nil, services.Invoke)
	ctx := context.WithValue(t.Context(), SessionIDContextKey, services.SessionID)
	params := AppLoopParams{Action: "plan", LoopID: "source-check", Criteria: []devloop.Criterion{{ID: "saved", Text: "Editor has saved text", Verify: "scenario", Assertions: []int{1}}}}
	data, err := json.Marshal(params)
	require.NoError(t, err)
	resp, err := tool.Run(ctx, fantasy.ToolCall{ID: "source-plan", Name: AppLoopToolName, Input: string(data)})
	require.NoError(t, err)
	require.False(t, resp.IsError, resp.Content)
	params = AppLoopParams{Action: "check", LoopID: "source-check", Scenario: &f.scenario}
	data, err = json.Marshal(params)
	require.NoError(t, err)
	resp, err = tool.Run(ctx, fantasy.ToolCall{ID: "source-check", Name: AppLoopToolName, Input: string(data)})
	require.NoError(t, err)
	require.True(t, resp.IsError, "source mutation must not become a completed loop: %s", resp.Content)
	require.Contains(t, resp.Content, "stale")
}
