package webui

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"path"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/accountlogin"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/env"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/fsext"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/proto"
)

const (
	// maxIndexedFiles bounds the walk behind file search. A project larger
	// than this still searches, over the files found first.
	maxIndexedFiles = 50_000
	// fileIndexTTL is how long a walk answers searches. A person typing
	// "@foo" asks once per key press; the walk should run once.
	fileIndexTTL = 20 * time.Second
	// fileIndexTimeout stops a walk of a huge tree.
	fileIndexTimeout = 10 * time.Second

	defaultFileSearchLimit = 30
	maxFileSearchLimit     = 200
)

// registerAppActions adds what the desktop app needs beyond the routes it
// shares with the web client: searching project files by name and connecting
// a model provider. None of it changes an existing route.
func (g *Gateway) registerAppActions(api *http.ServeMux) {
	api.HandleFunc("GET /api/workspaces/{id}/files/search", g.withHub(g.handleFileSearch))
	api.HandleFunc("POST /api/workspaces/{id}/sessions/{sid}/run", g.withHub(g.handleRunCommand))
	api.HandleFunc("GET /api/workspaces/{id}/providers", g.withHub(g.handleProviders))
	api.HandleFunc("POST /api/workspaces/{id}/providers/{provider}/key", g.withHub(g.handleProviderKey))
	api.HandleFunc("POST /api/workspaces/{id}/providers/{provider}/test", g.withHub(g.handleProviderTest))
	api.HandleFunc("POST /api/workspaces/{id}/providers/{provider}/login", g.withHub(g.handleLoginStart))
	api.HandleFunc("GET /api/workspaces/{id}/providers/{provider}/login", g.withHub(g.handleLoginStatus))
	api.HandleFunc("DELETE /api/workspaces/{id}/providers/{provider}/login", g.withHub(g.handleLoginCancel))
}

// fileIndex remembers one project's file list for a moment.
type fileIndex struct {
	files []string
	at    time.Time
}

var (
	fileIndexes   = map[string]*fileIndex{}
	fileIndexesMu sync.Mutex
)

