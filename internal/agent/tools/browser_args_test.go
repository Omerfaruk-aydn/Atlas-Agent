package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/browser"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/computer"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/stretchr/testify/require"
)

// decodeBrowser decodes arguments exactly as the tool's Run does.
func decodeBrowser(t *testing.T, raw string) (BrowserParams, error) {
	t.Helper()
	var p BrowserParams
	err := json.Unmarshal([]byte(raw), &p)
	return p, err
}

func contractCode(t *testing.T, err error) string {
	t.Helper()
	require.Error(t, err)
	code, _, _ := strings.Cut(err.Error(), ":")
	return code
}

// TestBrowserSetFieldTopLevelValue replays call 22 of session 14b5c83a:
// {"action":"set_field","ref":"e240","value":"..."} reported requested:""
// because value was not a browser field and the decoder dropped it.
func TestBrowserSetFieldTopLevelValue(t *testing.T) {
	t.Parallel()
	p, err := decodeBrowser(t, `{"action":"set_field","ref":"e240","value":"Eşleştirme — Başlangıç","advanced":{"mode":"replace","commit":"enter"}}`)
	require.NoError(t, err)
	require.Equal(t, "Eşleştirme — Başlangıç", p.Advanced.Value)
	require.True(t, p.Advanced.ValueSet)
	require.Equal(t, "replace", p.Advanced.Mode)

	same, err := decodeBrowser(t, `{"action":"set_field","ref":"e1","value":"24","advanced":{"value":"24"}}`)
	require.NoError(t, err)
	require.Equal(t, "24", same.Advanced.Value)
}

func TestBrowserSetFieldValueContract(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		input string
		code  string
	}{
		"missing value":            {`{"action":"set_field","ref":"e1"}`, "missing_value"},
		"missing value with mode":  {`{"action":"set_field","ref":"e1","advanced":{"mode":"replace","commit":"enter"}}`, "missing_value"},
		"conflicting value":        {`{"action":"set_field","ref":"e1","value":"24","advanced":{"value":"25"}}`, "conflicting_fields"},
		"conflicting text":         {`{"action":"set_field","ref":"e1","text":"a","advanced":{"value":"b"}}`, "conflicting_fields"},
		"clear with value":         {`{"action":"set_field","ref":"e1","advanced":{"mode":"clear","value":"x"}}`, "conflicting_fields"},
		"empty append":             {`{"action":"set_field","ref":"e1","advanced":{"mode":"append","value":""}}`, "missing_value"},
		"number value":             {`{"action":"set_field","ref":"e1","value":24}`, "invalid_arguments"},
		"advanced number value":    {`{"action":"set_field","ref":"e1","advanced":{"value":24}}`, "invalid_arguments"},
		"empty option":             {`{"action":"select_option","ref":"e1","advanced":{"value":""}}`, "missing_value"},
		"value on click":           {`{"action":"click","ref":"e1","value":"x"}`, "unknown_field"},
		"advanced value on assert": {`{"action":"assert","advanced":{"condition":"value","role":"textbox","name":"W","value":"390"}}`, "unknown_field"},
		"top-level role":           {`{"action":"semantic_click","role":"button","name":"Share"}`, "unknown_field"},
		"unknown advanced key":     {`{"action":"find","advanced":{"role":"button","nme":"Share"}}`, "unknown_field"},
		"ref conflict":             {`{"action":"read_field","ref":"e1","advanced":{"ref":"e2"}}`, "conflicting_fields"},
		"type without text":        {`{"action":"type","ref":"e1"}`, "missing_value"},
		"semantic_type text clash": {`{"action":"semantic_type","text":"a","advanced":{"role":"textbox","text":"b"}}`, "conflicting_fields"},
		"fill_form field no value": {`{"action":"fill_form","advanced":{"fields":[{"label":"Email"}]}}`, "missing_value"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := decodeBrowser(t, tc.input)
			require.Equal(t, tc.code, contractCode(t, err), err)
			require.Contains(t, err.Error(), `"input_sent":false`)
		})
	}
}

