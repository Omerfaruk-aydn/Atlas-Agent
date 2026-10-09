package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/stretchr/testify/require"
)

func fixtureTool(name, description string, ran *atomic.Int32) fantasy.AgentTool {
	return fantasy.NewAgentTool(name, description, func(context.Context, struct{}, fantasy.ToolCall) (fantasy.ToolResponse, error) {
		if ran != nil {
			ran.Add(1)
		}
		return fantasy.NewTextResponse("ran"), nil
	})
}

func toolNameList(ts []fantasy.AgentTool) []string {
	out := make([]string, 0, len(ts))
	for _, tool := range ts {
		out = append(out, tool.Info().Name)
	}
	return out
}

func TestDeferredPaletteCannotRestoreDisabledTool(t *testing.T) {
	t.Parallel()
	store := engineering.NewStore(t.TempDir())
	search := NewToolSearchTool(ToolSearchOptions{Palette: func(context.Context) []fantasy.AgentTool { return nil }})
	optional := fixtureTool("optional", "fixture", nil)
	view := fixtureTool("view", "read files", nil)
	palette := []fantasy.AgentTool{optional, search, view}

	loaded, deferred, err := SplitDeferredPalette(t.Context(), store, "session", palette, nil)
	require.NoError(t, err)
	require.Equal(t, []string{"tool_search", "view"}, toolNameList(loaded))
	require.Equal(t, []string{"optional"}, toolNameList(deferred))

	require.NoError(t, DiscoverTools(t.Context(), store, "session", []string{"optional"}))
	loaded, deferred, err = SplitDeferredPalette(t.Context(), store, "session", palette, nil)
	require.NoError(t, err)
	require.Equal(t, []string{"optional", "tool_search", "view"}, toolNameList(loaded))
	require.Empty(t, deferred)

	// The user disables the tool (or its MCP server disconnects): the old
	// record must not bring it back.
	loaded, _, err = SplitDeferredPalette(t.Context(), store, "session", []fantasy.AgentTool{search, view}, nil)
	require.NoError(t, err)
	require.Equal(t, []string{"tool_search", "view"}, toolNameList(loaded))

	// Another session never inherits the discovery.
	loaded, _, err = SplitDeferredPalette(t.Context(), store, "other-session", palette, nil)
	require.NoError(t, err)
	require.Equal(t, []string{"tool_search", "view"}, toolNameList(loaded))

	// Without tool_search there is no way to load deferred tools, so the
	// whole authorized palette is sent.
	loaded, deferred, err = SplitDeferredPalette(t.Context(), store, "session", []fantasy.AgentTool{optional, view}, nil)
	require.NoError(t, err)
	require.Len(t, loaded, 2)
	require.Empty(t, deferred)
}

func TestDeferredPalettePinsStateTools(t *testing.T) {
	t.Parallel()
	store := engineering.NewStore(t.TempDir())
	palette := []fantasy.AgentTool{NewToolSearchTool(ToolSearchOptions{}), fixtureTool("todos", "tasks", nil), fixtureTool("workflow", "recover", nil)}
	loaded, deferred, err := SplitDeferredPalette(t.Context(), store, "s", palette, []string{"todos", "absent"})
	require.NoError(t, err)
	require.Equal(t, []string{"tool_search", "todos"}, toolNameList(loaded))
	require.Equal(t, []string{"workflow"}, toolNameList(deferred))
}

func TestCorruptDiscoveryRecordFallsBackToCorePalette(t *testing.T) {
	t.Parallel()
	store := engineering.NewStore(t.TempDir())
	_, err := store.PutRecordStrict(t.Context(), discoveryNamespace("s"), "enabled", 0, []byte(`{"tools":`))
	require.NoError(t, err)
	palette := []fantasy.AgentTool{NewToolSearchTool(ToolSearchOptions{}), fixtureTool("browser", "web", nil), fixtureTool("view", "read", nil)}
	loaded, deferred, err := SplitDeferredPalette(t.Context(), store, "s", palette, nil)
	require.NoError(t, err, "a damaged record must not fail the turn")
	require.Equal(t, []string{"tool_search", "view"}, toolNameList(loaded), "a damaged record must not open every tool")
	require.Equal(t, []string{"browser"}, toolNameList(deferred))

	// The next discovery replaces the damaged record.
	require.NoError(t, DiscoverTools(t.Context(), store, "s", []string{"browser"}))
	discovered, err := DiscoveredToolNames(t.Context(), store, "s")
	require.NoError(t, err)
	require.Equal(t, []string{"browser"}, discovered)
}

