// Package whatsapp implements the voice call transport for the WhatsApp
// Business Calling API. Signalling uses the Graph API and webhooks; audio
// travels over WebRTC as G.711 mu-law, so the gateway sees the same stream
// it gets from a telephone network.
package whatsapp

import (
	"bytes"
	"cmp"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/voice"
)

const defaultAPIBase = "https://graph.facebook.com"

// errNoPermission is the Graph error code for a user who has not granted
// this business number permission to call them.
const errNoPermission = 138006

// ValidSignature reports whether header is Meta's X-Hub-Signature-256 for
// body under the app secret.
func ValidSignature(appSecret string, body []byte, header string) bool {
	sig, ok := strings.CutPrefix(header, "sha256=")
	if !ok || appSecret == "" {
		return false
	}
	got, err := hex.DecodeString(sig)
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(appSecret))
	mac.Write(body)
	return hmac.Equal(got, mac.Sum(nil))
}

// Sign returns the signature header value for body, for tests and
// simulators.
func Sign(appSecret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(appSecret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// Client is a minimal Cloud API calling client.
type Client struct {
	AccessToken   voice.Secret
	PhoneNumberID string
	BaseURL       string
	Version       string
	HTTP          *http.Client
}

func (c *Client) endpoint() string {
	return fmt.Sprintf("%s/%s/%s/calls", strings.TrimRight(cmp.Or(c.BaseURL, defaultAPIBase), "/"),
		cmp.Or(c.Version, voice.DefaultWhatsAppGraphVersion), url.PathEscape(c.PhoneNumberID))
}

func (c *Client) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 20 * time.Second}
}

type session struct {
	Type string `json:"sdp_type"`
	SDP  string `json:"sdp"`
}

type apiResponse struct {
	Calls []struct {
		ID string `json:"id"`
	} `json:"calls"`
	Success bool `json:"success"`
	Error   struct {
		Message string `json:"message"`
		Code    int    `json:"code"`
	} `json:"error"`
}

// post sends one calling action. Errors never include the access token or
// the request body.
func (c *Client) post(ctx context.Context, body map[string]any) (apiResponse, error) {
	body["messaging_product"] = "whatsapp"
	raw, err := json.Marshal(body)
	if err != nil {
		return apiResponse{}, &voice.DialError{NotPlaced: true, Code: voice.CodeInvalidInput, Err: errors.New("invalid request")}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint(), bytes.NewReader(raw))
	if err != nil {
		return apiResponse{}, &voice.DialError{NotPlaced: true, Code: voice.CodeNotConfigured, Err: errors.New("invalid request")}
	}
	req.Header.Set("Authorization", "Bearer "+c.AccessToken.Reveal())
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return apiResponse{}, &voice.DialError{NotPlaced: notSent(err), Code: voice.CodeTransportFailed, Err: errors.New("request failed")}
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	var out apiResponse
	_ = json.Unmarshal(data, &out)
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return out, &voice.DialError{NotPlaced: true, Code: voice.CodeProviderAuth, Err: fmt.Errorf("status %d", resp.StatusCode)}
	case out.Error.Code == errNoPermission:
		return out, &voice.DialError{NotPlaced: true, Code: voice.CodePermissionRequired, Err: errors.New("the user has not allowed this number to call them")}
	case resp.StatusCode >= 400 && resp.StatusCode < 500:
		return out, &voice.DialError{NotPlaced: true, Code: voice.CodeRejected, Err: fmt.Errorf("status %d, graph code %d", resp.StatusCode, out.Error.Code)}
	case resp.StatusCode >= 500:
		// The call may have been created before the error.
		return out, &voice.DialError{Code: voice.CodeTransportFailed, Err: fmt.Errorf("status %d", resp.StatusCode)}
	}
	return out, nil
}

// notSent reports whether err proves the request never left this machine.
func notSent(err error) bool {
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return true
	}
	var opErr *net.OpError
	return errors.As(err, &opErr) && opErr.Op == "dial"
}

// Connect places a business-initiated call with an SDP offer and returns
// the WhatsApp call id.
func (c *Client) Connect(ctx context.Context, to, offerSDP string) (string, error) {
	out, err := c.post(ctx, map[string]any{
		"to": to, "action": "connect",
		"session": session{Type: "offer", SDP: offerSDP},
	})
	if err != nil {
		return "", err
	}
	if len(out.Calls) == 0 || out.Calls[0].ID == "" {
		return "", &voice.DialError{Code: voice.CodeProviderProtocol, Err: errors.New("no call id in the response")}
	}
	return out.Calls[0].ID, nil
}

// Answer sends pre_accept or accept for an incoming call with the SDP
// answer.
func (c *Client) Answer(ctx context.Context, callID, action, answerSDP string) error {
	_, err := c.post(ctx, map[string]any{
		"call_id": callID, "action": action,
		"session": session{Type: "answer", SDP: answerSDP},
	})
	return err
}

// Reject declines an incoming call.
func (c *Client) Reject(ctx context.Context, callID string) error {
	_, err := c.post(ctx, map[string]any{"call_id": callID, "action": "reject"})
	return err
}

// Terminate ends a call. WhatsApp asks for this even when the media path
// already carried a goodbye, so billing stays accurate.
func (c *Client) Terminate(ctx context.Context, callID string) error {
	_, err := c.post(ctx, map[string]any{"call_id": callID, "action": "terminate"})
	return err
}