// TestBrowserEmptyValueIsExplicitClear: an empty string clears the field;
// it never becomes a typed no-op that reports the old value as committed.
func TestBrowserEmptyValueIsExplicitClear(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{
		`{"action":"set_field","ref":"e1","value":""}`,
		`{"action":"set_field","ref":"e1","advanced":{"value":"","mode":"replace"}}`,
		`{"action":"set_field","ref":"e1","advanced":{"mode":"clear"}}`,
	} {
		p, err := decodeBrowser(t, raw)
		require.NoError(t, err, raw)
		require.Equal(t, "clear", p.Advanced.Mode, raw)
		require.Empty(t, p.Advanced.Value, raw)
	}
}

// TestBrowserAdvancedRefIsHoisted replays call 95: read_field with
// advanced.ref failed with "semantic target is required".
func TestBrowserAdvancedRefIsHoisted(t *testing.T) {
	t.Parallel()
	p, err := decodeBrowser(t, `{"action":"read_field","advanced":{"ref":"e555"}}`)
	require.NoError(t, err)
	require.Equal(t, "e555", p.Ref)
	request, err := fieldRequest("read_field", p)
	require.NoError(t, err)
	require.Equal(t, `[data-atlas-ref="e555"]`, request.Selector)
}

// TestBrowserRequestRoundTripKeepsEmptyValue: a workflow step is encoded
// and decoded again before it runs; an explicit empty value must survive.
func TestBrowserRequestRoundTripKeepsEmptyValue(t *testing.T) {
	t.Parallel()
	var p BrowserWorkflowParams
	require.NoError(t, json.Unmarshal([]byte(`{"steps":[{"id":"clear","action":"set_field","ref":"e1","target":{"value":""}},{"id":"name","action":"set_field","ref":"e2","text":"Atlas"}]}`), &p))
	steps, err := compileBrowserWorkflow(p)
	require.NoError(t, err)
	encoded, err := json.Marshal(steps[0].Arguments)
	require.NoError(t, err)
	decoded, err := decodeBrowser(t, string(encoded))
	require.NoError(t, err, string(encoded))
	require.Equal(t, "clear", decoded.Advanced.Mode)

	encoded, err = json.Marshal(steps[1].Arguments)
	require.NoError(t, err)
	decoded, err = decodeBrowser(t, string(encoded))
	require.NoError(t, err, string(encoded))
	require.Equal(t, "Atlas", decoded.Advanced.Value, "a workflow step's text is the set_field value")
}

// TestBrowserValueSamePathDirectAndPipeline sends the same set_field as a
// direct call and as a tool_pipeline step and requires the driver to
// receive the same request; a malformed step is refused before any step
// of the pipeline runs.
func TestBrowserValueSamePathDirectAndPipeline(t *testing.T) {
	t.Parallel()
	const call = `{"action":"set_field","ref":"e1","value":"390","advanced":{"commit":"enter"}}`

	direct := newObservingSession()
	h := newBrowserHarness(t, direct)
	h.run(`{"action":"observe","advanced":{"mode":"full"}}`)
	resp := h.run(call)
	require.False(t, resp.IsError, resp.Content)
	require.Len(t, direct.fields, 1)
	require.Equal(t, "390", direct.fields[0].Value)

	piped := newObservingSession()
	ph := newBrowserHarness(t, piped)
	ph.run(`{"action":"observe","advanced":{"mode":"full"}}`)
	pipeline := newToolPipeline(func(ctx context.Context, c fantasy.ToolCall) (fantasy.ToolResponse, error) {
		return ph.tool.Run(ph.ctx, c)
	})
	out, err := pipeline.Run(ph.ctx, fantasy.ToolCall{ID: "p1", Name: "tool_pipeline", Input: fmt.Sprintf(`{"steps":[{"id":"w","tool":"browser","arguments":%s}]}`, call)})
	require.NoError(t, err)
	require.False(t, out.IsError, out.Content)
	require.Len(t, piped.fields, 1)
	require.Equal(t, direct.fields[0].Value, piped.fields[0].Value)
	require.Equal(t, direct.fields[0].Commit, piped.fields[0].Commit)

	refused, err := pipeline.Run(ph.ctx, fantasy.ToolCall{ID: "p2", Name: "tool_pipeline", Input: `{"steps":[{"id":"ok","tool":"browser","arguments":{"action":"set_field","ref":"e1","value":"1"}},{"id":"bad","tool":"browser","arguments":{"action":"set_field","ref":"e1"}}]}`})
	require.NoError(t, err)
	require.True(t, refused.IsError)
	require.Contains(t, refused.Content, "missing_value")
	require.Len(t, piped.fields, 1, "no step runs when a later step cannot")

	// The direct call is refused the same way, before anything is sent.
	missing := h.run(`{"action":"set_field","ref":"e1"}`)
	require.True(t, missing.IsError)
	require.Contains(t, missing.Content, "missing_value")
	require.Len(t, direct.fields, 1)
}

