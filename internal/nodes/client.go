package nodes

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/transfer"
)

// Client talks to one pinned node.
type Client struct {
	BaseURL     string
	Fingerprint string
	Token       string
	HTTP        *http.Client
}

// NewClient builds a client that only accepts the pinned certificate.
func NewClient(baseURL, fingerprint, token string) (*Client, error) {
	u, err := url.Parse(baseURL)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
		return nil, fmt.Errorf("node address must be an https:// URL with a host, got %q", baseURL)
	}
	transport := &http.Transport{
		TLSClientConfig:       PinnedTLSConfig(fingerprint),
		DialContext:           (&net.Dialer{Timeout: 10 * time.Second}).DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 0,
		MaxIdleConnsPerHost:   4,
		Proxy:                 nil,
	}
	return &Client{BaseURL: strings.TrimRight(u.String(), "/"), Fingerprint: fingerprint, Token: token, HTTP: &http.Client{Transport: transport}}, nil
}

// ConnError classifies transport failures so callers can tell "the node
// never got the request" from "the outcome is unknown".
type ConnError struct {
	// Sent is true when the request may have reached the node.
	Sent bool
	Err  error
}

func (e *ConnError) Error() string {
	if e.Sent {
		return "connection lost after the request was sent; outcome unknown: " + e.Err.Error()
	}
	return "node unreachable: " + e.Err.Error()
}

func (e *ConnError) Unwrap() error { return e.Err }

// call sends a request and decodes the JSON response into out (when non-nil).
func (c *Client) call(ctx context.Context, method, path string, body io.Reader, header http.Header, out any) error {
	resp, err := c.send(ctx, method, path, body, header)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<20)).Decode(out); err != nil {
		return &ConnError{Sent: true, Err: err}
	}
	return nil
}

// send performs a request and turns error statuses into *APIError. The
// caller owns the returned body.
func (c *Client) send(ctx context.Context, method, path string, body io.Reader, header http.Header) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, body)
	if err != nil {
		return nil, err
	}
	for k, v := range header {
		req.Header[k] = v
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	if body != nil && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		var pin *PinError
		if errors.As(err, &pin) {
			return nil, pin
		}
		// Dial and TLS failures happen before the request is written.
		var opErr *net.OpError
		sent := !errors.As(err, &opErr) || opErr.Op != "dial"
		return nil, &ConnError{Sent: sent && method != http.MethodGet, Err: err}
	}
	if resp.StatusCode >= 300 {
		defer resp.Body.Close()
		var apiErr APIError
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		if json.Unmarshal(data, &apiErr) != nil || apiErr.Code == "" {
			apiErr = APIError{Code: "http_" + strconv.Itoa(resp.StatusCode), Message: strings.TrimSpace(string(data)), Effect: "unknown"}
		}
		return nil, &apiErr
	}
	return resp, nil
}

func jsonBody(v any) io.Reader {
	data, _ := json.Marshal(v)
	return bytes.NewReader(data)
}

// Hello fetches the unauthenticated identity.
func (c *Client) Hello(ctx context.Context) (Hello, error) {
	var h Hello
	err := c.call(ctx, http.MethodGet, "/v1/node/hello", nil, nil, &h)
	return h, err
}

// Pair redeems a pairing code. The client must have been built with the
// node's fingerprint, so a man in the middle cannot complete pairing.
func (c *Client) Pair(ctx context.Context, code, deviceName string) (PairResponse, error) {
	var out PairResponse
	err := c.call(ctx, http.MethodPost, "/v1/node/pair", jsonBody(PairRequest{Code: code, DeviceName: deviceName}), nil, &out)
	return out, err
}

// Info returns the node's authenticated capabilities.
func (c *Client) Info(ctx context.Context) (Info, error) {
	var out Info
	err := c.call(ctx, http.MethodGet, "/v1/node/info", nil, nil, &out)
	return out, err
}

// Revoke revokes this controller's own token on the node.
func (c *Client) Revoke(ctx context.Context) error {
	err := c.call(ctx, http.MethodPost, "/v1/node/revoke", jsonBody(struct{}{}), nil, nil)
	return err
}

// Exec runs argv and waits for the result.
func (c *Client) Exec(ctx context.Context, req ExecRequest) (Operation, error) {
	var op Operation
	err := c.call(ctx, http.MethodPost, "/v1/node/exec", jsonBody(req), nil, &op)
	return op, err
}

// Operation reads a journaled operation (reconciliation after reconnect).
func (c *Client) Operation(ctx context.Context, id string) (Operation, error) {
	var op Operation
	err := c.call(ctx, http.MethodGet, "/v1/node/ops/"+url.PathEscape(id), nil, nil, &op)
	return op, err
}

