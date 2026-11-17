package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/design"
	"github.com/stretchr/testify/require"
)

func runCanvas(t *testing.T, tool fantasy.AgentTool, input string) fantasy.ToolResponse {
	t.Helper()
	rsp, err := tool.Run(context.Background(), fantasy.ToolCall{ID: "c1", Name: DesignCanvasToolName, Input: input})
	require.NoError(t, err)
	return rsp
}

func TestDesignCanvasCreateApplyReadExport(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	tool := NewDesignCanvasTool(dir)
	require.Equal(t, DesignCanvasToolName, tool.Info().Name)

	rsp := runCanvas(t, tool, `{"action":"list"}`)
	require.False(t, rsp.IsError)
	require.Contains(t, rsp.Content, "No design files yet")

	rsp = runCanvas(t, tool, `{"action":"create","title":"Açılış Sayfası","ops":[
		{"op":"add","node":{"id":"desk","type":"frame","name":"Desktop","width":1440,"height":900,"children":[
			{"id":"title","type":"text","x":120,"y":160,"text":"Build faster","font":{"size":64,"weight":700}}]}}]}`)
	require.False(t, rsp.IsError, rsp.Content)
	require.Contains(t, rsp.Content, "acilis-sayfasi")
	require.Contains(t, rsp.Content, "op0→desk")
	require.FileExists(t, filepath.Join(dir, ".atlas", "design", "files", "acilis-sayfasi.json"))

	rsp = runCanvas(t, tool, `{"action":"apply","file":"acilis-sayfasi","ops":[
		{"op":"add","parent":"desk","node":{"id":"cta","type":"rect","x":120,"y":300,"width":160,"height":48,"radius":12,"fills":[{"type":"solid","color":"#5b5bf7"}]}},
		{"op":"update","id":"title","props":{"fills":[{"type":"solid","color":"#111111"}]}}]}`)
	require.False(t, rsp.IsError, rsp.Content)
	require.Contains(t, rsp.Content, `rect "Rectangle" id=cta`)
	require.Contains(t, rsp.Content, "fill=#111111")

	rsp = runCanvas(t, tool, `{"action":"read","file":"acilis-sayfasi"}`)
	require.False(t, rsp.IsError)
	require.Contains(t, rsp.Content, `frame "Desktop" id=desk`)
	require.Contains(t, rsp.Content, `  - rect "Rectangle" id=cta`)

	rsp = runCanvas(t, tool, `{"action":"read","file":"acilis-sayfasi","node_id":"cta"}`)
	require.False(t, rsp.IsError)
	require.Contains(t, rsp.Content, `inside frame "Desktop" (desk)`)
	require.Contains(t, rsp.Content, `"color": "#5B5BF7"`)

	rsp = runCanvas(t, tool, `{"action":"export","file":"acilis-sayfasi","node_id":"desk"}`)
	require.False(t, rsp.IsError, rsp.Content)
	svg, err := os.ReadFile(filepath.Join(dir, ".atlas", "design", "exports", "acilis-sayfasi-desk.svg"))
	require.NoError(t, err)
	require.Contains(t, string(svg), "Build faster")
	require.Contains(t, rsp.Content, ".atlas/design/exports/acilis-sayfasi-desk.svg")

	// A failing batch changes nothing.
	before, err := os.ReadFile(filepath.Join(dir, ".atlas", "design", "files", "acilis-sayfasi.json"))
	require.NoError(t, err)
	rsp = runCanvas(t, tool, `{"action":"apply","file":"acilis-sayfasi","ops":[{"op":"delete","id":"cta"},{"op":"update","id":"ghost","props":{"x":1}}]}`)
	require.True(t, rsp.IsError)
	require.Contains(t, rsp.Content, "op 1 (update)")
	after, err := os.ReadFile(filepath.Join(dir, ".atlas", "design", "files", "acilis-sayfasi.json"))
	require.NoError(t, err)
	require.Equal(t, string(before), string(after))

	doc, _, err := design.NewStore(dir).Load("acilis-sayfasi")
	require.NoError(t, err)
	require.Equal(t, "Açılış Sayfası", doc.Name)
}

func TestDesignCanvasRefusesBadInput(t *testing.T) {
	t.Parallel()
	tool := NewDesignCanvasTool(t.TempDir())
	for input, want := range map[string]string{
		`{"action":"paint"}`:                        "unknown action",
		`{"action":"read"}`:                         "file is required",
		`{"action":"read","file":"../etc"}`:         "file is required",
		`{"action":"read","file":"nope"}`:           `no design named "nope"`,
		`{"action":"create","file":"Bad Name"}`:     "invalid file",
		`{"action":"create","file":"a","ops":[{}]}`: "unknown op",
	} {
		rsp := runCanvas(t, tool, input)
		require.True(t, rsp.IsError, input)
		require.Contains(t, rsp.Content, want, input)
	}
	rsp := runCanvas(t, tool, `{"action":"create","file":"a"}`)
	require.False(t, rsp.IsError, rsp.Content)
	rsp = runCanvas(t, tool, `{"action":"create","file":"a"}`)
	require.True(t, rsp.IsError)
	require.Contains(t, rsp.Content, "already exists")

	many := make([]map[string]any, designMaxOps+1)
	for i := range many {
		many[i] = map[string]any{"op": "add_page"}
	}
	raw, err := json.Marshal(map[string]any{"action": "apply", "file": "a", "ops": many})
	require.NoError(t, err)
	rsp = runCanvas(t, tool, string(raw))
	require.True(t, rsp.IsError)
	require.True(t, strings.Contains(rsp.Content, "at most"))
}

