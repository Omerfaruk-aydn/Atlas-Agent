package browser

import (
	"context"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"
)

// Request describes advanced actions without exposing arbitrary JavaScript.
type Request struct {
	DownloadID     string    `json:"download_id,omitempty"`
	Scope          string    `json:"scope,omitempty" description:"CSS container limiting the semantic target, including open shadow roots."`
	Match          string    `json:"match,omitempty" description:"Name/label comparison: exact (default), contains, or case_insensitive."`
	ExpectedOrigin string    `json:"expected_origin,omitempty" description:"Require this exact scheme/host/port before acting."`
	ExpectedTabID  string    `json:"expected_tab_id,omitempty" description:"Fail if another tab is selected."`
	DocumentID     string    `json:"document_id,omitempty" description:"Optional loader id from find; fail if the document was replaced."`
	URL            string    `json:"url,omitempty"`
	Accept         bool      `json:"accept,omitempty"`
	PromptText     string    `json:"prompt_text,omitempty"`
	Action         string    `json:"action,omitempty"`
	Selector       string    `json:"selector,omitempty"`
	Role           string    `json:"role,omitempty"`
	Name           string    `json:"name,omitempty"`
	Label          string    `json:"label,omitempty"`
	Text           string    `json:"text,omitempty"`
	FrameID        string    `json:"frame_id,omitempty"`
	TabID          string    `json:"tab_id,omitempty"`
	Condition      string    `json:"condition,omitempty"`
	Expected       string    `json:"expected,omitempty"`
	TimeoutMS      int       `json:"timeout_ms,omitempty"`
	Paths          []string  `json:"paths,omitempty"`
	X              float64   `json:"x,omitempty"`
	Y              float64   `json:"y,omitempty"`
	Width          float64   `json:"width,omitempty"`
	Height         float64   `json:"height,omitempty"`
	NewerThan      time.Time `json:"newer_than,omitempty"`
	// Pointer and keyboard input for canvas-grade pages. Coordinates are
	// pixels of the screenshot that issued ViewportID.
	ViewportID string   `json:"viewport_id,omitempty" description:"For pointer_*: viewport_id from the latest browser screenshot. Scroll, zoom, resize, tab change or navigation invalidates it."`
	ToX        float64  `json:"to_x,omitempty" description:"For pointer_drag: end x in screenshot pixels."`
	ToY        float64  `json:"to_y,omitempty" description:"For pointer_drag: end y in screenshot pixels."`
	Button     string   `json:"button,omitempty" description:"For pointer_click/pointer_drag: left (default), right or middle."`
	ClickCount int      `json:"click_count,omitempty" description:"For pointer_click: 1 (default), 2 for double click, 3 for triple click."`
	Modifiers  []string `json:"modifiers,omitempty" description:"Held during pointer_* or key_combo: alt, ctrl, meta, shift."`
	Steps      int      `json:"steps,omitempty" description:"For pointer_drag: intermediate moves, 1-100 (default 12)."`
	HoldMS     int      `json:"hold_ms,omitempty" description:"For pointer_click/pointer_drag: press duration before release or movement, 0-5000."`
	Key        string   `json:"key,omitempty" description:"For key_combo: a letter, digit, punctuation or named key such as enter, escape, delete, space, arrowleft, f2."`
	// Field, form and target-context parameters.
	Within     string `json:"within,omitempty" description:"Scope the target to the one visible container (dialog, panel, card, form, region) whose accessible name or heading contains this text, e.g. \"Share\" or \"Computer\"."`
	WithinRole string `json:"within_role,omitempty" description:"Optional role/tag narrowing within, e.g. dialog, region, article."`
	Value      string `json:"value,omitempty" description:"For set_field/select_option: the value or option to apply. Required; an empty string clears the field (same as mode clear)."`
	// ValueSet records that the JSON carried value, so "" (clear) and a
	// missing value stay distinct. Atlas sets it when decoding.
	ValueSet bool   `json:"-"`
	Mode     string `json:"mode,omitempty" description:"For set_field: replace (default), append, clear or replace_selection. For observe: full, delta (default) or scope."`
	Commit   string `json:"commit,omitempty" description:"For set_field: auto (default; chosen per field kind), enter, tab, blur or none."`
	SettleMS int    `json:"settle_ms,omitempty" description:"For set_field and select_option: observation window after the commit, 0-5000 ms (default 300). The value must hold through the whole window (and 250 ms after its last change) to count as applied; raise it for applications that confirm or reject a value asynchronously."`
	// LearnedCommit is filled by Atlas from verified history, never by the model.
	LearnedCommit string      `json:"-"`
	Fields        []FormField `json:"fields,omitempty" description:"For fill_form: fields to set in order, each with its own target and value."`
	OnlyInvalid   bool        `json:"only_invalid,omitempty" description:"For fill_form: skip fields that already hold their value and pass validation; fix only the rest."`
	URLs          []string    `json:"urls,omitempty" description:"For research: 1-8 HTTP(S) pages to read in parallel background tabs."`
	Purpose       string      `json:"purpose,omitempty" description:"For tab_new: why the tab exists, recorded with it (e.g. \"checkout\")."`
	RowSelector   string      `json:"row_selector,omitempty" description:"For collect: CSS selector matching one row/item."`
	RowKey        string      `json:"row_key,omitempty" description:"For collect: attribute that identifies a row (default data-id, data-key, aria-rowindex or id)."`
	NextSelector  string      `json:"next_selector,omitempty" description:"For collect: unique pagination control clicked when the list end is reached."`
	MaxItems      int         `json:"max_items,omitempty" description:"For collect: stop after this many unique rows (1-2000, default 500)."`
	MaxScrolls    int         `json:"max_scrolls,omitempty" description:"For collect: bound on scroll steps (1-200, default 40)."`
	Cursor        int         `json:"cursor,omitempty" description:"For observe: element offset to continue a truncated list. For collect: scroll offset to resume from."`
	AllowResubmit bool        `json:"allow_resubmit,omitempty" description:"For submit: permit repeating an identical submission. Only after verifying the first one was not accepted."`
	// Verify asks captcha_solve to click the challenge's submit button
	// after filling the answer.
	Verify bool `json:"verify,omitempty" description:"For captcha_solve/captcha_fill: click the challenge verify/submit button after filling the answer."`
	// VerifySelector names the challenge submit button for captcha_fill
	// when verify is set. Empty reuses selector.
	VerifySelector string `json:"verify_selector,omitempty" description:"For captcha_fill: CSS selector of the challenge verify button, used when verify is true."`
}

