package tools

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/webverify"
)

type scriptedBrowser struct {
	calls   []BrowserParams
	console string
	network string
	reply   func(BrowserParams) (fantasy.ToolResponse, error)
}

func (s *scriptedBrowser) invoke(_ context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
	var p BrowserParams
	if err := json.Unmarshal([]byte(call.Input), &p); err != nil {
		return fantasy.ToolResponse{}, err
	}
	s.calls = append(s.calls, p)
	if s.reply != nil {
		if r, err := s.reply(p); r.Content != "" || err != nil || r.IsError {
			return r, err
		}
	}
	switch p.Action {
	case "console":
		return fantasy.NewTextResponse(s.console), nil
	case "network":
		return fantasy.NewTextResponse(s.network), nil
	case "screenshot":
		return fantasy.NewImageResponse([]byte("img"), "image/png"), nil
	}
	return fantasy.NewTextResponse("ok"), nil
}

func newTestDriver(s *scriptedBrowser) *WebVerifyDriver {
	return &WebVerifyDriver{invoke: s.invoke, prefix: "t", netSeen: map[string]bool{}}
}

func TestParseConsole(t *testing.T) {
	t.Parallel()
	require.Empty(t, ParseConsole("No console output captured."))
	got := ParseConsole("[10:00:01] log: hello\n[10:00:02] error: boom\n    at app.js:1\n[10:00:03] exception: Uncaught X\n")
	require.Equal(t, []webverify.ConsoleMessage{
		{Type: "log", Text: "hello"},
		{Type: "error", Text: "boom\n    at app.js:1"},
		{Type: "exception", Text: "Uncaught X"},
	}, got)
}

func TestWebVerifyDriverConsoleIsIncremental(t *testing.T) {
	t.Parallel()
	b := &scriptedBrowser{console: "[10:00:01] error: old\n"}
	d := newTestDriver(b)
	first, err := d.ConsoleSince(context.Background())
	require.NoError(t, err)
	require.Len(t, first, 1)
	b.console = "[10:00:01] error: old\n[10:00:05] error: new\n"
	second, err := d.ConsoleSince(context.Background())
	require.NoError(t, err)
	require.Equal(t, []webverify.ConsoleMessage{{Type: "error", Text: "new"}}, second)
	// A log that rolled over is all new.
	b.console = "[10:00:09] error: rolled\n"
	third, err := d.ConsoleSince(context.Background())
	require.NoError(t, err)
	require.Len(t, third, 1)
	require.Equal(t, "rolled", third[0].Text)
}

func TestWebVerifyDriverNetworkDedupes(t *testing.T) {
	t.Parallel()
	b := &scriptedBrowser{network: `[{"request_id":"1","url":"http://localhost/a","status":500},{"request_id":"2","failed":true}]`}
	d := newTestDriver(b)
	first, err := d.NetworkSince(context.Background())
	require.NoError(t, err)
	require.Len(t, first, 2)
	b.network = `[{"request_id":"1","url":"http://localhost/a","status":500},{"request_id":"3","url":"http://localhost/b","status":404}]`
	second, err := d.NetworkSince(context.Background())
	require.NoError(t, err)
	require.Len(t, second, 1)
	require.Equal(t, 404, second[0].Status)
}

func TestWebVerifyDriverRequests(t *testing.T) {
	t.Parallel()
	b := &scriptedBrowser{}
	d := newTestDriver(b)
	ctx := context.Background()
	require.NoError(t, d.SetViewport(ctx, webverify.Viewport{Name: "mobile", Width: 390, Height: 844, Mobile: true}))
	require.Equal(t, "cdp", b.calls[0].Action)
	require.Equal(t, "Emulation.setDeviceMetricsOverride", b.calls[0].CDPMethod)
	require.JSONEq(t, `{"width":390,"height":844,"deviceScaleFactor":1,"mobile":true}`, string(b.calls[0].CDPParams))

	img, err := d.Screenshot(ctx)
	require.NoError(t, err)
	require.Equal(t, "image/png", img.MediaType)
	require.NotNil(t, b.calls[1].Annotate)
	require.False(t, *b.calls[1].Annotate, "verification screenshots are clean")

	_, err = d.Eval(ctx, "1+1", 40*time.Second)
	require.NoError(t, err)
	require.Equal(t, 30000, b.calls[2].Advanced.TimeoutMS)
}

func TestWebVerifyDriverAssert(t *testing.T) {
	t.Parallel()
	b := &scriptedBrowser{reply: func(p BrowserParams) (fantasy.ToolResponse, error) {
		if p.Action == "assert" && p.Advanced.Expected == "no" {
			return fantasy.NewTextErrorResponse("assert failed: text_contains \"no\" not found"), nil
		}
		return fantasy.ToolResponse{}, nil
	}}
	d := newTestDriver(b)
	ok, detail, err := d.Assert(context.Background(), webverify.Assertion{Condition: "text_contains", Selector: "body", Expected: "yes"})
	require.NoError(t, err)
	require.True(t, ok)
	require.Empty(t, detail)
	ok, detail, err = d.Assert(context.Background(), webverify.Assertion{Condition: "text_contains", Selector: "body", Expected: "no"})
	require.NoError(t, err)
	require.False(t, ok)
	require.Contains(t, detail, "not found")
	require.Equal(t, 3000, b.calls[0].Advanced.TimeoutMS)
}

func TestWebVerifyDriverDenialIsUnavailable(t *testing.T) {
	t.Parallel()
	denied := NewPermissionDeniedResponse(nil)
	d := newTestDriver(&scriptedBrowser{reply: func(BrowserParams) (fantasy.ToolResponse, error) { return denied, nil }})
	err := d.Navigate(context.Background(), "http://localhost:3000")
	require.ErrorIs(t, err, webverify.ErrUnavailable)

	d = newTestDriver(&scriptedBrowser{reply: func(BrowserParams) (fantasy.ToolResponse, error) {
		return fantasy.ToolResponse{}, errors.New("session ID is required")
	}})
	require.ErrorIs(t, d.Navigate(context.Background(), "http://localhost:3000"), webverify.ErrUnavailable)

	// An ordinary tool error is the page's problem, not unavailability.
	d = newTestDriver(&scriptedBrowser{reply: func(BrowserParams) (fantasy.ToolResponse, error) {
		return fantasy.NewTextErrorResponse("navigate failed: net::ERR_CONNECTION_REFUSED"), nil
	}})
	err = d.Navigate(context.Background(), "http://localhost:3000")
	require.Error(t, err)
	require.NotErrorIs(t, err, webverify.ErrUnavailable)
}

func TestNewWebVerifyDriverWithoutBrowser(t *testing.T) {
	t.Parallel()
	_, _, err := NewWebVerifyDriver(context.Background(), nil, "x", "http://localhost")
	require.ErrorIs(t, err, webverify.ErrUnavailable)
}
