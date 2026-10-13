package tools

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/stretchr/testify/require"
)

func runDesignTest(t *testing.T, tool fantasy.AgentTool, ctx context.Context, id string, p DesignSystemParams) fantasy.ToolResponse {
	t.Helper()
	data, err := json.Marshal(p)
	require.NoError(t, err)
	r, err := tool.Run(ctx, fantasy.ToolCall{Name: DesignSystemToolName, ID: id, Input: string(data)})
	require.NoError(t, err)
	return r
}

func validTestDesign() DesignSystem {
	s := mobileSystem()
	s.Colors["muted"] = "#A0A4AE"
	s.Colors["on_accent"] = "#0F1115"
	s.Viewports = []DesignViewport{{Name: "phone", Width: 390, Height: 844}}
	return s
}

func TestDesignApprovalRequiresActualUserAndCurrentCandidate(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	tool := NewDesignSystemTool(root)
	sys := validTestDesign()
	require.False(t, runDesignTest(t, tool, t.Context(), "set", DesignSystemParams{Action: "set", System: &sys}).IsError)
	for _, ctx := range []context.Context{t.Context(), WithUserInstruction(t.Context(), "tasarımı incele")} {
		r := runDesignTest(t, tool, ctx, "approve-false", DesignSystemParams{Action: "approve", ExpectedVersion: 1, Note: "User approved"})
		require.True(t, r.IsError)
		require.Contains(t, r.Content, "design_approval_required")
	}
	ctx := WithUserInstruction(t.Context(), "Bu tasarımı onaylıyorum")
	r := runDesignTest(t, tool, ctx, "approve-stale", DesignSystemParams{Action: "approve", ExpectedVersion: 2})
	require.True(t, r.IsError)
	r = runDesignTest(t, tool, ctx, "approve-real", DesignSystemParams{Action: "approve", ExpectedVersion: 1})
	require.False(t, r.IsError, r.Content)
	store, err := loadDesignStore(root)
	require.NoError(t, err)
	require.Equal(t, 1, store.Approved.Version)
}

func TestDesignRendererPreservesChildStopTurnAndPromptMetadata(t *testing.T) {
	t.Parallel()
	child := fantasy.NewTextErrorResponse("Waiting for the user's response")
	child.StopTurn = true
	child.Metadata = `{"question":{"id":"question-1"},"waiting_for_user":true,"hook":{"decision":"ask"}}`
	tool := NewDesignSystemTool(t.TempDir(), DesignServices{Invoke: func(context.Context, fantasy.ToolCall) (fantasy.ToolResponse, error) { return child, nil }})
	r := runDesignTest(t, tool, t.Context(), "stopped-preview", DesignSystemParams{Action: "directions", Directions: []DesignSystem{validTestDesign(), func() DesignSystem { s := validTestDesign(); s.Direction.Name = "Alternative"; return s }()}, Brief: "A representative permission screen"})
	require.True(t, r.StopTurn)
	require.True(t, r.IsError)
	require.JSONEq(t, child.Metadata, r.Metadata)
}

func TestDesignPackageRejectsSecretsAndUnboundEvidence(t *testing.T) {
	t.Parallel()
	_, err := validateDesignURLs([]string{"https://example.com/design?token=private"})
	require.Error(t, err)
	_, err = validateDesignURLs([]string{"https://example.com/design?node-id=1-2"})
	require.NoError(t, err)
	m := designManifest{SystemStatus: "approved", QualityGate: designReport{Passed: true}, RequiredScreens: []string{"screen.png"}, Screens: []deliveredScreen{{Source: "screen.png", SHA256: "screenhash"}}, Sources: []designSource{{Source: "screen.html", SHA256: "sourcehash"}}, Audits: []designAudit{{Passed: true, ScreenshotSHA256: "screenhash", PrototypeSteps: 1, AssertionCount: 1}}}
	require.Equal(t, "partial", designPackageCompletion(m), "unbound source is not proof of the inspected design")
	m.Audits[0].SourceHashes = map[string]string{"screen.html": "sourcehash"}
	require.Equal(t, "complete", designPackageCompletion(m))
	m.Audits[0].SourceHashes["screen.html"] = "different"
	require.Equal(t, "partial", designPackageCompletion(m))
}

func TestDesignMemoryConcurrentRevisionsHaveDistinctVersions(t *testing.T) {
	root := t.TempDir()
	var wg sync.WaitGroup
	versions := make(chan int, 8)
	errors := make(chan error, 8)
	for range 8 {
		wg.Go(func() {
			sys := validTestDesign()
			out, err := runDesignSystem(t.Context(), root, DesignSystemParams{Action: "set", System: &sys}, time.Now)
			if err != nil {
				errors <- err
				return
			}
			versions <- out.(map[string]any)["version"].(int)
		})
	}
	wg.Wait()
	close(versions)
	close(errors)
	for err := range errors {
		require.NoError(t, err)
	}
	seen := map[int]bool{}
	for version := range versions {
		require.False(t, seen[version])
		seen[version] = true
	}
	require.Len(t, seen, 8)
}

