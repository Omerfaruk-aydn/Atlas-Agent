package tools

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/android"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/computer"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/permission"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/transfer"
)

const AndroidToolName = "android"

//go:embed android.md
var androidDescription string

// AndroidStep is one flat step of a sequence.
type AndroidStep struct {
	Action      string `json:"action" description:"tap, type, key, launch, swipe or wait_text"`
	ResourceID  string `json:"resource_id,omitempty"`
	Text        string `json:"text,omitempty" description:"tap: match element text exactly; type: text to type; wait_text: text to wait for"`
	ContentDesc string `json:"content_desc,omitempty"`
	Key         string `json:"key,omitempty"`
	Package     string `json:"package,omitempty"`
	Activity    string `json:"activity,omitempty"`
	X           int    `json:"x,omitempty"`
	Y           int    `json:"y,omitempty"`
	EndX        int    `json:"end_x,omitempty"`
	EndY        int    `json:"end_y,omitempty"`
	TimeoutMS   int    `json:"timeout_ms,omitempty"`
	Sensitive   bool   `json:"sensitive,omitempty"`
}

// AndroidParams is flat; sequences use one level of flat steps.
type AndroidParams struct {
	Action        string        `json:"action" enum:"devices,observe,tap,swipe,type,key,launch,verify_text,install,push,pull,sequence" description:"devices lists phones/emulators; observe reads geometry, foreground app, UI tree (and optionally a screenshot); tap/swipe need observation_id; type/key act on focus; verify_text checks a fresh UI dump; install/push/pull move files; sequence runs up to 24 steps."`
	Serial        string        `json:"serial,omitempty" description:"Device serial from devices (e.g. emulator-5554). Required for every device action; there is no default device."`
	ObservationID string        `json:"observation_id,omitempty" description:"tap/swipe: required observation_id from the latest observe of this serial. type/key: optional foreground and geometry guard, checked again before input."`
	Element       int           `json:"element,omitempty" description:"tap: one-based element index from that observation."`
	ResourceID    string        `json:"resource_id,omitempty" description:"tap/verify_text: element resource id, e.g. com.example:id/submit."`
	Text          string        `json:"text,omitempty" description:"type: text to type; tap: exact element text; verify_text: text expected on screen."`
	ContentDesc   string        `json:"content_desc,omitempty" description:"tap: element content description."`
	X             int           `json:"x,omitempty" description:"tap/swipe: device pixel x from the observation."`
	Y             int           `json:"y,omitempty" description:"tap/swipe: device pixel y."`
	EndX          int           `json:"end_x,omitempty" description:"swipe end x."`
	EndY          int           `json:"end_y,omitempty" description:"swipe end y."`
	DurationMS    int           `json:"duration_ms,omitempty" description:"swipe duration (default 300, max 5000)."`
	Key           string        `json:"key,omitempty" description:"key: back, home, enter, tab, delete, app_switch, menu, escape, space, dpad_up/down/left/right."`
	Package       string        `json:"package,omitempty" description:"launch: application id, e.g. com.android.settings."`
	Activity      string        `json:"activity,omitempty" description:"launch: optional activity, e.g. .MainActivity."`
	UI            *bool         `json:"ui,omitempty" description:"observe: include the UI tree (default true)."`
	Image         bool          `json:"image,omitempty" description:"observe: include a screenshot (default false; use only when the UI tree is insufficient)."`
	APKPath       string        `json:"apk_path,omitempty" description:"install: .apk path relative to this workspace."`
	Replace       bool          `json:"replace,omitempty" description:"install: replace an installed app (-r)."`
	LocalPath     string        `json:"local_path,omitempty" description:"push source / pull destination relative to this workspace."`
	DevicePath    string        `json:"device_path,omitempty" description:"push destination / pull source under /sdcard/ or /data/local/tmp/."`
	Overwrite     bool          `json:"overwrite,omitempty" description:"pull: replace an existing local file."`
	Sensitive     bool          `json:"sensitive,omitempty" description:"type: redact the text from results and logs."`
	Steps         []AndroidStep `json:"steps,omitempty" description:"sequence: steps; taps resolve resource_id/text/content_desc on a fresh UI dump before each step."`
}