func TestLegacyDiscoveryRecordIsRead(t *testing.T) {
	t.Parallel()
	store := engineering.NewStore(t.TempDir())
	_, err := store.PutRecordStrict(t.Context(), discoveryNamespace("s"), "enabled", 0, []byte(`["browser","../escape","fetch"]`))
	require.NoError(t, err)
	discovered, err := DiscoveredToolNames(t.Context(), store, "s")
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"browser", "fetch"}, discovered, "invalid names are dropped")
}

func TestDiscoveriesAgeOutAndStayBounded(t *testing.T) {
	t.Parallel()
	store := engineering.NewStore(t.TempDir())
	ctx := t.Context()
	// A session with no discoveries is not written on every turn.
	require.NoError(t, BeginToolDiscoveryTurn(ctx, store, "s"))
	_, _, exists, err := readDiscovery(ctx, store, "s")
	require.NoError(t, err)
	require.False(t, exists)

	require.NoError(t, DiscoverTools(ctx, store, "s", []string{"browser", "fetch"}))
	for range discoveryIdleTurns {
		require.NoError(t, BeginToolDiscoveryTurn(ctx, store, "s"))
		// Using fetch keeps it alive.
		require.NoError(t, DiscoverTools(ctx, store, "s", []string{"fetch"}))
	}
	discovered, err := DiscoveredToolNames(ctx, store, "s")
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"browser", "fetch"}, discovered, "not idle long enough yet")
	require.NoError(t, BeginToolDiscoveryTurn(ctx, store, "s"))
	discovered, err = DiscoveredToolNames(ctx, store, "s")
	require.NoError(t, err)
	require.Equal(t, []string{"fetch"}, discovered)

	var many []string
	for i := range maxDiscoveredTools + 8 {
		many = append(many, fmt.Sprintf("tool_%02d", i))
	}
	require.NoError(t, DiscoverTools(ctx, store, "s", many))
	discovered, err = DiscoveredToolNames(ctx, store, "s")
	require.NoError(t, err)
	require.Len(t, discovered, maxDiscoveredTools)
}

func TestConcurrentDiscoveriesAreNotLost(t *testing.T) {
	t.Parallel()
	store := engineering.NewStore(t.TempDir())
	var wg sync.WaitGroup
	for i := range 3 {
		wg.Go(func() {
			require.NoError(t, DiscoverTools(t.Context(), store, "s", []string{fmt.Sprintf("tool_%d", i)}))
		})
	}
	wg.Wait()
	discovered, err := DiscoveredToolNames(t.Context(), store, "s")
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"tool_0", "tool_1", "tool_2"}, discovered)
}

func TestDeferredToolStubNeverRunsTheTool(t *testing.T) {
	t.Parallel()
	var loads []string
	attempts := 0
	stub := NewDeferredToolStub("browser", func(_ context.Context, name string) (bool, error) {
		attempts++
		loads = append(loads, name)
		return attempts == 1, nil
	})
	require.Empty(t, stub.Info().Required, "guessed arguments must reach the stub, not fail validation")
	response, err := stub.Run(t.Context(), fantasy.ToolCall{Name: "browser", Input: `{"action":"navigate","url":"https://example.com"}`})
	require.NoError(t, err)
	require.True(t, response.IsError)
	require.Contains(t, response.Content, "NOT executed")
	require.Contains(t, response.Content, "next step")
	response, err = stub.Run(t.Context(), fantasy.ToolCall{Name: "browser"})
	require.NoError(t, err)
	require.True(t, response.IsError)
	require.Contains(t, response.Content, "Do not repeat")
	require.Equal(t, []string{"browser", "browser"}, loads)
}

