// Package kimi implements the OAuth device-code login the official Kimi
// Code CLI uses, so Atlas-Agent can run Kimi models against a Kimi Code
// membership instead of a separate API key.
//
// The flow follows RFC 8628:
//
//  1. Request a device code from the Kimi authorization server and have
//     the user confirm it in a browser.
//  2. Poll the token endpoint until the user approves.
//
// Unlike Meta Muse there is no key-minting step: the OAuth access token
// itself is the bearer credential for the OpenAI-compatible endpoint at
// https://api.kimi.com/coding/v1. The token is stored as an
// oauth.Token, so the generic refresh-on-401 path keeps working through
// RefreshToken.
//
// The auth host defaults to https://auth.kimi.com and can be overridden
// with KIMI_CODE_OAUTH_HOST or KIMI_OAUTH_HOST, which the official CLI
// also honors.
package kimi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/oauth"
)

const (
	defaultAuthHost = "https://auth.kimi.com"
	clientID        = "17e5f671-d194-4dfb-9706-5516cb48c098"

	deviceAuthPath = "/api/oauth/device_authorization"
	tokenPath      = "/api/oauth/token"

	// deviceGrantType is the RFC 8628 device-code grant identifier.
	deviceGrantType = "urn:ietf:params:oauth:grant-type:device_code"

	// clientVersion is the X-Msh-Version value sent to Kimi.
	clientVersion = "atlas-agent"

	httpTimeout = 30 * time.Second

	maxRefreshAttempts = 3
)

var (
	errPending  = errors.New("authorization pending")
	errSlowDown = errors.New("slow down")
)

// minPollIntervalSecs floors the server-provided poll interval so a
// missing interval does not turn into a hot loop. Tests lower it.
var minPollIntervalSecs = 1

// retryBackoff is the base delay between refresh retries on 429/5xx.
// Tests lower it.
var retryBackoff = time.Second

// DataDir returns the directory the persistent device id lives in. The
// config package installs it at init time; when unset the user config
// directory is used.
var DataDir func() string

// ErrAccessDenied is returned when the user rejects the authorization.
var ErrAccessDenied = errors.New("kimi authorization denied")

// AuthHost returns the OAuth host in effect.
func AuthHost() string {
	for _, name := range []string{"KIMI_CODE_OAUTH_HOST", "KIMI_OAUTH_HOST"} {
		if v := strings.TrimSpace(os.Getenv(name)); v != "" {
			return strings.TrimSuffix(v, "/")
		}
	}
	return defaultAuthHost
}

// DeviceCode is the pending authorization the user must confirm in a
// browser.
type DeviceCode struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval"`
}

// VerificationURL returns the complete verification URL when the server
// provides one, falling back to the bare verification URI.
func (d *DeviceCode) VerificationURL() string {
	if d.VerificationURIComplete != "" {
		return d.VerificationURIComplete
	}
	return d.VerificationURI
}

// RequestDeviceCode starts the device-code flow.
func RequestDeviceCode(ctx context.Context) (*DeviceCode, error) {
	form := url.Values{}
	form.Set("client_id", clientID)

	body, status, err := postForm(ctx, AuthHost()+deviceAuthPath, form)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("kimi device code request failed: status %d - %s", status, string(body))
	}
	var dc DeviceCode
	if err := json.Unmarshal(body, &dc); err != nil {
		return nil, fmt.Errorf("decode kimi device code response: %w", err)
	}
	if dc.DeviceCode == "" || dc.UserCode == "" {
		return nil, errors.New("kimi device code response missing device_code or user_code")
	}
	if dc.VerificationURL() == "" {
		return nil, errors.New("kimi device code response missing verification_uri")
	}
	return &dc, nil
}

// PollForToken polls the token endpoint until the user approves the
// device code.
func PollForToken(ctx context.Context, dc *DeviceCode) (*oauth.Token, error) {
	interval := max(dc.Interval, minPollIntervalSecs, 0)
	expiresIn := dc.ExpiresIn
	if expiresIn <= 0 {
		expiresIn = 900
	}
	deadline := time.Now().Add(time.Duration(expiresIn) * time.Second)
	wait := time.Duration(interval) * time.Second

	for {
		token, err := tryPollToken(ctx, dc.DeviceCode)
		switch {
		case err == nil:
			return token, nil
		case errors.Is(err, errSlowDown):
			interval += 5
			wait = time.Duration(interval) * time.Second
		case errors.Is(err, errPending):
		default:
			return nil, err
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(wait):
		}
		if time.Now().After(deadline) {
			return nil, errors.New("kimi authorization timed out")
		}
	}
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	Scope        string `json:"scope"`
	TokenType    string `json:"token_type"`
	Error        string `json:"error"`
	ErrorDesc    string `json:"error_description"`
}

