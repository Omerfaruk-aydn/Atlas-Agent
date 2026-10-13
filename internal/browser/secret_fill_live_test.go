package browser

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// The login fixture is synthetic: a two-step form like many identity
// providers (identifier, then password in the same document), a wrong
// password error, and a one-time-code step after the right password.
const loginFixture = `<!doctype html><title>Sign in</title>
<form id="f" onsubmit="return submitLogin(event)">
 <div id="step1"><label>Email <input id="email" type="email" autocomplete="username"></label><button id="next" type="button" onclick="next1()">Next</button></div>
 <div id="step2" hidden><p id="who"></p><label>Password <input id="pw" type="password" autocomplete="current-password"></label><p id="err" role="alert" hidden>Wrong password. Try again.</p><button id="go">Sign in</button></div>
 <div id="step3" hidden><p>2-Step Verification</p><label>Code <input id="otp" autocomplete="one-time-code" inputmode="numeric" maxlength="6"></label></div>
</form>
<script>
window.inputEvents=0;
document.addEventListener('input',e=>{if(e.target.id==='pw')window.inputEvents++});
function next1(){document.getElementById('step1').hidden=true;document.getElementById('step2').hidden=false;document.getElementById('who').textContent=document.getElementById('email').value;history.pushState({},'', '/challenge/pwd')}
function submitLogin(e){e.preventDefault();const pw=document.getElementById('pw');
 if(pw.value==='Synth-Right-Pass-1'){document.getElementById('step2').hidden=true;document.getElementById('step3').hidden=false}
 else{pw.value='';pw.setAttribute('aria-invalid','true');document.getElementById('err').hidden=false}
 return false}
</script>`