func searchPalette(ran *atomic.Int32) []fantasy.AgentTool {
	return []fantasy.AgentTool{
		fixtureTool("bash", "Run shell commands in a persistent terminal.", ran),
		fixtureTool("browser", "Drive a real Chrome browser: navigate pages, click DOM targets, fill forms and take screenshots.", ran),
		fixtureTool("computer", "Control the Windows desktop: observe windows, click, type and verify GUI applications.", ran),
		fixtureTool("tool_pipeline", "Run several tool calls as one bounded workflow with bindings and checkpoints.", ran),
		fixtureTool("fetch", "Fetch a URL and return its content as text or markdown.", ran),
		fixtureTool("git_diff", "Show git diff for the working tree or between revisions.", ran),
		fixtureTool("test_run", "Run the project's test suite and summarize failures.", ran),
		fixtureTool("memory", "Save and recall durable notes across sessions.", ran),
		fixtureTool("todos", "Create and update the session task list.", ran),
		fixtureTool("lsp_references", "Find references to a symbol through the language server.", ran),
		fixtureTool("view", "Read a file with line numbers.", ran),
		NewToolSearchTool(ToolSearchOptions{}),
	}
}

type searchResponse struct {
	Results []struct {
		Name   string `json:"name"`
		Loaded bool   `json:"loaded"`
	} `json:"results"`
	Total       int      `json:"total_matches"`
	Unavailable []string `json:"unavailable"`
}

func runToolSearch(t *testing.T, palette []fantasy.AgentTool, deferred bool, input string) (searchResponse, []string, string) {
	t.Helper()
	var loaded []string
	tool := NewToolSearchTool(ToolSearchOptions{
		Palette:  func(context.Context) []fantasy.AgentTool { return palette },
		Deferred: func(context.Context) bool { return deferred },
		Load: func(_ context.Context, names []string) error {
			loaded = append(loaded, names...)
			return nil
		},
	})
	response, err := tool.Run(t.Context(), fantasy.ToolCall{Input: input})
	require.NoError(t, err)
	require.False(t, response.IsError, response.Content)
	var parsed searchResponse
	require.NoError(t, json.Unmarshal([]byte(response.Content), &parsed))
	return parsed, loaded, response.Content
}

func TestToolSearchRanksTurkishAndEnglishTaskPhrases(t *testing.T) {
	t.Parallel()
	var ran atomic.Int32
	palette := searchPalette(&ran)
	for _, tc := range []struct{ query, want string }{
		{"browser", "browser"},
		{"open the website in chrome and click login", "browser"},
		{"tarayıcıda sayfayı aç ve ekran görüntüsü al", "browser"},
		{"TARAYICI", "browser"},
		{"masaüstünde Not Defteri penceresini kullan", "computer"},
		{"desktop window keyboard", "computer"},
		{"çok adımlı otomasyon akışı", "tool_pipeline"},
		{"multi step pipeline with checkpoints", "tool_pipeline"},
		{"git diff", "git_diff"},
		{"değişiklikleri git ile karşılaştır", "git_diff"},
		{"testleri çalıştır", "test_run"},
		{"run the tests", "test_run"},
		{"hafızaya kaydet", "memory"},
		{"remember this preference", "memory"},
		{"görev listesi", "todos"},
		{"find references to symbol", "lsp_references"},
		{"web sayfasının içeriğini indir url", "fetch"},
	} {
		t.Run(tc.query, func(t *testing.T) {
			t.Parallel()
			input, _ := json.Marshal(map[string]string{"query": tc.query})
			parsed, loaded, _ := runToolSearch(t, palette, true, string(input))
			require.NotEmpty(t, parsed.Results, tc.query)
			require.Equal(t, tc.want, parsed.Results[0].Name, "%+v", parsed.Results)
			require.True(t, parsed.Results[0].Loaded)
			require.Contains(t, loaded, tc.want)
		})
	}
	require.Zero(t, ran.Load(), "searching must never run a tool")
}