func tryPollToken(ctx context.Context, deviceCode string) (*oauth.Token, error) {
	form := url.Values{}
	form.Set("client_id", clientID)
	form.Set("device_code", deviceCode)
	form.Set("grant_type", deviceGrantType)

	body, status, err := postForm(ctx, AuthHost()+tokenPath, form)
	if err != nil {
		return nil, err
	}
	var result tokenResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("decode kimi token response (status %d): %w", status, err)
	}
	// Pending and slow-down arrive as an error payload on a non-2xx
	// status, so the error field is checked before the status.
	switch result.Error {
	case "":
	case "authorization_pending":
		return nil, errPending
	case "slow_down":
		return nil, errSlowDown
	case "access_denied":
		return nil, ErrAccessDenied
	case "expired_token":
		return nil, errors.New("kimi device code expired")
	default:
		return nil, fmt.Errorf("kimi authorization failed: %s", result.Error)
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("kimi token request failed: status %d - %s", status, string(body))
	}
	if result.AccessToken == "" {
		return nil, errPending
	}
	return tokenFrom(result, ""), nil
}

// RefreshToken exchanges a refresh token for a new access token. A
// rejected token (401/403 or an OAuth error payload) surfaces as
// *oauth.TokenExchangeError so the caller can trigger re-authentication;
// 429 and 5xx responses are retried a few times first.
func RefreshToken(ctx context.Context, refreshToken string) (*oauth.Token, error) {
	if refreshToken == "" {
		return nil, errors.New("kimi refresh requires a refresh token; sign in again with `atlas-agent login kimi`")
	}
	form := url.Values{}
	form.Set("client_id", clientID)
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", refreshToken)

	var (
		body   []byte
		status int
	)
	for attempt := range maxRefreshAttempts {
		var err error
		body, status, err = postForm(ctx, AuthHost()+tokenPath, form)
		if err != nil {
			return nil, err
		}
		if status != http.StatusTooManyRequests && status < 500 {
			break
		}
		if attempt == maxRefreshAttempts-1 {
			return nil, fmt.Errorf("kimi refresh failed: status %d - %s", status, string(body))
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(retryBackoff << attempt):
		}
	}

	if status != http.StatusOK {
		return nil, &oauth.TokenExchangeError{StatusCode: status, Body: string(body)}
	}
	var result tokenResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("decode kimi refresh response: %w", err)
	}
	if result.Error != "" {
		return nil, &oauth.TokenExchangeError{StatusCode: status, Body: string(body)}
	}
	if result.AccessToken == "" {
		return nil, errors.New("kimi refresh response missing access_token")
	}
	return tokenFrom(result, refreshToken), nil
}

// tokenFrom packages a token response; fallbackRefresh is kept when the
// server does not rotate the refresh token.
func tokenFrom(r tokenResponse, fallbackRefresh string) *oauth.Token {
	tok := &oauth.Token{
		AccessToken:  r.AccessToken,
		RefreshToken: r.RefreshToken,
		ExpiresIn:    r.ExpiresIn,
	}
	if tok.RefreshToken == "" {
		tok.RefreshToken = fallbackRefresh
	}
	tok.SetExpiresAt()
	return tok
}

// Headers returns the X-Msh-* identification headers the Kimi servers
// expect on OAuth and inference requests.
func Headers() map[string]string {
	host, _ := os.Hostname()
	return map[string]string{
		"X-Msh-Platform":     "kimi_cli",
		"X-Msh-Version":      clientVersion,
		"X-Msh-Device-Name":  asciiHeader(host),
		"X-Msh-Device-Model": asciiHeader(runtime.GOOS + "/" + runtime.GOARCH),
		"X-Msh-Os-Version":   asciiHeader(runtime.GOOS),
		"X-Msh-Device-Id":    DeviceID(),
	}
}

// asciiHeader strips everything outside printable ASCII so the value is
// a valid header.
func asciiHeader(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= 0x20 && r < 0x7f {
			b.WriteRune(r)
		}
	}
	if out := strings.TrimSpace(b.String()); out != "" {
		return out
	}
	return "unknown"
}

// DeviceID returns the persistent random device id, creating it on first
// use. When the id cannot be persisted an ephemeral one is returned.
func DeviceID() string {
	dir := ""
	if DataDir != nil {
		dir = DataDir()
	}
	if dir == "" {
		if base, err := os.UserConfigDir(); err == nil {
			dir = filepath.Join(base, "atlas-agent")
		}
	}
	path := filepath.Join(dir, "kimi_device_id")
	if dir != "" {
		if data, err := os.ReadFile(path); err == nil {
			if id := strings.TrimSpace(string(data)); id != "" {
				return id
			}
		}
	}
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "0000000000000000"
	}
	id := hex.EncodeToString(buf)
	if dir != "" {
		if err := os.MkdirAll(dir, 0o755); err == nil {
			_ = os.WriteFile(path, []byte(id), 0o600)
		}
	}
	return id
}

func postForm(ctx context.Context, rawURL string, form url.Values) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rawURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for k, v := range Headers() {
		req.Header.Set(k, v)
	}
	client := &http.Client{Timeout: httpTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, err
	}
	return body, resp.StatusCode, nil
}
