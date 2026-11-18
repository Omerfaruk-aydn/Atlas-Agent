package designstudio

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/design"
	"github.com/stretchr/testify/require"
)

func TestStudioServesTheEditorAndTheProjectsDesigns(t *testing.T) {
	renderer := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(renderer, StudioPage), []byte("<!doctype html><title>studio</title>"), 0o644))
	t.Setenv("ATLAS_DESIGN_STUDIO_DIR", renderer)
	t.Cleanup(Close)

	root := t.TempDir()
	_, err := design.NewStore(root).Save("home", design.New("Home"), "", true)
	require.NoError(t, err)

	s, err := Open(root)
	require.NoError(t, err)
	again, err := Open(root)
	require.NoError(t, err)
	require.Same(t, s, again, "one server per project")
	require.Contains(t, s.URL("home"), "/design.html?file=home")

	get := func(path string) (int, string) {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, s.Base()+path, nil)
		require.NoError(t, err)
		res, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer res.Body.Close()
		body, _ := io.ReadAll(res.Body)
		return res.StatusCode, string(body)
	}
	code, body := get("/design.html")
	require.Equal(t, http.StatusOK, code)
	require.Contains(t, body, "studio")

	code, body = get("/studio.json")
	require.Equal(t, http.StatusOK, code)
	var info map[string]string
	require.NoError(t, json.Unmarshal([]byte(body), &info))
	require.Equal(t, Workspace, info["id"])
	require.NotEmpty(t, info["csrf"])

	code, body = get("/api/workspaces/studio/design/file?name=home")
	require.Equal(t, http.StatusOK, code)
	require.Contains(t, body, `"rev"`)

	// A save needs the studio token: another page cannot write the file.
	doc := `{"name":"home","doc":{"format":"atlas-design","version":1,"name":"Home","pages":[{"id":"p","name":"Page 1","children":[]}]},"create":false,"base_rev":""}`
	post, err := http.NewRequestWithContext(t.Context(), http.MethodPost, s.Base()+"/api/workspaces/studio/design/file", strings.NewReader(doc))
	require.NoError(t, err)
	post.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(post)
	require.NoError(t, err)
	res.Body.Close()
	require.Equal(t, http.StatusForbidden, res.StatusCode)

	// Another host name (DNS rebinding) is refused.
	req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, s.Base()+"/studio.json", nil)
	req.Host = "evil.example:80"
	res, err = http.DefaultClient.Do(req)
	require.NoError(t, err)
	res.Body.Close()
	require.Equal(t, http.StatusForbidden, res.StatusCode)
}

func TestRendererDirNamesTheBuildStepWhenMissing(t *testing.T) {
	t.Setenv("ATLAS_DESIGN_STUDIO_DIR", t.TempDir())
	t.Chdir(t.TempDir())
	if _, err := RendererDir(); err != nil {
		require.Contains(t, err.Error(), "npm run build")
	}
}
