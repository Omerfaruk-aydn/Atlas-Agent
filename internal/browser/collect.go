package browser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// CollectedRow is one unique row found while scanning a list.
type CollectedRow struct {
	Key       string `json:"key"`
	Index     int    `json:"index,omitempty"`
	Page      int    `json:"page,omitempty"`
	Text      string `json:"text"`
	SourceURL string `json:"source_url,omitempty"`
}

// CollectResult reports rows and, separately, how complete the scan is.
// A scan that stopped early says so instead of presenting the visible rows
// as the whole list.
type CollectResult struct {
	Items           []CollectedRow `json:"items"`
	SourceURL       string         `json:"source_url,omitempty"`
	Count           int            `json:"count"`
	Complete        bool           `json:"complete"`
	EndReached      bool           `json:"end_reached"`
	ExpectedTotal   int            `json:"expected_total,omitempty"`
	Gaps            []string       `json:"gaps,omitempty"`
	ConflictingKeys []string       `json:"conflicting_keys,omitempty"`
	Reseen          int            `json:"overlap_reseen"`
	Scrolls         int            `json:"scrolls"`
	Pages           int            `json:"pages"`
	Cursor          int            `json:"cursor"`
	StopReason      string         `json:"stop_reason"`
	Next            string         `json:"next,omitempty"`
}

const collectScript = `(() => {
  const p = __PARAMS__;
  const roots = [document];
  for (let i = 0; i < roots.length && i < 200; i++) for (const e of roots[i].querySelectorAll('*')) if (e.shadowRoot) roots.push(e.shadowRoot);
  const q = sel => roots.flatMap(r => Array.from(r.querySelectorAll(sel)));
  let box = q('[data-atlas-collect="' + p.token + '"]')[0];
  if (!box) {
    if (p.scope) {
      const found = q(p.scope);
      if (found.length !== 1) return {error: 'scope_not_unique', count: found.length};
      box = found[0];
    } else {
      const first = q(p.row_selector)[0];
      for (let n = first && first.parentElement; n; n = n.parentElement) {
        const st = getComputedStyle(n);
        if (/(auto|scroll)/.test(st.overflowY) && n.scrollHeight > n.clientHeight + 2) { box = n; break; }
      }
      box = box || document.scrollingElement;
    }
    box.setAttribute('data-atlas-collect', p.token);
  }
  if (p.op === 'scroll') { const before = box.scrollTop; box.scrollTop = before + Math.max(40, box.clientHeight * 0.8); return {moved: box.scrollTop !== before, scroll_top: box.scrollTop}; }
  if (p.op === 'seek') { box.scrollTop = p.cursor; return {scroll_top: box.scrollTop}; }
  if (p.op === 'cleanup') { box.removeAttribute('data-atlas-collect'); return {}; }
  const keyOf = (e, text) => (p.row_key && e.getAttribute(p.row_key)) || e.getAttribute('data-id') || e.getAttribute('data-key') ||
    e.getAttribute('aria-rowindex') || e.id || ('text:' + text.slice(0, 80));
  const view = box === document.scrollingElement ? {top: 0, bottom: innerHeight} : box.getBoundingClientRect();
  const rows = q(p.row_selector).filter(e => box.contains(e) && e.getClientRects().length).filter(e => {
    const r = e.getBoundingClientRect(); return r.bottom > view.top && r.top < view.bottom;
  }).map(e => {
    const text = (e.innerText || '').replace(/\s+/g, ' ').trim();
    const idx = e.getAttribute('aria-rowindex') || e.getAttribute('aria-posinset') || e.getAttribute('data-index');
    const source = new URL(location.href); source.username = ''; source.password = '';
    return {key: keyOf(e, text), index: idx ? parseInt(idx, 10) : 0, text: text.slice(0, 300), source_url: source.href};
  });
  const total = parseInt(box.getAttribute('aria-rowcount') || box.getAttribute('aria-setsize') || q(p.row_selector)[0]?.getAttribute('aria-setsize') || '0', 10) || 0;
  return {rows, scroll_top: box.scrollTop, at_end: box.scrollTop + box.clientHeight >= box.scrollHeight - 2, total};
})()`

type collectPoll struct {
	Error     string         `json:"error"`
	Count     int            `json:"count"`
	Rows      []CollectedRow `json:"rows"`
	ScrollTop float64        `json:"scroll_top"`
	AtEnd     bool           `json:"at_end"`
	Moved     bool           `json:"moved"`
	Total     int            `json:"total"`
}

