package codemap

import (
	"bytes"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// pngBytes encodes a blank image of the given size.
func pngBytes(t *testing.T, w, h int) string {
	t.Helper()
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, image.NewGray(image.Rect(0, 0, w, h))))
	return buf.String()
}

// webProject is a site with documents, images, markup, styles, SQL and
// npm scripts, linked to each other the ways real projects are.
func webProject(t *testing.T) map[string]string {
	return map[string]string{
		"README.md": "# Solar\n\n" +
			"## Install\n\n" +
			"Run `npm run build`. See the [usage guide](docs/guide.md#usage) and [old notes](docs/guide.md#removed).\n\n" +
			"![logo](public/logo.png)\n" +
			"![diagram](docs/img/missing.png)\n\n" +
			"The `solveOrbit` function computes positions.\n",
		"docs/guide.md":           "# Guide\n\n## Usage\n\nOpen index.html.\n",
		"public/logo.png":         pngBytes(t, 2, 3),
		"public/unused.png":       pngBytes(t, 4, 4),
		"public/textures/sun.png": pngBytes(t, 8, 4),
		"index.html": `<!doctype html>
<html>
<head><link rel="stylesheet" href="styles.css"></head>
<body>
  <div id="app" class="card primary"></div>
  <script type="module" src="src/main.js"></script>
</body>
</html>
`,
		"styles.css": `.card { color: red; }
.primary { font-weight: bold; }
.orphan { display: none; }
`,
		"src/main.js": `export function solveOrbit(t) {
  return t * 2;
}

export function start() {
  const el = document.getElementById("app");
  el.classList.add("card");
  loader.load("/textures/sun.png");
  return solveOrbit(1);
}

export async function loadUser(id) {
  return fetch(` + "`/api/users/${id}`" + `);
}
`,
		"api/openapi.yaml": `openapi: 3.0.0
paths:
  /users/{id}:
    get:
      operationId: getUser
`,
		"db/schema.sql": "CREATE TABLE users (\n  id INTEGER PRIMARY KEY,\n  name TEXT\n);\n",
		"go.mod":        "module example.com/site\n",
		"server/users.go": `package server

func listUsers(db DB) {
	db.Query("SELECT id, name FROM users WHERE id > 0")
}
`,
		"package.json": `{
  "name": "solar",
  "scripts": {
    "build": "node scripts/build.js",
    "release": "npm run build"
  }
}
`,
		"scripts/build.js": "export function build() {}\n",
	}
}

func buildWeb(t *testing.T) *Map {
	t.Helper()
	m, err := NewBuilder(t.TempDir()).Map(t.Context(), writeProject(t, webProject(t)))
	require.NoError(t, err)
	return m
}

func TestMapOverviewCoversDocumentsAndAssets(t *testing.T) {
	t.Parallel()
	m := buildWeb(t)
	overview := m.Overview(0)
	require.Contains(t, overview, "code: javascript 2, css 1, go 1, html 1, npm 1, openapi 1, sql 1; docs: markdown 2; assets: image 3")
	require.Contains(t, overview, "Documents: 2 files, 4 sections.")
	require.Contains(t, overview, "README.md: # Solar: 2 sections, mentions 2 definitions")
	require.Contains(t, overview, "Assets: 3 files")
	require.Contains(t, overview, "public/textures/sun.png (image, png, 8x4,")
	require.Contains(t, overview, "Named by no file (1; may still be loaded by a computed name or a directory listing): public/unused.png")
	require.Contains(t, overview, "README.md:5 links to docs/guide.md#removed (no such heading)")
	require.Contains(t, overview, "README.md:8 embeds docs/img/missing.png (not found)")

	// Only code is ranked; documents and assets have their own sections.
	ranked := overview[:strings.Index(overview, "Documents:")]
	require.NotContains(t, ranked, "README.md")
	require.NotContains(t, ranked, "logo.png")
}

func TestMapExplainsDocumentsAndAssets(t *testing.T) {
	t.Parallel()
	m := buildWeb(t)

	readme := m.Explain(t.Context(), "README.md")
	require.Contains(t, readme, "L5 links to docs/guide.md#usage -> docs/guide.md at line 3")
	require.Contains(t, readme, "L5 links to docs/guide.md#removed -> docs/guide.md (no such heading)")
	require.Contains(t, readme, "L7 embeds public/logo.png -> public/logo.png")
	require.Contains(t, readme, "L8 embeds docs/img/missing.png (not found)")
	require.Contains(t, readme, "script npm.build at package.json:4")
	require.Contains(t, readme, "function solveOrbit at src/main.js:1 [inferred]")

	logo := m.Explain(t.Context(), "public/logo.png")
	require.Contains(t, logo, "public/logo.png: image, png, 2x3,")
	require.Contains(t, logo, "README.md:7 embeds public/logo.png")
	require.Contains(t, m.Explain(t.Context(), "public/unused.png"), "Named by no file in the project")
	require.Contains(t, m.Explain(t.Context(), "sun.png"), "src/main.js:8 names /textures/sun.png")

	usage := m.Explain(t.Context(), "Usage")
	require.Contains(t, usage, "section Usage: docs/guide.md:3")
	require.Contains(t, usage, "Linked to from (1):\n  README.md:5 links to docs/guide.md#usage")

	// A document naming a definition is a guess, never resolved.
	require.Contains(t, m.Explain(t.Context(), "solveOrbit"), "README.md:10 in section Install [mention, inferred]")

	callers := m.Callers("public/logo.png", 2)
	require.Contains(t, callers, "reaches 1 places in 1 files")
	require.Contains(t, callers, "1. README.md:7 embeds public/logo.png")
	require.Contains(t, m.Path("README.md", "public/logo.png"), "README.md\n  -> public/logo.png")
}