// projectFiles lists a project's files relative to its root, respecting its
// ignore files. Symlinks are not followed, so the walk stays in the project.
func projectFiles(ctx context.Context, root string) ([]string, error) {
	fileIndexesMu.Lock()
	cached := fileIndexes[root]
	fileIndexesMu.Unlock()
	if cached != nil && time.Since(cached.at) < fileIndexTTL {
		return cached.files, nil
	}
	ctx, cancel := context.WithTimeout(ctx, fileIndexTimeout)
	defer cancel()
	found, _, err := fsext.GlobGitignoreAwareCtx(ctx, "**/*", root, maxIndexedFiles)
	if err != nil {
		return nil, err
	}
	prefix := strings.TrimRight(strings.ReplaceAll(root, `\`, "/"), "/") + "/"
	files := make([]string, 0, len(found))
	for _, f := range found {
		files = append(files, strings.TrimPrefix(strings.ReplaceAll(f, `\`, "/"), prefix))
	}
	fileIndexesMu.Lock()
	fileIndexes[root] = &fileIndex{files: files, at: time.Now()}
	fileIndexesMu.Unlock()
	return files, nil
}

// fileMatchScore ranks a path for a query, higher being better, and returns
// 0 when it does not match. A match in the file name beats one in the
// directories, and every letter must appear in order.
func fileMatchScore(file, query string) int {
	lower := strings.ToLower(file)
	base := path.Base(lower)
	switch {
	case base == query:
		return 1000
	case strings.HasPrefix(base, query):
		return 800 - len(base)
	case strings.Contains(base, query):
		return 600 - len(base)
	case strings.Contains(lower, query):
		return 400 - len(lower)/4
	}
	at := 0
	for _, r := range query {
		i := strings.IndexRune(lower[at:], r)
		if i < 0 {
			return 0
		}
		at += i + len(string(r))
	}
	return max(1, 200-len(lower)/2)
}

// searchFiles returns the best matches for query, best first. An empty query
// keeps the given order, which is most recently changed first.
func searchFiles(files []string, query string, limit int) []string {
	limit = max(0, min(limit, maxFileSearchLimit))
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return files[:min(limit, len(files))]
	}
	type hit struct {
		file  string
		score int
	}
	hits := make([]hit, 0, 64)
	for _, f := range files {
		if s := fileMatchScore(f, query); s > 0 {
			hits = append(hits, hit{f, s})
		}
	}
	slices.SortStableFunc(hits, func(a, b hit) int { return b.score - a.score })
	out := make([]string, 0)
	for _, h := range hits[:min(limit, len(hits))] {
		out = append(out, h.file)
	}
	return out
}

func (g *Gateway) handleFileSearch(w http.ResponseWriter, r *http.Request, h *hub) {
	limit := defaultFileSearchLimit
	if v, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && v > 0 {
		limit = min(v, maxFileSearchLimit)
	}
	files, err := projectFiles(r.Context(), h.path)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not list the project's files")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"files": searchFiles(files, r.URL.Query().Get("q"), limit)})
}

// handleRunCommand runs a command the person typed and keeps it in the chat:
// unlike the web client's shell route, the output becomes part of the session.
func (g *Gateway) handleRunCommand(w http.ResponseWriter, r *http.Request, h *hub) {
	var req struct {
		Command string `json:"command"`
	}
	if !decodeJSON(w, r, maxJSONBody, &req) {
		return
	}
	if strings.TrimSpace(req.Command) == "" {
		writeError(w, http.StatusBadRequest, "command is empty")
		return
	}
	// A command may run for a while; the server's own shell timeout bounds it.
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Minute)
	defer cancel()
	body := proto.ShellCommandRequest{SessionID: r.PathValue("sid"), Command: req.Command, TermWidth: 120}
	var out proto.ShellCommandResponse
	if err := g.sendJSON(ctx, http.MethodPost, upstreamPath(r, h, "/agent/sessions/{sid}/shell"), nil, body, &out); err != nil {
		writeUpstreamError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// providerChoice is one provider Atlas knows how to connect. It carries no
// credentials: only whether some are set.
type providerChoice struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Models     int    `json:"models"`
	Configured bool   `json:"configured"`
	// Account means the provider signs in through a browser, not with a key.
	Account bool `json:"account"`
}

func (g *Gateway) handleProviders(w http.ResponseWriter, r *http.Request, h *hub) {
	ctx, cancel := context.WithTimeout(r.Context(), upstreamTimeout)
	defer cancel()
	var known []struct {
		ID     string            `json:"id"`
		Name   string            `json:"name"`
		Models []json.RawMessage `json:"models"`
	}
	if err := g.getJSON(ctx, "/workspaces/"+url.PathEscape(h.workspaceID())+"/providers", nil, &known); err != nil {
		writeUpstreamError(w, err)
		return
	}
	cfg, err := g.workspaceConfig(ctx, h)
	if err != nil {
		writeUpstreamError(w, err)
		return
	}
	enabled := map[string]bool{}
	for _, p := range cfg.EnabledProviders() {
		enabled[p.ID] = true
	}
	out := make([]providerChoice, 0, len(known))
	for _, p := range known {
		name := p.Name
		if name == "" {
			name = p.ID
		}
		out = append(out, providerChoice{ID: p.ID, Name: name, Models: len(p.Models), Configured: enabled[p.ID], Account: accountlogin.Supported(p.ID)})
	}
	slices.SortStableFunc(out, func(a, b providerChoice) int {
		return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	})
	writeJSON(w, http.StatusOK, out)
}

// handleProviderKey stores an API key in the user's global config, where
// `atlas-agent login` keeps it too, and brings the agent up on it.
func (g *Gateway) handleProviderKey(w http.ResponseWriter, r *http.Request, h *hub) {
	var req struct {
		APIKey string `json:"api_key"`
	}
	if !decodeJSON(w, r, maxJSONBody, &req) {
		return
	}
	key := strings.TrimSpace(req.APIKey)
	if key == "" {
		writeError(w, http.StatusBadRequest, "api_key is required")
		return
	}
	raw, err := json.Marshal(key)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid api_key")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*upstreamTimeout)
	defer cancel()
	body := proto.ConfigProviderKeyRequest{Scope: config.ScopeGlobal, ProviderID: r.PathValue("provider"), Kind: proto.APIKeyKindString, APIKey: raw}
	if err := g.storeCredential(ctx, h, body); err != nil {
		writeUpstreamError(w, err)
		return
	}
	g.handleModels(w, r, h)
}

// storeCredential saves a provider credential in the user's global config and
// brings the agent up on it.
func (g *Gateway) storeCredential(ctx context.Context, h *hub, body proto.ConfigProviderKeyRequest) error {
	id := "/workspaces/" + url.PathEscape(h.workspaceID())
	if err := g.sendJSON(ctx, http.MethodPost, id+"/config/provider-key", nil, body, nil); err != nil {
		return err
	}
	var agent proto.AgentInfo
	step := "/agent/init"
	if err := g.getJSON(ctx, id+"/agent", nil, &agent); err == nil && agent.IsReady {
		step = "/agent/update"
	}
	return g.sendJSON(ctx, http.MethodPost, id+step, nil, nil, nil)
}

// handleProviderTest checks a connected provider's credentials with the same
// free request the terminal's setup uses, never a completion.
func (g *Gateway) handleProviderTest(w http.ResponseWriter, r *http.Request, h *hub) {
	ctx, cancel := context.WithTimeout(r.Context(), upstreamTimeout)
	defer cancel()
	cfg, err := g.workspaceConfig(ctx, h)
	if err != nil {
		writeUpstreamError(w, err)
		return
	}
	id := r.PathValue("provider")
	i := slices.IndexFunc(cfg.EnabledProviders(), func(p config.ProviderConfig) bool { return p.ID == id })
	if i < 0 {
		writeError(w, http.StatusNotFound, "this provider is not connected")
		return
	}
	p := cfg.EnabledProviders()[i]
	if err := p.TestConnectionContext(ctx, config.NewShellVariableResolver(env.New())); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