const androidExample = `{"action":"observe","serial":"emulator-5554"}`

type androidTool struct {
	permissions permission.Service
	workingDir  string
	adbPath     func() string
	mu          sync.Mutex
	ctrl        *android.Controller
	ctrlPath    string
}

// NewAndroidTool builds the Android device tool.
func NewAndroidTool(permissions permission.Service, workingDir string, adbPath func() string) fantasy.AgentTool {
	t := &androidTool{permissions: permissions, workingDir: workingDir, adbPath: adbPath}
	return fantasy.NewAgentTool(AndroidToolName, androidDescription, func(ctx context.Context, _ AndroidParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
		var p AndroidParams
		if err := strictDecode(call.Input, &p, androidExample); err != nil {
			return deviceErrorResponse(err), nil
		}
		resp, err := t.run(ctx, p, call)
		if err != nil {
			return deviceErrorResponse(androidErr(err)), nil
		}
		return resp, nil
	})
}

func androidErr(err error) error {
	var de *android.DeviceError
	if errors.As(err, &de) {
		return &DeviceToolError{Code: de.Code, Message: de.Message, Effect: "none", NextStep: "Call android devices; ask the user to fix the device state if needed."}
	}
	var stale *android.StaleError
	if errors.As(err, &stale) {
		return &DeviceToolError{Code: "stale_observation", Message: stale.Reason, Effect: "none", NextStep: "Observe again and aim with the new observation; nothing was sent."}
	}
	var ue *android.UnicodeError
	if errors.As(err, &ue) {
		code, _, _ := strings.Cut(ue.Error(), ":")
		return &DeviceToolError{Code: code, Message: ue.Error(), Effect: "none", NextStep: "Tell the user; do not claim the text was entered."}
	}
	var le *android.LocateError
	if errors.As(err, &le) {
		return &DeviceToolError{Code: "adb_not_found", Message: le.Error(), Effect: "none", NextStep: "Ask the user to install Android SDK Platform-Tools; do not download it yourself."}
	}
	return err
}

