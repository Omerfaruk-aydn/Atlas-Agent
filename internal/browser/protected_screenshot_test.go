	data, viewport, _, err := s.ProtectedViewportScreenshot(t.Context(), redact)
	require.NoError(t, err)
	require.NotEmpty(t, data)
	require.NotEmpty(t, viewport.ID, "stable protected capture preserves pointer grants")
	_, err = s.Eval(`globalThis.privacyTicker = setInterval(() => document.querySelector('#safe').textContent = String(performance.now()), 1); true`)
	require.NoError(t, err)
	data, _, err = s.ProtectedScreenshot(t.Context(), false, false, redact)
	require.ErrorContains(t, err, "stale_layout")
	require.Empty(t, data, "unstable layouts must not emit unverified pixels")
	_, _ = s.Eval(`clearInterval(globalThis.privacyTicker); true`)
}

func TestExternalDialogClosureClearsBrowserState(t *testing.T) {
	exe := findChrome()
	if exe == "" {
		t.Skip("No installed browser")
	}
	driver, err := newChromedpSession(Options{ExecutablePath: exe, Headless: true, UserDataDir: t.TempDir(), OverlayDisabled: true, ActionTimeout: 10 * time.Second})
	require.NoError(t, err)
	defer driver.Close()
	s := driver.(*chromedpSession)
	_, err = s.Eval(`setTimeout(() => alert('Synthetic native dialog'), 0); true`)
	require.NoError(t, err)
	require.Eventually(t, func() bool { return len(s.PendingDialogs()) == 1 }, 3*time.Second, 10*time.Millisecond)
	_, err = s.RawCDP("Page.handleJavaScriptDialog", map[string]any{"accept": true})
	require.NoError(t, err)
	require.Eventually(t, func() bool { return len(s.PendingDialogs()) == 0 }, 3*time.Second, 10*time.Millisecond)
}
