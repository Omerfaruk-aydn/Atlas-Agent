package webui

import (
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/design"
	"github.com/stretchr/testify/require"
)

// The design tool lists, opens and saves the project's design files, sees
// the agent's edits by revision, and refuses to overwrite them blindly.
func TestDesignFilesRoundTrip(t *testing.T) {
	h := newHarness(t)
	b := h.signIn()
	ws := b.open(h.project)

	var list struct {
		Files []design.Info `json:"files"`
	}
	require.Equal(t, http.StatusOK, b.json(http.MethodGet, wsPath(ws.ID, "/design/files"), nil, &list))
	require.Empty(t, list.Files)

	var saved struct {
		Name string `json:"name"`
		Rev  string `json:"rev"`
	}
	doc := design.New("Home")
	require.Equal(t, http.StatusOK, b.json(http.MethodPost, wsPath(ws.ID, "/design/file"), map[string]any{"name": "home", "doc": doc, "create": true}, &saved))
	require.NotEmpty(t, saved.Rev)
	require.FileExists(t, filepath.Join(h.project, ".atlas", "design", "files", "home.json"))

	status, _ := b.raw(http.MethodPost, wsPath(ws.ID, "/design/file"), map[string]any{"name": "home", "doc": doc, "create": true})
	require.Equal(t, http.StatusBadRequest, status, "create never overwrites")

	var got struct {
		Rev       string          `json:"rev"`
		Doc       json.RawMessage `json:"doc"`
		Unchanged bool            `json:"unchanged"`
	}
	require.Equal(t, http.StatusOK, b.json(http.MethodGet, wsPath(ws.ID, "/design/file?name=home&rev="+url.QueryEscape(saved.Rev)), nil, &got))
	require.True(t, got.Unchanged, "the same revision answers without the document")

	// The agent edits the file on disk.
	store := design.NewStore(h.project)
	disk, rev, err := store.Load("home")
	require.NoError(t, err)
	next, _, err := design.Apply(disk, "", []design.Op{{Op: "add", Node: json.RawMessage(`{"id":"r1","type":"rect"}`)}})
	require.NoError(t, err)
	agentRev, err := store.Save("home", next, rev, false)
	require.NoError(t, err)

	got = struct {
		Rev       string          `json:"rev"`
		Doc       json.RawMessage `json:"doc"`
		Unchanged bool            `json:"unchanged"`
	}{}
	require.Equal(t, http.StatusOK, b.json(http.MethodGet, wsPath(ws.ID, "/design/file?name=home&rev="+url.QueryEscape(saved.Rev)), nil, &got))
	require.False(t, got.Unchanged)
	require.Equal(t, agentRev, got.Rev)
	require.Contains(t, string(got.Doc), `"r1"`)

	// A save based on the old revision is refused, with the current one.
	status, body := b.raw(http.MethodPost, wsPath(ws.ID, "/design/file"), map[string]any{"name": "home", "doc": doc, "base_rev": saved.Rev})
	require.Equal(t, http.StatusConflict, status)
	require.Contains(t, body, agentRev)

	require.Equal(t, http.StatusOK, b.json(http.MethodPost, wsPath(ws.ID, "/design/file"), map[string]any{"name": "home", "doc": json.RawMessage(got.Doc), "base_rev": agentRev}, &saved))

	require.Equal(t, http.StatusOK, b.json(http.MethodGet, wsPath(ws.ID, "/design/files"), nil, &list))
	require.Len(t, list.Files, 1)

	for _, bad := range []string{"../x", "Home", ""} {
		status, _ = b.raw(http.MethodGet, wsPath(ws.ID, "/design/file?name="+url.QueryEscape(bad)), nil)
		require.Equal(t, http.StatusBadRequest, status, bad)
	}
	status, _ = b.raw(http.MethodGet, wsPath(ws.ID, "/design/file?name=missing"), nil)
	require.Equal(t, http.StatusNotFound, status)
	status, _ = b.raw(http.MethodPost, wsPath(ws.ID, "/design/file"), map[string]any{"name": "x", "doc": map[string]any{"format": "other"}})
	require.Equal(t, http.StatusBadRequest, status)
}