// FormField is one entry of a fill_form request.
type FormField struct {
	Selector string `json:"selector,omitempty"`
	Ref      string `json:"ref,omitempty"`
	Role     string `json:"role,omitempty"`
	Name     string `json:"name,omitempty"`
	Label    string `json:"label,omitempty"`
	Within   string `json:"within,omitempty"`
	Value    string `json:"value"`
	Commit   string `json:"commit,omitempty"`
	Kind     string `json:"kind,omitempty" description:"field (default) or option for selects and custom comboboxes."`
	FrameID  string `json:"frame_id,omitempty" description:"Frame of this field when it differs from the form's frame_id."`
}

// AdvancedSession is optional so lightweight and mock drivers remain usable.
type AdvancedSession interface {
	Advanced(context.Context, Request) (json.RawMessage, []byte, error)
}

func (s *chromedpSession) command(ctx context.Context, method string, params any, browserLevel bool) (map[string]any, error) {
	var result map[string]any
	err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		c := chromedp.FromContext(ctx)
		if browserLevel {
			return c.Browser.Execute(ctx, method, params, &result)
		}
		return c.Target.Execute(ctx, method, params, &result)
	}))
	return result, err
}

// Advanced executes one bounded action on the selected tab.
func (s *chromedpSession) Advanced(parent context.Context, p Request) (json.RawMessage, []byte, error) {
	if err := parent.Err(); err != nil {
		return nil, nil, err
	}
	if len(p.Scope) > 4096 || len(p.ExpectedOrigin) > 2048 || len(p.Selector) > 4096 || len(p.Name) > 4096 || len(p.Label) > 4096 || len(p.Expected) > 16384 || len(p.Text) > 1024*1024 || len(p.Paths) > 32 {
		return nil, nil, errors.New("advanced action parameters exceed limits")
	}
	if p.Action == "semantic_click" || p.Action == "semantic_type" {
		if err := s.recoverInput(parent); err != nil {
			return nil, nil, err
		}
	}
	switch p.Action {
	case "capture_region", "find", "assert", "text", "html", "accessibility", "inspect", "frames", "read", "snapshot":
		restore, err := s.suspendActivity()
		if err != nil {
			return nil, nil, err
		}
		defer restore()
	}
	if p.Action == "tab_new" || p.Action == "tab_select" {
		restore, err := s.suspendActivity(true)
		if err != nil {
			return nil, nil, err
		}
		defer restore()
	}
	timeout := 10 * time.Second
	if p.TimeoutMS < 0 || p.TimeoutMS > 30000 {
		return nil, nil, errors.New("timeout_ms must be 0-30000")
	}
	if p.TimeoutMS > 0 {
		timeout = time.Duration(p.TimeoutMS) * time.Millisecond
	}
	base := s.currentContext()
	if s.tabClosed() {
		// The selected tab closed (a popup that closed itself). Tab
		// inventory and selection still work through the browser; page
		// actions report tab_closed instead of a disconnected browser.
		switch p.Action {
		case "tabs", "tab_select", "tab_return", "tab_new", "popup_wait":
			base = s.rootCtx
		default:
			return nil, nil, ErrTabClosed
		}
	}
	ctx, cancel := context.WithTimeout(base, timeout)
	defer cancel()
	stop := context.AfterFunc(parent, cancel)
	defer stop()
	if err := s.guardBrowserDocument(ctx, p); err != nil {
		return nil, nil, err
	}
	finish := func(v any, err error) (json.RawMessage, []byte, error) {
		if err != nil {
			return nil, nil, err
		}
		data, err := json.Marshal(v)
		return data, nil, err
	}
	switch p.Action {
	case "wait_for":
		return s.waitBrowserCondition(ctx, p)
	case "dialog_wait", "dialog_handle":
		return s.browserDialogAction(ctx, p)
	case "popup_wait":
		return s.waitBrowserPopup(ctx, p)
	case "tabs":
		v, err := s.command(ctx, "Target.getTargets", map[string]any{}, true)
		if err != nil {
			return nil, nil, err
		}
		var pages []any
		if list, ok := v["targetInfos"].([]any); ok {
			for _, item := range list {
				if info, ok := item.(map[string]any); ok && info["type"] == "page" {
					pages = append(pages, info)
				}
			}
		}
		s.rememberBrowserTabs(pages)
		s.annotateTabs(pages)
		active := s.ActiveTab()
		if s.currentTabGone() {
			active = ""
		}
		return finish(map[string]any{"tabs": pages, "active_tab": active}, nil)
	case "tab_return":
		s.ensureSessionTab()
		id, err := s.returnTab(s.ActiveTab())
		if err != nil {
			return nil, nil, err
		}
		s.mu.Lock()
		if s.observedTabs == nil {
			s.observedTabs = map[string]bool{}
		}
		s.observedTabs[id] = true
		s.mu.Unlock()
		p.Action, p.TabID = "tab_select", id
		data, image, err := s.Advanced(parent, p)
		if err != nil {
			return nil, nil, fmt.Errorf("tab_return to %s failed: %w", id, err)
		}
		return data, image, nil
	case "tab_new", "tab_select", "tab_close":
		if p.Action != "tab_new" {
			if err := s.validateObservedTab(ctx, p.TabID); err != nil {
				return nil, nil, err
			}
		}
		if p.Action == "tab_close" {
			if p.TabID == "" {
				return nil, nil, errors.New("tab_id is required")
			}
			if p.TabID == s.ActiveTab() && !s.currentTabGone() {
				return nil, nil, errors.New("select another tab before closing the active tab")
			}
			v, err := s.command(ctx, "Target.closeTarget", map[string]any{"targetId": p.TabID}, true)
			if err == nil {
				s.mu.Lock()
				if rec := s.tabRecords[p.TabID]; rec != nil {
					rec.Closed = true
				}
				s.mu.Unlock()
			}
			return finish(v, err)
		}
		id := p.TabID
		if p.Action == "tab_new" {
			id = "new"
		}
		if id == "" {
			return nil, nil, errors.New("tab_id is required")
		}
		var next context.Context
		var closeTab context.CancelFunc
		if p.Action == "tab_new" {
			next, closeTab = chromedp.NewContext(s.rootCtx)
		} else {
			next, closeTab = chromedp.NewContext(s.rootCtx, chromedp.WithTargetID(target.ID(id)))
		}
		launchTimer := time.AfterFunc(timeout, closeTab)
		stopParent := context.AfterFunc(parent, closeTab)
		err := chromedp.Run(next)
		launchTimer.Stop()
		stopParent()
		if err != nil {
			closeTab()
			return nil, nil, fmt.Errorf("attach browser tab: %w", err)
		}
		previous := s.ActiveTab()
		// A document indicator belongs to its old tab. Remove it before
		// switching so cancellation cannot leave it running in the background.
		if s.activityNative == nil {
			_ = s.activityCleanup(activityRemove)
		}
		s.mu.Lock()
		s.tabCancels = append(s.tabCancels, closeTab)
		s.ctx = next
		s.dialogs = nil
		s.requests = nil
		s.requestTrackingIncomplete = false
		s.mu.Unlock()
		id = string(chromedp.FromContext(next).Target.TargetID)
		s.ensureSessionTab()
		if p.Action == "tab_new" {
			s.recordTab(TabRecord{ID: id, Opener: previous, CreatedBy: "tab_new", Owned: true, Purpose: p.Purpose})
		}
		s.mu.Lock()
		if s.observedTabs == nil {
			s.observedTabs = map[string]bool{}
		}
		s.observedTabs[id] = true
		s.mu.Unlock()
		s.listenBrowserTarget(next)
		_, err = s.command(next, "Network.enable", map[string]any{}, false)
		if err != nil {
			return nil, nil, err
		}
		_, err = s.RawCDP("Runtime.enable", map[string]any{})
		if err != nil {
			return nil, nil, err
		}
		_, err = s.RawCDP("Page.enable", map[string]any{})
		if err == nil {
			// Attaching a target does not activate its tab. Background tabs
			// can suspend animation frames used to verify stable click targets.
			_, err = s.command(next, "Page.bringToFront", map[string]any{}, false)
		}
		if err == nil && s.activityNative != nil && p.Action == "tab_select" {
			err = s.activityCleanup(activityRemove)
		}
		return finish(map[string]any{"active_tab": id}, err)
	case "frames":
		v, err := s.command(ctx, "Page.getFrameTree", map[string]any{}, false)
		return finish(v, err)
	case "network":
		s.mu.Lock()
		entries := append([]NetworkEntry(nil), s.network...)
		s.mu.Unlock()
		return finish(entries, nil)
	case "capture_region":
		if p.X < 0 || p.Y < 0 || p.Width <= 0 || p.Height <= 0 || p.Width > 4096 || p.Height > 4096 {
			return nil, nil, errors.New("invalid capture rectangle; maximum 4096 pixels per dimension")
		}
		v, err := s.command(ctx, "Page.captureScreenshot", map[string]any{"format": "png", "captureBeyondViewport": false, "clip": map[string]any{"x": p.X, "y": p.Y, "width": p.Width, "height": p.Height, "scale": 1}}, false)
		if err != nil {
			return nil, nil, err
		}
		raw, _ := v["data"].(string)
		data, err := base64.StdEncoding.DecodeString(raw)
		return nil, data, err
	case "download_start":
		if len(p.Paths) != 1 {
			return nil, nil, errors.New("provide one validated download directory")
		}
		if err := os.MkdirAll(p.Paths[0], 0o700); err != nil {
			return nil, nil, err
		}
		if err := s.armDownloads(ctx); err != nil {
			return nil, nil, err
		}
		v, err := s.command(ctx, "Browser.setDownloadBehavior", map[string]any{"behavior": "allow", "downloadPath": p.Paths[0], "eventsEnabled": true}, true)
		if v == nil {
			v = map[string]any{}
		}
		v["started_at"] = time.Now().UTC()
		return finish(v, err)
	case "download_wait":
		if len(p.Paths) != 1 {
			return nil, nil, errors.New("provide one expected download file")
		}
		if p.NewerThan.IsZero() {
			return nil, nil, errors.New("newer_than is required to distinguish a new download from an existing file")
		}
		path := p.Paths[0]
		for {
			if err := s.guardBrowserDocument(ctx, p); err != nil {
				return nil, nil, err
			}
			download, err := s.completedDownload(p)
			if err != nil {
				return nil, nil, err
			}
			info, err := os.Stat(path)
			_, partial := os.Stat(path + ".crdownload")
			if download.ID != "" && err == nil && info.Mode().IsRegular() && info.ModTime().After(p.NewerThan) && os.IsNotExist(partial) {
				return finish(map[string]any{"passed": true, "download_id": download.ID, "path": path, "bytes": info.Size(), "extension": filepath.Ext(path), "completed": true}, nil)
			}
			select {
			case <-ctx.Done():
				return nil, nil, fmt.Errorf("download_timeout: %w", ctx.Err())
			case <-time.After(250 * time.Millisecond):
			}
		}
	}
	world := 0.0
	if p.FrameID != "" {
		v, err := s.command(ctx, "Page.createIsolatedWorld", map[string]any{"frameId": p.FrameID, "worldName": "atlas-interaction"}, false)
		if err != nil {
			return nil, nil, err
		}
		world, _ = v["executionContextId"].(float64)
	}
	eval := func(script string) (map[string]any, error) {
		args := map[string]any{"expression": script, "returnByValue": true, "awaitPromise": true}
		if world != 0 {
			args["contextId"] = world
		}
		v, err := s.command(ctx, "Runtime.evaluate", args, false)
		if err != nil {
			return nil, err
		}
		if v["exceptionDetails"] != nil {
			return nil, errors.New("target_missing: evaluation failed; inspect frame and target")
		}
		r, _ := v["result"].(map[string]any)
		value, _ := r["value"].(map[string]any)
		return value, nil
	}
	if p.Action == "upload" {
		if p.Selector == "" || len(p.Paths) == 0 {
			return nil, nil, errors.New("selector and paths are required")
		}
		selector, _ := json.Marshal(p.Selector)
		script := `(()=>{const roots=[document];let matches=[];for(let i=0;i<roots.length&&i<100;i++){for(const e of roots[i].querySelectorAll('*'))if(e.shadowRoot)roots.push(e.shadowRoot);matches.push(...roots[i].querySelectorAll(` + string(selector) + `))}if(matches.length!==1||matches[0].tagName!=='INPUT'||matches[0].type!=='file')throw new Error('Unique file input required');return matches[0]})()`
		args := map[string]any{"expression": script, "returnByValue": false}
		if world != 0 {
			args["contextId"] = world
		}
		v, err := s.command(ctx, "Runtime.evaluate", args, false)
		if err != nil {
			return nil, nil, err
		}
		if v["exceptionDetails"] != nil {
			return nil, nil, errors.New("upload requires exactly one observed file input")
		}
		object, _ := v["result"].(map[string]any)
		objectID, _ := object["objectId"].(string)
		if objectID == "" {
			return nil, nil, errors.New("file input is no longer available")
		}
		defer func() { _, _ = s.command(ctx, "Runtime.releaseObject", map[string]any{"objectId": objectID}, false) }()
		_, err = s.command(ctx, "DOM.setFileInputFiles", map[string]any{"objectId": objectID, "files": p.Paths}, false)
		if err != nil {
			return nil, nil, err
		}
		v, err = s.command(ctx, "Runtime.callFunctionOn", map[string]any{"objectId": objectID, "functionDeclaration": `function(){return {count:this.files.length,files:Array.from(this.files).map(f=>({name:f.name,bytes:f.size}))}}`, "returnByValue": true}, false)
		if err != nil {
			return nil, nil, fmt.Errorf("upload_sent_verification_unavailable: %w", err)
		}
		result, _ := v["result"].(map[string]any)
		value, _ := result["value"].(map[string]any)
		if count, _ := value["count"].(float64); int(count) != len(p.Paths) {
			return nil, nil, errors.New("upload_verification_failed: file input count does not match")
		}
		value["passed"], value["verify_required"] = true, true
		return finish(value, nil)
	}
	if p.Action != "find" && p.Action != "assert" && p.Action != "semantic_click" && p.Action != "semantic_type" {
		return nil, nil, fmt.Errorf("unsupported advanced action %q", p.Action)
	}
	if p.Action == "assert" && !validCondition(p.Condition) {
		return nil, nil, errors.New("condition must be visible, hidden, text, value, enabled, url, title, checked, selected or count")
	}
	if p.Match != "" && p.Match != "exact" && p.Match != "contains" && p.Match != "case_insensitive" {
		return nil, nil, errors.New("match must be exact, contains or case_insensitive")
	}
	if p.Selector == "" && p.Name == "" && p.Label == "" && p.Role == "" && p.Condition != "url" && p.Condition != "title" {
		return nil, nil, errors.New("a selector or semantic target is required")
	}
	encoded, _ := json.Marshal(p)
	script := strings.Replace(strings.Replace(semanticScript, "__TARGET_LIB__", targetLibScript, 1), "__PARAMS__", string(encoded), 1)
	var last map[string]any
	waits := newResolutionWaits(time.Now(), p)
	for {
		if err := s.guardBrowserDocument(ctx, p); err != nil {
			return nil, nil, err
		}
		v, err := eval(script)
		if err != nil {
			// Only observation/target resolution is retried. A dispatched
			// click or text insertion is never replayed by this loop.
			if p.FrameID != "" || !transientBrowserContextError(err) {
				return nil, nil, err
			}
			if err := browserPoll(ctx); err != nil {
				return nil, nil, err
			}
			continue
		}
		last = v
		reason, _ := v["reason"].(string)
		scopeCount, _ := v["count"].(float64)
		if reason == "target_scan_limit" || (reason == "scope_not_unique" && scopeCount > 1) || reason == "password inspection prohibited" || (reason == "within_not_unique" && scopeCount > 1) {
			candidates, _ := json.Marshal(v["candidates"])
			return nil, nil, fmt.Errorf("target_resolution_failed: %s; refine the target or scope %s", reason, candidates)
		}
		if p.Action == "find" {
			if tree, err := s.command(ctx, "Page.getFrameTree", map[string]any{}, false); err == nil {
				frame := browserFrameInfo(tree, p.FrameID)
				v["document_id"] = frame["loaderId"]
			}
			return finish(v, nil)
		}
		if passed, _ := v["passed"].(bool); passed {
			if p.Action == "assert" {
				return finish(v, nil)
			}
			if p.Action == "semantic_click" {
				x, _ := v["x"].(float64)
				y, _ := v["y"].(float64)
				dx, dy, err := s.frameOffset(ctx, p.FrameID)
				if err != nil {
					return nil, nil, err
				}
				s.updateActivityPoint(int(x+dx), int(y+dy))
				m, id := s.pointerOwner()
				presentPointer(m, id, "aim", x+dx, y+dy)
				if err := s.guardBrowserDocument(ctx, p); err != nil {
					return nil, nil, err
				}
				fresh, err := eval(script)
				if err != nil {
					return nil, nil, err
				}
				if fresh["passed"] != true || fresh["x"] != v["x"] || fresh["y"] != v["y"] {
					return nil, nil, errors.New("target_changed_before_dispatch: resolve the current target; no click was sent")
				}
				err = chromedp.Run(ctx, chromedp.MouseClickXY(x+dx, y+dy))
				if err == nil {
					presentPointer(m, id, "click", x+dx, y+dy)
				}
				return finish(map[string]any{"action_sent": err == nil, "verify_required": true, "target": v["target"]}, err)
			}
			if err := s.guardBrowserDocument(ctx, p); err != nil {
				return nil, nil, err
			}
			fresh, err := eval(script)
			if err != nil {
				return nil, nil, err
			}
			if fresh["passed"] != true {
				return nil, nil, errors.New("target_changed_before_dispatch: no text was sent")
			}
			_, err = s.command(ctx, "Input.insertText", map[string]any{"text": p.Text}, false)
			return finish(map[string]any{"action_sent": err == nil, "verify_required": true, "committed": false, "target": v["target"], "next": "Text was inserted but not committed. Use set_field for fields whose value must be applied and read back."}, err)
		}
		if count, _ := v["count"].(float64); count > 1 && p.Condition != "hidden" && p.Condition != "count" {
			candidates, _ := json.Marshal(v["candidates"])
			return nil, nil, fmt.Errorf("ambiguous_target: %d visible matches %s; refine with within, scope, role, name or label. Nothing was dispatched", int(count), candidates)
		}
		if err := waits.check(time.Now(), v); err != nil {
			return nil, nil, err
		}
		select {
		case <-ctx.Done():
			return nil, nil, fmt.Errorf("condition_timeout: expected %s not reached; last observed %s: %w", describeWait(p), describeLastState(last), ctx.Err())
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func validCondition(c string) bool {
	switch c {
	case "visible", "hidden", "text", "value", "enabled", "url", "title", "checked", "selected", "count", "text_contains", "value_not", "not_busy":
		return true
	}
	return false
}

// describeWait names what a wait or assertion was looking for, so a
// timeout says which condition failed rather than only that time ran out.
func describeWait(p Request) string {
	target := p.Selector
	if target == "" {
		target = strings.TrimSpace(strings.Join([]string{p.Role, quoteIf(p.Name), quoteIf(p.Label)}, " "))
	}
	if p.Within != "" {
		target += " within " + quoteIf(p.Within)
	}
	condition := p.Condition
	if condition == "" {
		condition = p.Action
	}
	if p.Expected != "" {
		condition += "=" + quoteIf(p.Expected)
	}
	if target == "" {
		return condition
	}
	return condition + " of " + target
}

func quoteIf(s string) string {
	if s == "" {
		return ""
	}
	return fmt.Sprintf("%q", s)
}

// describeLastState summarizes the final poll of a wait for its error.
func describeLastState(v map[string]any) string {
	if v == nil {
		return "nothing (page not evaluable)"
	}
	keep := map[string]any{}
	for _, key := range []string{"count", "actual", "blocker", "reason", "target", "candidates"} {
		if value, ok := v[key]; ok && value != nil && value != "" {
			keep[key] = value
		}
	}
	data, _ := json.Marshal(keep)
	if len(data) > 600 {
		data = append(data[:600], []byte("...")...)
	}
	return string(data)
}

// Semantic observations cross open shadow roots and never emit password values.
//
//go:embed semantic.js
var semanticScript string

func (s *chromedpSession) frameOffset(ctx context.Context, id string) (float64, float64, error) {
	if id == "" {
		return 0, 0, nil
	}
	tree, err := s.command(ctx, "Page.getFrameTree", map[string]any{}, false)
	if err != nil {
		return 0, 0, err
	}
	parents := map[string]string{}
	var walk func(map[string]any, string)
	walk = func(node map[string]any, parent string) {
		frame, _ := node["frame"].(map[string]any)
		current, _ := frame["id"].(string)
		parents[current] = parent
		children, _ := node["childFrames"].([]any)
		for _, child := range children {
			if child, ok := child.(map[string]any); ok {
				walk(child, current)
			}
		}
	}
	root, _ := tree["frameTree"].(map[string]any)
	walk(root, "")
	if _, ok := parents[id]; !ok {
		return 0, 0, errors.New("target_missing: frame disappeared")
	}
	// DOM.getBoxModel reports quads in the main frame's viewport, so the
	// innermost frame owner's content box already includes every ancestor
	// frame's offset; adding the ancestors again double-counts nesting.
	var x, y float64
	for depth := 0; parents[id] != "" && depth < 1; depth++ {
		owner, err := s.command(ctx, "DOM.getFrameOwner", map[string]any{"frameId": id}, false)
		if err != nil {
			return 0, 0, err
		}
		box, err := s.command(ctx, "DOM.getBoxModel", map[string]any{"backendNodeId": owner["backendNodeId"]}, false)
		if err != nil {
			return 0, 0, err
		}
		model, _ := box["model"].(map[string]any)
		quad, _ := model["content"].([]any)
		if len(quad) < 2 {
			return 0, 0, errors.New("frame has no visible bounds")
		}
		qx, _ := quad[0].(float64)
		qy, _ := quad[1].(float64)
		x += qx
		y += qy
	}
	return x, y, nil
}
