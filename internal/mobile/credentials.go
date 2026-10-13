		return
	}
	code, expires, err := g.Auth.NewTransfer(d.ID)
	if err != nil {
		apiError(w, http.StatusUnauthorized, "not_paired", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"code": code[:4] + "-" + code[4:], "expires": expires.UnixMilli()})
}

// handleTransferRedeem pairs the Home Screen app with a transfer code from
// the same phone's paired browser.
func (g *Gateway) handleTransferRedeem(w http.ResponseWriter, r *http.Request) {
	if !g.sameOrigin(r) {
		apiError(w, http.StatusForbidden, "bad_origin", "request origin does not match this gateway")
		return
	}
	if g.rateLimited("pair:"+clientIP(r), 10) {
		apiError(w, http.StatusTooManyRequests, "rate_limited", "too many pairing attempts")
		return
	}
	var req struct {
		Code string `json:"code"`
	}
	if !strictJSON(w, r, &req) {
		return
	}
	d, session, err := g.Auth.RedeemTransfer(req.Code, r.UserAgent())
	if err != nil {
		apiError(w, http.StatusForbidden, "transfer_invalid", "the code is wrong, expired or already used; create a new one in the paired browser")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: session, Path: "/", HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode, MaxAge: int(SessionAbsolute.Seconds())})
	g.ring.add("device_paired", "", map[string]any{"device": d.Name})
	writeJSON(w, http.StatusOK, map[string]any{"state": "accepted", "csrf": d.CSRF, "device": map[string]string{"id": d.ID, "name": d.Name}})
}

// subscribeCredentials makes this gateway a secure input channel for
// credential cards while it runs.
func (g *Gateway) subscribeCredentials() func() {
	return vault.Ephemeral.Subscribe(func(ev vault.CredentialEvent) {
		kind := "credential"
		if ev.Request.State != vault.CredentialPending {
			kind = "credential_resolved"
		}
		g.ring.add(kind, ev.Request.SessionID, credentialView(ev.Request))
	})
}

func credentialCount(sessionID string) int { return len(vault.Ephemeral.Pending(sessionID)) }

// credentialViews lists open cards of a session, newest last.
func credentialViews(sessionID string) []map[string]any {
	reqs := vault.Ephemeral.Pending(sessionID)
	out := make([]map[string]any, 0, len(reqs))
	for _, r := range reqs {
		out = append(out, credentialView(r))
	}
	return out
}
