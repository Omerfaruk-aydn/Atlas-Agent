package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agent/tools"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/csync"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
)

// fakeBrowserTool answers the browser actions product verification uses.
type fakeBrowserTool struct {
	mu      sync.Mutex
	console string
	log     string
	a11y    string
	denied  bool
	calls   []string
}

func (f *fakeBrowserTool) Info() fantasy.ToolInfo {
	return fantasy.ToolInfo{Name: tools.BrowserToolName}
}

func (f *fakeBrowserTool) ProviderOptions() fantasy.ProviderOptions {
	return fantasy.ProviderOptions{}
}
func (f *fakeBrowserTool) SetProviderOptions(fantasy.ProviderOptions) {}

func (f *fakeBrowserTool) Run(_ context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
	var p tools.BrowserParams
	if err := json.Unmarshal([]byte(call.Input), &p); err != nil {
		return fantasy.ToolResponse{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, p.Action)
	if f.denied {
		return tools.NewPermissionDeniedResponse(nil), nil
	}
	switch p.Action {
	case "tabs":
		return fantasy.NewTextResponse(`{"active_tab":"agent"}`), nil
	case "tab_new":
		return fantasy.NewTextResponse(`{"active_tab":"verify"}`), nil
	case "navigate":
		// Each page load logs the scripted console output again, as an
		// app that errors on every load does.
		f.log += f.console
		return fantasy.NewTextResponse("ok"), nil
	case "console":
		return fantasy.NewTextResponse(verifyOr(f.log, "No console output captured.")), nil
	case "network":
		return fantasy.NewTextResponse("[]"), nil
	case "screenshot":
		return fantasy.NewImageResponse([]byte("png"), "image/png"), nil
	case "eval":
		switch {
		case strings.Contains(p.Script, "getEntriesByType('navigation')"):
			return fantasy.NewTextResponse(`"{\"load\":120}"`), nil
		case strings.Contains(p.Script, "scroll_width"):
			return fantasy.NewTextResponse(`"{\"inner_width\":390,\"scroll_width\":390,\"scrolls\":false,\"wide\":[],\"blank\":false,\"body\":\"\"}"`), nil
		case strings.Contains(p.Script, "image-alt"):
			return fantasy.NewTextResponse(verifyOr(f.a11y, `"[]"`)), nil
		}
		return fantasy.NewTextResponse(`""`), nil
	}
	return fantasy.NewTextResponse("ok"), nil
}

func verifyOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func productAgent(root string, tool fantasy.AgentTool) *sessionAgent {
	return &sessionAgent{
		workingDir:     root,
		tools:          csync.NewSliceFrom([]fantasy.AgentTool{tool}),
		evaluatorModel: csync.NewValue(ptrBox[Model]{}),
		largeModel:     csync.NewValue(Model{}),
	}
}

func localApp(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("<html><body>app</body></html>"))
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestPlanProductVerifyOffAndAuto(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	url := localApp(t)

	plan, err := planProductVerify(context.Background(), nil, root, "update the page at "+url, []string{"a.tsx"}, nil)
	require.NoError(t, err)
	require.Nil(t, plan, "off by default")

	auto := &config.ProductVerify{Mode: "auto"}
	plan, err = planProductVerify(context.Background(), auto, root, "speed up the importer", []string{"importer.go"}, []string{"Local: " + url})
	require.NoError(t, err)
	require.Nil(t, plan, "backend work is not verified in auto mode")

	plan, err = planProductVerify(context.Background(), auto, root, "restyle the header", []string{"header.css"}, []string{"Local: " + url + "/"})
	require.NoError(t, err)
	require.NotNil(t, plan)
	require.Equal(t, url, plan.url)
	require.False(t, plan.unreachable)

	always := &config.ProductVerify{Mode: "always"}
	plan, err = planProductVerify(context.Background(), always, root, "speed up the importer", []string{"importer.go"}, []string{url})
	require.NoError(t, err)
	require.NotNil(t, plan)
}

func TestPlanProductVerifyAddresses(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	always := &config.ProductVerify{Mode: "always", URL: "https://example.com"}
	_, err := planProductVerify(context.Background(), always, root, "x", nil, nil)
	require.Error(t, err, "a configured remote address is refused")

	// Nothing discovered means nothing to verify.
	plan, err := planProductVerify(context.Background(), &config.ProductVerify{Mode: "always"}, root, "x", nil, nil)
	require.NoError(t, err)
	require.Nil(t, plan)

	// A configured address nobody answers on is reported as unreachable.
	plan, err = planProductVerify(context.Background(), &config.ProductVerify{Mode: "always", URL: "http://127.0.0.1:1"}, root, "x", nil, nil)
	require.NoError(t, err)
	require.NotNil(t, plan)
	require.True(t, plan.unreachable)

	// A discovered address nobody answers on is ignored.
	plan, err = planProductVerify(context.Background(), &config.ProductVerify{Mode: "always"}, root, "see http://127.0.0.1:1", nil, nil)
	require.NoError(t, err)
	require.Nil(t, plan)
}

func TestPlanProductVerifyReadsSpecAndPlan(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	url := localApp(t)
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".atlas"), 0o755))
	spec := `{"url":"` + url + `","flows":[{"name":"home","steps":[{"action":"assert","condition":"visible","selector":"body"}]}]}`
	require.NoError(t, os.WriteFile(filepath.Join(root, ".atlas", "verify.json"), []byte(spec), 0o644))
	work := `{"goal":"g","features":[{"id":"F1","title":"Pricing","acceptance":"the /pricing page shows \"Monthly\" and \"Yearly\""}]}`
	require.NoError(t, os.WriteFile(filepath.Join(root, ".atlas", "work.json"), []byte(work), 0o644))

	// A spec with flows makes even a backend-looking turn a verification.
	plan, err := planProductVerify(context.Background(), &config.ProductVerify{Mode: "auto"}, root, "work", []string{"x.go"}, nil)
	require.NoError(t, err)
	require.NotNil(t, plan)
	var names []string
	for _, f := range plan.flows {
		names = append(names, f.Name)
	}
	require.Contains(t, names, "home")
	require.Contains(t, names, "page /pricing loads")
	require.Contains(t, names, `feature F1 shows "Monthly"`)

	require.NoError(t, os.WriteFile(filepath.Join(root, ".atlas", "verify.json"), []byte(`{"url":"https://example.com"}`), 0o644))
	_, err = planProductVerify(context.Background(), &config.ProductVerify{Mode: "auto"}, root, "work", []string{"x.css"}, nil)
	require.Error(t, err, "an unusable spec is reported")
}

