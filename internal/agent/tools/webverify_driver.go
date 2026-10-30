package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/browser"
	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/webverify"
)

// WebVerifyDriver drives product verification through Atlas's own browser
// tool, so every action goes through the same permission checks, activity
// overlay and session handling as the agent's own browser use. It works in
// a tab of its own and leaves the agent's tab as it found it.
type WebVerifyDriver struct {
	invoke      ToolInvoker
	prefix      string
	seq         int
	consoleSeen int
	netSeen     map[string]bool
}

// NewWebVerifyDriver opens a dedicated tab on startURL and returns the
// driver with a function that closes the tab and clears viewport emulation.
func NewWebVerifyDriver(ctx context.Context, invoke ToolInvoker, id, startURL string) (*WebVerifyDriver, func(), error) {
	if invoke == nil {
		return nil, nil, fmt.Errorf("%w: the browser tool is not enabled", webverify.ErrUnavailable)
	}
	d := &WebVerifyDriver{invoke: invoke, prefix: id, netSeen: map[string]bool{}}
	closeTab, err := openDesignTab(ctx, DesignServices{Invoke: invoke}, id, startURL)
	if err != nil {
		return nil, nil, d.classify(err)
	}
	return d, func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_, _ = d.call(cleanup, BrowserParams{Action: "cdp", CDPMethod: "Emulation.clearDeviceMetricsOverride"})
		closeTab()
	}, nil
}

// classify marks errors that mean the browser cannot be used at all.
func (d *WebVerifyDriver) classify(err error) error {
	if err == nil || errors.Is(err, webverify.ErrUnavailable) {
		return err
	}
	var failure *designBrowserFailure
	if errors.As(err, &failure) {
		if failure.response.StopTurn || strings.HasPrefix(failure.response.Content, "Denied:") {
			return fmt.Errorf("%w: %s", webverify.ErrUnavailable, firstLineOf(failure.response.Content))
		}
		return errors.New(firstLineOf(failure.response.Content))
	}
	return fmt.Errorf("%w: %v", webverify.ErrUnavailable, err)
}

func firstLineOf(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	if len(line) > 300 {
		line = line[:300]
	}
	return line
}

func (d *WebVerifyDriver) call(ctx context.Context, p BrowserParams) (fantasy.ToolResponse, error) {
	d.seq++
	data, err := json.Marshal(p)
	if err != nil {
		return fantasy.ToolResponse{}, err
	}
	r, err := d.invoke(ctx, fantasy.ToolCall{Name: BrowserToolName, ID: fmt.Sprintf("%s-%d", d.prefix, d.seq), Input: string(data)})
	if err != nil {
		if ctx.Err() != nil {
			return r, ctx.Err()
		}
		return r, fmt.Errorf("%w: %v", webverify.ErrUnavailable, err)
	}
	if r.StopTurn || strings.HasPrefix(r.Content, "Denied:") {
		return r, fmt.Errorf("%w: %s", webverify.ErrUnavailable, firstLineOf(r.Content))
	}
	if r.IsError {
		return r, errors.New(firstLineOf(r.Content))
	}
	return r, nil
}

// Navigate implements webverify.Driver.
func (d *WebVerifyDriver) Navigate(ctx context.Context, url string) error {
	_, err := d.call(ctx, BrowserParams{Action: "navigate", URL: url})
	return err
}

// SetViewport implements webverify.Driver.
func (d *WebVerifyDriver) SetViewport(ctx context.Context, v webverify.Viewport) error {
	metrics, _ := json.Marshal(map[string]any{"width": v.Width, "height": v.Height, "deviceScaleFactor": 1, "mobile": v.Mobile})
	_, err := d.call(ctx, BrowserParams{Action: "cdp", CDPMethod: "Emulation.setDeviceMetricsOverride", CDPParams: BrowserCDPParams(metrics)})
	return err
}

// Eval implements webverify.Driver.
func (d *WebVerifyDriver) Eval(ctx context.Context, script string, timeout time.Duration) (string, error) {
	p := BrowserParams{Action: "eval", Script: script}
	if ms := int(timeout.Milliseconds()); ms > 0 {
		p.Advanced.TimeoutMS = min(ms, 30000)
	}
	r, err := d.call(ctx, p)
	return r.Content, err
}