func TestSecretFillRealBrowserLoginFlow(t *testing.T) {
	if testing.Short() {
		t.Skip("Real browser excluded in short mode")
	}
	exe := findChrome()
	if exe == "" {
		t.Skip("No installed browser")
	}
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<!doctype html><input id="pw" type="password">`)
	}))
	defer other.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		switch r.URL.Path {
		case "/framed":
			fmt.Fprintf(w, `<!doctype html><p>outer</p><iframe id="same" src="/inner"></iframe><iframe id="cross" src="%s/"></iframe>`, other.URL)
		case "/inner":
			fmt.Fprint(w, `<!doctype html><input id="pw" type="password">`)
		case "/hidden":
			fmt.Fprint(w, `<!doctype html><input id="pw" type="password" style="display:none">`)
		default:
			fmt.Fprint(w, loginFixture)
		}
	}))
	defer server.Close()
	origin := server.URL

	sess, err := newChromedpSession(Options{ExecutablePath: exe, Headless: true, UserDataDir: t.TempDir(), OverlayDisabled: true, ActionTimeout: 15 * time.Second})
	require.NoError(t, err)
	defer sess.Close()
	s := sess.(*chromedpSession)
	require.NoError(t, s.Navigate(origin+"/"))

	// Before the password step there is no visible password field.
	target, err := s.PasswordTarget(t.Context(), "", "")
	require.NoError(t, err)
	require.False(t, target.Found)
	require.NoError(t, s.Type("#email", "user@example.test"))
	require.NoError(t, s.Click("#next"))

	// The identifier step changed the form inside the same document; that
	// transition is not a failure and the password field is now the target.
	target, err = s.PasswordTarget(t.Context(), "", "")
	require.NoError(t, err)
	require.True(t, target.Found, target.Reason)
	require.Equal(t, "#pw", target.Selector)
	require.Equal(t, origin, target.Origin)
	bound := SecretTarget{Origin: origin, TabID: target.TabID, Document: target.Document, Selector: target.Selector, NodeToken: target.NodeToken}

	state, err := s.LoginState(t.Context(), "")
	require.NoError(t, err)
	require.True(t, state.PasswordVisible)
	require.False(t, state.PasswordFilled)

	// Wrong password: dispatched once, then the page reports failure.
	res, err := s.FillSecret(t.Context(), bound, "Synth-Wrong-Pass")
	require.NoError(t, err)
	require.Equal(t, SecretFillDispatched, res.Status)
	length, err := s.Eval("document.getElementById('pw').value.length")
	require.NoError(t, err)
	require.Equal(t, "16", length)
	events, err := s.Eval("window.inputEvents")
	require.NoError(t, err)
	require.NotEqual(t, "0", events, "the page saw real input events")
	require.NoError(t, s.Click("#go"))
	state = waitLoginState(t, s, func(st LoginState) bool { return st.AlertVisible })
	require.True(t, state.PasswordVisible)
	require.True(t, state.PasswordInvalid || state.AlertVisible, "%+v", state)
	require.False(t, state.OneTimeCode)

	// Right password: the next screen asks for a one-time code, which is
	// a user verification step, not a completed sign-in.
	target, err = s.PasswordTarget(t.Context(), "", "#pw")
	require.NoError(t, err)
	bound.NodeToken = target.NodeToken
	res, err = s.FillSecret(t.Context(), bound, "Synth-Right-Pass-1")
	require.NoError(t, err)
	require.Equal(t, SecretFillDispatched, res.Status)
	require.NoError(t, s.Click("#go"))
	state = waitLoginState(t, s, func(st LoginState) bool { return st.OneTimeCode })
	require.True(t, state.OneTimeCode, "%+v", state)
	require.False(t, state.PasswordVisible)

	// A new document (navigation) gets zero input.
	require.NoError(t, s.Navigate(origin+"/"))
	require.NoError(t, s.Type("#email", "user@example.test"))
	require.NoError(t, s.Click("#next"))
	res, err = s.FillSecret(t.Context(), bound, "Synth-Should-Not-Arrive")
	require.Error(t, err)
	require.False(t, res.Dispatched)
	require.Equal(t, SecretTargetStale, res.Status)
	length, err = s.Eval("document.getElementById('pw').value.length")
	require.NoError(t, err)
	require.Equal(t, "0", length)

	// A wrong origin gets zero input, even with the right document.
	target, err = s.PasswordTarget(t.Context(), "", "")
	require.NoError(t, err)
	wrong := SecretTarget{Origin: "https://login.example.test", TabID: target.TabID, Document: target.Document, Selector: "#pw"}
	res, err = s.FillSecret(t.Context(), wrong, "Synth-Should-Not-Arrive")
	require.Error(t, err)
	require.False(t, res.Dispatched)
	length, err = s.Eval("document.getElementById('pw').value.length")
	require.NoError(t, err)
	require.Equal(t, "0", length)

	// Hidden fields are never targets.
	require.NoError(t, s.Navigate(origin+"/hidden"))
	target, err = s.PasswordTarget(t.Context(), "", "#pw")
	require.NoError(t, err)
	require.False(t, target.Found)
	id := s.ExecutionIdentity(t.Context())
	res, err = s.FillSecret(t.Context(), SecretTarget{Origin: origin, TabID: id.TabID, Document: id.Document, Selector: "#pw"}, "Synth-Should-Not-Arrive")
	require.Error(t, err)
	require.False(t, res.Dispatched)

	// Frames: a same-origin frame is fillable by its frame ID; a
	// cross-origin frame refuses the authorized origin.
	require.NoError(t, s.Navigate(origin+"/framed"))
	time.Sleep(300 * time.Millisecond)
	tree, err := s.command(s.currentContext(), "Page.getFrameTree", map[string]any{}, false)
	require.NoError(t, err)
	var sameID, crossID string
	root, _ := tree["frameTree"].(map[string]any)
	children, _ := root["childFrames"].([]any)
	for _, c := range children {
		f := c.(map[string]any)["frame"].(map[string]any)
		if strings.HasPrefix(f["url"].(string), origin) {
			sameID = f["id"].(string)
		} else {
			crossID = f["id"].(string)
		}
	}
	require.NotEmpty(t, sameID)
	require.NotEmpty(t, crossID)
	id = s.ExecutionIdentity(t.Context())
	res, err = s.FillSecret(t.Context(), SecretTarget{Origin: origin, TabID: id.TabID, Document: id.Document, FrameID: crossID, Selector: "#pw"}, "Synth-Should-Not-Arrive")
	require.Error(t, err)
	require.False(t, res.Dispatched)
	require.Equal(t, SecretOriginMismatch, res.Status)
	target, err = s.PasswordTarget(t.Context(), sameID, "")
	require.NoError(t, err)
	require.True(t, target.Found)
	res, err = s.FillSecret(t.Context(), SecretTarget{Origin: origin, TabID: id.TabID, Document: id.Document, FrameID: sameID, Selector: target.Selector, NodeToken: target.NodeToken}, "Synth-Frame-Pass")
	require.NoError(t, err)
	require.Equal(t, SecretFillDispatched, res.Status)
}

func waitLoginState(t *testing.T, s *chromedpSession, done func(LoginState) bool) LoginState {
	t.Helper()
	var last LoginState
	require.Eventually(t, func() bool {
		st, err := s.LoginState(t.Context(), "")
		if err != nil {
			return false
		}
		last = st
		return done(st)
	}, 5*time.Second, 50*time.Millisecond, "last state %+v", last)
	return last
}
