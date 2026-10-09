package tools

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"
	_ "time/tzdata"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/browser"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/execution"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/qualityflows"
)

//go:embed stress_journey.md
var stressJourneyDescription string

//go:embed time_clock.js
var timeClockScript string

type StressJourneyParams struct {
	Manifest string `json:"manifest" description:"Project-relative version 1 JSON journey specification; inspect or create it before running."`
}

type JourneyStep struct {
	Action    string `json:"action"`
	Selector  string `json:"selector,omitempty"`
	Text      string `json:"text,omitempty"`
	Key       string `json:"key,omitempty"`
	Condition string `json:"condition,omitempty"`
	Expected  string `json:"expected,omitempty"`
	Repeat    int    `json:"repeat,omitempty"`
	Network   string `json:"network,omitempty"`
	TimeoutMS int    `json:"timeout_ms,omitempty"`
	AdvanceMS int64  `json:"advance_ms,omitempty"`
}

type JourneyCase struct {
	Name                   string        `json:"name"`
	Width                  int           `json:"width"`
	Height                 int           `json:"height"`
	LatencyMS              int           `json:"latency_ms,omitempty"`
	DownloadBytesPerSecond int           `json:"download_bytes_per_second,omitempty"`
	UploadBytesPerSecond   int           `json:"upload_bytes_per_second,omitempty"`
	BlockedURLs            []string      `json:"blocked_urls,omitempty"`
	Steps                  []JourneyStep `json:"steps"`
}

type JourneySpec struct {
	Version int           `json:"version"`
	URL     string        `json:"url"`
	Cases   []JourneyCase `json:"cases"`
	Clock   *JourneyClock `json:"clock,omitempty"`
}

type JourneyClock struct {
	Start    string `json:"start"`
	Timezone string `json:"timezone,omitempty"`
}

type JourneyStepResult struct {
	Index      int    `json:"index"`
	Action     string `json:"action"`
	Status     string `json:"status"`
	DurationMS int64  `json:"duration_ms"`
	Error      string `json:"error,omitempty"`
}

type JourneyCaseResult struct {
	Name   string              `json:"name"`
	Passed bool                `json:"passed"`
	Steps  []JourneyStepResult `json:"steps"`
}

type JourneyReport struct {
	Passed        bool                `json:"passed"`
	SpecHash      string              `json:"spec_hash"`
	Cases         []JourneyCaseResult `json:"cases"`
	CleanupErrors []string            `json:"cleanup_errors"`
}

func ValidateJourney(spec JourneySpec) error {
	u, err := url.Parse(spec.URL)
	if err != nil || u.Host == "" || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") || len(spec.URL) > 2048 || spec.Version != 1 || len(spec.Cases) == 0 || len(spec.Cases) > 12 {
		return fmt.Errorf("version 1, HTTP(S) URL and 1-12 cases required")
	}
	if spec.Clock != nil {
		start, err := time.Parse(time.RFC3339Nano, spec.Clock.Start)
		if err != nil || start.Year() < 1970 || start.Year() > 2100 {
			return fmt.Errorf("clock start must be RFC3339 between 1970 and 2100")
		}
		if spec.Clock.Timezone != "" {
			if _, err := time.LoadLocation(spec.Clock.Timezone); err != nil {
				return fmt.Errorf("invalid IANA timezone: %w", err)
			}
		}
	}
	names := map[string]bool{}
	for _, c := range spec.Cases {
		if c.Name == "" || len(c.Name) > 128 || names[c.Name] || c.Width < 240 || c.Width > 3840 || c.Height < 240 || c.Height > 2160 || c.LatencyMS < 0 || c.LatencyMS > 10000 || c.DownloadBytesPerSecond < 0 || c.UploadBytesPerSecond < 0 || len(c.BlockedURLs) > 16 || len(c.Steps) == 0 || len(c.Steps) > 48 {
			return fmt.Errorf("invalid case bounds: %s", c.Name)
		}
		names[c.Name] = true
		for _, pattern := range c.BlockedURLs {
			if pattern == "" || len(pattern) > 2048 {
				return fmt.Errorf("invalid blocked URL pattern")
			}
		}
		assertions := 0
		for _, s := range c.Steps {
			if s.Repeat < 0 || s.Repeat > 2 || (s.Repeat == 2 && s.Action != "click") || s.TimeoutMS < 0 || s.TimeoutMS > 30000 || len(s.Text) > 16384 || len(s.Selector) > 4096 {
				return fmt.Errorf("invalid journey step bounds")
			}
			switch s.Action {
			case "advance_time":
				if spec.Clock == nil || s.AdvanceMS <= 0 || s.AdvanceMS > 31622400000 {
					return fmt.Errorf("advance_time requires a clock and 1-31622400000 ms")
				}
			case "click", "type":
				if s.Selector == "" {
					return fmt.Errorf("click/type requires selector")
				}
			case "key":
				if s.Key == "" {
					return fmt.Errorf("key requires key name")
				}
			case "network":
				if s.Network != "offline" && s.Network != "online" {
					return fmt.Errorf("network requires offline/online")
				}
			case "assert", "wait_for":
				if err := browser.ValidateWorkflowRequest(browser.Request{Action: s.Action, Selector: s.Selector, Condition: s.Condition, Expected: s.Expected, TimeoutMS: s.TimeoutMS}); err != nil {
					return err
				}
				assertions++
			default:
				return fmt.Errorf("unsupported journey action: %s", s.Action)
			}
		}
		if assertions == 0 {
			return fmt.Errorf("each case needs an explicit observed assertion")
		}
	}
	return nil
}