func TestDesignAuditCannotBeCertifiedByWritingItsJSON(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	audit := designAudit{ID: "1234567890abcdef12345678", Passed: true, CapturedAt: time.Now().UnixMilli(), ScreenshotSHA256: "screenshot-hash"}
	require.NoError(t, storeDesignRuntime(root, ".atlas/design/audits/"+audit.ID+".json", audit))
	_, err := loadPackageAudits(root, []string{audit.ID}, []deliveredScreen{{SHA256: audit.ScreenshotSHA256}}, nil, time.Now())
	require.ErrorContains(t, err, "not current adapter evidence")
	registerDesignAudit(root, audit)
	_, err = loadPackageAudits(root, []string{audit.ID}, []deliveredScreen{{SHA256: audit.ScreenshotSHA256}}, nil, time.Now())
	require.NoError(t, err)
	audit.SmallTargets = 1
	require.NoError(t, storeDesignRuntime(root, ".atlas/design/audits/"+audit.ID+".json", audit))
	_, err = loadPackageAudits(root, []string{audit.ID}, []deliveredScreen{{SHA256: audit.ScreenshotSHA256}}, nil, time.Now())
	require.ErrorContains(t, err, "not current adapter evidence", "changing an adapter-owned observation invalidates it")
}

func TestDesignCorruptBackupsDoNotOverwriteWithinTheSameSecond(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	file := filepath.Join(root, filepath.FromSlash(designSystemPath))
	require.NoError(t, os.MkdirAll(filepath.Dir(file), 0o700))
	now := func() time.Time { return time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC) }
	sys := validTestDesign()
	var paths []string
	for _, corrupt := range []string{"first broken", "second broken"} {
		require.NoError(t, os.WriteFile(file, []byte(corrupt), 0o600))
		result, err := runDesignSystem(t.Context(), root, DesignSystemParams{Action: "set", System: &sys}, now)
		require.NoError(t, err)
		path := result.(map[string]any)["corrupt_backup"].(string)
		paths = append(paths, path)
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
		require.NoError(t, err)
		require.Equal(t, corrupt, string(data))
	}
	require.NotEqual(t, paths[0], paths[1])
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(paths[0])))
	require.NoError(t, err)
	require.Equal(t, "first broken", string(data))
}

type designEntryContext struct {
	context.Context
	first   sync.Once
	entered chan struct{}
}

func (c *designEntryContext) Err() error {
	err := c.Context.Err()
	c.first.Do(func() { close(c.entered) })
	return err
}

func TestDesignCanceledWhileWaitingForStoreDoesNotWrite(t *testing.T) {
	root := t.TempDir()
	key, err := filepath.EvalSymlinks(root)
	require.NoError(t, err)
	sum := sha256.Sum256([]byte(strings.ToLower(key)))
	mu := &designStoreLocks[int(sum[0])%len(designStoreLocks)]
	mu.Lock()
	parent, cancel := context.WithCancel(t.Context())
	defer cancel()
	ctx := &designEntryContext{Context: parent, entered: make(chan struct{})}
	done := make(chan error, 1)
	go func() {
		sys := validTestDesign()
		_, err := runDesignSystem(ctx, root, DesignSystemParams{Action: "set", System: &sys}, time.Now)
		done <- err
	}()
	<-ctx.entered
	cancel()
	mu.Unlock()
	require.ErrorIs(t, <-done, context.Canceled)
	require.NoFileExists(t, filepath.Join(root, filepath.FromSlash(designSystemPath)))
}

func TestLiveDesignAuditReportsRealLayoutContrastAndTouchDefects(t *testing.T) {
	l := newLiveTool(t)
	page := `<!doctype html><style>body{background:white;color:#bbb;font:16px Arial;margin:0}button{width:20px;height:20px}p{width:700px}</style><p>Unusable layout</p><button></button>`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(page)) }))
	t.Cleanup(server.Close)
	deps := DesignServices{Invoke: func(ctx context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
		return l.tool.Run(ctx, call)
	}, Permissions: &mockPermissionService{}}
	r := runDesignTest(t, NewDesignSystemTool(l.root, deps), l.ctx, "audit-bad-live", DesignSystemParams{Action: "audit", URL: server.URL, Width: 390, Height: 844})
	require.True(t, r.IsError, r.Content)
	var out struct {
		Audit designAudit `json:"audit"`
	}
	require.NoError(t, json.Unmarshal([]byte(r.Content), &out))
	require.Positive(t, out.Audit.Overflow)
	require.Positive(t, out.Audit.SmallTargets)
	require.Positive(t, out.Audit.UnnamedTargets)
	require.Positive(t, out.Audit.LowContrast)
	require.False(t, out.Audit.Passed)
}