func TestToolSearchIsDeterministicCompactAndBounded(t *testing.T) {
	t.Parallel()
	palette := searchPalette(nil)
	first, _, raw := runToolSearch(t, palette, true, `{"query":"browser desktop","limit":3}`)
	second, _, _ := runToolSearch(t, palette, true, `{"query":"browser desktop","limit":3}`)
	require.Equal(t, first, second)
	require.LessOrEqual(t, len(first.Results), 3)
	require.NotContains(t, raw, `"parameters"`, "results must not embed schemas")
	require.Less(t, len(raw), 1500)

	// Exact names load even when nothing in a query would match.
	parsed, loaded, _ := runToolSearch(t, palette, true, `{"names":["computer","missing_tool"]}`)
	require.Equal(t, "computer", parsed.Results[0].Name)
	require.Equal(t, []string{"computer"}, loaded)
	require.Equal(t, []string{"missing_tool"}, parsed.Unavailable)

	// Weak description-only matches are listed but not loaded.
	parsed, loaded, _ = runToolSearch(t, palette, true, `{"query":"markdown"}`)
	require.Equal(t, "fetch", parsed.Results[0].Name)
	require.False(t, parsed.Results[0].Loaded)
	require.Empty(t, loaded)

	// Without deferral nothing is recorded; every result is callable.
	parsed, loaded, _ = runToolSearch(t, palette, false, `{"query":"browser"}`)
	require.True(t, parsed.Results[0].Loaded)
	require.Empty(t, loaded)

	tool := NewToolSearchTool(ToolSearchOptions{Palette: func(context.Context) []fantasy.AgentTool { return palette }})
	for _, input := range []string{`{}`, `{"query":"the a"}`, `{"limit":21,"query":"x"}`, fmt.Sprintf(`{"query":%q}`, string(make([]byte, 300)))} {
		response, err := tool.Run(t.Context(), fantasy.ToolCall{Input: input})
		require.NoError(t, err)
		require.True(t, response.IsError, input)
	}
}

// A live session searched {"query":"project_map"} and got five tools loaded
// because "project" and "map" matched neighbours too. Naming a tool loads
// that tool alone; its neighbours stay listed as suggestions.
func TestToolSearchExactNameQueryLoadsOnlyThatTool(t *testing.T) {
	t.Parallel()
	var ran atomic.Int32
	palette := append(searchPalette(&ran),
		fixtureTool("project_map", "Inspect a persistent bounded project map with entry points, test files and Go imports.", &ran),
		fixtureTool("contract_diff", "Select focused context or candidate tests from a refreshed bounded project map.", &ran),
		fixtureTool("api_surface", "List what a Go package exposes to its callers in this project.", &ran),
	)

	for _, query := range []string{"project_map", "PROJECT_MAP", " project_map, "} {
		input, _ := json.Marshal(map[string]string{"query": query})
		parsed, loaded, _ := runToolSearch(t, palette, true, string(input))
		require.Equal(t, []string{"project_map"}, loaded, query)
		require.Equal(t, "project_map", parsed.Results[0].Name)
		require.True(t, parsed.Results[0].Loaded)
		for _, other := range parsed.Results[1:] {
			require.NotEqual(t, "project_map", other.Name, "the named tool is listed once")
			require.False(t, other.Loaded, "%s must stay a suggestion", other.Name)
		}
		require.Greater(t, parsed.Total, 1, "related tools are still reported")
	}

	// Several exact names load exactly those tools.
	_, loaded, _ := runToolSearch(t, palette, true, `{"query":"browser computer"}`)
	require.ElementsMatch(t, []string{"browser", "computer"}, loaded)

	// One capability word turns it back into a ranked search.
	_, loaded, _ = runToolSearch(t, palette, true, `{"query":"project map"}`)
	require.Contains(t, loaded, "project_map")
	require.Greater(t, len(loaded), 1)

	// tool_search itself and tools outside the palette are never named.
	_, loaded, _ = runToolSearch(t, palette, true, `{"query":"tool_search"}`)
	require.NotContains(t, loaded, ToolSearchToolName)
	require.Zero(t, ran.Load(), "searching must never run a tool")
}