func (s *chromedpSession) collectEval(ctx context.Context, params map[string]any) (collectPoll, error) {
	encoded, _ := json.Marshal(params)
	v, err := s.command(ctx, "Runtime.evaluate", map[string]any{"expression": strings.Replace(collectScript, "__PARAMS__", string(encoded), 1), "returnByValue": true}, false)
	if err != nil {
		return collectPoll{}, s.explainFailure(err)
	}
	if v["exceptionDetails"] != nil {
		return collectPoll{}, errors.New("target_missing: list evaluation failed; check row_selector and scope")
	}
	result, _ := v["result"].(map[string]any)
	data, _ := json.Marshal(result["value"])
	var out collectPoll
	err = json.Unmarshal(data, &out)
	if err == nil && out.Error != "" {
		err = fmt.Errorf("target_resolution_failed: %s (%d)", out.Error, out.Count)
	}
	return out, err
}

// Collect scans a scrolling or virtualized list (and optional pagination)
// in bounded steps, keeping each row's identity. The result says whether the
// end was reached, which indexes were skipped and where to resume.
func (s *chromedpSession) Collect(parent context.Context, p Request) (CollectResult, error) {
	if p.RowSelector == "" || len(p.RowSelector) > 4096 || len(p.NextSelector) > 4096 || len(p.Scope) > 4096 {
		return CollectResult{}, errors.New("collect requires row_selector (and bounded scope/next_selector)")
	}
	maxItems, maxScrolls := p.MaxItems, p.MaxScrolls
	if maxItems <= 0 {
		maxItems = 500
	}
	if maxScrolls <= 0 {
		maxScrolls = 40
	}
	if maxItems > 2000 || maxScrolls > 200 {
		return CollectResult{}, errors.New("max_items is limited to 2000 and max_scrolls to 200")
	}
	timeout := 60 * time.Second
	if p.TimeoutMS > 0 && p.TimeoutMS <= 30000 {
		timeout = time.Duration(p.TimeoutMS) * time.Millisecond * 4
	}
	ctx, cancel := context.WithTimeout(s.currentContext(), timeout)
	defer cancel()
	stop := context.AfterFunc(parent, cancel)
	defer stop()
	restore, err := s.suspendActivity()
	if err != nil {
		return CollectResult{}, err
	}
	defer restore()
	token := fieldToken()
	base := map[string]any{"token": token, "row_selector": p.RowSelector, "row_key": p.RowKey, "scope": p.Scope}
	with := func(op string, extra map[string]any) map[string]any {
		m := map[string]any{"op": op}
		for k, v := range base {
			m[k] = v
		}
		for k, v := range extra {
			m[k] = v
		}
		return m
	}
	defer func() {
		cleanup, done := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		defer done()
		_, _ = s.collectEval(cleanup, with("cleanup", nil))
	}()
	// A scan starts at the top (or the resume cursor), never wherever an
	// earlier scroll left the list, so leading rows are not silently skipped.
	if _, err := s.collectEval(ctx, with("seek", map[string]any{"cursor": p.Cursor})); err != nil {
		return CollectResult{}, err
	}
	_ = s.waitQuiet(ctx, 150*time.Millisecond, 2*time.Second)
	result := CollectResult{Pages: 1}
	seen := map[string]CollectedRow{}
	indexes := map[int]map[int]bool{}
	firstKeyOfPage := ""
	for {
		poll, err := s.collectEval(ctx, with("read", nil))
		if err != nil {
			return result, err
		}
		result.Cursor = int(poll.ScrollTop)
		if result.SourceURL == "" && len(poll.Rows) > 0 {
			result.SourceURL = poll.Rows[0].SourceURL
		}
		if poll.Total > 0 {
			result.ExpectedTotal = poll.Total
		}
		for i, row := range poll.Rows {
			if i == 0 && firstKeyOfPage == "" {
				firstKeyOfPage = row.Key
			}
			row.Page = result.Pages
			if prev, ok := seen[row.Key]; ok {
				result.Reseen++
				if prev.Text != row.Text && len(result.ConflictingKeys) < 20 {
					result.ConflictingKeys = append(result.ConflictingKeys, row.Key)
				}
				continue
			}
			if len(seen) >= maxItems {
				result.StopReason = "max_items"
				break
			}
			seen[row.Key] = row
			if row.Index > 0 {
				if indexes[result.Pages] == nil {
					indexes[result.Pages] = map[int]bool{}
				}
				indexes[result.Pages][row.Index] = true
			}
			result.Items = append(result.Items, row)
		}
		if result.StopReason != "" {
			break
		}
		if poll.AtEnd {
			if p.NextSelector == "" || result.Pages >= 20 {
				result.EndReached = p.NextSelector == ""
				result.StopReason = "end_of_list"
				if p.NextSelector != "" {
					result.StopReason = "max_pages"
				}
				break
			}
			// A disabled or missing next control is the end of the list; a
			// click that does not change the rows is not, and stays incomplete.
			if _, _, err := s.Advanced(ctx, Request{Action: "assert", Selector: p.NextSelector, Condition: "enabled", TimeoutMS: 400}); err != nil {
				result.EndReached, result.StopReason = true, "no_next_page"
				break
			}
			if _, _, err := s.Advanced(ctx, Request{Action: "semantic_click", Selector: p.NextSelector, TimeoutMS: 5000}); err != nil {
				result.StopReason = "next_page_click_failed: " + err.Error()
				break
			}
			_ = s.waitQuiet(ctx, 250*time.Millisecond, 5*time.Second)
			if _, err := s.collectEval(ctx, with("seek", map[string]any{"cursor": 0})); err != nil {
				return result, err
			}
			next, err := s.collectEval(ctx, with("read", nil))
			if err != nil {
				return result, err
			}
			if len(next.Rows) > 0 && next.Rows[0].Key == firstKeyOfPage {
				result.StopReason = "pagination_did_not_advance"
				break
			}
			result.Pages++
			firstKeyOfPage = ""
			continue
		}
		if result.Scrolls >= maxScrolls {
			result.StopReason = "max_scrolls"
			break
		}
		moved, err := s.collectEval(ctx, with("scroll", nil))
		if err != nil {
			return result, err
		}
		result.Scrolls++
		if !moved.Moved {
			result.EndReached, result.StopReason = true, "scroll_did_not_move"
			break
		}
		if err := s.waitQuiet(ctx, 150*time.Millisecond, 2*time.Second); err != nil && ctx.Err() != nil {
			result.StopReason = "canceled_or_timeout"
			break
		}
	}
	result.Count = len(result.Items)
	for page := 1; page <= result.Pages; page++ {
		result.Gaps = append(result.Gaps, indexGaps(indexes[page], page, result.Pages > 1, p.Cursor > 0)...)
	}
	if len(result.Gaps) > 20 {
		result.Gaps = result.Gaps[:20]
	}
	result.Complete = result.EndReached && len(result.Gaps) == 0 && (result.ExpectedTotal == 0 || result.Count >= result.ExpectedTotal)
	if !result.Complete {
		result.Next = fmt.Sprintf("Coverage is incomplete (%s). Resume with collect cursor=%d, or report the missing range instead of treating these rows as the whole list.", result.StopReason, result.Cursor)
	}
	if err := ctx.Err(); err != nil && parent.Err() != nil {
		return result, parent.Err()
	}
	return result, nil
}

