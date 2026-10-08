package tools

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// These tests replay targeting calls of session 14b5c83a against
// panelwait.html. Each failed in the session or waited 10-30 s.

func openPanelWait(t *testing.T) *liveTool {
	t.Helper()
	l := newLiveTool(t)
	l.ok(fmt.Sprintf(`{"action":"navigate","url":%q}`, l.base+"/panelwait.html"))
	return l
}

func (l *liveTool) panelModel() map[string]float64 {
	l.t.Helper()
	raw, err := l.session().Eval("JSON.stringify(window.model)")
	require.NoError(l.t, err)
	var text string
	require.NoError(l.t, json.Unmarshal([]byte(raw), &text))
	var m map[string]float64
	require.NoError(l.t, json.Unmarshal([]byte(text), &m))
	return m
}

// TestLiveLabelMatchesWrappingAriaLabel replays calls 96 and 99: the field
// sits in <label aria-label="Right padding"> with only an icon, so label
// matching found nothing while find by name did.
func TestLiveLabelMatchesWrappingAriaLabel(t *testing.T) {
	l := openPanelWait(t)
	resp := l.ok(`{"action":"set_field","advanced":{"label":"Right padding","value":"24","commit":"enter"}}`)
	require.Contains(t, resp.Content, `"accepted_value":"24"`)
	l.ok(`{"action":"set_field","advanced":{"label":"Left padding","within":"Auto layout","value":"24","commit":"enter"}}`)
	m := l.panelModel()
	require.Equal(t, 24.0, m["padR"])
	require.Equal(t, 24.0, m["padL"])
}

// TestLiveWithinAcceptsReportedContext replays calls 38/53/123: within was
// given in the form the tool itself reports (toolbar "Editor"), matched no
// container and waited out 10-30 s.
func TestLiveWithinAcceptsReportedContext(t *testing.T) {
	l := openPanelWait(t)
	for _, within := range []string{`toolbar \"Editor\"`, `region \"Right sidebar\" > toolbar \"Editor\"`, `Editor`} {
		started := time.Now()
		l.ok(fmt.Sprintf(`{"action":"semantic_click","advanced":{"role":"button","name":"Text","within":"%s"}}`, within))
		require.Less(t, time.Since(started), 5*time.Second, within)
	}
	started := time.Now()
	hidden := l.ok(`{"action":"assert","advanced":{"condition":"hidden","role":"button","name":"Stop","within":"region \"Left sidebar\"","timeout_ms":30000}}`)
	require.Contains(t, hidden.Content, `"passed":true`)
	require.Less(t, time.Since(started), 5*time.Second)
}

// TestLiveUnresolvableTargetsFailFast: an absent container, an absent
// target and a covered control used to hold the call for the whole
// timeout. They now stop within their grace and say what exists.
func TestLiveUnresolvableTargetsFailFast(t *testing.T) {
	l := openPanelWait(t)

	started := time.Now()
	missing := l.run(`{"action":"semantic_click","advanced":{"role":"button","name":"Text","within":"Inspector"}}`)
	require.True(t, missing.IsError)
	require.Contains(t, missing.Content, "within_not_found")
	require.Contains(t, missing.Content, "Right sidebar", "the error lists real containers")
	require.Less(t, time.Since(started), 5*time.Second)
	require.Equal(t, "none", opOf(t, missing.Content).Dispatch)

	started = time.Now()
	absent := l.run(`{"action":"semantic_click","advanced":{"role":"button","name":"Collapse chat"}}`)
	require.True(t, absent.IsError)
	require.Contains(t, absent.Content, "target_not_found")
	require.Less(t, time.Since(started), 5*time.Second)

	found := l.ok(`{"action":"find","advanced":{"role":"button","name":"Main menu"}}`)
	var result struct {
		Matches []struct {
			Ref string `json:"ref"`
		} `json:"matches"`
	}
	require.NoError(t, json.Unmarshal([]byte(found.Content), &result))
	require.Len(t, result.Matches, 1)
	started = time.Now()
	covered := l.run(fmt.Sprintf(`{"action":"click","ref":%q}`, result.Matches[0].Ref))
	require.True(t, covered.IsError)
	require.Contains(t, covered.Content, "div#shield", "the blocker is named, not \"covered by none\"")
	require.Less(t, time.Since(started), 8*time.Second, "a stable blocker does not hold the call for the 20 s action timeout")
	require.Zero(t, l.panelModel()["menu"])
}
