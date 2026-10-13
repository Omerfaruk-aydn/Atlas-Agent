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
