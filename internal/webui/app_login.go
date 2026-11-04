package webui

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/accountlogin"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/oauth"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/proto"
)

// loginTimeout bounds one sign-in. It matches the terminal's longest wait:
// Google's project setup may take minutes after the browser step.
const loginTimeout = 10 * time.Minute

type loginState string

const (
	loginWaiting loginState = "waiting"
	loginDone    loginState = "done"
	loginFailed  loginState = "failed"
)

// loginView is what the app shows while a sign-in runs. It never carries the
// token.
type loginView struct {
	State    loginState `json:"state"`
	URL      string     `json:"url,omitempty"`
	UserCode string     `json:"user_code,omitempty"`
	Message  string     `json:"message,omitempty"`
	Error    string     `json:"error,omitempty"`
}

type accountLogin struct {
	view   loginView
	cancel context.CancelFunc
}

// accountLogins are the sign-ins in progress, one per provider. They run on
// the gateway, because the browser step finishes long after the request that
// started it.
type accountLogins struct {
	mu    sync.Mutex
	flows map[string]*accountLogin
	// start and supported stand in for the accountlogin package in tests.
	start     func(ctx context.Context, provider string) (*accountlogin.Session, error)
	supported func(provider string) bool
}

func (l *accountLogins) supports(provider string) bool {
	if l.supported != nil {
		return l.supported(provider)
	}
	return accountlogin.Supported(provider)
}

func (l *accountLogins) view(provider string) (loginView, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	flow, ok := l.flows[provider]
	if !ok {
		return loginView{}, false
	}
	return flow.view, true
}

// update changes a flow's view, unless a newer sign-in has replaced it.
func (l *accountLogins) update(provider string, flow *accountLogin, change func(*loginView)) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.flows[provider] == flow {
		change(&flow.view)
	}
}

// replace makes flow the provider's sign-in and stops any earlier one, which
// also frees the local callback port most flows share.
func (l *accountLogins) replace(provider string, flow *accountLogin) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.flows == nil {
		l.flows = map[string]*accountLogin{}
	}
	if old := l.flows[provider]; old != nil {
		old.cancel()
	}
	if flow == nil {
		delete(l.flows, provider)
		return
	}
	l.flows[provider] = flow
}

func (g *Gateway) handleLoginStart(w http.ResponseWriter, r *http.Request, h *hub) {
	provider := r.PathValue("provider")
	if !g.logins.supports(provider) {
		writeError(w, http.StatusBadRequest, "this provider uses an API key")
		return
	}
	// The sign-in outlives this request, so it hangs off the gateway instead.
	ctx, cancel := context.WithTimeout(g.ctx, loginTimeout)
	flow := &accountLogin{cancel: cancel}
	g.logins.replace(provider, flow)

	start := g.logins.start
	if start == nil {
		start = accountlogin.Start
	}
	session, err := start(ctx, provider)
	if err != nil {
		cancel()
		g.logins.update(provider, flow, func(v *loginView) { *v = loginView{State: loginFailed, Error: err.Error()} })
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	view := loginView{State: loginWaiting, URL: session.URL, UserCode: session.UserCode}
	g.logins.update(provider, flow, func(v *loginView) { *v = view })
	go g.finishLogin(ctx, h, provider, flow, session)
	writeJSON(w, http.StatusOK, view)
}

func (g *Gateway) finishLogin(ctx context.Context, h *hub, provider string, flow *accountLogin, session *accountlogin.Session) {
	defer flow.cancel()
	defer session.Close()
	token, err := session.Wait(ctx, func(message string) {
		g.logins.update(provider, flow, func(v *loginView) { v.Message = message })
	})
	if err == nil {
		err = g.storeToken(ctx, h, provider, token)
	}
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return
		}
		if errors.Is(err, context.DeadlineExceeded) {
			err = errors.New("the sign-in timed out")
		}
		slog.Warn("Account sign-in failed", "provider", provider, "error", err)
		g.logins.update(provider, flow, func(v *loginView) { *v = loginView{State: loginFailed, Error: err.Error()} })
		return
	}
	g.logins.update(provider, flow, func(v *loginView) { *v = loginView{State: loginDone} })
}

func (g *Gateway) storeToken(ctx context.Context, h *hub, provider string, token *oauth.Token) error {
	raw, err := json.Marshal(token)
	if err != nil {
		return err
	}
	return g.storeCredential(ctx, h, proto.ConfigProviderKeyRequest{Scope: config.ScopeGlobal, ProviderID: provider, Kind: proto.APIKeyKindOAuth, APIKey: raw})
}

func (g *Gateway) handleLoginStatus(w http.ResponseWriter, r *http.Request, _ *hub) {
	view, ok := g.logins.view(r.PathValue("provider"))
	if !ok {
		writeError(w, http.StatusNotFound, "no sign-in is running for this provider")
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (g *Gateway) handleLoginCancel(w http.ResponseWriter, r *http.Request, _ *hub) {
	g.logins.replace(r.PathValue("provider"), nil)
	w.WriteHeader(http.StatusNoContent)
}
