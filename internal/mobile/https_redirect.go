package mobile

import (
	"net/http"
	"net/url"
)

// redirectToHTTPS never serves the phone application, consumes pairing
// nonces or processes API requests over plaintext. Browsers inherit the
// original pairing fragment when Location has no fragment (RFC 9110).
func (g *Gateway) redirectToHTTPS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Connection", "close")
	if !g.allowedHost(r.Host) {
		http.Error(w, "unknown host", http.StatusMisdirectedRequest)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "Use HTTPS for phone control", http.StatusUpgradeRequired)
		return
	}
	target := url.URL{Scheme: "https", Host: r.Host, Path: r.URL.Path, RawPath: r.URL.RawPath, RawQuery: r.URL.RawQuery}
	w.Header().Set("Location", target.String())
	w.WriteHeader(http.StatusTemporaryRedirect)
}