// waitQuiet waits until the DOM mutation counter holds still for quiet,
// bounded by limit; lists render rows asynchronously after a scroll.
func (s *chromedpSession) waitQuiet(ctx context.Context, quiet, limit time.Duration) error {
	deadline := time.Now().Add(limit)
	last, since := -1.0, time.Now()
	for time.Now().Before(deadline) {
		n, err := s.domMutationCount(ctx, "")
		if err != nil {
			return err
		}
		if n != last {
			last, since = n, time.Now()
		} else if time.Since(since) >= quiet {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(40 * time.Millisecond):
		}
	}
	return nil
}

// indexGaps lists missing row indexes on one page. A resumed scan (cursor)
// does not report the rows before where it started as missing.
func indexGaps(seen map[int]bool, page int, paged, resumed bool) []string {
	if len(seen) == 0 {
		return nil
	}
	list := make([]int, 0, len(seen))
	for i := range seen {
		list = append(list, i)
	}
	sort.Ints(list)
	prefix := ""
	if paged {
		prefix = "page " + strconv.Itoa(page) + ": "
	}
	var gaps []string
	if list[0] > 1 && (!resumed || page != 1) {
		gaps = append(gaps, prefix+"1-"+strconv.Itoa(list[0]-1))
	}
	for i := 1; i < len(list); i++ {
		if list[i] > list[i-1]+1 {
			gaps = append(gaps, prefix+strconv.Itoa(list[i-1]+1)+"-"+strconv.Itoa(list[i]-1))
		}
	}
	return gaps
}
