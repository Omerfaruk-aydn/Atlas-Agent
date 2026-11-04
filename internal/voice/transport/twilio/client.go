// Package twilio is the Twilio Programmable Voice transport: inbound and
// outbound phone calls, bidirectional Media Streams, signed webhooks and
// SMS/WhatsApp written fallback.
package twilio

import (
	"cmp"
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/voice"
)

const defaultAPIBase = "https://api.twilio.com"

// Signature computes Twilio's request signature: HMAC-SHA1 over the full
// URL followed by every POST parameter name and value, sorted by name,
// base64 encoded.
func Signature(authToken, fullURL string, params url.Values) string {
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	var b strings.Builder
	b.WriteString(fullURL)
	for _, k := range keys {
		vals := slices.Clone(params[k])
		slices.Sort(vals)
		for _, v := range vals {
			b.WriteString(k)
			b.WriteString(v)
		}
	}
	mac := hmac.New(sha1.New, []byte(authToken))
	mac.Write([]byte(b.String()))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// ValidSignature reports whether signature matches.
func ValidSignature(authToken, fullURL string, params url.Values, signature string) bool {
	if authToken == "" || signature == "" {
		return false
	}
	want := Signature(authToken, fullURL, params)
	return subtle.ConstantTimeCompare([]byte(want), []byte(signature)) == 1
}

// Client is a minimal Twilio REST client.
type Client struct {
	AccountSID voice.Secret
	AuthToken  voice.Secret
	BaseURL    string
	HTTP       *http.Client
}

func (c *Client) base() string { return cmp.Or(strings.TrimRight(c.BaseURL, "/"), defaultAPIBase) }

func (c *Client) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 20 * time.Second}
}

type apiResponse struct {
	SID     string `json:"sid"`
	Status  string `json:"status"`
	Message string `json:"message"`
	Code    int    `json:"code"`
}

// post sends a form to an account resource. Errors never include the
// credentials or the request body.
func (c *Client) post(ctx context.Context, resource string, form url.Values) (apiResponse, error) {
	endpoint := fmt.Sprintf("%s/2010-04-01/Accounts/%s/%s", c.base(), url.PathEscape(c.AccountSID.Reveal()), resource)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return apiResponse{}, &voice.DialError{NotPlaced: true, Code: voice.CodeNotConfigured, Err: errors.New("invalid request")}
	}
	req.SetBasicAuth(c.AccountSID.Reveal(), c.AuthToken.Reveal())
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return apiResponse{}, &voice.DialError{NotPlaced: notSent(err), Code: voice.CodeTransportFailed, Err: errors.New("request failed")}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	var out apiResponse
	_ = json.Unmarshal(body, &out)
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return out, &voice.DialError{NotPlaced: true, Code: voice.CodeProviderAuth, Err: fmt.Errorf("status %d", resp.StatusCode)}
	case resp.StatusCode >= 400 && resp.StatusCode < 500:
		return out, &voice.DialError{NotPlaced: true, Code: voice.CodeRejected, Err: fmt.Errorf("status %d, twilio code %d", resp.StatusCode, out.Code)}
	case resp.StatusCode >= 500:
		// The resource may have been created before the error.
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

// CallRequest is an outbound call.
type CallRequest struct {
	To, From         string
	URL              string
	StatusCallback   string
	Timeout          int
	MachineDetection bool
}

// CreateCall places a call and returns its SID.
func (c *Client) CreateCall(ctx context.Context, r CallRequest) (string, error) {
	form := url.Values{}
	form.Set("To", r.To)
	form.Set("From", r.From)
	form.Set("Url", r.URL)
	form.Set("Method", http.MethodPost)
	form.Set("StatusCallback", r.StatusCallback)
	form.Set("StatusCallbackMethod", http.MethodPost)
	for _, ev := range []string{"initiated", "ringing", "answered", "completed"} {
		form.Add("StatusCallbackEvent", ev)
	}
	if r.Timeout > 0 {
		form.Set("Timeout", fmt.Sprint(r.Timeout))
	}
	if r.MachineDetection {
		form.Set("MachineDetection", "Enable")
	}
	out, err := c.post(ctx, "Calls.json", form)
	if err != nil {
		return "", err
	}
	if out.SID == "" {
		return "", &voice.DialError{Code: voice.CodeProviderProtocol, Err: errors.New("response carried no call sid")}
	}
	return out.SID, nil
}

// EndCall completes or cancels a call.
func (c *Client) EndCall(ctx context.Context, sid string) error {
	form := url.Values{}
	form.Set("Status", "completed")
	_, err := c.post(ctx, "Calls/"+url.PathEscape(sid)+".json", form)
	return err
}

// SendMessage sends an SMS or, with whatsapp: prefixed numbers, a WhatsApp
// message.
func (c *Client) SendMessage(ctx context.Context, from, to, body string) error {
	form := url.Values{}
	form.Set("From", from)
	form.Set("To", to)
	form.Set("Body", body)
	_, err := c.post(ctx, "Messages.json", form)
	return err
}

// Notifier sends written fallback over SMS or WhatsApp to the owner.
type Notifier struct {
	Client  *Client
	Kind    string // voice.ChannelSMS or voice.ChannelWhatsAppText
	From    string
	OwnerTo string
}

// Channel implements voice.Notifier.
func (n *Notifier) Channel() string { return n.Kind }

// Notify implements voice.Notifier.
func (n *Notifier) Notify(ctx context.Context, text string) error {
	from, to := n.From, n.OwnerTo
	if n.Kind == voice.ChannelWhatsAppText {
		from, to = "whatsapp:"+from, "whatsapp:"+to
	}
	if r := []rune(text); len(r) > 1500 {
		text = string(r[:1500])
	}
	return n.Client.SendMessage(ctx, from, to, text)
}
