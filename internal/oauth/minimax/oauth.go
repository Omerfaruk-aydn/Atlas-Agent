// Package minimax implements the OAuth device-code login the official
// MiniMax CLI (mmx) uses, so Atlas-Agent can run MiniMax models against a
// MiniMax Token Plan (coding plan) subscription instead of an API key.
//
// The flow is RFC 8628 with PKCE:
//
//  1. Request a device code from the MiniMax account server (scope
//     "openid profile coding_plan") and have the user confirm it in a
//     browser.
//  2. Poll the token endpoint until the user approves.
//
// There is no key-minting step: the OAuth access token itself is the
// credential for the Anthropic-compatible endpoint at
// https://api.minimax.io/anthropic (https://api.minimax.cn/anthropic for
// the China region), sent in the x-api-key header like mmx does. The token
// is stored as an oauth.Token so the generic refresh path keeps working
// through RefreshToken.
//
// MiniMax runs two independent regions with separate accounts. The region
// selects the account host; override it with MINIMAX_OAUTH_HOST (used by
// tests and self-hosted gateways).
package minimax

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/oauth"
)

// Region selects the MiniMax account region.
type Region string

const (
	// Global is the international platform (minimax.io).
	Global Region = "global"
	// China is the mainland China platform (minimaxi.com).
	China Region = "cn"
)

const (
	globalAuthHost = "https://account.minimax.io"
	chinaAuthHost  = "https://account.minimaxi.com"

	clientID = "659cf4c1-615c-45f6-a5f6-4bf15eb476e5"
	scope    = "openid profile coding_plan"

	deviceAuthPath = "/oauth2/device/code"
	tokenPath      = "/oauth2/token"

	// deviceGrantType is the RFC 8628 device-code grant identifier.
	deviceGrantType = "urn:ietf:params:oauth:grant-type:device_code"

	httpTimeout        = 30 * time.Second
	maxRefreshAttempts = 3

	// SignInFallbackHint is appended to errors that suggest the browser
	// flow is unavailable for the account.
	SignInFallbackHint = "MiniMax browser sign-in is not available for every account (some return 404); sign in with an API key instead: atlas-agent login minimax-coding"
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

// ErrAccessDenied is returned when the user rejects the authorization.
var ErrAccessDenied = errors.New("minimax authorization denied")

// ParseRegion maps a user-supplied region name to a Region.
func ParseRegion(s string) (Region, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "global", "intl", "international", "io":
		return Global, nil
	case "cn", "china", "com", "minimaxi":
		return China, nil
	}
	return "", fmt.Errorf("unknown MiniMax region %q; use global or cn", s)
}

// AuthHost returns the account host in effect for the region.
func AuthHost(r Region) string {
	if v := strings.TrimSpace(os.Getenv("MINIMAX_OAUTH_HOST")); v != "" {
		return strings.TrimSuffix(v, "/")
	}
	if r == China {
		return chinaAuthHost
	}
	return globalAuthHost
}

// DeviceCode is the pending authorization the user must confirm in a
// browser. It also carries the PKCE verifier for the polling step.
type DeviceCode struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval"`

	verifier string
}

// VerificationURL returns the complete verification URL when the server
// provides one, falling back to the bare verification URI.
func (d *DeviceCode) VerificationURL() string {
	if d.VerificationURIComplete != "" {
		return d.VerificationURIComplete
	}
	return d.VerificationURI
}

// newPKCE returns a random verifier (32 bytes, base64url, unpadded) and
// its S256 challenge.
func newPKCE() (verifier, challenge string, err error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", "", err
	}
	verifier = base64.RawURLEncoding.EncodeToString(buf)
	return verifier, challengeFor(verifier), nil
}

