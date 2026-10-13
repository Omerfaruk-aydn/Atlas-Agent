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
