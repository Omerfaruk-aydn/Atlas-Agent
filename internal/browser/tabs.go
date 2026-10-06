package browser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/chromedp/chromedp"
)

func (s *chromedpSession) rememberBrowserTabs(pages []any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.observedTabs = make(map[string]bool, len(pages))
	for _, page := range pages {
		if info, ok := page.(map[string]any); ok {
			if id, ok := info["targetId"].(string); ok {
				s.observedTabs[id] = true
			}
		}
	}
}

// Target listeners keep inactive tabs from contributing dialogs or network
// state to the page currently controlled by this session.
func (s *chromedpSession) listenBrowserTarget(ctx context.Context) {
	listened := chromedp.FromContext(ctx).Target
	chromedp.ListenTarget(ctx, func(event any) {
		current := chromedp.FromContext(s.currentContext())
		if current != nil && current.Target == listened {
			s.handleTargetEvent(event)
		}
	})
}

func (s *chromedpSession) validateObservedTab(ctx context.Context, id string) error {
	s.mu.Lock()
	observed := s.observedTabs[id]
	s.mu.Unlock()
	if id == "" || !observed {
		return errors.New("unobserved_tab: list tabs or wait for the popup before selecting/closing")
	}
	info, err := s.command(ctx, "Target.getTargetInfo", map[string]any{"targetId": id}, true)
	if err != nil {
		return fmt.Errorf("stale_tab: %w", err)
	}
	target, _ := info["targetInfo"].(map[string]any)
	if target["type"] != "page" {
		return errors.New("wrong_tab: target is not a browser page")
	}
	current := chromedp.FromContext(s.currentContext())
	active, err := s.command(ctx, "Target.getTargetInfo", map[string]any{"targetId": string(current.Target.TargetID)}, true)
	if err != nil {
		return err
	}
	activeInfo, _ := active["targetInfo"].(map[string]any)
	if target["browserContextId"] != activeInfo["browserContextId"] {
		return errors.New("wrong_browser_context: tab belongs to another browser context")
	}
	return nil
}

func (s *chromedpSession) waitBrowserPopup(ctx context.Context, p Request) (json.RawMessage, []byte, error) {
	opener := p.TabID
	if opener == "" {
		opener = string(chromedp.FromContext(s.currentContext()).Target.TargetID)
	}
	for {
		if err := s.guardBrowserDocument(ctx, p); err != nil {
			return nil, nil, err
		}
		result, err := s.command(ctx, "Target.getTargets", map[string]any{}, true)
		if err != nil {
			return nil, nil, err
		}
		list, _ := result["targetInfos"].([]any)
		var matches []map[string]any
		for _, item := range list {
			info, ok := item.(map[string]any)
			if !ok || info["type"] != "page" || info["openerId"] != opener {
				continue
			}
			id, _ := info["targetId"].(string)
			s.mu.Lock()
			seen := s.observedTabs[id]
			s.mu.Unlock()
			if seen {
				continue
			}
			if p.URL != "" && info["url"] != p.URL {
				continue
			}
			matches = append(matches, info)
		}
		if len(matches) > 1 {
			return nil, nil, errors.New("ambiguous_popup: multiple new pages match; list tabs and select explicitly")
		}
		if len(matches) == 1 {
			id, _ := matches[0]["targetId"].(string)
			s.mu.Lock()
			if s.observedTabs == nil {
				s.observedTabs = map[string]bool{}
			}
			s.observedTabs[id] = true
			s.mu.Unlock()
			data, err := json.Marshal(map[string]any{"passed": true, "tab_id": id, "opener_id": opener, "url": matches[0]["url"], "selected": false})
			return data, nil, err
		}
		if err := browserPoll(ctx); err != nil {
			return nil, nil, fmt.Errorf("popup_timeout: %w", err)
		}
	}
}