// Images a design places by path are served from the project, only as
// images, and never from outside it.
func TestDesignAssetServesProjectImagesOnly(t *testing.T) {
	h := newHarness(t)
	b := h.signIn()
	ws := b.open(h.project)
	png := []byte("\x89PNG\r\n\x1a\nfake")
	require.NoError(t, os.WriteFile(filepath.Join(h.project, "logo.png"), png, 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(h.project, "notes.txt"), []byte("x"), 0o644))

	rsp := b.request(http.MethodGet, wsPath(ws.ID, "/design/asset?path=logo.png"), nil)
	defer rsp.Body.Close()
	require.Equal(t, http.StatusOK, rsp.StatusCode)
	require.Equal(t, "image/png", rsp.Header.Get("Content-Type"))
	require.Contains(t, rsp.Header.Get("Content-Security-Policy"), "sandbox")

	outside := filepath.Join(t.TempDir(), "x.png")
	require.NoError(t, os.WriteFile(outside, png, 0o644))
	for _, p := range []string{"notes.txt", "../x.png", outside} {
		status, _ := b.raw(http.MethodGet, wsPath(ws.ID, "/design/asset?path="+url.QueryEscape(p)), nil)
		require.Equal(t, http.StatusBadRequest, status, p)
	}
	status, _ := b.raw(http.MethodGet, wsPath(ws.ID, "/design/asset?path=missing.png"), nil)
	require.Equal(t, http.StatusNotFound, status)
}

// The design tool's library lists the kit, draws block previews, searches
// icons and runs kit ops on the document it holds without saving it.
func TestDesignKitEndpoints(t *testing.T) {
	h := newHarness(t)
	b := h.signIn()
	ws := b.open(h.project)

	var kit struct {
		Themes []struct {
			ID     string               `json:"id"`
			Colors map[string][2]string `json:"colors"`
		} `json:"themes"`
		Accents []struct{ Name, Color string } `json:"accents"`
		Blocks  []design.BlockInfo             `json:"blocks"`
		Icons   int                            `json:"icons"`
	}
	require.Equal(t, http.StatusOK, b.json(http.MethodGet, wsPath(ws.ID, "/design/kit"), nil, &kit))
	require.GreaterOrEqual(t, len(kit.Themes), 11)
	require.NotEmpty(t, kit.Themes[0].Colors["accent"][0])
	require.NotEmpty(t, kit.Accents)
	require.GreaterOrEqual(t, len(kit.Blocks), 45)
	require.Greater(t, kit.Icons, 1500)

	var icons struct {
		Icons []design.Icon `json:"icons"`
	}
	require.Equal(t, http.StatusOK, b.json(http.MethodGet, wsPath(ws.ID, "/design/kit/icons?q=cart"), nil, &icons))
	require.NotEmpty(t, icons.Icons)
	require.Equal(t, http.StatusOK, b.json(http.MethodGet, wsPath(ws.ID, "/design/kit/icons"), nil, &icons))
	require.GreaterOrEqual(t, len(icons.Icons), 40)

	status, body := b.raw(http.MethodGet, wsPath(ws.ID, "/design/kit/preview?block=ios/list"), nil)
	require.Equal(t, http.StatusOK, status)
	require.Contains(t, string(body), "<svg")
	status, _ = b.raw(http.MethodGet, wsPath(ws.ID, "/design/kit/preview?block=web/nope"), nil)
	require.Equal(t, http.StatusBadRequest, status)

	var applied struct {
		Doc     json.RawMessage `json:"doc"`
		Created []string        `json:"created"`
	}
	ops := []map[string]any{{"op": "set_theme", "name": "matcha"}, {"op": "insert", "block": "web/cta", "id": "cta"}}
	require.Equal(t, http.StatusOK, b.json(http.MethodPost, wsPath(ws.ID, "/design/kit/apply"), map[string]any{"doc": design.New("Home"), "ops": ops}, &applied))
	require.Equal(t, []string{"cta"}, applied.Created)
	doc, err := design.Parse(applied.Doc)
	require.NoError(t, err)
	require.Equal(t, "matcha", doc.Theme.ID)
	require.NotNil(t, doc.Find("cta"))
	require.NoDirExists(t, filepath.Join(h.project, ".atlas", "design", "files"), "nothing is saved")
}
