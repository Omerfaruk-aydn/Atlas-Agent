package browser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/chromedp/chromedp"
)

func browserPoll(ctx context.Context) error {
	timer := time.NewTimer(100 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func transientBrowserContextError(err error) bool {
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "execution context was destroyed") || strings.Contains(text, "cannot find context") || strings.Contains(text, "inspected target navigated")
}

func browserOrigin(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil {
		return "", errors.New("origin must be an HTTP(S) scheme/host/port")
	}
	host := strings.ToLower(u.Host)
	if (u.Scheme == "https" && u.Port() == "443") || (u.Scheme == "http" && u.Port() == "80") {
		host = strings.ToLower(u.Hostname())
		if strings.Contains(host, ":") {
			host = "[" + host + "]"
		}
	}
	return u.Scheme + "://" + host, nil
}

func (s *chromedpSession) guardBrowserDocument(ctx context.Context, p Request) error {
	c := chromedp.FromContext(s.currentContext())
	if p.ExpectedTabID != "" && (c == nil || c.Target == nil || string(c.Target.TargetID) != p.ExpectedTabID) {
		return errors.New("wrong_tab: inspect/select the intended tab before acting")
	}
	if p.ExpectedOrigin == "" && p.DocumentID == "" {
		return nil
	}
	tree, err := s.command(ctx, "Page.getFrameTree", map[string]any{}, false)
	if err != nil {
		return err
	}
	frame := browserFrameInfo(tree, p.FrameID)
	if frame == nil {
		return errors.New("stale_frame: observe the current frame tree")
	}
	if p.DocumentID != "" && frame["loaderId"] != p.DocumentID {
		return errors.New("stale_document: resolve the semantic target in the current document")
	}
	if p.ExpectedOrigin != "" {
		expected, err := browserOrigin(p.ExpectedOrigin)
		if err != nil {
			return err
		}
		actual, _ := frame["url"].(string)
		origin, err := browserOrigin(actual)
		if err != nil || origin != expected {
			return errors.New("wrong_origin: current page does not match expected_origin")
		}
	}
	return nil
}

func browserFrameInfo(tree map[string]any, id string) map[string]any {
	root, _ := tree["frameTree"].(map[string]any)
	queue := []map[string]any{root}
	for i := 0; i < len(queue) && i < 256; i++ {
		frame, _ := queue[i]["frame"].(map[string]any)
		if frame != nil && (id == "" || frame["id"] == id) {
			return frame
		}
		children, _ := queue[i]["childFrames"].([]any)
		for _, item := range children {
			if child, ok := item.(map[string]any); ok {
				queue = append(queue, child)
			}
		}
	}
	return nil
}

// CheckDocument lets ordinary navigation/key calls enforce workflow guards.
func (s *chromedpSession) CheckDocument(ctx context.Context, p Request) error {
	checkCtx, cancel := context.WithTimeout(s.currentContext(), 2*time.Second)
	defer cancel()
	stop := context.AfterFunc(ctx, cancel)
	defer stop()
	return s.guardBrowserDocument(checkCtx, p)
}

func (s *chromedpSession) waitBrowserCondition(ctx context.Context, p Request) (json.RawMessage, []byte, error) {
	if validCondition(p.Condition) {
		p.Action = "assert"
		return s.Advanced(ctx, p)
	}
	if p.Condition != "ready" && p.Condition != "network_idle" {
		return nil, nil, errors.New("wait_for condition must be ready, network_idle or a supported assert condition")
	}
	var quietSince time.Time
	for {
		if err := s.guardBrowserDocument(ctx, p); err != nil {
			return nil, nil, err
		}
		ready, err := s.browserDocumentReady(ctx, p.FrameID)
		if err != nil && !transientBrowserContextError(err) {
			return nil, nil, err
		}
		passed := ready
		if p.Condition == "network_idle" {
			s.mu.Lock()
			pending := len(s.requests)
			incomplete := s.requestTrackingIncomplete
			s.mu.Unlock()
			if incomplete {
				return nil, nil, errors.New("network_tracking_incomplete: use an explicit DOM result condition")
			}
			if ready && pending == 0 {
				if quietSince.IsZero() {
					quietSince = time.Now()
				}
				passed = time.Since(quietSince) >= 400*time.Millisecond
			} else {
				quietSince = time.Time{}
			}
		}
		if passed {
			data, err := json.Marshal(map[string]any{"passed": true, "condition": p.Condition})
			return data, nil, err
		}
		if err := browserPoll(ctx); err != nil {
			return nil, nil, fmt.Errorf("condition_timeout: %w", err)
		}
	}
}

func (s *chromedpSession) browserDocumentReady(ctx context.Context, frameID string) (bool, error) {
	args := map[string]any{"expression": `document.readyState === 'complete'`, "returnByValue": true}
	if frameID != "" {
		world, err := s.command(ctx, "Page.createIsolatedWorld", map[string]any{"frameId": frameID, "worldName": "atlas-interaction"}, false)
		if err != nil {
			return false, err
		}
		args["contextId"] = world["executionContextId"]
	}
	result, err := s.command(ctx, "Runtime.evaluate", args, false)
	if err != nil {
		return false, err
	}
	value, _ := result["result"].(map[string]any)
	ready, _ := value["value"].(bool)
	return ready, nil
}

func (s *chromedpSession) browserDialogAction(ctx context.Context, p Request) (json.RawMessage, []byte, error) {
	for {
		if err := s.guardBrowserDocument(ctx, p); err != nil {
			return nil, nil, err
		}
		dialogs := s.PendingDialogs()
		if len(dialogs) > 0 {
			if p.Expected != "" && dialogs[0].Message != p.Expected {
				return nil, nil, errors.New("dialog_mismatch: inspect the pending dialog")
			}
			if p.Action == "dialog_handle" {
				if err := s.HandleDialog(p.Accept, p.PromptText); err != nil {
					return nil, nil, err
				}
			}
			data, err := json.Marshal(map[string]any{"passed": true, "type": dialogs[0].Type, "message": dialogs[0].Message, "handled": p.Action == "dialog_handle"})
			return data, nil, err
		}
		if err := browserPoll(ctx); err != nil {
			return nil, nil, fmt.Errorf("dialog_timeout: %w", err)
		}
	}
}
