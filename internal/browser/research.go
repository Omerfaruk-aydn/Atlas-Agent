package browser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sync"
	"time"

	"github.com/chromedp/chromedp"
)

// ResearchSource is what one background read found. Each result keeps its
// own requested and final URL so sources are never mixed across tabs.
type ResearchSource struct {
	Index       int    `json:"index"`
	Requested   string `json:"requested_url"`
	URL         string `json:"final_url,omitempty"`
	Title       string `json:"title,omitempty"`
	Published   string `json:"published,omitempty"`
	Modified    string `json:"modified,omitempty"`
	Heading     string `json:"heading,omitempty"`
	Excerpt     string `json:"excerpt,omitempty"`
	Truncated   bool   `json:"truncated,omitempty"`
	Error       string `json:"error,omitempty"`
	DurationMS  int64  `json:"duration_ms"`
	ReadOnlyTab bool   `json:"read_only_tab"`
}

const researchExtractScript = `(() => {
  const meta = n => document.querySelector('meta[property="' + n + '"],meta[name="' + n + '"],meta[itemprop="' + n + '"]')?.content || '';
  let published = meta('article:published_time') || meta('datePublished') || meta('date') || meta('pubdate');
  let modified = meta('article:modified_time') || meta('dateModified');
  for (const s of document.querySelectorAll('script[type="application/ld+json"]')) {
    try { const d = JSON.parse(s.textContent); const items = Array.isArray(d) ? d : [d, ...(d['@graph'] || [])];
      for (const it of items) { published = published || it.datePublished || ''; modified = modified || it.dateModified || ''; } } catch (e) {}
  }
  if (!published) published = document.querySelector('time[datetime]')?.getAttribute('datetime') || '';
  const main = document.querySelector('main, article, [role=main]') || document.body;
  const text = (main?.innerText || '').replace(/\s+/g, ' ').trim();
  return JSON.stringify({url: location.href, title: document.title, published, modified,
    heading: (document.querySelector('h1')?.innerText || '').trim().slice(0, 200),
    excerpt: text.slice(0, 2400), truncated: text.length > 2400});
})()`

// maxResearchURLs and researchConcurrency bound background reading: enough
// to compare a handful of sources, never a crawl.
const (
	maxResearchURLs     = 8
	researchConcurrency = 3
)

// Research reads independent pages in background tabs, at most three at a
// time. It never selects those tabs, types or clicks; the controlled tab and
// its focus are untouched, and every background tab is closed afterwards.
func (s *chromedpSession) Research(parent context.Context, p Request) ([]ResearchSource, error) {
	if len(p.URLs) == 0 || len(p.URLs) > maxResearchURLs {
		return nil, fmt.Errorf("research requires 1-%d urls", maxResearchURLs)
	}
	for _, raw := range p.URLs {
		u, err := url.Parse(raw)
		if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
			return nil, fmt.Errorf("research url %q must be HTTP(S)", raw)
		}
	}
	perPage := 20 * time.Second
	if p.TimeoutMS > 0 && p.TimeoutMS <= 30000 {
		perPage = time.Duration(p.TimeoutMS) * time.Millisecond
	}
	results := make([]ResearchSource, len(p.URLs))
	sem := make(chan struct{}, researchConcurrency)
	var wg sync.WaitGroup
	for i, raw := range p.URLs {
		wg.Add(1)
		go func(i int, raw string) {
			defer wg.Done()
			results[i] = ResearchSource{Index: i, Requested: raw, ReadOnlyTab: true}
			select {
			case sem <- struct{}{}:
			case <-parent.Done():
				results[i].Error = "canceled"
				return
			}
			defer func() { <-sem }()
			started := time.Now()
			defer func() { results[i].DurationMS = time.Since(started).Milliseconds() }()
			tab, closeTab := chromedp.NewContext(s.rootCtx)
			defer closeTab()
			ctx, cancel := context.WithTimeout(tab, perPage)
			defer cancel()
			stop := context.AfterFunc(parent, cancel)
			defer stop()
			var raw2 string
			err := chromedp.Run(ctx, chromedp.Navigate(raw), chromedp.ActionFunc(func(ctx context.Context) error {
				for {
					var ready bool
					if err := chromedp.Evaluate(`document.readyState !== 'loading'`, &ready).Do(ctx); err == nil && ready {
						return nil
					}
					select {
					case <-ctx.Done():
						return ctx.Err()
					case <-time.After(100 * time.Millisecond):
					}
				}
			}), chromedp.Evaluate(researchExtractScript, &raw2))
			if err != nil {
				if parent.Err() != nil {
					results[i].Error = "canceled"
				} else {
					results[i].Error = err.Error()
				}
				return
			}
			var extracted ResearchSource
			if err := json.Unmarshal([]byte(raw2), &extracted); err != nil {
				results[i].Error = "extraction failed"
				return
			}
			extracted.Index, extracted.Requested, extracted.ReadOnlyTab = i, raw, true
			if u, err := url.Parse(extracted.URL); err == nil {
				u.User = nil
				extracted.URL = u.String()
			}
			results[i] = extracted
		}(i, raw)
	}
	wg.Wait()
	if err := parent.Err(); err != nil {
		return results, err
	}
	failed := 0
	for _, r := range results {
		if r.Error != "" {
			failed++
		}
	}
	if failed == len(results) {
		return results, errors.New("research_failed: no source could be read; see per-source errors")
	}
	return results, nil
}