func journeyBrowser(ctx context.Context, invoke ToolInvoker, id string, p BrowserParams) (fantasy.ToolResponse, error) {
	if invoke == nil {
		return fantasy.ToolResponse{}, fmt.Errorf("guarded browser dispatch unavailable")
	}
	data, err := json.Marshal(p)
	if err != nil {
		return fantasy.ToolResponse{}, err
	}
	resp, err := invoke(ctx, fantasy.ToolCall{ID: id, Name: BrowserToolName, Input: string(data)})
	if err == nil && (resp.IsError || resp.StopTurn) {
		err = fmt.Errorf("browser %s rejected: %s", p.Action, truncateVerification(resp.Content, 1024))
	}
	return resp, err
}

func journeyNetwork(c JourneyCase, offline bool) json.RawMessage {
	download, upload := -1, -1
	if c.DownloadBytesPerSecond > 0 {
		download = c.DownloadBytesPerSecond
	}
	if c.UploadBytesPerSecond > 0 {
		upload = c.UploadBytesPerSecond
	}
	data, _ := json.Marshal(map[string]any{"offline": offline, "latency": c.LatencyMS, "downloadThroughput": download, "uploadThroughput": upload})
	return data
}

// RunJourney owns a new tab for each case. It restores the original selection
// and closes only its own tab; no emulation is applied to the user's tab.
func RunJourney(ctx context.Context, spec JourneySpec, invoke ToolInvoker, id string) (JourneyReport, error) {
	report := JourneyReport{Passed: true, Cases: []JourneyCaseResult{}, CleanupErrors: []string{}}
	if err := ValidateJourney(spec); err != nil {
		return report, err
	}
	encoded, _ := json.Marshal(spec)
	report.SpecHash = engineering.Hash(string(encoded))
	count := 0
	var stopped error
	run := func(ctx context.Context, p BrowserParams) (fantasy.ToolResponse, error) {
		count++
		resp, err := journeyBrowser(ctx, invoke, fmt.Sprintf("%s-journey-%d", id, count), p)
		if resp.StopTurn && stopped == nil {
			stopped = err
		}
		return resp, err
	}
	tabs, err := run(ctx, BrowserParams{Action: "tabs"})
	if err != nil {
		return report, err
	}
	var current struct {
		Active string `json:"active_tab"`
	}
	if err := json.Unmarshal([]byte(tabs.Content), &current); err != nil || current.Active == "" {
		return report, fmt.Errorf("browser did not report the original tab")
	}
	for _, c := range spec.Cases {
		if err := ctx.Err(); err != nil {
			return report, err
		}
		result := JourneyCaseResult{Name: c.Name, Passed: true, Steps: []JourneyStepResult{}}
		func() {
			created, err := run(ctx, BrowserParams{Action: "tab_new"})
			if err != nil {
				result.Passed = false
				result.Steps = append(result.Steps, JourneyStepResult{Action: "setup", Status: "failed", Error: err.Error()})
				return
			}
			var owned struct {
				Active string `json:"active_tab"`
			}
			if err := json.Unmarshal([]byte(created.Content), &owned); err != nil || owned.Active == "" || owned.Active == current.Active {
				result.Passed = false
				result.Steps = append(result.Steps, JourneyStepResult{Action: "setup", Status: "failed", Error: "new tab identity unavailable"})
				return
			}
			defer func() {
				cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
				defer cancel()
				if _, err := run(cleanup, BrowserParams{Action: "tab_select", Advanced: browser.Request{TabID: current.Active}}); err != nil {
					report.CleanupErrors = append(report.CleanupErrors, err.Error())
					return
				}
				if _, err := run(cleanup, BrowserParams{Action: "tab_close", Advanced: browser.Request{TabID: owned.Active}}); err != nil {
					report.CleanupErrors = append(report.CleanupErrors, err.Error())
				}
			}()
			guard := browser.Request{ExpectedTabID: owned.Active}
			cdp := func(method string, params json.RawMessage) error {
				_, err := run(ctx, BrowserParams{Action: "cdp", CDPMethod: method, CDPParams: BrowserCDPParams(params), Advanced: guard})
				if err != nil {
					return fmt.Errorf("%s: %w", method, err)
				}
				return err
			}
			metrics, _ := json.Marshal(map[string]any{"width": c.Width, "height": c.Height, "deviceScaleFactor": 1, "mobile": false})
			patterns := append([]string{}, c.BlockedURLs...)
			blocked, _ := json.Marshal(map[string]any{"urls": patterns})
			setup := func() error {
				if spec.Clock != nil {
					start, _ := time.Parse(time.RFC3339Nano, spec.Clock.Start)
					source := strings.ReplaceAll(timeClockScript, "__ATLAS_TIME__", fmt.Sprint(start.UnixMilli()))
					params, _ := json.Marshal(map[string]any{"source": source})
					if err := cdp("Page.addScriptToEvaluateOnNewDocument", params); err != nil {
						return err
					}
					if spec.Clock.Timezone != "" {
						params, _ = json.Marshal(map[string]any{"timezoneId": spec.Clock.Timezone})
						if err := cdp("Emulation.setTimezoneOverride", params); err != nil {
							return err
						}
					}
				}
				if err := cdp("Emulation.setDeviceMetricsOverride", metrics); err != nil {
					return err
				}
				if err := cdp("Network.setCacheDisabled", json.RawMessage(`{"cacheDisabled":true}`)); err != nil {
					return err
				}
				if err := cdp("Network.setBypassServiceWorker", json.RawMessage(`{"bypass":true}`)); err != nil {
					return err
				}
				if err := cdp("Network.emulateNetworkConditions", journeyNetwork(c, false)); err != nil {
					return err
				}
				if err := cdp("Network.setBlockedURLs", blocked); err != nil {
					return err
				}
				_, err := run(ctx, BrowserParams{Action: "navigate", URL: spec.URL, Advanced: guard})
				return err
			}
			if err := setup(); err != nil {
				result.Passed = false
				result.Steps = append(result.Steps, JourneyStepResult{Action: "setup", Status: "failed", Error: err.Error()})
			}
			for i, s := range c.Steps {
				r := JourneyStepResult{Index: i + 1, Action: s.Action, Status: "passed"}
				if !result.Passed {
					r.Status = "blocked"
					result.Steps = append(result.Steps, r)
					continue
				}
				start := time.Now()
				switch s.Action {
				case "advance_time":
					params, _ := json.Marshal(map[string]any{"expression": fmt.Sprintf("globalThis.__atlasTimeLab.advance(%d)", s.AdvanceMS), "awaitPromise": true, "returnByValue": true})
					var resp fantasy.ToolResponse
					resp, err = run(ctx, BrowserParams{Action: "cdp", CDPMethod: "Runtime.evaluate", CDPParams: BrowserCDPParams(params), Advanced: guard})
					if err == nil {
						var evaluation struct {
							Exception json.RawMessage `json:"exceptionDetails"`
							Result    struct {
								Value *struct {
									NowMS *int64 `json:"now_ms"`
								} `json:"value"`
							} `json:"result"`
						}
						if json.Unmarshal([]byte(resp.Content), &evaluation) != nil || len(evaluation.Exception) > 0 || evaluation.Result.Value == nil || evaluation.Result.Value.NowMS == nil {
							err = fmt.Errorf("virtual clock advance was not observed")
						}
					}
				case "network":
					err = cdp("Network.emulateNetworkConditions", journeyNetwork(c, s.Network == "offline"))
				default:
					advanced := guard
					advanced.Selector = s.Selector
					advanced.Condition = s.Condition
					advanced.Expected = s.Expected
					advanced.TimeoutMS = s.TimeoutMS
					var resp fantasy.ToolResponse
					for n := 0; n < max(1, s.Repeat); n++ {
						resp, err = run(ctx, BrowserParams{Action: s.Action, Selector: s.Selector, Text: s.Text, Key: s.Key, Advanced: advanced})
						if err != nil {
							break
						}
					}
					if err == nil && (s.Action == "assert" || s.Action == "wait_for") {
						var assertion struct {
							Passed *bool `json:"passed"`
						}
						if json.Unmarshal([]byte(resp.Content), &assertion) != nil || assertion.Passed == nil || !*assertion.Passed {
							err = fmt.Errorf("explicit assertion was not observed passing")
						}
					}
				}
				r.DurationMS = time.Since(start).Milliseconds()
				if err != nil {
					r.Status = "failed"
					r.Error = err.Error()
					result.Passed = false
				}
				result.Steps = append(result.Steps, r)
			}
		}()
		report.Cases = append(report.Cases, result)
		report.Passed = report.Passed && result.Passed
		if stopped != nil {
			report.Passed = false
			return report, stopped
		}
		if len(report.CleanupErrors) > 0 {
			report.Passed = false
			break
		}
	}
	return report, nil
}

