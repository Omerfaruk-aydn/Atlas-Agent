package support

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/mobilerelay"
	"github.com/gorilla/websocket"
)

// The relay pairs exactly two outbound WebSocket connections that name the
// same slot id and then copies bytes between them. It sees slot ids, which
// identify an offer but not the code's secret part, and opaque end-to-end
// TLS traffic. It never logs slot ids, codes or payloads.
const (
	relayHostPath = "/v1/support/host"
	relayJoinPath = "/v1/support/join"
)

var slotPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)

// RelayConfig is how an endpoint reaches a relay you operate.
type RelayConfig struct {
	URL   string
	Token string
	// Dialer is optional; tests pass one trusting a private CA.
	Dialer *websocket.Dialer
}

// Configured reports whether internet mode is available.
func (c *RelayConfig) Configured() bool { return c != nil && c.URL != "" }

func (c *RelayConfig) endpoint(path, slot string) (string, error) {
	u, err := url.Parse(c.URL)
	if err != nil || u.Host == "" || u.User != nil {
		return "", Errorf(CodeRelayNotConfigured, "support relay URL %q is not a valid https:// URL", c.URL)
	}
	switch u.Scheme {
	case "https":
		u.Scheme = "wss"
	case "http":
		// Plain HTTP is only for a relay on this computer behind a proxy.
		if host := u.Hostname(); host != "localhost" && !net.ParseIP(host).IsLoopback() {
			return "", Errorf(CodeRelayNotConfigured, "support relay must use https:// unless it runs on this computer")
		}
		u.Scheme = "ws"
	default:
		return "", Errorf(CodeRelayNotConfigured, "support relay URL must start with https://")
	}
	u.Path = strings.TrimRight(u.Path, "/") + path
	u.RawQuery = url.Values{"slot": {slot}}.Encode()
	return u.String(), nil
}

func (c *RelayConfig) dial(ctx context.Context, path, slot string) (*websocket.Conn, error) {
	endpoint, err := c.endpoint(path, slot)
	if err != nil {
		return nil, err
	}
	dialer := c.Dialer
	if dialer == nil {
		d := *websocket.DefaultDialer
		d.HandshakeTimeout = 15 * time.Second
		dialer = &d
	}
	header := http.Header{}
	if c.Token != "" {
		header.Set("Authorization", "Bearer "+c.Token)
	}
	ws, resp, err := dialer.DialContext(ctx, endpoint, header)
	if err != nil {
		if resp != nil {
			defer resp.Body.Close()
			var body struct {
				Code string `json:"code"`
			}
			_ = json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&body)
			switch {
			case resp.StatusCode == http.StatusNotFound && body.Code == "no_host":
				return nil, Errorf(CodePairingInvalid, "no computer is offering support with this code (it may be wrong or expired)")
			case resp.StatusCode == http.StatusTooManyRequests:
				return nil, Errorf(CodePairingRateLimited, "the relay is limiting attempts; wait a minute")
			case resp.StatusCode == http.StatusConflict:
				return nil, Errorf(CodeProtocol, "the relay already has an offer with this code")
			case resp.StatusCode == http.StatusUnauthorized:
				return nil, Errorf(CodeRelayNotConfigured, "the relay refused the access token; check support_relay_token_file")
			}
		}
		// Never echo the URL query (the slot) or the token.
		return nil, Errorf(CodeRemoteOffline, "cannot reach the support relay; check its address, certificate and your connection")
	}
	return ws, nil
}

