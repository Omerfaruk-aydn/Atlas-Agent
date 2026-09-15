// Package muse implements the Meta OIDC device-code login flow the
// official Muse Code CLI uses, so Atlas-Agent can run Muse Spark
// models against a Meta Muse Code subscription instead of a
// separate pay-per-token API key.
//
// The flow has three steps:
//
//  1. Request a device code from the Meta authorization server and
//     have the user confirm it in a browser (RFC 8628).
//  2. Poll the token endpoint until the user approves, yielding an
//     OIDC access token (and usually a refresh token).
//  3. Mint a Model API key for that access token. Inference calls
//     authenticate with the minted key, never with the OIDC token
//     directly.
//
// The minted key is what gets stored as the OAuth access token (the
// same shape GitHub Copilot uses: usable credential in AccessToken,
// long-lived grant in RefreshToken), so the coordinator's generic
// refresh-on-401 path keeps working: RefreshToken replays the
// refresh grant and re-mints, returning a fresh pair.
//
// The endpoint URLs and client id default to Meta's production
// values and can be overridden with MUSE_CODE_AUTH_HOST,
// MUSE_CODE_HOST, and MUSE_CODE_CLIENT_ID. The same variables are
// honored by other third-party clients, which keeps local
// debugging setups interchangeable.
package muse

import (
	"bytes"
	"context"
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

const (
	defaultAuthHost = "https://auth.meta.com"
	defaultAPIHost  = "https://api.meta.ai"
	defaultClientID = "1031625952748946"

	deviceAuthPath  = "/oidc/device/authorization/"
	deviceTokenPath = "/oidc/device/token/"
	mintKeyPath     = "/muse-code/key"

	// deviceGrantType is the RFC 8628 device-code grant identifier
	// the Meta token endpoint expects while polling.
	deviceGrantType = "urn:ietf:params:oauth:grant-type:device_code"

	// mintAPIVersion is the x-api-version header the key-mint
	// endpoint requires.
	mintAPIVersion = "1.0.0"

	userAgent = "atlas-agent-muse"

	httpTimeout = 30 * time.Second
)

var (
	errPending  = errors.New("authorization pending")
	errSlowDown = errors.New("slow down")
)

// minPollIntervalSecs floors the server-provided poll interval so a
// missing or zero interval does not turn into a hot loop. Tests
// lower it; production keeps the RFC-friendly 5 seconds.
var minPollIntervalSecs = 5

// ErrAccessDenied is returned when the user rejects the device-code
// authorization in the browser.
var ErrAccessDenied = errors.New("muse authorization denied")

func authHost() string {
	if v := os.Getenv("MUSE_CODE_AUTH_HOST"); v != "" {
		return strings.TrimSuffix(v, "/")
	}
	return defaultAuthHost
}

func apiHost() string {
	if v := os.Getenv("MUSE_CODE_HOST"); v != "" {
		return strings.TrimSuffix(v, "/")
	}
	return defaultAPIHost
}

func clientID() string {
	if v := os.Getenv("MUSE_CODE_CLIENT_ID"); v != "" {
		return v
	}
	return defaultClientID
}

// DeviceCode is the pending authorization the user must confirm in
// a browser. VerificationURIComplete embeds the user code so the
// login command can open it directly.
type DeviceCode struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval"`
}

// VerificationURL returns the complete verification URL when the
// server provides one, falling back to the bare verification URI.
func (d *DeviceCode) VerificationURL() string {
	if d.VerificationURIComplete != "" {
		return d.VerificationURIComplete
	}
	return d.VerificationURI
}

// RequestDeviceCode starts the device-code flow with Meta's
// authorization server.
func RequestDeviceCode(ctx context.Context) (*DeviceCode, error) {
	form := url.Values{}
	form.Set("client_id", clientID())

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, authHost()+deviceAuthPath, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", userAgent)

	client := &http.Client{Timeout: httpTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("muse device code request failed: %s - %s", resp.Status, string(body))
	}

	var dc DeviceCode
	if err := json.Unmarshal(body, &dc); err != nil {
		return nil, fmt.Errorf("decode muse device code response: %w", err)
	}
	if dc.DeviceCode == "" || dc.UserCode == "" {
		return nil, errors.New("muse device code response missing device_code or user_code")
	}
	if dc.VerificationURI == "" {
		return nil, errors.New("muse device code response missing verification_uri")
	}
	return &dc, nil
}

// PollForToken polls Meta's token endpoint until the user approves
// the device code, then mints a Model API key for the resulting
// access token. The returned token carries the minted key as its
// access token and the OIDC refresh token (when the server issues
// one) as its refresh token.
func PollForToken(ctx context.Context, dc *DeviceCode) (*oauth.Token, error) {
	interval := max(dc.Interval, minPollIntervalSecs, 1)
	expiresIn := dc.ExpiresIn
	if expiresIn <= 0 {
		expiresIn = 600
	}
	deadline := time.Now().Add(time.Duration(expiresIn) * time.Second)
	ticker := time.NewTicker(time.Duration(interval) * time.Second)
	defer ticker.Stop()

	// The first poll runs immediately: when the user approved
	// before the command started waiting there is no reason to sit
	// out the first interval.
	for {
		token, done, err := pollOnce(ctx, dc.DeviceCode)
		switch {
		case err == errSlowDown:
			interval += 5
			ticker.Reset(time.Duration(interval) * time.Second)
		case err != nil || done:
			return token, err
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
			if time.Now().After(deadline) {
				return nil, errors.New("muse authorization timed out")
			}
		}
	}
}