func TestRunProductGatePassesCleanApp(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	url := localApp(t)
	browserTool := &fakeBrowserTool{}
	a := productAgent(root, browserTool)
	opts := &config.ProductVerify{Mode: "always", URL: url}

	decision, stop, note := a.runProductGate(context.Background(), "s1", opts, root, "restyle", []string{"a.css"}, nil, 0, 2)
	require.False(t, stop)
	require.Empty(t, decision.Continue)
	require.Contains(t, note, "Product verification passed")
	require.Contains(t, note, ".atlas/verify/")
	latest, err := os.ReadFile(filepath.Join(root, ".atlas", "verify", "latest.md"))
	require.NoError(t, err)
	require.Contains(t, string(latest), "report.md")
	require.Contains(t, browserTool.calls, "navigate")
	require.Equal(t, "tab_select", browserTool.calls[len(browserTool.calls)-1], "the agent's own tab is restored")
}

func TestRunProductGateSendsBackEvidencedFindings(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	url := localApp(t)
	browserTool := &fakeBrowserTool{
		console: "[10:00:00] error: TypeError: cannot read properties of undefined\n",
		a11y:    `"[{\"id\":\"image-alt\",\"impact\":\"critical\",\"help\":\"Images must have alternate text\",\"nodes\":[{\"target\":\"img\",\"html\":\"<img src=hero.png>\"}]}]"`,
	}
	a := productAgent(root, browserTool)
	opts := &config.ProductVerify{Mode: "always", URL: url}

	decision, stop, _ := a.runProductGate(context.Background(), "s1", opts, root, "restyle", []string{"a.css"}, nil, 0, 2)
	require.True(t, stop)
	require.Contains(t, decision.Continue, "image-alt")
	require.Contains(t, decision.Continue, "TypeError")
	require.Contains(t, decision.Continue, "check 1 of 2")
	require.Empty(t, decision.Unresolved)

	// At the round limit the turn ends with the findings reported.
	decision, stop, _ = a.runProductGate(context.Background(), "s1", opts, root, "restyle", []string{"a.css"}, nil, 2, 2)
	require.True(t, stop)
	require.Empty(t, decision.Continue)
	require.Contains(t, decision.Unresolved, "Product verification findings remain")
}

func TestRunProductGateFailsOpenWhenBrowserDenied(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	a := productAgent(root, &fakeBrowserTool{denied: true})
	decision, stop, note := a.runProductGate(context.Background(), "s1", &config.ProductVerify{Mode: "always", URL: localApp(t)}, root, "x", []string{"a.css"}, nil, 0, 2)
	require.False(t, stop)
	require.Empty(t, decision.Continue)
	require.Empty(t, note)
}

func TestRunProductGateWithoutBrowserTool(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	a := &sessionAgent{workingDir: root, tools: csync.NewSliceFrom([]fantasy.AgentTool{})}
	decision, stop, _ := a.runProductGate(context.Background(), "s1", &config.ProductVerify{Mode: "always", URL: localApp(t)}, root, "x", []string{"a.css"}, nil, 0, 2)
	require.False(t, stop)
	require.Empty(t, decision.Continue)
}

func TestRunProductGateUnreachableServerIsSentBack(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	a := productAgent(root, &fakeBrowserTool{})
	opts := &config.ProductVerify{Mode: "always", URL: "http://127.0.0.1:1"}
	decision, stop, _ := a.runProductGate(context.Background(), "s1", opts, root, "x", []string{"a.css"}, nil, 0, 2)
	require.True(t, stop)
	require.Contains(t, decision.Continue, "nothing answers")
	decision, stop, _ = a.runProductGate(context.Background(), "s1", opts, root, "x", []string{"a.css"}, nil, 2, 2)
	require.True(t, stop)
	require.Contains(t, decision.Unresolved, "could not run")
}

func TestCheckProductLedgerBookkeeping(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	a := productAgent(root, &fakeBrowserTool{})
	// Without options the gate is off and never touches the ledger.
	ledger := &evaluatorLedger{root: root, changed: true, lastEdit: 3}
	ledger.mu.Lock()
	decision, stop := a.checkProduct(context.Background(), "s1", ledger)
	ledger.mu.Unlock()
	require.False(t, stop)
	require.Empty(t, decision.Continue)
	require.Zero(t, ledger.productRounds)
}