// Cancel asks the node to stop an operation.
func (c *Client) Cancel(ctx context.Context, id string) (Operation, error) {
	var op Operation
	err := c.call(ctx, http.MethodPost, "/v1/node/ops/"+url.PathEscape(id)+"/cancel", jsonBody(struct{}{}), nil, &op)
	return op, err
}

// Push uploads a local file to rel under the node root.
func (c *Client) Push(ctx context.Context, operationID, localPath, rel string, overwrite bool, maxBytes int64) (FileResult, error) {
	if maxBytes <= 0 {
		maxBytes = transfer.DefaultMaxBytes
	}
	info, err := os.Stat(localPath)
	if err != nil {
		return FileResult{}, err
	}
	if !info.Mode().IsRegular() || info.Size() > maxBytes {
		return FileResult{}, fmt.Errorf("local file must be a regular file of at most %d bytes", maxBytes)
	}
	sum, err := transfer.HashFile(localPath)
	if err != nil {
		return FileResult{}, err
	}
	f, err := os.Open(localPath)
	if err != nil {
		return FileResult{}, err
	}
	defer f.Close()
	q := url.Values{"path": {rel}}
	if overwrite {
		q.Set("overwrite", "1")
	}
	h := http.Header{}
	h.Set("Content-Type", "application/octet-stream")
	h.Set("X-Atlas-SHA256", sum)
	h.Set("X-Atlas-Size", strconv.FormatInt(info.Size(), 10))
	h.Set("X-Atlas-Operation", operationID)
	var out FileResult
	err = c.call(ctx, http.MethodPut, "/v1/node/files?"+q.Encode(), f, h, &out)
	if err == nil && out.SHA256 != sum {
		return out, fmt.Errorf("node reported sha256 %s for a %s upload", out.SHA256, sum)
	}
	return out, err
}

// Pull downloads rel from the node into localRoot/localRel and verifies the
// announced size and SHA-256 before the file appears.
func (c *Client) Pull(ctx context.Context, rel, localRoot, localRel string, overwrite bool, maxBytes int64) (FileResult, error) {
	resp, err := c.send(ctx, http.MethodGet, "/v1/node/files?"+url.Values{"path": {rel}}.Encode(), nil, nil)
	if err != nil {
		return FileResult{}, err
	}
	defer resp.Body.Close()
	size, err := strconv.ParseInt(resp.Header.Get("X-Atlas-Size"), 10, 64)
	if err != nil {
		return FileResult{}, errors.New("node response lacks a size")
	}
	sum := resp.Header.Get("X-Atlas-SHA256")
	// A rerun of the same pull finds the identical file already in place;
	// that is a replay, not a conflict. Different content still needs
	// overwrite.
	if !overwrite && transfer.ValidSHA256(sum) {
		if dest, rerr := transfer.Resolve(localRoot, localRel); rerr == nil {
			if info, serr := os.Lstat(dest); serr == nil && info.Mode().IsRegular() && info.Size() == size {
				if have, herr := transfer.HashFile(dest); herr == nil && have == sum {
					return FileResult{Path: dest, SHA256: sum, Size: size, Replayed: true}, nil
				}
			}
		}
	}
	path, err := transfer.Receive(localRoot, localRel, resp.Body, size, sum, maxBytes, overwrite)
	if err != nil {
		return FileResult{}, err
	}
	return FileResult{Path: path, SHA256: sum, Size: size}, nil
}

// Observe captures the node desktop.
func (c *Client) Observe(ctx context.Context, image bool) (DesktopObservation, error) {
	var out DesktopObservation
	q := ""
	if !image {
		q = "?image=0"
	}
	err := c.call(ctx, http.MethodGet, "/v1/node/desktop/observe"+q, nil, nil, &out)
	return out, err
}

// Input sends one observation-bound input action.
func (c *Client) Input(ctx context.Context, in DesktopInput) (Operation, error) {
	var op Operation
	err := c.call(ctx, http.MethodPost, "/v1/node/desktop/input", jsonBody(in), nil, &op)
	return op, err
}

// Automation runs a read-only accessibility query.
func (c *Client) Automation(ctx context.Context, req AutomationRequest) (json.RawMessage, error) {
	resp, err := c.send(ctx, http.MethodPost, "/v1/node/desktop/automation", jsonBody(req), nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	return json.RawMessage(data), err
}

// Launch starts a GUI program on the node desktop.
func (c *Client) Launch(ctx context.Context, operationID string, argv []string) (Operation, error) {
	var op Operation
	err := c.call(ctx, http.MethodPost, "/v1/node/desktop/launch", jsonBody(LaunchRequest{OperationID: operationID, Argv: argv}), nil, &op)
	return op, err
}

// OperationID derives a stable operation id from a tool call and a step, so
// a retried tool call reuses the id and the node deduplicates it.
func OperationID(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return "op-" + hex.EncodeToString(sum[:12])
}
