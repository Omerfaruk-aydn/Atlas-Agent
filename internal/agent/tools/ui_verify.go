package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
)

type UIAssertion struct {
	Selector string `json:"selector"`
	Text     string `json:"text,omitempty"`
	Focused  bool   `json:"focused,omitempty"`
	Visible  bool   `json:"visible,omitempty"`
}

type UIVerifyParams struct {
	URL        string          `json:"url" description:"Actual page to inspect. Start its server first."`
	Width      int             `json:"width,omitempty" description:"Viewport width, 240-3840; default 1280. Repeat at narrow and wide sizes."`
	Height     int             `json:"height,omitempty" description:"Viewport height, 240-2160; default 800."`
	Steps      []BrowserParams `json:"steps,omitempty" description:"At most 16 real click/type/key interactions. All retain browser permissions."`
	Assertions []UIAssertion   `json:"assertions,omitempty" description:"At most 16 DOM text, visibility and focus assertions after interactions."`
}

func NewUIVerifyTool(store *engineering.Store, invoke ToolInvoker, roots ...string) fantasy.AgentTool {
	return fantasy.NewAgentTool("ui_verify", "Exercise an actual web UI through the existing browser, capture viewport/DOM/console and screenshot evidence, and check explicit state/focus/text assertions. Browser-disabled or failed actions are failures, never visual proof. Repeat for responsive sizes and loading/error/empty/success paths. The screenshot still requires human/model inspection for visual quality; this does not certify accessibility or aesthetics.", func(ctx context.Context, p UIVerifyParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
		if p.Width == 0 {
			p.Width = 1280
		}
		if p.Height == 0 {
			p.Height = 800
		}
		if p.Width < 240 || p.Width > 3840 || p.Height < 240 || p.Height > 2160 || len(p.Steps) > 16 || len(p.Assertions) > 16 {
			return fantasy.NewTextErrorResponse("invalid viewport or too many steps/assertions"), nil
		}
		for _, step := range p.Steps {
			if step.Action != "click" && step.Action != "type" && step.Action != "key" {
				return fantasy.NewTextErrorResponse("steps support click, type and key only"), nil
			}
		}
		for _, a := range p.Assertions {
			if a.Selector == "" || len(a.Selector) > 512 || len(a.Text) > 2048 {
				return fantasy.NewTextErrorResponse("invalid assertion"), nil
			}
		}
		index := 0
		var source string
		if len(roots) > 0 && roots[0] != "" {
			var err error
			source, err = engineering.SourceFingerprint(ctx, roots[0], store.Dir())
			if err != nil {
				return fantasy.NewTextErrorResponse(err.Error()), nil
			}
		}
		run := func(params BrowserParams) (fantasy.ToolResponse, error) {
			index++
			data, _ := json.Marshal(params)
			resp, err := invoke(ctx, fantasy.ToolCall{ID: fmt.Sprintf("%s-ui-%d", call.ID, index), Name: BrowserToolName, Input: string(data)})
			if err == nil && (resp.IsError || resp.StopTurn) {
				err = fmt.Errorf("browser action %s failed: %s", params.Action, resp.Content)
			}
			return resp, err
		}
		metrics, _ := json.Marshal(map[string]any{"width": p.Width, "height": p.Height, "deviceScaleFactor": 1, "mobile": false})
		if _, err := run(BrowserParams{Action: "cdp", CDPMethod: "Emulation.setDeviceMetricsOverride", CDPParams: metrics}); err != nil {
			return fantasy.NewTextErrorResponse(err.Error()), nil
		}
		if _, err := run(BrowserParams{Action: "navigate", URL: p.URL}); err != nil {
			return fantasy.NewTextErrorResponse(err.Error()), nil
		}
		for _, step := range p.Steps {
			if _, err := run(step); err != nil {
				return fantasy.NewTextErrorResponse(err.Error()), nil
			}
		}
		passed := true
		var checks []map[string]any
		for _, a := range p.Assertions {
			selector, _ := json.Marshal(a.Selector)
			text, _ := json.Marshal(a.Text)
			script := fmt.Sprintf("(()=>{const e=document.querySelector(%s);return !!e && e.textContent.includes(%s) && (!%t || document.activeElement===e) && (!%t || (e.getClientRects().length>0 && getComputedStyle(e).visibility!=='hidden'));})()", selector, text, a.Focused, a.Visible)
			resp, err := run(BrowserParams{Action: "eval", Script: script})
			if err != nil {
				return fantasy.NewTextErrorResponse(err.Error()), nil
			}
			var ok bool
			if err := json.Unmarshal([]byte(resp.Content), &ok); err != nil {
				return fantasy.NewTextErrorResponse("browser returned an invalid assertion result"), nil
			}
			passed = passed && ok
			checks = append(checks, map[string]any{"selector": a.Selector, "passed": ok})
		}
		dom, err := run(BrowserParams{Action: "snapshot", Full: true})
		if err != nil {
			return fantasy.NewTextErrorResponse(err.Error()), nil
		}
		console, err := run(BrowserParams{Action: "console"})
		if err != nil {
			return fantasy.NewTextErrorResponse(err.Error()), nil
		}
		image, err := run(BrowserParams{Action: "screenshot", FullPage: true})
		if err != nil {
			return fantasy.NewTextErrorResponse(err.Error()), nil
		}
		if len(image.Data) == 0 || len(image.Data) > 16*1024*1024 {
			return fantasy.NewTextErrorResponse("browser did not return a bounded screenshot"), nil
		}
		artifactDir := filepath.Join(store.Dir(), "ui-evidence")
		if err := os.MkdirAll(artifactDir, 0o700); err != nil {
			return fantasy.ToolResponse{}, err
		}
		artifact := filepath.Join(artifactDir, engineering.Hash(GetSessionFromContext(ctx)+call.ID)+".png")
		if err := engineering.AtomicWrite(artifact, image.Data); err != nil {
			return fantasy.ToolResponse{}, err
		}
		scope := engineering.GetScope(ctx, GetSessionFromContext(ctx))
		if source != "" {
			after, err := engineering.SourceFingerprint(ctx, roots[0], store.Dir())
			if err != nil {
				return fantasy.NewTextErrorResponse(err.Error()), nil
			}
			if after != source {
				return fantasy.NewTextErrorResponse("source changed during UI capture; recapture the current implementation"), nil
			}
		}
		evidence := fmt.Sprintf("Screenshot %s (%dx%d); browser call %s; %d assertions observed", artifact, p.Width, p.Height, call.ID, len(checks))
		if err := store.Update(ctx, scope.SessionID, func(st *engineering.State) error {
			st.Checks = append(st.Checks, engineering.Check{TaskID: scope.TaskID, RunID: call.ID, Name: "ui viewport", Tool: "browser", Passed: passed, Evidence: evidence, CheckedAt: time.Now().UnixMilli()})
			entry := engineering.UIEvidence{ID: call.ID, TaskID: scope.TaskID, Path: artifact, Hash: engineering.Hash(string(image.Data)), Width: p.Width, Height: p.Height, Target: "web", SourceFingerprint: source, AssertionsPassed: passed, Origin: "observed browser capture; visual inspection still required", RecordedAt: time.Now().UnixMilli()}
			for i := range st.UIEvidence {
				if st.UIEvidence[i].ID == call.ID {
					st.UIEvidence[i] = entry
					return nil
				}
			}
			st.UIEvidence = append(st.UIEvidence, entry)
			return nil
		}); err != nil {
			return fantasy.ToolResponse{}, err
		}
		report, _ := json.Marshal(map[string]any{"artifact_id": call.ID, "source_fingerprint": source, "assertions_passed": passed, "assertions": checks, "screenshot": artifact, "dom": truncateVerification(dom.Content, 8192), "console": truncateVerification(console.Content, 4096), "visual_inspection_required": true})
		image.Content = string(report)
		image.IsError = !passed
		return image, nil
	})
}