// Screenshot implements webverify.Driver. The capture is clean: no target
// boxes are drawn over it.
func (d *WebVerifyDriver) Screenshot(ctx context.Context) (webverify.Image, error) {
	clean := false
	r, err := d.call(ctx, BrowserParams{Action: "screenshot", Annotate: &clean})
	if err != nil {
		return webverify.Image{}, err
	}
	if len(r.Data) == 0 {
		return webverify.Image{}, errors.New("the screenshot returned no image")
	}
	return webverify.Image{Data: r.Data, MediaType: r.MediaType}, nil
}

var consoleLine = regexp.MustCompile(`^\[\d{2}:\d{2}:\d{2}\] ([A-Za-z]+): ?(.*)$`)

// ParseConsole reads the console action's text into messages. A line that
// does not start a new message continues the previous one.
func ParseConsole(text string) []webverify.ConsoleMessage {
	if strings.HasPrefix(text, "No console output") {
		return nil
	}
	var out []webverify.ConsoleMessage
	for _, line := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
		if m := consoleLine.FindStringSubmatch(line); m != nil {
			out = append(out, webverify.ConsoleMessage{Type: m[1], Text: m[2]})
			continue
		}
		if len(out) > 0 {
			out[len(out)-1].Text += "\n" + line
		}
	}
	return out
}

// ConsoleSince implements webverify.Driver. The browser keeps a bounded
// log for the whole session, so the driver remembers how much it has seen;
// a shorter log means the oldest lines rolled off and everything is new.
func (d *WebVerifyDriver) ConsoleSince(ctx context.Context) ([]webverify.ConsoleMessage, error) {
	r, err := d.call(ctx, BrowserParams{Action: "console"})
	if err != nil {
		return nil, err
	}
	all := ParseConsole(r.Content)
	start := d.consoleSeen
	if start > len(all) {
		start = 0
	}
	d.consoleSeen = len(all)
	return all[start:], nil
}

// NetworkSince implements webverify.Driver.
func (d *WebVerifyDriver) NetworkSince(ctx context.Context) ([]webverify.NetworkEntry, error) {
	r, err := d.call(ctx, BrowserParams{Action: "network"})
	if err != nil {
		return nil, err
	}
	var entries []browser.NetworkEntry
	if err := json.Unmarshal([]byte(r.Content), &entries); err != nil {
		return nil, fmt.Errorf("network log: %w", err)
	}
	var out []webverify.NetworkEntry
	for _, e := range entries {
		if e.RequestID != "" {
			if d.netSeen[e.RequestID] {
				continue
			}
			d.netSeen[e.RequestID] = true
		}
		out = append(out, webverify.NetworkEntry{RequestID: e.RequestID, URL: e.URL, Status: e.Status, Failed: e.Failed, DurationMS: e.DurationMS})
	}
	return out, nil
}

// Click implements webverify.Driver.
func (d *WebVerifyDriver) Click(ctx context.Context, selector string) error {
	_, err := d.call(ctx, BrowserParams{Action: "click", Selector: selector})
	return err
}

// Type implements webverify.Driver.
func (d *WebVerifyDriver) Type(ctx context.Context, selector, text string) error {
	_, err := d.call(ctx, BrowserParams{Action: "type", Selector: selector, Text: text})
	return err
}

// Press implements webverify.Driver.
func (d *WebVerifyDriver) Press(ctx context.Context, key string) error {
	_, err := d.call(ctx, BrowserParams{Action: "key", Key: key})
	return err
}

// Assert implements webverify.Driver using the browser tool's assert action.
// A failed assertion is a result, not an error.
func (d *WebVerifyDriver) Assert(ctx context.Context, a webverify.Assertion) (bool, string, error) {
	timeout := a.TimeoutMS
	if timeout <= 0 {
		timeout = 3000
	}
	_, err := d.call(ctx, BrowserParams{Action: "assert", Advanced: browser.Request{
		Condition: a.Condition, Selector: a.Selector, Role: a.Role, Name: a.Name, Label: a.Label,
		Expected: a.Expected, TimeoutMS: min(timeout, 30000),
	}})
	switch {
	case err == nil:
		return true, "", nil
	case errors.Is(err, webverify.ErrUnavailable), ctx.Err() != nil:
		return false, "", err
	}
	return false, err.Error(), nil
}

var _ webverify.Driver = (*WebVerifyDriver)(nil)
