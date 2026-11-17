package tools

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/design"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/designstudio"
	"github.com/stretchr/testify/require"
)

func TestDesignStudioOpensTheEditorWithTheGuide(t *testing.T) {
	renderer := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(renderer, designstudio.StudioPage), []byte("<!doctype html>"), 0o644))
	t.Setenv("ATLAS_DESIGN_STUDIO_DIR", renderer)
	t.Cleanup(designstudio.Close)

	root := t.TempDir()
	store := design.NewStore(root)
	_, err := runDesignCanvas(root, store, DesignCanvasParams{Action: "studio"})
	require.ErrorContains(t, err, "file is required")

	out, err := runDesignCanvas(root, store, DesignCanvasParams{Action: "studio", Title: "Ferry Home"})
	require.NoError(t, err)
	require.Contains(t, out, "Created an empty design")
	require.Contains(t, out, "/design.html?file=ferry-home")
	require.Contains(t, out, "DESIGN IN PHASES")
	require.Contains(t, out, "BY HAND IN THE STUDIO")
	require.Contains(t, out, "action progress")
	require.Contains(t, out, "pointer_drag")
	_, err = store.Stat("ferry-home")
	require.NoError(t, err, "the file exists for the editor to open")

	out, err = runDesignCanvas(root, store, DesignCanvasParams{Action: "studio", File: "ferry-home"})
	require.NoError(t, err)
	require.Contains(t, out, "Opened ferry-home")
}