func TestDesignCanvasDescriptionNamesTheFormat(t *testing.T) {
	t.Parallel()
	for _, want := range []string{".atlas/design/files", "auto_resize", "component_id", "make_component", "export"} {
		require.Contains(t, designCanvasDescription, want)
	}
}

func TestDesignImportCreatesAndAdds(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store := design.NewStore(dir)
	html := `<body><div style="display:flex;gap:8px;padding:16px;background:#fff"><h1>Hi</h1><p>there</p></div></body>`
	out, err := runDesignCanvas(dir, store, DesignCanvasParams{Action: "import", File: "ui", Source: html, Width: 600})
	require.NoError(t, err)
	require.Contains(t, out, "Imported")
	doc, _, err := store.Load("ui")
	require.NoError(t, err)
	require.Len(t, doc.Pages[0].Children, 1)
	require.InDelta(t, 600, doc.Pages[0].Children[0].Width, 1e-9)

	_, err = runDesignCanvas(dir, store, DesignCanvasParams{Action: "import", File: "ui", Source: html})
	require.NoError(t, err)
	doc, _, _ = store.Load("ui")
	require.Len(t, doc.Pages[0].Children, 2)
	require.Greater(t, doc.Pages[0].Children[1].X, 600.0, "a second import lands beside the first")

	_, err = runDesignCanvas(dir, store, DesignCanvasParams{Action: "import", File: "ui", Path: "../outside.html"})
	require.Error(t, err)
	_, err = runDesignCanvas(dir, store, DesignCanvasParams{Action: "import", File: "ui"})
	require.Error(t, err)
}

func TestDesignApplyKeepsParallelAgentsEdits(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store := design.NewStore(dir)
	_, err := runDesignCanvas(dir, store, DesignCanvasParams{Action: "create", File: "swarm", Title: "Swarm", Ops: []DesignCanvasOp{
		{Op: "add_page", Name: "A"}, {Op: "add_page", Name: "B"},
	}})
	require.NoError(t, err)
	var wg sync.WaitGroup
	for _, page := range []string{"A", "B"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 5; i++ {
				_, err := runDesignCanvas(dir, store, DesignCanvasParams{Action: "apply", File: "swarm", Page: page, Ops: []DesignCanvasOp{
					{Op: "add", Node: map[string]any{"type": "rect", "width": 10, "height": 10}},
				}})
				require.NoError(t, err)
			}
		}()
	}
	wg.Wait()
	doc, _, err := store.Load("swarm")
	require.NoError(t, err)
	for _, name := range []string{"A", "B"} {
		require.Len(t, doc.PageByRef(name).Children, 5, "page %s keeps all its edits", name)
	}
}

func TestDesignCanvasKit(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	tool := NewDesignCanvasTool(dir)

	rsp := runCanvas(t, tool, `{"action":"themes"}`)
	require.False(t, rsp.IsError, rsp.Content)
	for _, want := range []string{"zinc", "butter", "ios", "material", "crimson", "c-<token>", "t-<step>"} {
		require.Contains(t, rsp.Content, want)
	}
	rsp = runCanvas(t, tool, `{"action":"blocks","query":"ios"}`)
	require.False(t, rsp.IsError, rsp.Content)
	require.Contains(t, rsp.Content, "ios/tab-bar")
	require.NotContains(t, rsp.Content, "web/hero")
	rsp = runCanvas(t, tool, `{"action":"icons","query":"cart"}`)
	require.False(t, rsp.IsError, rsp.Content)
	require.Contains(t, rsp.Content, "shopping-cart")

	rsp = runCanvas(t, tool, `{"action":"create","title":"Trailhead","ops":[
		{"op":"set_theme","name":"stone","accent":"grass"},
		{"op":"add","node":{"id":"page","type":"frame","name":"Landing","width":1440,"height":10,"sizing_y":"hug","layout":{"dir":"column","gap":0,"pad":0}}},
		{"op":"insert","block":"web/navbar","parent":"page","props":{"brand":"Trailhead"}},
		{"op":"insert","block":"web/hero","id":"hero","parent":"page","props":{"title":"Find the trail that fits your weekend"}},
		{"op":"add","parent":"page","node":{"type":"path","icon":"mountain","width":32}}]}`)
	require.False(t, rsp.IsError, rsp.Content)
	require.Contains(t, rsp.Content, "op3→hero")

	rsp = runCanvas(t, tool, `{"action":"lint","file":"trailhead"}`)
	require.False(t, rsp.IsError, rsp.Content)
	require.Contains(t, rsp.Content, "Quality score")

	rsp = runCanvas(t, tool, `{"action":"apply","file":"trailhead","ops":[{"op":"insert","block":"web/nope"}]}`)
	require.True(t, rsp.IsError)
	require.Contains(t, rsp.Content, "no block")
}