// pollOnce performs a single token poll. It returns done=false with
// a nil error while the user has not approved yet.
func pollOnce(ctx context.Context, deviceCode string) (token *oauth.Token, done bool, err error) {
	token, err = tryPollToken(ctx, deviceCode)
	if err == errPending {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return token, true, nil
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	Error        string `json:"error"`
	ErrorDesc    string `json:"error_description"`
}

func tryPollToken(ctx context.Context, deviceCode string) (*oauth.Token, error) {
	form := url.Values{}
	form.Set("client_id", clientID())
	form.Set("device_code", deviceCode)
	form.Set("grant_type", deviceGrantType)

	respBody, status, err := postForm(ctx, authHost()+deviceTokenPath, form)
	if err != nil {
		return nil, err
	}

	var result tokenResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("decode muse token response: %w", err)
	}

	// The pending/slow-down signals arrive as an error payload, on
	// either 200 or 400 depending on server mood, so they are
	// matched before the status check below.
	switch result.Error {
	case "":
		// Authorized (or a malformed success, handled below).
	case "authorization_pending":
		return nil, errPending
	case "slow_down":
		return nil, errSlowDown
	case "access_denied":
		return nil, ErrAccessDenied
	case "expired_token":
		return nil, errors.New("muse device code expired")
	default:
		return nil, fmt.Errorf("muse authorization failed: %s", result.Error)
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("muse token request failed: status %d - %s", status, string(respBody))
	}
	if result.AccessToken == "" {
		return nil, errPending
	}
	return tokenFromOIDC(ctx, result.AccessToken, result.RefreshToken, result.ExpiresIn)
}

// RefreshToken exchanges a refresh token for a new OIDC access
// token and re-mints the Model API key, returning the fresh pair.
// A revoked or invalid grant surfaces as *oauth.TokenExchangeError
// so the coordinator can trigger interactive re-authentication.
func RefreshToken(ctx context.Context, refreshToken string) (*oauth.Token, error) {
	if refreshToken == "" {
		return nil, errors.New("muse refresh requires a refresh token; sign in again with `atlas login muse`")
	}

	form := url.Values{}
	form.Set("client_id", clientID())
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", refreshToken)

	respBody, status, err := postForm(ctx, authHost()+deviceTokenPath, form)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, &oauth.TokenExchangeError{StatusCode: status, Body: string(respBody)}
	}

	var result tokenResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("decode muse refresh response: %w", err)
	}
	if result.Error != "" {
		return nil, &oauth.TokenExchangeError{StatusCode: status, Body: string(respBody)}
	}
	if result.AccessToken == "" {
		return nil, errors.New("muse refresh response missing access_token")
	}
	if result.RefreshToken == "" {
		// Not every grant rotates: keep the still-valid refresh
		// token rather than storing an empty one.
		result.RefreshToken = refreshToken
	}
	return tokenFromOIDC(ctx, result.AccessToken, result.RefreshToken, result.ExpiresIn)
}

// tokenFromOIDC mints a Model API key for an OIDC access token and
// packages the pair the way Atlas-Agent stores subscription
// credentials: the usable key as the access token, the OIDC grant
// as the refresh token, and the OIDC lifetime as the expiry so the
// key is re-minted on the same cadence the grant rotates.
func tokenFromOIDC(ctx context.Context, accessToken, refreshToken string, expiresIn int) (*oauth.Token, error) {
	apiKey, err := MintAPIKey(ctx, accessToken)
	if err != nil {
		return nil, err
	}
	tok := &oauth.Token{
		AccessToken:  apiKey,
		RefreshToken: refreshToken,
		ExpiresIn:    expiresIn,
	}
	tok.SetExpiresAt()
	return tok, nil
}

// MintAPIKey exchanges an OIDC access token for a Model API key.
// Inference requests authenticate with the minted key.
func MintAPIKey(ctx context.Context, accessToken string) (string, error) {
	if accessToken == "" {
		return "", errors.New("muse key mint requires an access token")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiHost()+mintKeyPath, bytes.NewReader([]byte("{}")))
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("x-api-version", mintAPIVersion)
	req.Header.Set("Authorization", "Bearer "+accessToken)

	client := &http.Client{Timeout: httpTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("muse key mint failed: %s - %s", resp.Status, string(body))
	}

	var minted struct {
		APIKey string `json:"api_key"`
	}
	if err := json.Unmarshal(body, &minted); err != nil {
		return "", fmt.Errorf("decode muse key mint response: %w", err)
	}
	if minted.APIKey == "" {
		return "", errors.New("muse key mint response missing api_key")
	}
	return minted.APIKey, nil
}

// postForm sends a form-encoded POST and returns the raw body with
// its status code, leaving status interpretation to the caller:
// polling treats some non-2xx bodies as retry signals rather than
// failures.
func postForm(ctx context.Context, rawURL string, form url.Values) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rawURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", userAgent)

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