func (t *androidTool) controller(ctx context.Context) (*android.Controller, error) {
	configured := ""
	if t.adbPath != nil {
		configured = t.adbPath()
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.ctrl != nil && t.ctrlPath == configured {
		return t.ctrl, nil
	}
	path, err := android.Locate(ctx, configured)
	if err != nil {
		return nil, err
	}
	t.ctrl, t.ctrlPath = android.NewController(&android.ADB{Path: path}), configured
	return t.ctrl, nil
}

func (t *androidTool) approve(ctx context.Context, call fantasy.ToolCall, action, desc string, params any, safe bool) error {
	if t.permissions == nil {
		return nil
	}
	ok, err := t.permissions.Request(ctx, permission.CreatePermissionRequest{SessionID: GetSessionFromContext(ctx), ToolCallID: call.ID, ToolName: AndroidToolName, Action: action, Description: desc, Params: params, Path: t.workingDir, Safe: safe})
	if err != nil {
		return err
	}
	if !ok {
		return deviceErr("permission_denied", "", "the user denied this device action; nothing was sent", "")
	}
	return nil
}

func redactedParams(p AndroidParams) AndroidParams {
	if p.Sensitive {
		p.Text = "<redacted>"
	}
	for i := range p.Steps {
		if p.Steps[i].Sensitive {
			p.Steps[i].Text = "<redacted>"
		}
	}
	return p
}

func (t *androidTool) run(ctx context.Context, p AndroidParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
	c, err := t.controller(ctx)
	if err != nil {
		return fantasy.ToolResponse{}, err
	}
	if p.Action != "devices" && p.Serial == "" {
		return fantasy.ToolResponse{}, deviceErr("serial_required", "serial", "every device action needs the serial from android devices", `{"action":"devices"}`)
	}
	switch p.Action {
	case "devices":
		devices, err := c.ADB.Devices(ctx)
		if err != nil {
			return fantasy.ToolResponse{}, err
		}
		note := ""
		if len(devices) == 0 {
			note = "No device or emulator is attached. Ask the user to connect a device with USB debugging enabled or start an emulator; Atlas will not create one."
		}
		return deviceResponse(map[string]any{"ok": true, "adb": c.ADB.Path, "devices": devices, "note": note, "platform": "android"}), nil
	case "observe":
		ui := p.UI == nil || *p.UI
		obs, err := c.Observe(ctx, p.Serial, ui, p.Image)
		if err != nil {
			return fantasy.ToolResponse{}, err
		}
		meta := map[string]any{"ok": true, "serial": obs.Serial, "observation_id": obs.ID, "screen": obs.Screen, "focus": obs.Focus, "elements": trimElements(obs.Elements), "element_count": len(obs.Elements), "coordinates": "device pixels for the current rotation"}
		data, _ := json.Marshal(meta)
		if p.Image && len(obs.PNG) > 0 {
			img, scale := obs.PNG, 1.0
			if scaled, err := computer.DownscaleScreenshot(obs.PNG, computer.MaxScreenshotWidth); err == nil {
				img, scale = scaled.PNG, scaled.Scale
			}
			meta["image_scale"] = scale
			data, _ = json.Marshal(meta)
			resp := fantasy.NewImageResponse(img, "image/png")
			resp.Content = string(data)
			return resp, nil
		}
		return fantasy.NewTextResponse(string(data)), nil
	case "tap":
		if err := t.approve(ctx, call, "tap", "Tap on Android device "+p.Serial, p, true); err != nil {
			return fantasy.ToolResponse{}, err
		}
		x, y, err := c.Tap(ctx, p.Serial, p.ObservationID, android.Target{Element: p.Element, ResourceID: p.ResourceID, Text: p.Text, ContentDesc: p.ContentDesc, X: p.X, Y: p.Y})
		if err != nil {
			return fantasy.ToolResponse{}, err
		}
		return deviceResponse(map[string]any{"ok": true, "serial": p.Serial, "tapped": [2]int{x, y}, "input_sent": true, "next": "Observe again before the next tap; the observation was consumed."}), nil
	case "swipe":
		if err := t.approve(ctx, call, "swipe", "Swipe on Android device "+p.Serial, p, true); err != nil {
			return fantasy.ToolResponse{}, err
		}
		if err := c.Swipe(ctx, p.Serial, p.ObservationID, p.X, p.Y, p.EndX, p.EndY, p.DurationMS); err != nil {
			return fantasy.ToolResponse{}, err
		}
		return deviceResponse(map[string]any{"ok": true, "serial": p.Serial, "input_sent": true}), nil
	case "type":
		if err := t.approve(ctx, call, "type", "Type text on Android device "+p.Serial, redactedParams(p), true); err != nil {
			return fantasy.ToolResponse{}, err
		}
		method, err := c.TypeTextObserved(ctx, p.Serial, p.Text, p.ObservationID)
		if err != nil {
			return fantasy.ToolResponse{}, err
		}
		return deviceResponse(map[string]any{"ok": true, "serial": p.Serial, "method": method, "input_sent": true, "verified": false, "next": "Confirm with verify_text; typing is not proof the field holds the text."}), nil
	case "key":
		if err := t.approve(ctx, call, "key", "Press "+p.Key+" on Android device "+p.Serial, p, true); err != nil {
			return fantasy.ToolResponse{}, err
		}
		if err := c.KeyObserved(ctx, p.Serial, p.Key, p.ObservationID); err != nil {
			return fantasy.ToolResponse{}, err
		}
		return deviceResponse(map[string]any{"ok": true, "serial": p.Serial, "key": p.Key, "input_sent": true}), nil
	case "launch":
		if err := t.approve(ctx, call, "launch", "Open "+p.Package+" on Android device "+p.Serial, p, true); err != nil {
			return fantasy.ToolResponse{}, err
		}
		if err := c.Launch(ctx, p.Serial, p.Package, p.Activity); err != nil {
			return fantasy.ToolResponse{}, err
		}
		return deviceResponse(map[string]any{"ok": true, "serial": p.Serial, "package": p.Package, "next": "Observe to confirm the app is in the foreground."}), nil
	case "verify_text":
		el, obs, err := c.FindText(ctx, p.Serial, p.Text, android.Target{ResourceID: p.ResourceID})
		if err != nil {
			return fantasy.ToolResponse{}, err
		}
		resp := deviceResponse(map[string]any{"ok": el != nil, "serial": p.Serial, "found": el != nil, "element": el, "focus": obs.Focus, "observation_id": obs.ID})
		resp.IsError = el == nil
		return resp, nil
	case "install":
		apk, err := transfer.Resolve(t.workingDir, p.APKPath)
		if err != nil {
			return fantasy.ToolResponse{}, deviceErr(transfer.Code(err), "apk_path", err.Error(), "apk_path must be a .apk inside this workspace.")
		}
		sum, herr := transfer.HashFile(apk)
		if herr != nil {
			return fantasy.ToolResponse{}, deviceErr("not_found", "apk_path", herr.Error(), "")
		}
		// Installing software is never Safe: it always asks.
		if err := t.approve(ctx, call, "install", fmt.Sprintf("Install %s (sha256 %s) on Android device %s", p.APKPath, sum[:16], p.Serial), p, false); err != nil {
			return fantasy.ToolResponse{}, err
		}
		res, err := c.Install(ctx, p.Serial, apk, p.Replace)
		if err != nil {
			return fantasy.ToolResponse{}, err
		}
		return deviceResponse(map[string]any{"ok": true, "serial": p.Serial, "install": res}), nil
	case "push":
		local, err := transfer.Resolve(t.workingDir, p.LocalPath)
		if err != nil {
			return fantasy.ToolResponse{}, deviceErr(transfer.Code(err), "local_path", err.Error(), "")
		}
		if err := t.approve(ctx, call, "push", fmt.Sprintf("Copy %s to %s on %s", p.LocalPath, p.DevicePath, p.Serial), p, false); err != nil {
			return fantasy.ToolResponse{}, err
		}
		sum, err := c.Push(ctx, p.Serial, local, p.DevicePath)
		if err != nil {
			return fantasy.ToolResponse{}, err
		}
		return deviceResponse(map[string]any{"ok": true, "serial": p.Serial, "device_path": p.DevicePath, "sha256": sum, "verified": true}), nil
	case "pull":
		if err := t.approve(ctx, call, "pull", fmt.Sprintf("Copy %s from %s into %s", p.DevicePath, p.Serial, p.LocalPath), p, false); err != nil {
			return fantasy.ToolResponse{}, err
		}
		path, err := c.Pull(ctx, p.Serial, p.DevicePath, t.workingDir, p.LocalPath, p.Overwrite)
		if err != nil {
			return fantasy.ToolResponse{}, err
		}
		return deviceResponse(map[string]any{"ok": true, "serial": p.Serial, "path": path, "verified": true}), nil
	case "sequence":
		return t.sequence(ctx, c, p, call)
	}
	return fantasy.ToolResponse{}, deviceErr("invalid_action", "action", fmt.Sprintf("unknown action %q", p.Action), "")
}

func trimElements(els []android.Element) []android.Element {
	// Keep the tree useful but bounded: drop empty layout containers.
	var out []android.Element
	for _, e := range els {
		if e.Text == "" && e.ContentDesc == "" && e.ResourceID == "" && !e.Clickable {
			continue
		}
		out = append(out, e)
		if len(out) >= 150 {
			break
		}
	}
	return out
}

type stepOutcome struct {
	Index  int    `json:"index"`
	Action string `json:"action"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
	Error  string `json:"error,omitempty"`
}

func (t *androidTool) sequence(ctx context.Context, c *android.Controller, p AndroidParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
	if len(p.Steps) == 0 || len(p.Steps) > 24 {
		return fantasy.ToolResponse{}, deviceErr("invalid_request", "steps", "sequence needs 1-24 steps", "")
	}
	for i, s := range p.Steps {
		switch s.Action {
		case "tap":
			if s.ResourceID == "" && s.Text == "" && s.ContentDesc == "" {
				return fantasy.ToolResponse{}, deviceErr("invalid_request", "steps", fmt.Sprintf("step %d: sequence taps need resource_id, text or content_desc (coordinates would be stale after earlier steps)", i+1), "")
			}
		case "type", "wait_text":
			if s.Text == "" {
				return fantasy.ToolResponse{}, deviceErr("invalid_request", "steps", fmt.Sprintf("step %d: %s needs text", i+1, s.Action), "")
			}
		case "key", "launch", "swipe":
		default:
			return fantasy.ToolResponse{}, deviceErr("invalid_request", "steps", fmt.Sprintf("step %d: unknown action %q", i+1, s.Action), "")
		}
	}
	if err := t.approve(ctx, call, "sequence", fmt.Sprintf("Run %d steps on Android device %s", len(p.Steps), p.Serial), redactedParams(p), true); err != nil {
		return fantasy.ToolResponse{}, err
	}
	var outcomes []stepOutcome
	failed := false
	for i, s := range p.Steps {
		o := stepOutcome{Index: i + 1, Action: s.Action, Status: "completed"}
		if ctx.Err() != nil {
			o.Status, o.Error = "canceled", "sequence canceled"
			outcomes = append(outcomes, o)
			failed = true
			continue
		}
		if failed {
			o.Status, o.Error = "skipped", "an earlier step failed"
			outcomes = append(outcomes, o)
			continue
		}
		var err error
		switch s.Action {
		case "tap":
			var obs *android.Observation
			if obs, err = c.Observe(ctx, p.Serial, true, false); err == nil {
				var x, y int
				x, y, err = c.Tap(ctx, p.Serial, obs.ID, android.Target{ResourceID: s.ResourceID, Text: s.Text, ContentDesc: s.ContentDesc})
				o.Detail = fmt.Sprintf("tapped (%d,%d)", x, y)
			}
		case "swipe":
			var obs *android.Observation
			if obs, err = c.Observe(ctx, p.Serial, false, false); err == nil {
				err = c.Swipe(ctx, p.Serial, obs.ID, s.X, s.Y, s.EndX, s.EndY, 300)
			}
		case "type":
			var m android.TextMethod
			m, err = c.TypeText(ctx, p.Serial, s.Text)
			o.Detail = string(m)
		case "key":
			err = c.Key(ctx, p.Serial, s.Key)
		case "launch":
			err = c.Launch(ctx, p.Serial, s.Package, s.Activity)
		case "wait_text":
			timeout := time.Duration(min(max(s.TimeoutMS, 500), 15000)) * time.Millisecond
			deadline := time.Now().Add(timeout)
			for {
				var el *android.Element
				el, _, err = c.FindText(ctx, p.Serial, s.Text, android.Target{ResourceID: s.ResourceID})
				if err != nil || el != nil {
					break
				}
				if time.Now().After(deadline) {
					err = fmt.Errorf("text not shown within %s", timeout)
					break
				}
				select {
				case <-ctx.Done():
					err = ctx.Err()
				case <-time.After(400 * time.Millisecond):
				}
				if ctx.Err() != nil {
					break
				}
			}
		}
		if err != nil {
			o.Status, o.Error = "failed", androidErr(err).Error()
			failed = true
		}
		outcomes = append(outcomes, o)
	}
	resp := deviceResponse(map[string]any{"ok": !failed, "serial": p.Serial, "steps": outcomes})
	resp.IsError = failed
	return resp, nil
}