func TestToolSearchOnlySeesAuthorizedPalette(t *testing.T) {
	t.Parallel()
	authorized := []fantasy.AgentTool{fixtureTool("view", "Read a file", nil)}
	parsed, loaded, _ := runToolSearch(t, authorized, true, `{"query":"browser","names":["browser"]}`)
	require.Empty(t, parsed.Results)
	require.Empty(t, loaded)
	require.Equal(t, []string{"browser"}, parsed.Unavailable)
}

func TestIntentToolsNeedsExplicitCapabilityWords(t *testing.T) {
	t.Parallel()
	require.Empty(t, IntentTools("merhaba"))
	require.Empty(t, IntentTools("explore the codebase and fix the parser"))
	// Browser work also loads the pipeline, in its browser-scoped form, so
	// dependent steps can be grouped from the first step.
	require.Equal(t, []string{"browser", "tool_pipeline"}, IntentTools("Tarayıcıda example.com'u aç"))
	require.Equal(t, []string{"computer", "tool_pipeline"}, IntentTools("Masaüstünde hesap makinesini aç"))
}

// Loading a GUI tool also advertises the pipeline that orders and verifies
// its multi-step work; before, tool_search for "computer" left the pipeline
// deferred and invisible.
func TestDeferredPaletteAdvertisesPipelineWithGUITool(t *testing.T) {
	t.Parallel()
	store := engineering.NewStore(t.TempDir())
	search := NewToolSearchTool(ToolSearchOptions{Palette: func(context.Context) []fantasy.AgentTool { return nil }})
	palette := []fantasy.AgentTool{
		search, fixtureTool("view", "read files", nil),
		fixtureTool(ComputerToolName, "desktop", nil), fixtureTool(BrowserToolName, "web", nil), fixtureTool(ToolPipelineName, "steps", nil),
	}
	loaded, deferred, err := SplitDeferredPalette(t.Context(), store, "s", palette, nil)
	require.NoError(t, err)
	require.NotContains(t, toolNameList(loaded), ToolPipelineName, "no GUI tool loaded yet")
	require.Contains(t, toolNameList(deferred), ToolPipelineName)

	require.NoError(t, DiscoverTools(t.Context(), store, "s", []string{ComputerToolName}))
	loaded, deferred, err = SplitDeferredPalette(t.Context(), store, "s", palette, nil)
	require.NoError(t, err)
	require.Contains(t, toolNameList(loaded), ComputerToolName)
	require.Contains(t, toolNameList(loaded), ToolPipelineName)
	require.Equal(t, []string{BrowserToolName}, toolNameList(deferred))

	// A palette without the pipeline (disabled) is not changed.
	loaded, _, err = SplitDeferredPalette(t.Context(), store, "s", palette[:4], nil)
	require.NoError(t, err)
	require.NotContains(t, toolNameList(loaded), ToolPipelineName)
}

// Desktop intent comes from how the request names an application, not from
// a list of particular applications.
func TestIntentToolsRecognizeAnyNamedDesktopApplication(t *testing.T) {
	t.Parallel()
	for _, prompt := range []string{
		"Paint uygulamasında yeni bir resim oluştur ve kaydet",
		"Ayarlar uygulamasını açıp koyu temayı seç",
		"Open the Weather desktop app and read today's forecast",
		"Fill in the form in the app window and press Save",
	} {
		require.Equal(t, []string{"computer", "tool_pipeline"}, IntentTools(prompt), prompt)
	}
	require.NotContains(t, IntentTools("explain how this application handles code reloads"), "computer")
}
