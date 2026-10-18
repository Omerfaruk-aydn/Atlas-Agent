package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/stretchr/testify/require"
)

type recordedInvoker struct {
	mu    sync.Mutex
	calls []fantasy.ToolCall
	reply func(fantasy.ToolCall) fantasy.ToolResponse
}

func (r *recordedInvoker) invoke(_ context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
	r.mu.Lock()
	r.calls = append(r.calls, call)
	r.mu.Unlock()
	return r.reply(call), nil
}

func runSteps(t *testing.T, inv *recordedInvoker, body string) []PipelineResult {
	t.Helper()
	ctx := context.WithValue(t.Context(), SessionIDContextKey, "transform-test")
	resp, err := NewToolPipeline(inv.invoke).Run(ctx, fantasy.ToolCall{ID: "p", Name: ToolPipelineName, Input: body})
	require.NoError(t, err)
	var results []PipelineResult
	if json.Unmarshal([]byte(resp.Content), &results) == nil {
		return results
	}
	var out struct {
		Results []PipelineResult `json:"results"`
	}
	require.NoError(t, json.Unmarshal([]byte(resp.Content), &out), resp.Content)
	return out.Results
}

func resultByID(results []PipelineResult, id string) []PipelineResult {
	var out []PipelineResult
	for _, r := range results {
		if r.ID == id {
			out = append(out, r)
		}
	}
	return out
}

// A large search result is reduced in place: only the matching files are
// read, through the ordinary invoke path, and the subset carries its source.
func TestTransformFiltersLargeResultsAndDrivesItems(t *testing.T) {
	t.Parallel()

	var grep strings.Builder
	grep.WriteString("Found 300 matches\n")
	for i := range 100 {
		fmt.Fprintf(&grep, "D:/repo/pkg%d/a.go:\n  Line 3: x := LegacyClient{}\n", i%3)
	}
	inv := &recordedInvoker{reply: func(call fantasy.ToolCall) fantasy.ToolResponse {
		switch call.Name {
		case GrepToolName:
			return fantasy.NewTextResponse(grep.String())
		case ViewToolName:
			var p ViewParams
			_ = json.Unmarshal([]byte(call.Input), &p)
			return fantasy.NewTextResponse("// TODO: drop " + p.FilePath + "\nfunc main() {}\n")
		}
		return fantasy.NewTextErrorResponse("unexpected tool " + call.Name)
	}}
	results := runSteps(t, inv, `{"steps":[
 {"id":"hits","tool":"grep","arguments":{"pattern":"LegacyClient","path":"."}},
 {"id":"files","tool":"transform","arguments":{"source":"hits","op":"extract","pattern":"^(\\S.*):$"}},
 {"id":"unique","tool":"transform","arguments":{"source":"files#/items","op":"unique","limit":10}},
 {"id":"read","tool":"view","arguments":{"file_path":"$item"},"items_from":"unique#/items","when":{"source":"unique#/count","greater_than":0}},
 {"id":"todos","tool":"transform","arguments":{"source":"read","op":"filter","pattern":"TODO"}},
 {"id":"n","tool":"transform","arguments":{"source":"todos#/items","op":"count"}}]}`)

	var names []string
	for _, c := range inv.calls {
		names = append(names, c.Name)
	}
	require.Equal(t, []string{GrepToolName, ViewToolName, ViewToolName, ViewToolName}, names, "transforms invoke nothing; the loop ran once per unique file")

	unique := resultByID(results, "unique")
	require.Len(t, unique, 1)
	require.Equal(t, "none", unique[0].Dispatch)
	var u struct {
		Items      []string `json:"items"`
		InputCount int      `json:"input_count"`
		SourceHash string   `json:"source_sha256"`
	}
	require.NoError(t, json.Unmarshal([]byte(unique[0].Content), &u))
	require.Equal(t, []string{"D:/repo/pkg0/a.go", "D:/repo/pkg1/a.go", "D:/repo/pkg2/a.go"}, u.Items)
	require.Equal(t, 100, u.InputCount)
	require.NotEmpty(t, u.SourceHash)

	count := resultByID(results, "n")
	require.Contains(t, count[0].Content, `"count":3`)
}

func TestTransformConditionsSkipAndFailWithoutSending(t *testing.T) {
	t.Parallel()

	inv := &recordedInvoker{reply: func(call fantasy.ToolCall) fantasy.ToolResponse {
		return fantasy.NewTextResponse("No files found")
	}}
	results := runSteps(t, inv, `{"steps":[
 {"id":"hits","tool":"grep","arguments":{"pattern":"Nothing"}},
 {"id":"files","tool":"transform","arguments":{"source":"hits","op":"filter","pattern":"\\.go:$"}},
 {"id":"read","tool":"view","arguments":{"file_path":"$item"},"items_from":"files#/items"}]}`)
	require.Len(t, inv.calls, 1)
	read := resultByID(results, "read")
	require.Equal(t, StepSkipped, read[0].Status)
	require.Contains(t, read[0].Reason, "is empty")

	// A transform over a failed source fails before anything is sent.
	inv = &recordedInvoker{reply: func(call fantasy.ToolCall) fantasy.ToolResponse {
		return fantasy.NewTextErrorResponse("permission denied")
	}}
	results = runSteps(t, inv, `{"steps":[
 {"id":"hits","tool":"grep","arguments":{"pattern":"x"},"on_error":"continue"},
 {"id":"files","tool":"transform","arguments":{"source":"hits","op":"lines"}},
 {"id":"read","tool":"view","arguments":{"file_path":"$item"},"items_from":"files#/items"}]}`)
	require.Len(t, inv.calls, 1)
	files := resultByID(results, "files")
	require.Equal(t, StepFailed, files[0].Status)
	require.Contains(t, files[0].Content, "nothing was sent")
}

