package webui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// A running gateway writes a control file that only the current user can
// read. A second `atlas-agent web` uses it to find the gateway and ask it
// for a fresh sign-in link instead of failing on the busy port.
const controlHeader = "X-Atlas-Control"

// ControlInfo is the content of a gateway's control file.
type ControlInfo struct {
	PID    int    `json:"pid"`
	Port   string `json:"port"`
	Secret string `json:"secret"`
}

// ControlFilePath is where the gateway listening on port records itself.
func ControlFilePath(stateDir, port string) string {
	return filepath.Join(stateDir, "web", "gateway-"+port+".json")
}

// WriteControlFile records the gateway for later invocations and returns
// a function that removes the file.
func (g *Gateway) WriteControlFile(stateDir string) (func(), error) {
	g.mu.Lock()
	if g.controlSecret == "" {
		g.controlSecret = randomToken(32)
	}
	info := ControlInfo{PID: os.Getpid(), Port: g.port, Secret: g.controlSecret}
	g.mu.Unlock()
	p := ControlFilePath(stateDir, info.Port)
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(info)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(p, raw, 0o600); err != nil {
		return nil, err
	}
	return func() { _ = os.Remove(p) }, nil
}

func (g *Gateway) controlAuthorized(r *http.Request) bool {
	g.mu.Lock()
	secret := g.controlSecret
	g.mu.Unlock()
	return secret != "" && constantTimeEqual(r.Header.Get(controlHeader), secret)
}

func (g *Gateway) handleControlPing(w http.ResponseWriter, r *http.Request) {
	if !g.controlAuthorized(r) {
		writeError(w, http.StatusUnauthorized, "control secret required")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"app": "atlas-web", "instance": g.instance, "version": g.opts.Version})
}

func (g *Gateway) handleControlLaunch(w http.ResponseWriter, r *http.Request) {
	if !g.controlAuthorized(r) {
		writeError(w, http.StatusUnauthorized, "control secret required")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"url": g.LaunchURL()})
}

// ErrNoGateway reports that no Atlas web gateway answers on a port.
var ErrNoGateway = errors.New("no Atlas web UI is running on that port")

// RequestLaunchURL asks the gateway already listening on port for a new
// sign-in link, using the secret from its control file.
func RequestLaunchURL(ctx context.Context, stateDir, port string) (string, error) {
	raw, err := os.ReadFile(ControlFilePath(stateDir, port))
	if err != nil {
		return "", ErrNoGateway
	}
	var info ControlInfo
	if err := json.Unmarshal(raw, &info); err != nil || info.Secret == "" {
		return "", ErrNoGateway
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf("http://127.0.0.1:%s/control/launch", port), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set(controlHeader, info.Secret)
	rsp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", ErrNoGateway
	}
	defer rsp.Body.Close()
	if rsp.StatusCode != http.StatusOK {
		return "", ErrNoGateway
	}
	var out struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(rsp.Body).Decode(&out); err != nil || out.URL == "" {
		return "", ErrNoGateway
	}
	return out.URL, nil
}
