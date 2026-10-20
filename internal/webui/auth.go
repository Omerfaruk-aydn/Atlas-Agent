package webui

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"
)

// Browser credentials. A launch code is a one-time, short-lived secret
// printed by `atlas-agent web`; the browser trades it for an HttpOnly
// session cookie and the code stops working. Nothing persistent ever
// travels in a URL, and nothing is stored in the browser's script-visible
// storage: the CSRF token lives in page memory and is fetched again after
// a reload.
const (
	launchCodeTTL      = 10 * time.Minute
	sessionIdleTimeout = 12 * time.Hour
	sessionLifetime    = 7 * 24 * time.Hour
	csrfHeader         = "X-Atlas-CSRF"
)

type browserSession struct {
	csrf     string
	created  time.Time
	lastSeen time.Time
}

type auth struct {
	mu       sync.Mutex
	codes    map[[32]byte]time.Time
	sessions map[[32]byte]*browserSession
	now      func() time.Time
}

func newAuth() *auth {
	return &auth{
		codes:    make(map[[32]byte]time.Time),
		sessions: make(map[[32]byte]*browserSession),
		now:      time.Now,
	}
}

// randomToken returns n random bytes, URL-safe base64 encoded.
func randomToken(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic("webui: crypto/rand failed: " + err.Error())
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

func digest(s string) [32]byte { return sha256.Sum256([]byte(s)) }

// newLaunchCode mints a single-use code valid for launchCodeTTL.
func (a *auth) newLaunchCode() string {
	code := randomToken(24)
	a.mu.Lock()
	defer a.mu.Unlock()
	now := a.now()
	for k, exp := range a.codes {
		if now.After(exp) {
			delete(a.codes, k)
		}
	}
	a.codes[digest(code)] = now.Add(launchCodeTTL)
	return code
}

// redeem consumes a launch code and returns a new session token, or ""
// when the code is unknown, used or expired.
func (a *auth) redeem(code string) string {
	if code == "" {
		return ""
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	key := digest(code)
	exp, ok := a.codes[key]
	delete(a.codes, key)
	now := a.now()
	if !ok || now.After(exp) {
		return ""
	}
	token := randomToken(32)
	a.sessions[digest(token)] = &browserSession{csrf: randomToken(24), created: now, lastSeen: now}
	return token
}

// session returns the live session for token and refreshes its idle
// timer, or nil.
func (a *auth) session(token string) *browserSession {
	if token == "" {
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	key := digest(token)
	s, ok := a.sessions[key]
	if !ok {
		return nil
	}
	now := a.now()
	if now.Sub(s.lastSeen) > sessionIdleTimeout || now.Sub(s.created) > sessionLifetime {
		delete(a.sessions, key)
		return nil
	}
	s.lastSeen = now
	return s
}

func (a *auth) revoke(token string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.sessions, digest(token))
}

// constantTimeEqual compares two secrets without leaking their length
// through timing.
func constantTimeEqual(a, b string) bool {
	ha, hb := digest(a), digest(b)
	return subtle.ConstantTimeCompare(ha[:], hb[:]) == 1
}

// hostAllowed defends against DNS rebinding: a request must name the
// loopback address the gateway listens on, never an arbitrary hostname
// that merely resolves to it.
func (g *Gateway) hostAllowed(host string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return slices.Contains(g.hosts, strings.ToLower(host))
}

// originAllowed requires a browser-supplied Origin to be the gateway
// itself, or a development origin the operator named explicitly.
func (g *Gateway) originAllowed(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return false
	}
	if strings.EqualFold(origin, "http://"+r.Host) {
		return true
	}
	return slices.Contains(g.opts.DevOrigins, origin)
}

func (g *Gateway) cookieName() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	// Cookies are not isolated by port, so the name carries it: a second
	// gateway on another port must not overwrite this one's session.
	return "atlas_web_" + g.port
}

// browserSessionFor returns the session the request's cookie names.
func (g *Gateway) browserSessionFor(r *http.Request) (*browserSession, string) {
	c, err := r.Cookie(g.cookieName())
	if err != nil {
		return nil, ""
	}
	return g.auth.session(c.Value), c.Value
}

// requireSession wraps an API handler: it demands a valid session and,
// for state-changing methods, a same-origin request carrying the
// session's CSRF token. A request that names an Origin is checked even
// when it only reads.
func (g *Gateway) requireSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s, _ := g.browserSessionFor(r)
		if s == nil {
			writeError(w, http.StatusUnauthorized, "not signed in: open the link printed by `atlas-agent web`")
			return
		}
		mutating := r.Method != http.MethodGet && r.Method != http.MethodHead
		if (mutating || r.Header.Get("Origin") != "") && !g.originAllowed(r) {
			writeError(w, http.StatusForbidden, "cross-origin request refused")
			return
		}
		if mutating && !constantTimeEqual(r.Header.Get(csrfHeader), s.csrf) {
			writeError(w, http.StatusForbidden, "missing or wrong CSRF token; reload the page")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// handleLaunch trades a launch code for a session cookie and sends the
// browser on to the app, so the code leaves the address bar at once.
func (g *Gateway) handleLaunch(w http.ResponseWriter, r *http.Request) {
	token := g.auth.redeem(r.URL.Query().Get("code"))
	if token == "" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(expiredLinkPage))
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     g.cookieName(),
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   int(sessionLifetime.Seconds()),
	})
	target := "/"
	if next := r.URL.Query().Get("next"); strings.HasPrefix(next, "/") && !strings.HasPrefix(next, "//") && !strings.Contains(next, `\`) {
		target = next
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

// handleSignOut ends the browser session.
func (g *Gateway) handleSignOut(w http.ResponseWriter, r *http.Request) {
	_, token := g.browserSessionFor(r)
	g.auth.revoke(token)
	http.SetCookie(w, &http.Cookie{Name: g.cookieName(), Value: "", Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: -1})
	w.WriteHeader(http.StatusNoContent)
}

const expiredLinkPage = `<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Atlas</title>
<style>body{font:15px/1.5 system-ui,sans-serif;max-width:32rem;margin:15vh auto;padding:0 1rem;color:#1f2328;background:#fff}@media(prefers-color-scheme:dark){body{color:#e6e6e6;background:#151515}}code{font:13px ui-monospace,monospace}</style></head>
<body><h1>This link has expired</h1><p>Sign-in links work once and expire after 10 minutes. Run <code>atlas-agent web</code> again to get a new one.</p></body></html>`