// identityObservingSession reports ref identities so the stale-identity
// guard can be exercised: the application reuses the node behind a ref.
type identityObservingSession struct {
	*observingSession
	current map[string]browser.RefIdentity
	clicks  []string
}

func (s *identityObservingSession) RefIdentities(_ context.Context, refs []string) (map[string]browser.RefIdentity, error) {
	out := map[string]browser.RefIdentity{}
	for _, ref := range refs {
		if id, ok := s.current[ref]; ok {
			out[ref] = id
		}
	}
	return out, nil
}

func (s *identityObservingSession) Click(selector string) error {
	s.clicks = append(s.clicks, selector)
	return nil
}

// TestBrowserStaleRefIdentityRefused: a ref whose node now shows another
// item (a virtualized layer row) is refused before input; nothing is sent
// to the other object.
func TestBrowserStaleRefIdentityRefused(t *testing.T) {
	t.Parallel()
	sess := &identityObservingSession{observingSession: newObservingSession(), current: map[string]browser.RefIdentity{
		"e1": {Role: "textbox", Name: "Width"}, "e3": {Role: "button", Name: "Close"},
	}}
	h := newBrowserHarness(t, sess)
	h.run(`{"action":"observe","advanced":{"mode":"full"}}`)

	sess.current["e3"] = browser.RefIdentity{Role: "button", Name: "Delete"}
	resp := h.run(`{"action":"click","ref":"e3"}`)
	require.True(t, resp.IsError)
	require.Contains(t, resp.Content, "stale_ref")
	require.Contains(t, resp.Content, `"Delete"`)
	require.Empty(t, sess.clicks, "no click reaches the reused node")
	require.Equal(t, "none", opOf(t, resp.Content).Dispatch)

	delete(sess.current, "e3")
	gone := h.run(`{"action":"click","ref":"e3"}`)
	require.True(t, gone.IsError)
	require.Contains(t, gone.Content, "gone from the page")

	// A fresh observation shows the new item; its ref is usable again.
	sess.current["e3"] = browser.RefIdentity{Role: "button", Name: "Delete"}
	sess.elements[2].Name = "Delete"
	h.run(`{"action":"observe","advanced":{"mode":"full"}}`)
	ok := h.run(`{"action":"click","ref":"e3"}`)
	require.False(t, ok.IsError, ok.Content)
	require.Len(t, sess.clicks, 1)
}

// TestBrowserContractErrorsReportNoDispatch: a refused call never reports
// an unknown outcome, so the model is not told to verify a non-event.
func TestBrowserContractErrorsReportNoDispatch(t *testing.T) {
	t.Parallel()
	h := newBrowserHarness(t, newObservingSession())
	resp := h.run(`{"action":"wait_for","advanced":{"condition":"not_busy","timeout_ms":30000}}`)
	require.True(t, resp.IsError)
	require.Contains(t, resp.Content, "invalid_arguments")
	op := opOf(t, resp.Content)
	require.Equal(t, "none", op.Dispatch)
	require.Equal(t, "invalid_arguments", op.ErrorCode)
}

// The advertised schema and the decoder accept the same keys, at the top
// level and inside advanced: nothing the model is shown is refused, and
// nothing the runtime reads is hidden from the model.
func TestBrowserSchemaMatchesDecoder(t *testing.T) {
	t.Parallel()
	prepared := fantasy.PrepareFunctionTool(newBrowserTool(&mockPermissionService{}, t.TempDir(), newFakeBrowserSessions(), browserDescription(false)))
	props := prepared.InputSchema["properties"].(map[string]any)
	advanced := props["advanced"].(map[string]any)["properties"].(map[string]any)
	keys := func(m map[string]any) []string {
		out := make([]string, 0, len(m))
		for key := range m {
			out = append(out, key)
		}
		return out
	}
	require.ElementsMatch(t, computer.KnownFields(BrowserParams{}), keys(props))
	require.ElementsMatch(t, computer.KnownFields(browser.Request{}), keys(advanced))
	require.Contains(t, props, "value", "the top-level value alias the decoder accepts is advertised")
}
