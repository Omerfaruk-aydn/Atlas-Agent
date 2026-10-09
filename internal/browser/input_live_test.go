package browser

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
	"github.com/stretchr/testify/require"
)

func TestTypedInputRealBrowserDragAndCanceledRelease(t *testing.T) {
	if testing.Short() {
		t.Skip("Real browser excluded in short mode")
	}
	exe := findChrome()
	if exe == "" {
		t.Skip("No installed browser")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<!doctype html><style>html,body{margin:0;width:100%;height:100%;overflow:hidden}</style><textarea id="editor"></textarea><script>
window.trace=[];
for(const kind of ['mousedown','mouseup'])document.addEventListener(kind,e=>{
 trace.push({kind,buttons:e.buttons,x:e.clientX,y:e.clientY});
 if(kind==='mousedown')console.log('atlas-test-pressed');
});</script>`)
	}))
	defer server.Close()
	sess, err := newChromedpSession(Options{ExecutablePath: exe, Headless: true, UserDataDir: t.TempDir(), OverlayDisabled: true, ActionTimeout: 15 * time.Second})
	require.NoError(t, err)
	defer sess.Close()
	s := sess.(*chromedpSession)
	require.NoError(t, s.Navigate(server.URL))
	_, vp, err := s.ViewportScreenshot(t.Context())
	require.NoError(t, err)
	require.NotEmpty(t, vp.ID)
	request := Request{Action: InputPointerDrag, ViewportID: vp.ID, X: 200 * vp.Scale, Y: 100 * vp.Scale, ToX: 260 * vp.Scale, ToY: 160 * vp.Scale, Steps: 3}
	result, err := s.Input(t.Context(), request)
	require.NoError(t, err)
	require.Equal(t, 6, result.EventsSent)
	trace, err := s.Eval("window.trace")
	require.NoError(t, err)
	require.Contains(t, trace, "mouseup")
	require.Contains(t, trace, `"x":260`)

	_, vp, err = s.ViewportScreenshot(t.Context())
	require.NoError(t, err)
	caller, cancel := context.WithCancel(t.Context())
	defer cancel()
	chromedp.ListenTarget(s.currentContext(), func(ev any) {
		if log, ok := ev.(*runtime.EventConsoleAPICalled); ok && len(log.Args) == 1 && string(log.Args[0].Value) == `"atlas-test-pressed"` {
			cancel()
		}
	})
	request.ViewportID = vp.ID
	request.HoldMS = 300
	result, err = s.Input(caller, request)
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, "released", result.CleanupStatus, "actual Chrome must acknowledge cleanup after caller cancellation")
	require.Nil(t, s.inputRecovery)
	trace, err = s.Eval("window.trace.filter(e=>e.kind==='mouseup').length")
	require.NoError(t, err)
	require.Equal(t, "2", trace, "the canceled gesture must also deliver mouseup to the page")
}