func TestMapLinksStylesMarkupSQLAndScripts(t *testing.T) {
	t.Parallel()
	m := buildWeb(t)

	card := m.Explain(t.Context(), "card")
	require.Contains(t, card, "css class card: styles.css:1")
	require.Contains(t, card, "index.html:5")
	require.Contains(t, card, "src/main.js:7")
	require.Contains(t, m.Explain(t.Context(), "orphan"), "Used by (0)")

	app := m.Explain(t.Context(), "app")
	require.Contains(t, app, "element id app: index.html:5")
	require.Contains(t, app, "src/main.js:6 in function start")

	users := m.Explain(t.Context(), "users")
	require.Contains(t, users, "table users: db/schema.sql:1-4")
	require.Contains(t, users, "server/users.go:4 in function listUsers")
	require.Contains(t, m.Explain(t.Context(), "USERS"), "table users", "SQL names match in any case")

	// A request under a prefix such as /api still matches the endpoint.
	endpoint := m.Explain(t.Context(), "GET /users/{id}")
	require.Contains(t, endpoint, "endpoint GET /users/{id}: api/openapi.yaml:4")
	require.Contains(t, endpoint, "src/main.js:13 in function loadUser")

	html := m.Explain(t.Context(), "index.html")
	require.Contains(t, html, "styles.css -> styles.css")
	require.Contains(t, html, "src/main.js -> src/main.js")

	build := m.Explain(t.Context(), "npm.build")
	require.Contains(t, build, "package.json:5")
	require.Contains(t, m.Explain(t.Context(), "package.json"), "runs scripts/build.js -> scripts/build.js")

	// The npm script and the function share a name; find lists both.
	find := m.Find("build")
	require.Contains(t, find, "function build  scripts/build.js:1")
	require.Contains(t, find, "script npm.build  package.json:4")
}

// An embed directive names files, globs and directories; a link through the
// author's absolute path to the project resolves, one elsewhere is not a
// broken link.
func TestMapResolvesEmbedsAndAbsolutePaths(t *testing.T) {
	t.Parallel()
	files := map[string]string{
		"go.mod":                   "module example.com/x\n",
		"ui/ui.go":                 "package ui\n\nimport \"embed\"\n\n//go:embed logo.svg\nvar logo string\n\n//go:embed static/*.css templates\nvar files embed.FS\n",
		"ui/logo.svg":              `<svg width="10" height="10"></svg>`,
		"ui/static/app.css":        ".app {}\n",
		"ui/templates/page.html":   "<p>hi</p>\n",
		"ui/templates/part/x.html": "<p>x</p>\n",
	}
	root := writeProject(t, files)
	require.NoError(t, os.WriteFile(filepath.Join(root, "settings.json"), []byte("{}"), 0o644))
	notes := "See [theirs](Q:/elsewhere/ui.go), [ours](" + filepath.ToSlash(root) + "/ui/ui.go), " +
		"[the settings](settings.json), [the templates](" + filepath.ToSlash(root) + "/ui/templates/) and [gone](gone.json).\n"
	require.NoError(t, os.WriteFile(filepath.Join(root, "notes.md"), []byte(notes), 0o644))
	m, err := NewBuilder(t.TempDir()).Map(t.Context(), root)
	require.NoError(t, err)

	ui := m.Explain(t.Context(), "ui/ui.go")
	require.Contains(t, ui, "L5 embeds ./logo.svg -> ui/logo.svg")
	require.Contains(t, ui, "L8 embeds ./static/*.css -> 1 file such as ui/static/app.css")
	require.Contains(t, ui, "L8 embeds ./templates -> 2 files such as ui/templates/page.html")
	require.Contains(t, m.Explain(t.Context(), "ui/logo.svg"), "ui/ui.go:5 embeds ./logo.svg")

	doc := m.Explain(t.Context(), "notes.md")
	require.Contains(t, doc, "links to Q:/elsewhere/ui.go (outside the project)")
	require.Contains(t, doc, "/ui/ui.go -> ui/ui.go")
	require.Contains(t, doc, "links to settings.json -> settings.json (not mapped)")
	require.Contains(t, doc, "/ui/templates/ -> 2 files such as ui/templates/page.html")
	overview := m.Overview(0)
	require.Contains(t, overview, "Broken links: 1 to missing files")
	require.Contains(t, overview, "notes.md:1 links to gone.json (not found)")
	require.NotContains(t, overview, "Named by no file")
}
