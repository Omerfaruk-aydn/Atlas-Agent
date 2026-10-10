package remotecontrol

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/mobilerelay"
	"github.com/gorilla/websocket"
)

// Check states.
const (
	CheckUnknown = "unknown"
	CheckOK      = "ok"
	CheckFailed  = "failed"
	CheckPending = "pending"
)

// Check names in a status.
const (
	CheckSSH     = "ssh"
	CheckService = "service"
	CheckHTTPS   = "https"
	CheckLink    = "relay_link"
)

// Check is the result of one verification.
type Check struct {
	State  string    `json:"state"`
	Detail Message   `json:"detail,omitzero"`
	Next   Message   `json:"next,omitzero"`
	At     time.Time `json:"at,omitzero"`
}

// PublicChecker verifies HTTPS and the relay link from this computer.
type PublicChecker interface {
	HTTPS(ctx context.Context, host string) Check
	Link(ctx context.Context, host, token string, gatewayAttached bool) Check
}

// Verifier checks the public side from this computer, with normal
// certificate verification. Tests supply clients that trust a test CA.
type Verifier struct {
	HTTP *http.Client
	WS   *websocket.Dialer
}

func (v Verifier) client() *http.Client {
	if v.HTTP != nil {
		return v.HTTP
	}
	return &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			Proxy:               http.ProxyFromEnvironment,
			TLSClientConfig:     &tls.Config{MinVersion: tls.VersionTLS12},
			TLSHandshakeTimeout: 10 * time.Second,
		},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

// get returns the status code and at most 16 KiB of the body.
func (v Verifier) get(ctx context.Context, url string) (int, []byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, nil, err
	}
	resp, err := v.client().Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<10))
	return resp.StatusCode, body, nil
}

// HTTPS checks that https://host presents a valid certificate and that the
// Atlas relay answers behind it.
func (v Verifier) HTTPS(ctx context.Context, host string) Check {
	now := time.Now()
	status, body, err := v.get(ctx, "https://"+host+"/")
	if err != nil {
		var unknown x509.UnknownAuthorityError
		var invalid x509.CertificateInvalidError
		var hostErr x509.HostnameError
		var verify *tls.CertificateVerificationError
		var netErr net.Error
		switch {
		case errors.As(err, &unknown), errors.As(err, &invalid), errors.As(err, &hostErr), errors.As(err, &verify):
			return Check{State: CheckFailed, At: now, Detail: Msg("The HTTPS certificate for %s is not valid yet.", host), Next: Msg("Caddy obtains it automatically once DNS points to the server and ports 80 and 443 are open; test again in a minute.")}
		case errors.As(err, &netErr) && netErr.Timeout():
			return Check{State: CheckFailed, At: now, Detail: Msg("https://%s did not answer in time.", host), Next: Msg("Check DNS and that TCP 443 is open in every firewall.")}
		}
		return Check{State: CheckFailed, At: now, Detail: Msg("https://%s could not be reached.", host), Next: Msg("Check DNS and that TCP 443 is open in every firewall.")}
	}
	text := string(body)
	switch {
	case status == http.StatusServiceUnavailable && strings.Contains(text, "Atlas is offline"):
		return Check{State: CheckOK, At: now, Detail: Msg("HTTPS works and the relay answers; this computer is not attached.")}
	case status == http.StatusBadGateway && strings.Contains(text, "Atlas disconnected"):
		return Check{State: CheckOK, At: now, Detail: Msg("HTTPS works and the relay answers; this computer just disconnected.")}
	case status == http.StatusOK && strings.Contains(text, "<title>Atlas Remote</title>"):
		return Check{State: CheckOK, At: now, Detail: Msg("HTTPS works and the phone panel answers.")}
	case status == http.StatusMisdirectedRequest:
		return Check{State: CheckFailed, At: now, Detail: Msg("The relay at %s is configured for a different hostname.", host), Next: Msg("Install again to update its configuration.")}
	}
	return Check{State: CheckFailed, At: now, Detail: Msg("https://%s answered with HTTP %d, not the Atlas relay.", host, status), Next: Msg("Check that the hostname points to this server and nothing else serves it.")}
}

// Link checks the whole path phone → HTTPS → relay → this computer. When
// the desktop gateway is attached it asks the real phone API, which must
// refuse an unpaired request; otherwise it briefly attaches a probe
// connector with the connector key and fetches a random value through it.
func (v Verifier) Link(ctx context.Context, host, token string, gatewayAttached bool) Check {
	now := time.Now()
	if gatewayAttached {
		status, body, err := v.get(ctx, "https://"+host+"/api/me")
		if err == nil && status == http.StatusUnauthorized {
			var payload struct {
				Error struct{ Code string } `json:"error"`
			}
			if json.Unmarshal(body, &payload) == nil && payload.Error.Code == "not_paired" {
				return Check{State: CheckOK, At: now, Detail: Msg("Phones reach this computer through the relay.")}
			}
		}
		return Check{State: CheckFailed, At: now, Detail: Msg("The relay is up but requests do not reach this computer."), Next: Msg("Turn remote access off and on again, then test.")}
	}
	var nonce [16]byte
	_, _ = rand.Read(nonce[:])
	want := hex.EncodeToString(nonce[:])
	probe := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/atlas-relay-probe" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		_, _ = io.WriteString(w, want)
	})
	cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	conn, err := mobilerelay.Connect(cctx, mobilerelay.ConnectorOptions{URL: "https://" + host, Token: token, Handler: probe, Dialer: v.WS})
	if err != nil {
		return Check{State: CheckFailed, At: now, Detail: Msg("The relay refused this computer's connector, or another computer is attached."), Next: Msg("Install again to put this computer's key on the server.")}
	}
	defer conn.Close()
	for range 10 {
		status, body, err := v.get(cctx, "https://"+host+"/api/atlas-relay-probe")
		if err == nil && status == http.StatusOK && string(body) == want {
			return Check{State: CheckOK, At: now, Detail: Msg("The relay forwards phone requests to this computer.")}
		}
		select {
		case <-cctx.Done():
		case <-time.After(300 * time.Millisecond):
			continue
		}
		break
	}
	return Check{State: CheckFailed, At: now, Detail: Msg("The relay accepted this computer but did not forward a test request."), Next: Msg("Open Diagnostics and check the relay logs.")}
}
