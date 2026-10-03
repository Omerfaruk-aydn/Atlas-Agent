package browser

import (
	"context"
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
	Action    string    `json:"action,omitempty"`
	Selector  string    `json:"selector,omitempty"`
	Role      string    `json:"role,omitempty"`
	Name      string    `json:"name,omitempty"`
	Label     string    `json:"label,omitempty"`
	Text      string    `json:"text,omitempty"`
	FrameID   string    `json:"frame_id,omitempty"`
	TabID     string    `json:"tab_id,omitempty"`
	Condition string    `json:"condition,omitempty"`
	Expected  string    `json:"expected,omitempty"`
	TimeoutMS int       `json:"timeout_ms,omitempty"`
	Paths     []string  `json:"paths,omitempty"`
	X         float64   `json:"x,omitempty"`
	Y         float64   `json:"y,omitempty"`
	Width     float64   `json:"width,omitempty"`
	Height    float64   `json:"height,omitempty"`
	NewerThan time.Time `json:"newer_than,omitempty"`
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
	if len(p.Selector) > 4096 || len(p.Name) > 4096 || len(p.Label) > 4096 || len(p.Expected) > 16384 || len(p.Text) > 1024*1024 || len(p.Paths) > 32 {
		return nil, nil, errors.New("advanced action parameters exceed limits")
	}
	timeout := 10 * time.Second
	if p.TimeoutMS < 0 || p.TimeoutMS > 30000 {
		return nil, nil, errors.New("timeout_ms must be 0-30000")
	}
	if p.TimeoutMS > 0 {
		timeout = time.Duration(p.TimeoutMS) * time.Millisecond
	}
	ctx, cancel := context.WithTimeout(s.currentContext(), timeout)
	defer cancel()
	stop := context.AfterFunc(parent, cancel)
	defer stop()
	finish := func(v any, err error) (json.RawMessage, []byte, error) {
		if err != nil {
			return nil, nil, err
		}
		data, err := json.Marshal(v)
		return data, nil, err
	}
	switch p.Action {
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
		return finish(map[string]any{"tabs": pages, "active_tab": string(chromedp.FromContext(s.currentContext()).Target.TargetID)}, nil)
	case "tab_new", "tab_select", "tab_close":
		if p.Action == "tab_close" {
			if p.TabID == "" {
				return nil, nil, errors.New("tab_id is required")
			}
			if p.TabID == string(chromedp.FromContext(s.currentContext()).Target.TargetID) {
				return nil, nil, errors.New("select another tab before closing the active tab")
			}
			v, err := s.command(ctx, "Target.closeTarget", map[string]any{"targetId": p.TabID}, true)
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
		s.mu.Lock()
		s.tabCancels = append(s.tabCancels, closeTab)
		s.ctx = next
		s.mu.Unlock()
		id = string(chromedp.FromContext(next).Target.TargetID)
		chromedp.ListenTarget(next, s.handleTargetEvent)
		_, err = s.command(next, "Network.enable", map[string]any{}, false)
		if err != nil {
			return nil, nil, err
		}
		_, err = s.RawCDP("Runtime.enable", map[string]any{})
		if err != nil {
			return nil, nil, err
		}
		_, err = s.RawCDP("Page.enable", map[string]any{})
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
		v, err := s.command(ctx, "Browser.setDownloadBehavior", map[string]any{"behavior": "allow", "downloadPath": p.Paths[0], "eventsEnabled": true}, true)
		return finish(v, err)
	case "download_wait":
		if len(p.Paths) != 1 {
			return nil, nil, errors.New("provide one expected download file")
		}
		if p.NewerThan.IsZero() {
			return nil, nil, errors.New("newer_than is required to distinguish a new download from an existing file")
		}
		path := p.Paths[0]
		var previous int64 = -1
		for {
			info, err := os.Stat(path)
			_, partial := os.Stat(path + ".crdownload")
			if err == nil && info.Mode().IsRegular() && info.Size() > 0 && info.ModTime().After(p.NewerThan) && os.IsNotExist(partial) {
				if info.Size() == previous {
					return finish(map[string]any{"path": path, "bytes": info.Size(), "extension": filepath.Ext(path)}, nil)
				}
				previous = info.Size()
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
		return finish(map[string]any{"files": len(p.Paths), "verify_required": true}, err)
	}
	if p.Action != "find" && p.Action != "assert" && p.Action != "semantic_click" && p.Action != "semantic_type" {
		return nil, nil, fmt.Errorf("unsupported advanced action %q", p.Action)
	}
	if p.Action == "assert" && !validCondition(p.Condition) {
		return nil, nil, errors.New("condition must be visible, hidden, text, value, enabled, url or title")
	}
	if p.Selector == "" && p.Name == "" && p.Label == "" && p.Role == "" && p.Condition != "url" && p.Condition != "title" {
		return nil, nil, errors.New("a selector or semantic target is required")
	}
	encoded, _ := json.Marshal(p)
	script := strings.ReplaceAll(semanticScript, "__PARAMS__", string(encoded))
	for {
		v, err := eval(script)
		if err != nil {
			return nil, nil, err
		}
		if p.Action == "find" {
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
				err = chromedp.Run(ctx, chromedp.MouseClickXY(x+dx, y+dy))
				return finish(map[string]any{"action_sent": err == nil, "verify_required": true}, err)
			}
			_, err := s.command(ctx, "Input.insertText", map[string]any{"text": p.Text}, false)
			return finish(map[string]any{"action_sent": err == nil, "verify_required": true}, err)
		}
		if count, _ := v["count"].(float64); count > 1 {
			return nil, nil, errors.New("ambiguous_target: refine role, name, label or selector")
		}
		select {
		case <-ctx.Done():
			return nil, nil, fmt.Errorf("condition_timeout: expected state not reached: %w", ctx.Err())
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func validCondition(c string) bool {
	switch c {
	case "visible", "hidden", "text", "value", "enabled", "url", "title":
		return true
	}
	return false
}

// Semantic observations cross open shadow roots and never emit password values.
const semanticScript = `(async()=>{const p=__PARAMS__;const roots=[document];let nodes=[];for(let i=0;i<roots.length&&i<100;i++){const all=Array.from(roots[i].querySelectorAll('*'));for(const e of all){if(e.shadowRoot)roots.push(e.shadowRoot)}nodes.push(...all);if(nodes.length>20000)break}
const name=e=>e.getAttribute('aria-label')||((e.getAttribute('aria-labelledby')||'').split(/\s+/).map(id=>document.getElementById(id)?.textContent||'').join(' ').trim())||(e.labels&&Array.from(e.labels).map(l=>l.textContent).join(' ').trim())||(e.innerText||'').trim()||e.getAttribute('title')||'';
const role=e=>e.getAttribute('role')||({BUTTON:'button',A:'link',SELECT:'combobox',TEXTAREA:'textbox'}[e.tagName])||(e.tagName==='INPUT'?({checkbox:'checkbox',radio:'radio',submit:'button',button:'button'}[e.type]||'textbox'):'');
let matches=p.selector?roots.flatMap(r=>Array.from(r.querySelectorAll(p.selector))):nodes.filter(e=>(!p.role||role(e)===p.role)&&(!p.name||name(e)===p.name)&&(!p.label||Array.from(e.labels||[]).some(l=>l.textContent.trim()===p.label)));
const visible=e=>e.getClientRects().length>0&&getComputedStyle(e).visibility!=='hidden';
if(p.condition==='url'||p.condition==='title'){const actual=p.condition==='url'?location.href:document.title;return {passed:actual===p.expected,actual,count:0}}
if(p.action==='find')return {count:matches.length,truncated:matches.length>100,matches:matches.slice(0,100).map(e=>({role:role(e),name:e.type==='password'?'[password]':name(e),visible:visible(e),disabled:!!e.disabled,tag:e.tagName,rect:(()=>{const r=e.getBoundingClientRect();return {x:r.x,y:r.y,width:r.width,height:r.height}})()}))};
if(p.condition==='hidden')return {count:matches.length,passed:matches.every(e=>!visible(e))};if(matches.length!==1)return {count:matches.length,passed:false};const e=matches[0];
if(p.action==='assert'){if(p.condition==='value'&&e.type==='password')return {count:1,passed:false,reason:'password inspection prohibited'};const actual=p.condition==='text'?(e.innerText||'').trim():p.condition==='value'?e.value:null;return {count:1,passed:p.condition==='visible'?visible(e):p.condition==='enabled'?!e.disabled&&e.getAttribute('aria-disabled')!=='true':actual===p.expected,actual}}
e.scrollIntoView({block:'center',inline:'center'});const a=e.getBoundingClientRect();await new Promise(r=>requestAnimationFrame(()=>requestAnimationFrame(r)));const b=e.getBoundingClientRect();const x=b.x+b.width/2,y=b.y+b.height/2;const hit=e.getRootNode().elementFromPoint(x,y);const stable=a.x===b.x&&a.y===b.y&&a.width===b.width&&a.height===b.height;const editable=p.action!=='semantic_type'||(!e.readOnly&&(e.tagName==='INPUT'||e.tagName==='TEXTAREA'||e.isContentEditable));const passed=visible(e)&&stable&&!e.disabled&&e.getAttribute('aria-disabled')!=='true'&&editable&&(hit===e||e.contains(hit));if(passed&&p.action==='semantic_type'){e.focus();if(e.select)e.select();else if(e.isContentEditable){const range=document.createRange();range.selectNodeContents(e);const selection=getSelection();selection.removeAllRanges();selection.addRange(range)}}return {count:1,passed,x,y};})()`

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
	var x, y float64
	for depth := 0; parents[id] != ""; depth++ {
		if depth >= 32 {
			return 0, 0, errors.New("frame nesting exceeds limit")
		}
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
		id = parents[id]
	}
	return x, y, nil
}
