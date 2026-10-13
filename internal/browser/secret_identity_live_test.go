	"github.com/stretchr/testify/require"
)

func TestSecretFillPinsElementAndHandlesUTF16(t *testing.T) {
	exe := findChrome()
	if exe == "" {
		t.Skip("No installed browser")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		if r.URL.Path == "/signed-in" {
			fmt.Fprint(w, `<title>user@example.test</title><p>Inbox</p><button>Sign out</button>`)
			return
		}
		fmt.Fprint(w, `<input id="pw" type="password"><input id="other">`)
	}))
	defer server.Close()
	driver, err := newChromedpSession(Options{ExecutablePath: exe, Headless: true, UserDataDir: t.TempDir(), OverlayDisabled: true, ActionTimeout: 15 * time.Second})
	require.NoError(t, err)
	defer driver.Close()
	s := driver.(*chromedpSession)
	require.NoError(t, s.Navigate(server.URL))
	bind := func() SecretTarget {
		target, err := s.PasswordTarget(t.Context(), "", "#pw")
		require.NoError(t, err)
		require.True(t, target.Found)
		require.NotEmpty(t, target.NodeToken)
		return SecretTarget{Origin: target.Origin, TabID: target.TabID, Document: target.Document, Selector: target.Selector, NodeToken: target.NodeToken}
	}
	old := bind()
	_, err = s.Eval(`document.querySelector('#pw').replaceWith(document.querySelector('#pw').cloneNode())`)
	require.NoError(t, err)
	res, err := s.FillSecret(t.Context(), old, "Synth-Not-Dispatched")
	require.Error(t, err)
	require.False(t, res.Dispatched)
	length, err := s.Eval(`document.querySelector('#pw').value.length`)
	require.NoError(t, err)
	require.Equal(t, "0", length)
	current := bind()
	require.NotEqual(t, old.NodeToken, current.NodeToken)
	res, err = s.FillSecret(t.Context(), current, "Synth-🙂-𐐀-ş")
	require.NoError(t, err)
	require.Equal(t, SecretFillDispatched, res.Status)
	retained, err := s.callIn(s.currentContext(), mustSecretWorld(t, s), `function(){return globalThis.__atlasSecretTargets.size}`)
	require.NoError(t, err)
	require.Equal(t, float64(0), retained, "used element handles must be released")
	current = bind()
	// A page moving focus cannot redirect the secret into an unrelated field.
	_, err = s.Eval(`document.querySelector('#other').focus();document.querySelector('#pw').addEventListener('focus',()=>document.querySelector('#other').focus())`)
	require.NoError(t, err)
	res, err = s.FillSecret(t.Context(), current, "Synth-Focus-Test")
	require.Error(t, err)
	require.False(t, res.Dispatched)
	length, err = s.Eval(`document.querySelector('#other').value.length`)
	require.NoError(t, err)
	require.Equal(t, "0", length)
	require.NoError(t, s.Navigate(server.URL+"/signed-in"))
	state, err := s.LoginState(t.Context(), "user@example.test")
	require.NoError(t, err)
	require.True(t, state.ExpectedText)
	require.True(t, state.AuthenticatedUI)
}

func mustSecretWorld(t *testing.T, s *chromedpSession) float64 {
	t.Helper()
	ctx, done := s.boundContext(t.Context(), 5*time.Second)
	defer done()
	world, err := s.isolatedWorld(ctx, "")
	require.NoError(t, err)
	return world
}