func saveQualityReport(ctx context.Context, store *engineering.Store, session, id, kind string, report any) error {
	if store == nil || session == "" {
		return fmt.Errorf("engineering store and session are required")
	}
	data, err := json.Marshal(report)
	if err != nil {
		return err
	}
	_, err = store.PutRecord(ctx, "quality-flows-"+engineering.Hash(session), kind+"-"+id, 0, data)
	return err
}

func NewStressJourneyTool(root string, store *engineering.Store, invoke ToolInvoker) fantasy.AgentTool {
	return newJourneyTool(root, store, invoke, "stress_journey", stressJourneyDescription, false)
}

//go:embed time_lab.md
var timeLabDescription string

func NewTimeLabTool(root string, store *engineering.Store, invoke ToolInvoker) fantasy.AgentTool {
	return newJourneyTool(root, store, invoke, "time_lab", timeLabDescription, true)
}

func newJourneyTool(root string, store *engineering.Store, invoke ToolInvoker, name, description string, requireClock bool) fantasy.AgentTool {
	return fantasy.NewAgentTool(name, description, func(ctx context.Context, p StressJourneyParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
		if execution.IsReadOnly(ctx) {
			return fantasy.NewTextErrorResponse("browser journey execution is unavailable in read-only mode"), nil
		}
		if store == nil || GetSessionFromContext(ctx) == "" {
			return fantasy.NewTextErrorResponse("engineering store and session required"), nil
		}
		data, err := qualityflows.ReadFile(root, p.Manifest, 256*1024)
		if err != nil {
			return fantasy.NewTextErrorResponse(err.Error()), nil
		}
		var spec JourneySpec
		if err := qualityflows.Decode(data, &spec); err != nil {
			return fantasy.NewTextErrorResponse(err.Error()), nil
		}
		if requireClock && spec.Clock == nil {
			return fantasy.NewTextErrorResponse("time_lab requires clock.start and explicit advance_time steps"), nil
		}
		var stopped *fantasy.ToolResponse
		guarded := func(ctx context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			if invoke == nil {
				return fantasy.ToolResponse{}, fmt.Errorf("guarded browser dispatch unavailable")
			}
			resp, err := invoke(ctx, call)
			if resp.StopTurn && stopped == nil {
				stopped = &resp
			}
			return resp, err
		}
		report, runErr := RunJourney(ctx, spec, guarded, call.ID)
		if runErr != nil {
			report.Passed = false
		}
		saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		kind := name
		if name == "stress_journey" {
			kind = "journey"
		}
		if err := saveQualityReport(saveCtx, store, GetSessionFromContext(ctx), call.ID, kind, report); err != nil {
			return fantasy.NewTextErrorResponse(err.Error()), nil
		}
		if runErr != nil {
			if stopped != nil {
				return *stopped, nil
			}
			return fantasy.NewTextErrorResponse(runErr.Error()), nil
		}
		encoded, _ := json.Marshal(report)
		resp := fantasy.NewTextResponse(string(encoded))
		resp.IsError = !report.Passed
		return fantasy.WithResponseMetadata(resp, MeasuredCheckMetadata{Observed: true, Passed: report.Passed}), nil
	})
}
