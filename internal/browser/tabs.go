package browser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/chromedp/cdproto/target"
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
	if s.currentTabGone() {
		// The selected tab is gone; compare against the session's own tab.
		current = chromedp.FromContext(s.rootCtx)
	}
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
		opener = s.ActiveTab()
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
			s.ensureSessionTab()
			s.recordTab(TabRecord{ID: id, Opener: opener, CreatedBy: "popup", Owned: true, Purpose: p.Purpose})
			data, err := json.Marshal(map[string]any{"passed": true, "tab_id": id, "opener_id": opener, "url": matches[0]["url"], "selected": false})
			return data, nil, err
		}
		if err := browserPoll(ctx); err != nil {
			return nil, nil, fmt.Errorf("popup_timeout: %w", err)
		}
	}
}

// TabRecord is what the session knows about a tab beyond its target info:
// whether automation opened it, why, and from which tab.
type TabRecord struct {
	ID        string    `json:"tab_id"`
	Purpose   string    `json:"purpose,omitempty"`
	Opener    string    `json:"opener_id,omitempty"`
	CreatedBy string    `json:"created_by"`
	Owned     bool      `json:"owned"`
	Closed    bool      `json:"closed,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// ActiveTab returns the selected tab's target id, or "" before the first
// action attached one.
func (s *chromedpSession) ActiveTab() string {
	c := chromedp.FromContext(s.currentContext())
	if c == nil || c.Target == nil {
		return ""
	}
	return string(c.Target.TargetID)
}

func (s *chromedpSession) recordTab(rec TabRecord) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.tabRecords == nil {
		s.tabRecords = map[string]*TabRecord{}
	}
	if existing, ok := s.tabRecords[rec.ID]; ok {
		if rec.Purpose != "" {
			existing.Purpose = rec.Purpose
		}
		return
	}
	if rec.CreatedAt.IsZero() {
		rec.CreatedAt = time.Now()
	}
	s.tabRecords[rec.ID] = &rec
}

// ensureSessionTab registers the tab the session started on as owned.
func (s *chromedpSession) ensureSessionTab() {
	root := chromedp.FromContext(s.rootCtx)
	if root == nil || root.Target == nil {
		return
	}
	s.recordTab(TabRecord{ID: string(root.Target.TargetID), CreatedBy: "session", Owned: true})
}

func (s *chromedpSession) tabRecord(id string) (TabRecord, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.tabRecords[id]
	if !ok {
		return TabRecord{}, false
	}
	return *rec, true
}

// annotateTabs adds ownership to target infos and marks recorded tabs that
// no longer exist as closed, so an old tab id is never selected again.
func (s *chromedpSession) annotateTabs(pages []any) {
	s.ensureSessionTab()
	present := map[string]bool{}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, page := range pages {
		info, ok := page.(map[string]any)
		if !ok {
			continue
		}
		id, _ := info["targetId"].(string)
		present[id] = true
		rec := s.tabRecords[id]
		if rec == nil {
			info["owned"] = false
			info["note"] = "not opened by this automation session; treat as the user's tab"
			continue
		}
		info["owned"] = rec.Owned
		info["created_by"] = rec.CreatedBy
		if rec.Purpose != "" {
			info["purpose"] = rec.Purpose
		}
		if rec.Opener != "" {
			info["opener_id"] = rec.Opener
		}
	}
	for id, rec := range s.tabRecords {
		if !present[id] {
			rec.Closed = true
		}
	}
}

// returnTab picks where tab_return goes: the current tab's opener while it
// is open, otherwise the most recently created owned tab still open.
func (s *chromedpSession) returnTab(current string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if rec := s.tabRecords[current]; rec != nil && rec.Opener != "" {
		if opener := s.tabRecords[rec.Opener]; opener == nil || !opener.Closed {
			return rec.Opener, nil
		}
	}
	var best *TabRecord
	for id, rec := range s.tabRecords {
		if id == current || !rec.Owned || rec.Closed {
			continue
		}
		if best == nil || rec.CreatedAt.After(best.CreatedAt) {
			best = rec
		}
	}
	if best == nil {
		return "", errors.New("no_return_tab: no open opener or automation-owned tab to return to; list tabs and select one explicitly")
	}
	return best.ID, nil
}

// handleBrowserEvent receives browser-level CDP events. Like the target
// listener it only records state; it never runs an action.
func (s *chromedpSession) handleBrowserEvent(event any) {
	switch ev := event.(type) {
	case *target.EventTargetDestroyed:
		s.markTabDestroyed(string(ev.TargetID))
	case *target.EventTargetCrashed:
		s.markTabDestroyed(string(ev.TargetID))
	default:
		s.handleDownloadEvent(event)
	}
}

func (s *chromedpSession) markTabDestroyed(id string) {
	if id == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.destroyedTabs == nil || len(s.destroyedTabs) > 512 {
		s.destroyedTabs = map[string]bool{}
	}
	s.destroyedTabs[id] = true
	if rec := s.tabRecords[id]; rec != nil {
		rec.Closed = true
	}
	delete(s.observedTabs, id)
}

// currentTabGone reports whether the selected tab closed or crashed while
// the browser itself may still be running.
func (s *chromedpSession) currentTabGone() bool {
	id := s.ActiveTab()
	s.mu.Lock()
	destroyed := id != "" && s.destroyedTabs[id]
	s.mu.Unlock()
	return destroyed || s.currentContext().Err() != nil
}
