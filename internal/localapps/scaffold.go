package localapps

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"html"
	"io/fs"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

//go:embed templates/python
var pythonTemplate embed.FS

// ScaffoldResult reports the files written for a new app.
type ScaffoldResult struct {
	Root  string   `json:"root"`
	Files []string `json:"files"`
	Port  int      `json:"port"`
	URL   string   `json:"url"`
}

// Scaffold writes a working app for the planned definition: a Python
// standard-library server, a SQLite schema with a first migration, an HTML
// interface built from the data model, end-to-end tests and the manifest.
// The agent then adapts the files to the request.
func (m *Manager) Scaffold(ctx context.Context, id string) (ScaffoldResult, error) {
	release, err := m.acquire(id, "scaffold")
	if err != nil {
		return ScaffoldResult{}, err
	}
	defer release()
	rec, err := m.record(id)
	if err != nil {
		return ScaffoldResult{}, err
	}
	m.continueJournal(id, "create")
	res, err := m.scaffoldLocked(ctx, rec)
	if err != nil {
		m.stage(id, StageFiles, StageFailed, err.Error())
		m.finishJournal(id, err)
	}
	return res, err
}

func (m *Manager) scaffoldLocked(ctx context.Context, rec Record) (ScaffoldResult, error) {
	if rec.State != StateDesigning || rec.Spec == nil {
		return ScaffoldResult{}, opErr(ErrInvalidState, "Plan a new app first.", "the app is %s; files are only written for a newly planned app", rec.State)
	}
	if err := emptyOrMissing(rec.Root); err != nil {
		return ScaffoldResult{}, err
	}
	m.stage(rec.ID, StageFiles, StageRunning, rec.Root)
	port, err := pickPort(0)
	if err != nil {
		return ScaffoldResult{}, err
	}
	now := time.Now().UTC().Truncate(time.Second)
	lang := rec.Spec.Language
	if lang == "" {
		lang = "en"
	}
	spec := *rec.Spec
	spec.Language = lang
	spec.Storage = "SQLite database in data/app.db"
	spec.RunMethod = "Atlas starts `python server.py` on 127.0.0.1 with PORT and ATLAS_APP_DATA"
	spec.Dependencies = []string{"python>=3.9 (standard library only)"}
	man := Manifest{
		ManifestVersion: ManifestVersion,
		ID:              rec.ID,
		Name:            rec.Name,
		Description:     rec.Description,
		Runtime:         RuntimePython,
		Dependencies:    []string{"python>=3.9"},
		Run:             RunSpec{Argv: []string{"{python}", "server.py"}},
		Build:           &Command{Argv: []string{"{python}", "-m", "py_compile", "server.py", "migrations.py"}, TimeoutSeconds: 60},
		Test:            &Command{Argv: []string{"{python}", "-m", "unittest", "discover", "-s", "tests", "-v"}, TimeoutSeconds: 300},
		Port:            port,
		URLPath:         "/",
		Health:          Health{Path: "/healthz", ExpectStatus: 200, ExpectContains: `"ok"`, TimeoutSeconds: 20},
		DataDirs:        []string{"data"},
		Backup:          BackupSpec{SQLite: []string{"data/app.db"}},
		Spec:            &spec,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if err := man.Validate(); err != nil {
		return ScaffoldResult{}, err
	}
	files, err := renderPythonApp(man)
	if err != nil {
		return ScaffoldResult{}, err
	}
	var written []string
	cleanup := func() {
		for i := len(written) - 1; i >= 0; i-- {
			_ = os.Remove(filepath.Join(rec.Root, filepath.FromSlash(written[i])))
		}
	}
	for _, name := range slices.Sorted(maps.Keys(files)) {
		if err := ctx.Err(); err != nil {
			cleanup()
			return ScaffoldResult{}, err
		}
		target := filepath.Join(rec.Root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			cleanup()
			return ScaffoldResult{}, err
		}
		if err := os.WriteFile(target, files[name], 0o644); err != nil {
			cleanup()
			return ScaffoldResult{}, err
		}
		written = append(written, name)
	}
	if err := os.MkdirAll(filepath.Join(rec.Root, "data"), 0o755); err != nil {
		cleanup()
		return ScaffoldResult{}, err
	}
	if err := SaveManifest(rec.Root, man); err != nil {
		cleanup()
		return ScaffoldResult{}, err
	}
	written = append(written, ManifestFile)
	if _, err := m.mutate(ctx, rec.ID, func(r *Record) error {
		r.State = StateScaffolded
		r.ManifestHash = manifestHash(r.Root)
		r.Spec = &spec
		return nil
	}); err != nil {
		return ScaffoldResult{}, err
	}
	m.stage(rec.ID, StageFiles, StagePassed, fmt.Sprintf("%d files in %s", len(written), rec.Root))
	return ScaffoldResult{Root: rec.Root, Files: written, Port: port, URL: fmt.Sprintf("http://127.0.0.1:%d/", port)}, nil
}

func sqlType(f Field) string {
	switch f.Type {
	case "integer", "boolean":
		return "INTEGER"
	case "number":
		return "REAL"
	}
	return "TEXT"
}

// renderPythonApp returns the app's files keyed by slash path.
func renderPythonApp(man Manifest) (map[string][]byte, error) {
	files := map[string][]byte{}
	err := fs.WalkDir(pythonTemplate, "templates/python", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := pythonTemplate.ReadFile(p)
		if err != nil {
			return err
		}
		files[strings.TrimPrefix(p, "templates/python/")] = data
		return nil
	})
	if err != nil {
		return nil, err
	}
	spec := man.Spec

	schema := map[string]any{
		"app_id":       man.ID,
		"name":         man.Name,
		"description":  man.Description,
		"language":     spec.Language,
		"default_port": man.Port,
		"entities":     spec.Entities,
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(schema); err != nil {
		return nil, err
	}
	files["schema.json"] = buf.Bytes()

	var mig strings.Builder
	mig.WriteString(`"""Database migrations, applied in order when the server starts.

Each entry is (version, [SQL statements]); each version runs in one
transaction and is recorded in SQLite's user_version. Never edit a
migration that has run on real data: append a new version instead, and keep
schema.json in step with it.
"""

MIGRATIONS = [
    (1, [
`)
	for _, e := range spec.Entities {
		cols := []string{"id INTEGER PRIMARY KEY AUTOINCREMENT"}
		for _, f := range e.Fields {
			col := fmt.Sprintf(`"%s" %s`, f.Name, sqlType(f))
			if f.Required {
				col += " NOT NULL"
			}
			cols = append(cols, col)
		}
		cols = append(cols, "created_at TEXT NOT NULL", "updated_at TEXT NOT NULL")
		stmt := fmt.Sprintf(`CREATE TABLE "%s" (%s)`, e.Name, strings.Join(cols, ", "))
		fmt.Fprintf(&mig, "        %s,\n", strconv.Quote(stmt))
	}
	mig.WriteString("    ]),\n]\n")
	files["migrations.py"] = []byte(mig.String())

	files["static/index.html"] = []byte(fmt.Sprintf(`<!doctype html>
<html lang="%s"%s>
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>%s</title>
  <link rel="stylesheet" href="/style.css">
</head>
<body>
  <header>
    <h1 id="title">%s</h1>
    <p id="subtitle">%s</p>
  </header>
  <nav aria-label="Sections"></nav>
  <main>
    <div id="notice" class="notice" role="status" hidden></div>
    <div id="content"></div>
  </main>
  <script src="/app.js"></script>
</body>
</html>
`, html.EscapeString(spec.Language), dirAttr(spec.Language), html.EscapeString(man.Name), html.EscapeString(man.Name), html.EscapeString(man.Description)))

	var readme strings.Builder
	fmt.Fprintf(&readme, "# %s\n\n%s\n\n", man.Name, man.Description)
	fmt.Fprintf(&readme, "## Purpose\n\n%s\n\n", spec.Purpose)
	if len(spec.Flows) > 0 {
		readme.WriteString("## Main flows\n\n")
		for _, f := range spec.Flows {
			fmt.Fprintf(&readme, "- %s\n", f)
		}
		readme.WriteString("\n")
	}
	readme.WriteString("## Data\n\n")
	for _, e := range spec.Entities {
		var names []string
		for _, f := range e.Fields {
			names = append(names, fmt.Sprintf("%s (%s)", f.Label, f.Type))
		}
		fmt.Fprintf(&readme, "- **%s**: %s\n", e.Label, strings.Join(names, ", "))
	}
	readme.WriteString(`
Records are stored in SQLite at ` + "`data/app.db`" + ` (or in the folder named by
` + "`ATLAS_APP_DATA`" + `). Atlas backs it up with a consistent snapshot, never by
copying the open file.

## Running

Open Atlas and use ` + "`/app`" + ` to start, stop, back up or update this app. Atlas
runs ` + "`python server.py`" + ` with ` + "`PORT`" + ` set and the server listens on 127.0.0.1
only. To run it by hand:

    PORT=` + strconv.Itoa(man.Port) + ` python server.py

## Changing it

- Data model: add a migration to ` + "`migrations.py`" + ` (never edit a released one)
  and update ` + "`schema.json`" + `.
- Interface: ` + "`static/`" + `. The HTML builds its forms from ` + "`/api/_schema`" + `.
- Tests: ` + "`python -m unittest discover -s tests -v`" + `.

## Acceptance criteria

`)
	for i, a := range spec.Acceptance {
		fmt.Fprintf(&readme, "%d. %s\n", i+1, a)
	}
	files["README.md"] = []byte(readme.String())
	files[".gitignore"] = []byte("data/\n__pycache__/\n*.pyc\n")
	for name := range files {
		if path.Clean(name) != name || strings.HasPrefix(name, "../") {
			return nil, fmt.Errorf("template file %q has an unsafe name", name)
		}
	}
	return files, nil
}

func dirAttr(lang string) string {
	if lang == "ar" {
		return ` dir="rtl"`
	}
	return ""
}