func challengeFor(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// RequestDeviceCode starts the device-code flow.
func RequestDeviceCode(ctx context.Context, region Region) (*DeviceCode, error) {
	verifier, challenge, err := newPKCE()
	if err != nil {
		return nil, fmt.Errorf("generate pkce verifier: %w", err)
	}
	form := url.Values{}
	form.Set("client_id", clientID)
	form.Set("scope", scope)
	form.Set("code_challenge", challenge)
	form.Set("code_challenge_method", "S256")

	body, status, err := postForm(ctx, AuthHost(region)+deviceAuthPath, form)
	if err != nil {
		return nil, err
	}
	if status == http.StatusNotFound {
		return nil, fmt.Errorf("minimax device code request failed: status 404. %s", SignInFallbackHint)
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("minimax device code request failed: status %d - %s", status, string(body))
	}
	var dc DeviceCode
	if err := json.Unmarshal(body, &dc); err != nil {
		return nil, fmt.Errorf("decode minimax device code response: %w", err)
	}
	if dc.DeviceCode == "" || dc.UserCode == "" {
		return nil, errors.New("minimax device code response missing device_code or user_code")
	}
	if dc.VerificationURL() == "" {
		return nil, errors.New("minimax device code response missing verification_uri")
	}
	dc.verifier = verifier
	return &dc, nil
}

// PollForToken polls the token endpoint until the user approves the
// device code.
func PollForToken(ctx context.Context, region Region, dc *DeviceCode) (*oauth.Token, error) {
	interval := max(dc.Interval, minPollIntervalSecs, 0)
	expiresIn := dc.ExpiresIn
	if expiresIn <= 0 {
		expiresIn = 900
	}
	deadline := time.Now().Add(time.Duration(expiresIn) * time.Second)
	wait := time.Duration(interval) * time.Second

	for {
		token, err := tryPollToken(ctx, region, dc)
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
			return nil, errors.New("minimax authorization timed out")
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

func tryPollToken(ctx context.Context, region Region, dc *DeviceCode) (*oauth.Token, error) {
	form := url.Values{}
	form.Set("grant_type", deviceGrantType)
	form.Set("device_code", dc.DeviceCode)
	form.Set("client_id", clientID)
	form.Set("code_verifier", dc.verifier)

	body, status, err := postForm(ctx, AuthHost(region)+tokenPath, form)
	if err != nil {
		return nil, err
	}
	var result tokenResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("decode minimax token response (status %d): %w", status, err)
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
		return nil, errors.New("minimax device code expired; run the login again")
	default:
		return nil, fmt.Errorf("minimax authorization failed: %s %s", result.Error, result.ErrorDesc)
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("minimax token request failed: status %d - %s", status, string(body))
	}
	if result.AccessToken == "" {
		return nil, errPending
	}
	return tokenFrom(result, ""), nil
}

// RefreshToken exchanges a refresh token for a new access token. A
// rejected token (4xx or an OAuth error payload) surfaces as
// *oauth.TokenExchangeError so the caller can trigger re-authentication;
// 429 and 5xx responses are retried a few times first.
func RefreshToken(ctx context.Context, region Region, refreshToken string) (*oauth.Token, error) {
	if refreshToken == "" {
		return nil, errors.New("minimax refresh requires a refresh token; sign in again with `atlas-agent login minimax`")
	}
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", refreshToken)
	form.Set("client_id", clientID)

	var (
		body   []byte
		status int
	)
	for attempt := range maxRefreshAttempts {
		var err error
		body, status, err = postForm(ctx, AuthHost(region)+tokenPath, form)
		if err != nil {
			return nil, err
		}
		if status != http.StatusTooManyRequests && status < 500 {
			break
		}
		if attempt == maxRefreshAttempts-1 {
			return nil, fmt.Errorf("minimax refresh failed: status %d - %s", status, string(body))
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
		return nil, fmt.Errorf("decode minimax refresh response: %w", err)
	}
	if result.Error != "" {
		return nil, &oauth.TokenExchangeError{StatusCode: status, Body: string(body)}
	}
	if result.AccessToken == "" {
		return nil, errors.New("minimax refresh response missing access_token")
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

func postForm(ctx context.Context, rawURL string, form url.Values) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rawURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
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
