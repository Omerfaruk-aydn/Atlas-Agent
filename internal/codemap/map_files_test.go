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

