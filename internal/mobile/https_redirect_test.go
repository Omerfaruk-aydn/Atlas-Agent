package mobile

import (
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPhoneHTTPRedirectUsesSamePortAndKeepsTLSWorking(t *testing.T) {
	t.Parallel()
	g, _ := newGateway(t)
	https := g.URLs()[1]
	client := &http.Client{
		Timeout:       3 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		req, err := http.NewRequestWithContext(t.Context(), method, strings.Replace(https, "https://", "http://", 1)+"/?language=tr", nil)
		require.NoError(t, err)
		resp, err := client.Do(req)
		require.NoError(t, err)
		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		require.NoError(t, resp.Body.Close())
		require.Equal(t, http.StatusTemporaryRedirect, resp.StatusCode)
		require.Equal(t, https+"/?language=tr", resp.Header.Get("Location"))
		require.NotContains(t, resp.Header.Get("Location"), "#", "browsers inherit the original QR fragment")
		require.Empty(t, resp.Header.Values("Set-Cookie"))
		require.Empty(t, body, "plaintext must not serve the application")
	}
	p := newPhone(t, g)
	defer p.client.CloseIdleConnections()
	require.Equal(t, "accepted", p.pair(true))
	resp, _ := p.do(http.MethodGet, "/api/me", nil, nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestPhoneHTTPRejectsOtherHostsAndDoesNotProcessPairing(t *testing.T) {
	t.Parallel()
	g, _ := newGateway(t)
	base := strings.Replace(g.URLs()[1], "https://", "http://", 1)
	client := &http.Client{Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	for _, tc := range []struct {
		method, path, host string
		status             int
	}{
		{method: http.MethodPost, path: "/api/pair", status: http.StatusUpgradeRequired},
		{method: http.MethodPost, path: "/api/sessions/s1/prompt", status: http.StatusUpgradeRequired},
		{method: http.MethodGet, path: "/", host: "evil.example", status: http.StatusMisdirectedRequest},
		{method: http.MethodGet, path: "//evil.example/", host: "evil.example", status: http.StatusMisdirectedRequest},
	} {
		req, err := http.NewRequestWithContext(t.Context(), tc.method, base+tc.path, nil)
		require.NoError(t, err)
		if tc.host != "" {
			req.Host = tc.host
		}
		resp, err := client.Do(req)
		require.NoError(t, err)
		require.NoError(t, resp.Body.Close())
		require.Equal(t, tc.status, resp.StatusCode)
		require.Empty(t, resp.Header.Get("Location"))
		require.Empty(t, resp.Header.Values("Set-Cookie"))
	}
	require.Empty(t, g.Auth.PendingList())
}

func TestPhoneIdleConnectionDoesNotBlockHTTPSAndClosesOnStop(t *testing.T) {
	t.Parallel()
	g, _ := newGateway(t)
	dialer := net.Dialer{Timeout: time.Second}
	idle, err := dialer.DialContext(t.Context(), "tcp", strings.TrimPrefix(g.URLs()[1], "https://"))
	require.NoError(t, err)
	defer idle.Close() //nolint:errcheck
	p := newPhone(t, g)
	p.client.Timeout = 2 * time.Second
	defer p.client.CloseIdleConnections()
	resp, _ := p.do(http.MethodGet, "/api/me", nil, nil)
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	require.NoError(t, g.Close())
	require.NoError(t, idle.SetReadDeadline(time.Now().Add(time.Second)))
	_, err = idle.Read(make([]byte, 1))
	require.Error(t, err)
	var timeout net.Error
	if err, ok := err.(net.Error); ok {
		timeout = err
	}
	if timeout != nil {
		require.False(t, timeout.Timeout(), "shutdown must close idle sniffing connections")
	}
}
