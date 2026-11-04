// Package grok signs in to a SuperGrok or X Premium subscription with xAI's
// device-code flow, so Atlas-Agent can spend that subscription instead of a
// pay-per-token XAI_API_KEY.
//
// xAI offers this sign-in to third-party tools through one shared public
// client, so its consent page may say "Grok Build". The access token is a
// plain bearer credential for api.x.ai. xAI decides which accounts get API
// access; an account it refuses answers model calls with HTTP 403.
package grok

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/oauth"
)

const (
	clientID = "b1a00492-073a-47ea-816f-4c329264a828"
	scopes   = "openid profile email offline_access grok-cli:access api:access"
	// referrer tells xAI which tool the sign-in is for.
	referrer = "atlas-agent"

	deviceGrant = "urn:ietf:params:oauth:grant-type:device_code"

	defaultInterval = 5 * time.Second
	defaultExpiry   = 30 * time.Minute
	// slowDownStep is what RFC 8628 adds to the interval on "slow_down".
	slowDownStep = 5 * time.Second
	// maxResponseBytes bounds what is read from the auth server.
	maxResponseBytes = 64 << 10
)

// issuer is a variable so tests can point the flow at a local server.
var issuer = "https://auth.x.ai/oauth2"

// DeviceCode is a sign-in waiting for the person to approve it.
type DeviceCode struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval"`
}

// VerificationURL is the page to open, with the code already filled in when
// xAI offers that.
func (d *DeviceCode) VerificationURL() string {
	if d.VerificationURIComplete != "" {
		return d.VerificationURIComplete
	}
	return d.VerificationURI
}

type tokenResponse struct {
	AccessToken      string `json:"access_token"`
	RefreshToken     string `json:"refresh_token"`
	ExpiresIn        int    `json:"expires_in"`
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

// RequestDeviceCode starts a sign-in.
func RequestDeviceCode(ctx context.Context) (*DeviceCode, error) {
	body, status, err := postForm(ctx, issuer+"/device/code", url.Values{
		"client_id": {clientID},
		"scope":     {scopes},
		"referrer":  {referrer},
	})
	if err != nil {
		return nil, fmt.Errorf("request xAI device code: %w", err)
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("xAI refused the sign-in: %s", failure(body, status))
	}
	var dc DeviceCode
	if err := json.Unmarshal(body, &dc); err != nil {
		return nil, fmt.Errorf("read xAI device code: %w", err)
	}
	if dc.DeviceCode == "" || dc.UserCode == "" || dc.VerificationURI == "" {
		return nil, errors.New("xAI device code response is incomplete")
	}
	return &dc, nil
}

// PollForToken waits until the person approves the sign-in, refuses it, or
// the code expires.
func PollForToken(ctx context.Context, dc *DeviceCode) (*oauth.Token, error) {
	interval := time.Duration(dc.Interval) * time.Second
	if interval <= 0 {
		interval = defaultInterval
	}
	expiry := time.Duration(dc.ExpiresIn) * time.Second
	if expiry <= 0 {
		expiry = defaultExpiry
	}
	ctx, cancel := context.WithTimeout(ctx, expiry)
	defer cancel()

	for {
		select {
		case <-ctx.Done():
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return nil, errors.New("the Grok sign-in code expired; start again")
			}
			return nil, ctx.Err()
		case <-time.After(interval):
		}

		body, status, err := postForm(ctx, issuer+"/token", url.Values{
			"grant_type":  {deviceGrant},
			"device_code": {dc.DeviceCode},
			"client_id":   {clientID},
		})
		if err != nil {
			if ctx.Err() != nil {
				continue
			}
			return nil, fmt.Errorf("poll xAI for the token: %w", err)
		}
		var r tokenResponse
		if err := json.Unmarshal(body, &r); err != nil && status == http.StatusOK {
			return nil, fmt.Errorf("read xAI token: %w", err)
		}
		switch r.Error {
		case "":
			if status != http.StatusOK || r.AccessToken == "" {
				return nil, fmt.Errorf("xAI sent no token: %s", failure(body, status))
			}
			return tokenFrom(r, ""), nil
		case "authorization_pending":
		case "slow_down":
			interval += slowDownStep
		case "access_denied":
			return nil, errors.New("the Grok sign-in was refused")
		case "expired_token":
			return nil, errors.New("the Grok sign-in code expired; start again")
		default:
			return nil, fmt.Errorf("xAI refused the sign-in: %s", failure(body, status))
		}
	}
}

// RefreshToken trades a refresh token for a new access token.
func RefreshToken(ctx context.Context, refreshToken string) (*oauth.Token, error) {
	body, status, err := postForm(ctx, issuer+"/token", url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
		"client_id":     {clientID},
	})
	if err != nil {
		return nil, fmt.Errorf("refresh the Grok token: %w", err)
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("refresh the Grok token: %s", failure(body, status))
	}
	var r tokenResponse
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, fmt.Errorf("read the refreshed Grok token: %w", err)
	}
	if r.AccessToken == "" {
		return nil, errors.New("xAI sent no token on refresh")
	}
	return tokenFrom(r, refreshToken), nil
}

// tokenFrom keeps the old refresh token when xAI does not rotate it.
func tokenFrom(r tokenResponse, previousRefresh string) *oauth.Token {
	token := &oauth.Token{AccessToken: r.AccessToken, RefreshToken: r.RefreshToken, ExpiresIn: r.ExpiresIn}
	if token.RefreshToken == "" {
		token.RefreshToken = previousRefresh
	}
	token.SetExpiresAt()
	return token
}

// failure describes a refused request without repeating anything secret: the
// server's error fields, or the status alone.
func failure(body []byte, status int) string {
	var r tokenResponse
	if json.Unmarshal(body, &r) == nil && r.Error != "" {
		if r.ErrorDescription != "" {
			return r.Error + ": " + r.ErrorDescription
		}
		return r.Error
	}
	return fmt.Sprintf("HTTP %d", status)
}

func postForm(ctx context.Context, target string, form url.Values) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	rsp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer rsp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(rsp.Body, maxResponseBytes))
	if err != nil {
		return nil, 0, err
	}
	return body, rsp.StatusCode, nil
}