func TestItemsFromRefusesTruncatedArguments(t *testing.T) {
	t.Parallel()
	_, err := resolveItemsFrom("t#/items", map[string]json.RawMessage{"t": json.RawMessage(`{"op":"extract","items":["partial-path"],"truncated":true}`)})
	require.ErrorContains(t, err, "truncated transform data")
}

func TestTransformDoesNotSilentlyDropLargeIterations(t *testing.T) {
	t.Parallel()
	inv := &recordedInvoker{reply: func(fantasy.ToolCall) fantasy.ToolResponse {
		return fantasy.NewTextResponse(strings.Repeat("x", maxPipelineTransformText/2+1))
	}}
	results := runSteps(t, inv, `{"steps":[{"id":"read","tool":"view","arguments":{"file_path":"$item"},"items":["a","b"]},{"id":"count","tool":"transform","arguments":{"source":"read","op":"count"}}]}`)
	require.Len(t, inv.calls, 2)
	count := resultByID(results, "count")
	require.Len(t, count, 1)
	require.Equal(t, StepFailed, count[0].Status)
	require.Contains(t, count[0].Content, "retained data limit")
}

func TestTransformPreflightRejectsBadPrograms(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct{ body, want string }{
		"unknown op":          {`{"steps":[{"id":"a","tool":"grep","arguments":{"pattern":"x"}},{"id":"b","tool":"transform","arguments":{"source":"a","op":"eval"}}]}`, "op must be one of"},
		"later source":        {`{"steps":[{"id":"b","tool":"transform","arguments":{"source":"a","op":"lines"}},{"id":"a","tool":"grep","arguments":{"pattern":"x"}}]}`, "earlier step"},
		"bad regex":           {`{"steps":[{"id":"a","tool":"grep","arguments":{"pattern":"x"}},{"id":"b","tool":"transform","arguments":{"source":"a","op":"filter","pattern":"("}}]}`, "does not compile"},
		"extract groups":      {`{"steps":[{"id":"a","tool":"grep","arguments":{"pattern":"x"}},{"id":"b","tool":"transform","arguments":{"source":"a","op":"extract","pattern":"x"}}]}`, "exactly one capture group"},
		"unknown field":       {`{"steps":[{"id":"a","tool":"grep","arguments":{"pattern":"x"}},{"id":"b","tool":"transform","arguments":{"source":"a","op":"lines","command":"rm"}}]}`, "unknown field"},
		"items with from":     {`{"steps":[{"id":"a","tool":"grep","arguments":{"pattern":"x"}},{"id":"b","tool":"view","arguments":{"file_path":"$item"},"items":["x"],"items_from":"a#/items"}]}`, "items_from must be"},
		"two condition kinds": {`{"steps":[{"id":"a","tool":"grep","arguments":{"pattern":"x"}},{"id":"b","tool":"view","arguments":{"file_path":"y"},"when":{"source":"a#/count","greater_than":1,"less_than":3}}]}`, "exactly one of"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			inv := &recordedInvoker{reply: func(fantasy.ToolCall) fantasy.ToolResponse { return fantasy.NewTextResponse("ok") }}
			ctx := context.WithValue(t.Context(), SessionIDContextKey, "preflight")
			resp, err := NewToolPipeline(inv.invoke).Run(ctx, fantasy.ToolCall{ID: "p", Name: ToolPipelineName, Input: tc.body})
			require.NoError(t, err)
			require.True(t, resp.IsError, resp.Content)
			require.Contains(t, resp.Content, tc.want)
			require.Empty(t, inv.calls, "preflight sends nothing")
		})
	}
}

func TestItemsFromIsBounded(t *testing.T) {
	t.Parallel()

	many := make([]string, 70)
	for i := range many {
		many[i] = fmt.Sprintf("f%d.go", i)
	}
	inv := &recordedInvoker{reply: func(call fantasy.ToolCall) fantasy.ToolResponse {
		return fantasy.NewTextResponse(strings.Join(many, "\n"))
	}}
	results := runSteps(t, inv, `{"steps":[
 {"id":"ls","tool":"glob","arguments":{"pattern":"*.go"}},
 {"id":"all","tool":"transform","arguments":{"source":"ls","op":"lines"}},
 {"id":"read","tool":"view","arguments":{"file_path":"$item"},"items_from":"all#/items"}]}`)
	require.Len(t, inv.calls, 1)
	read := resultByID(results, "read")
	require.Equal(t, StepFailed, read[0].Status)
	require.Contains(t, read[0].Content, "at most 64")
}

func TestTransformPreservesJSONArraysAndLargeIntegerIdentity(t *testing.T) {
	t.Parallel()
	texts := map[string]string{"source": `[9007199254740993,9007199254740992,"9007199254740993"]`}
	decoded, err := runTransform(map[string]any{"source": "source", "op": "json"}, texts, nil)
	require.NoError(t, err)
	require.Contains(t, string(decoded), `"count":3`)
	outputs := map[string]json.RawMessage{"decoded": decoded}
	items, err := resolveItemsFrom("decoded#/items", outputs)
	require.NoError(t, err)
	require.Equal(t, []string{"9007199254740993", "9007199254740992", "9007199254740993"}, items)
	unique, err := runTransform(map[string]any{"source": "decoded#/items", "op": "unique"}, nil, outputs)
	require.NoError(t, err)
	require.Contains(t, string(unique), `"count":3`, "numeric and string values are distinct")
	sorted, err := runTransform(map[string]any{"source": "decoded#/items", "op": "sort"}, nil, outputs)
	require.NoError(t, err)
	require.Contains(t, string(sorted), `"items":[9007199254740992,9007199254740993,"9007199254740993"]`)
}