func TestLiveDesignDirectionsRenderDistinctActualPreviews(t *testing.T) {
	l := newLiveTool(t)
	cleanCaptures := 0
	deps := DesignServices{Invoke: func(ctx context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
		var p BrowserParams
		require.NoError(t, json.Unmarshal([]byte(call.Input), &p))
		if p.Action == "screenshot" {
			require.NotNil(t, p.Annotate)
			require.False(t, *p.Annotate, "delivery images must omit automation target boxes")
			cleanCaptures++
		}
		return l.tool.Run(ctx, call)
	}, Permissions: &mockPermissionService{}}
	tool := NewDesignSystemTool(l.root, deps)
	dark, light := validTestDesign(), validTestDesign()
	light.Direction.Name = "Clear light"
	light.Colors = map[string]string{"bg": "#FFFFFF", "text": "#202127", "muted": "#595959", "accent": "#6046D8", "on_accent": "#FFFFFF"}
	r := runDesignTest(t, tool, l.ctx, "directions-live", DesignSystemParams{Action: "directions", Directions: []DesignSystem{dark, light}, Brief: "Uzak bilgisayardaki görevi izleyip değişiklikleri onayla.", Width: 390, Height: 844})
	require.False(t, r.IsError, r.Content)
	var result struct {
		Alternatives []renderedDirection `json:"alternatives"`
	}
	require.NoError(t, json.Unmarshal([]byte(r.Content), &result))
	require.Len(t, result.Alternatives, 2)
	require.Equal(t, 2, cleanCaptures)
	require.NotEqual(t, result.Alternatives[0].SHA256, result.Alternatives[1].SHA256)
	for _, a := range result.Alternatives {
		data, err := os.ReadFile(filepath.Join(l.root, filepath.FromSlash(a.Preview)))
		require.NoError(t, err)
		require.Equal(t, a.SHA256, imageDeliveryHash(data))
		d, err := PrepareImageForDelivery(data, "image/png")
		require.NoError(t, err)
		require.Equal(t, 390, d.OriginalWidth)
		require.Equal(t, 844, d.OriginalHeight)
	}
}

func TestLiveDesignAuditPrototypeAndSourceBoundDelivery(t *testing.T) {
	l := newLiveTool(t)
	const page = `<!doctype html><html lang="tr"><meta charset="utf-8"><style>body{margin:0;padding:24px;box-sizing:border-box;background:#fff;color:#111;font:16px Arial}button{width:150px;height:48px;background:#fff;color:#111;border:1px solid #111}#result{display:none}</style><h1>Remote control</h1><button id="continue" onclick="document.getElementById('result').style.display='block'">Devam et</button><p id="result">Görev hazır</p></html>`
	require.NoError(t, os.WriteFile(filepath.Join(l.root, "screen.html"), []byte(page), 0o600))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(page))
	}))
	t.Cleanup(server.Close)
	deps := DesignServices{Invoke: func(ctx context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
		return l.tool.Run(ctx, call)
	}, Permissions: &mockPermissionService{}}
	tool := NewDesignSystemTool(l.root, deps)
	r := runDesignTest(t, tool, l.ctx, "audit-live", DesignSystemParams{Action: "audit", URL: server.URL, Width: 390, Height: 844, SourceFiles: []string{"screen.html"}, PrototypeSteps: []DesignPrototypeStep{{Action: "click", Selector: "#continue"}}, Assertions: []UIAssertion{{Selector: "#result", Text: "Görev hazır", Visible: true}}})
	require.False(t, r.IsError, r.Content)
	var result struct {
		Audit designAudit `json:"audit"`
	}
	require.NoError(t, json.Unmarshal([]byte(r.Content), &result))
	require.True(t, result.Audit.Passed)
	require.Equal(t, 1, result.Audit.PrototypeSteps)
	require.Equal(t, 1, result.Audit.AssertionCount)
	require.NotEmpty(t, result.Audit.SourceHashes["screen.html"])
	sys := validTestDesign()
	require.False(t, runDesignTest(t, tool, l.ctx, "set-audit", DesignSystemParams{Action: "set", System: &sys}).IsError)
	require.False(t, runDesignTest(t, tool, WithUserInstruction(l.ctx, "tasarımı onaylıyorum"), "approve-audit", DesignSystemParams{Action: "approve", ExpectedVersion: 1}).IsError)
	p := DesignSystemParams{Action: "package", Name: "tested-design", Screens: []string{result.Audit.Screenshot}, RequiredScreens: []string{result.Audit.Screenshot}, SourceFiles: []string{"screen.html"}, AuditIDs: []string{result.Audit.ID}}
	r = runDesignTest(t, tool, l.ctx, "package-live", p)
	require.False(t, r.IsError, r.Content)
	require.Contains(t, r.Content, `"completion":"complete"`)
	require.Contains(t, r.Content, `"delivery":"assembled_locally_not_presented"`)
	require.NoError(t, os.WriteFile(filepath.Join(l.root, "screen.html"), []byte(page+"changed"), 0o600))
	p.Name = "changed-source"
	r = runDesignTest(t, tool, l.ctx, "package-changed", p)
	require.True(t, r.IsError)
	require.Contains(t, r.Content, "source changed since audit")
	_, err := loadPackageAudits(l.root, p.AuditIDs, []deliveredScreen{{SHA256: result.Audit.ScreenshotSHA256}}, nil, time.Now().Add(25*time.Hour))
	require.Error(t, err)
}
