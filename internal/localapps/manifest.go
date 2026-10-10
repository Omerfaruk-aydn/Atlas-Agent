// Package localapps builds, registers, runs, verifies, updates and backs up
// local web apps that Atlas creates for the user. An app is a directory with
// a versioned manifest (atlas-app.json) plus a record in a per-user registry,
// because apps outlive the project Atlas happened to be opened in.
//
// Only runtimes listed in SupportedRuntimes are managed. Atlas runs an app
// only with its runtime's interpreter, binds it to 127.0.0.1, and only ever
// stops processes it started and can still identify (PID plus creation
// time).
package localapps

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
)

// ManifestFile is the manifest's name in an app root.
const ManifestFile = "atlas-app.json"

// ManifestVersion is the manifest format this Atlas writes and reads.
const ManifestVersion = 1

// RuntimePython is the supported runtime: Python 3.9+ standard library
// only (http.server + sqlite3), so an app needs no package downloads.
const RuntimePython = "python-stdlib"

// RuntimeInfo describes a supported runtime.
type RuntimeInfo struct {
	Kind        string `json:"kind"`
	Name        string `json:"name"`
	Tool        string `json:"tool"`
	Constraint  string `json:"constraint"`
	Placeholder string `json:"placeholder"`
	Stack       string `json:"stack"`
	Remedy      string `json:"remedy"`
}

// SupportedRuntimes lists runtimes Atlas can build and manage end to end.
func SupportedRuntimes() []RuntimeInfo {
	return []RuntimeInfo{{
		Kind:        RuntimePython,
		Name:        "Python 3.9+ (local apps)",
		Tool:        "python",
		Constraint:  ">=3.9",
		Placeholder: "{python}",
		Stack:       "Python standard library web server, SQLite storage, HTML/JS interface",
		Remedy:      "Install Python 3.9 or newer from https://www.python.org/downloads/ and make sure `python` runs in a new terminal, then check again.",
	}}
}

// RuntimeFor returns the runtime of kind.
func RuntimeFor(kind string) (RuntimeInfo, bool) {
	for _, r := range SupportedRuntimes() {
		if r.Kind == kind {
			return r, true
		}
	}
	return RuntimeInfo{}, false
}

// EnvRef sets one environment variable without embedding secrets: a plain
// non-secret value, a variable copied from Atlas's environment, or a named
// secret sealed in Atlas's per-user secret store.
type EnvRef struct {
	Value   string `json:"value,omitempty"`
	FromEnv string `json:"from_env,omitempty"`
	Secret  string `json:"secret,omitempty"`
}

// Command is a literal argv; the first element must be the runtime
// placeholder.
type Command struct {
	Argv           []string `json:"argv"`
	TimeoutSeconds int      `json:"timeout_seconds,omitempty"`
}

// RunSpec says how to start the app.
type RunSpec struct {
	Argv []string `json:"argv"`
	// Cwd is relative to the app root.
	Cwd string            `json:"cwd,omitempty"`
	Env map[string]EnvRef `json:"env,omitempty"`
}

// Health is the readiness probe.
type Health struct {
	Path           string `json:"path"`
	ExpectStatus   int    `json:"expect_status,omitempty"`
	ExpectContains string `json:"expect_contains,omitempty"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty"`
}

// BackupSpec names what a backup captures. SQLite databases are copied as
// consistent snapshots; Files are plain copies.
type BackupSpec struct {
	SQLite []string `json:"sqlite,omitempty"`
	Files  []string `json:"files,omitempty"`
}

// Field is one attribute of an entity.
type Field struct {
	Name     string   `json:"name"`
	Label    string   `json:"label"`
	Type     string   `json:"type"`
	Required bool     `json:"required,omitempty"`
	Choices  []string `json:"choices,omitempty"`
}

// Entity is one kind of record the app stores.
type Entity struct {
	Name   string  `json:"name"`
	Label  string  `json:"label"`
	Fields []Field `json:"fields"`
}

// Spec is the short definition derived from the user's request.
type Spec struct {
	Purpose      string   `json:"purpose"`
	Flows        []string `json:"flows"`
	Entities     []Entity `json:"entities"`
	Acceptance   []string `json:"acceptance"`
	Dependencies []string `json:"dependencies,omitempty"`
	RunMethod    string   `json:"run_method,omitempty"`
	Storage      string   `json:"storage,omitempty"`
	// Language is the interface language code (en, tr, de, fr, it, ar).
	Language string `json:"language,omitempty"`
}

// Manifest is atlas-app.json.
type Manifest struct {
	ManifestVersion int        `json:"manifest_version"`
	ID              string     `json:"id"`
	Name            string     `json:"name"`
	Description     string     `json:"description"`
	Runtime         string     `json:"runtime"`
	Dependencies    []string   `json:"dependencies"`
	Run             RunSpec    `json:"run"`
	Build           *Command   `json:"build,omitempty"`
	Test            *Command   `json:"test,omitempty"`
	Port            int        `json:"port"`
	URLPath         string     `json:"url_path,omitempty"`
	Health          Health     `json:"health"`
	DataDirs        []string   `json:"data_dirs"`
	Backup          BackupSpec `json:"backup"`
	Spec            *Spec      `json:"spec,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

var (
	appIDPattern   = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,47}$`)
	identPattern   = regexp.MustCompile(`^[a-z][a-z0-9_]{0,39}$`)
	envNamePattern = regexp.MustCompile(`^[A-Z_][A-Z0-9_]{0,63}$`)
)

// FieldTypes lists the supported field types.
var FieldTypes = []string{"text", "longtext", "integer", "number", "date", "boolean", "choice"}

// ValidID reports whether id is a valid app id.
func ValidID(id string) bool { return appIDPattern.MatchString(id) }

// LoadManifest reads and validates root's manifest.
func LoadManifest(root string) (Manifest, error) {
	data, err := os.ReadFile(filepath.Join(root, ManifestFile))
	if err != nil {
		return Manifest{}, err
	}
	if len(data) > 1<<20 {
		return Manifest{}, errors.New("manifest is larger than 1 MiB")
	}
	var m Manifest
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&m); err != nil {
		return Manifest{}, fmt.Errorf("invalid %s: %w", ManifestFile, err)
	}
	if err := m.Validate(); err != nil {
		return m, err
	}
	return m, nil
}

// SaveManifest writes m to root after validating it.
func SaveManifest(root string, m Manifest) error {
	if err := m.Validate(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(filepath.Join(root, ManifestFile), append(data, '\n'), 0o644)
}

// relInside validates a relative path that must stay inside the root.
func relInside(p string) error {
	if p == "" {
		return errors.New("empty path")
	}
	if filepath.IsAbs(p) || filepath.VolumeName(p) != "" || strings.HasPrefix(p, "/") || strings.HasPrefix(p, `\`) {
		return fmt.Errorf("%q must be relative to the app folder", p)
	}
	clean := filepath.Clean(filepath.FromSlash(p))
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return fmt.Errorf("%q leaves the app folder", p)
	}
	return nil
}

func looksSecret(name string) bool {
	for _, w := range []string{"KEY", "TOKEN", "SECRET", "PASSWORD", "PASS", "CREDENTIAL", "AUTH"} {
		if strings.Contains(name, w) {
			return true
		}
	}
	return false
}

// Validate checks the manifest contract.
func (m Manifest) Validate() error {
	var errs []string
	add := func(format string, args ...any) { errs = append(errs, fmt.Sprintf(format, args...)) }
	if m.ManifestVersion != ManifestVersion {
		if m.ManifestVersion > ManifestVersion {
			add("manifest_version %d is newer than this Atlas supports (%d); update Atlas", m.ManifestVersion, ManifestVersion)
		} else {
			add("manifest_version %d is not supported (expected %d)", m.ManifestVersion, ManifestVersion)
		}
	}
	if !ValidID(m.ID) {
		add("id must be 2-48 lowercase letters, digits or '-'")
	}
	if strings.TrimSpace(m.Name) == "" || len(m.Name) > 80 {
		add("name is required (up to 80 characters)")
	}
	rt, ok := RuntimeFor(m.Runtime)
	if !ok {
		add("runtime %q is not supported; supported: %s", m.Runtime, RuntimePython)
	}
	checkArgv := func(what string, argv []string) {
		if len(argv) < 2 {
			add("%s needs the runtime and a script", what)
			return
		}
		if ok && argv[0] != rt.Placeholder {
			add("%s must start with %s so Atlas runs it with the checked runtime", what, rt.Placeholder)
		}
		for _, a := range argv[1:] {
			if strings.Contains(a, "{") && strings.Contains(a, "}") {
				add("%s has an unknown placeholder %q", what, a)
			}
		}
	}
	checkArgv("run.argv", m.Run.Argv)
	if m.Build != nil {
		checkArgv("build.argv", m.Build.Argv)
	}
	if m.Test != nil {
		checkArgv("test.argv", m.Test.Argv)
	}
	if m.Run.Cwd != "" && m.Run.Cwd != "." {
		if err := relInside(m.Run.Cwd); err != nil {
			add("run.cwd: %v", err)
		}
	}
	for name, ref := range m.Run.Env {
		if !envNamePattern.MatchString(name) {
			add("env name %q is invalid", name)
		}
		if name == "PORT" || name == "ATLAS_APP_DATA" {
			add("env %s is set by Atlas", name)
		}
		set := 0
		for _, v := range []string{ref.Value, ref.FromEnv, ref.Secret} {
			if v != "" {
				set++
			}
		}
		if set != 1 {
			add("env %s needs exactly one of value, from_env or secret", name)
		}
		if ref.Value != "" && looksSecret(name) {
			add("env %s looks secret; reference it with from_env or secret instead of a literal value", name)
		}
	}
	if m.Port != 0 && (m.Port < 1024 || m.Port > 65535) {
		add("port must be between 1024 and 65535")
	}
	if !strings.HasPrefix(m.Health.Path, "/") {
		add("health.path must start with /")
	}
	if m.URLPath != "" && !strings.HasPrefix(m.URLPath, "/") {
		add("url_path must start with /")
	}
	if len(m.DataDirs) == 0 {
		add("data_dirs needs at least one folder")
	}
	for _, d := range m.DataDirs {
		if err := relInside(d); err != nil {
			add("data_dirs: %v", err)
		} else if filepath.Clean(d) == "." {
			add("data_dirs must name a subfolder, not the app folder itself")
		}
	}
	inData := func(p string) bool {
		for _, d := range m.DataDirs {
			rel, err := filepath.Rel(filepath.Clean(d), filepath.Clean(p))
			if err == nil && !strings.HasPrefix(rel, "..") {
				return true
			}
		}
		return false
	}
	for _, p := range slices.Concat(m.Backup.SQLite, m.Backup.Files) {
		if err := relInside(p); err != nil {
			add("backup: %v", err)
		} else if !inData(p) {
			add("backup path %q must be inside a data folder", p)
		}
	}
	if m.Spec != nil {
		if err := m.Spec.Validate(); err != nil {
			add("spec: %v", err)
		}
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

// Validate checks the app definition.
func (s Spec) Validate() error {
	var errs []string
	if strings.TrimSpace(s.Purpose) == "" {
		errs = append(errs, "purpose is required")
	}
	if len(s.Entities) == 0 || len(s.Entities) > 12 {
		errs = append(errs, "1-12 entities are required")
	}
	if len(s.Acceptance) == 0 || len(s.Acceptance) > 20 {
		errs = append(errs, "1-20 acceptance criteria are required")
	}
	seen := map[string]bool{}
	for _, e := range s.Entities {
		if !identPattern.MatchString(e.Name) || seen[e.Name] || strings.HasPrefix(e.Name, "sqlite_") {
			errs = append(errs, fmt.Sprintf("entity %q needs a unique lowercase name (letters, digits, _)", e.Name))
		}
		seen[e.Name] = true
		if len(e.Fields) == 0 || len(e.Fields) > 30 {
			errs = append(errs, fmt.Sprintf("entity %s needs 1-30 fields", e.Name))
		}
		fields := map[string]bool{}
		for _, f := range e.Fields {
			if !identPattern.MatchString(f.Name) || fields[f.Name] || slices.Contains([]string{"id", "created_at", "updated_at"}, f.Name) {
				errs = append(errs, fmt.Sprintf("field %s.%s needs a unique lowercase name other than id, created_at, updated_at", e.Name, f.Name))
			}
			fields[f.Name] = true
			if !slices.Contains(FieldTypes, f.Type) {
				errs = append(errs, fmt.Sprintf("field %s.%s has unsupported type %q (use %s)", e.Name, f.Name, f.Type, strings.Join(FieldTypes, ", ")))
			}
			if f.Type == "choice" && len(f.Choices) == 0 {
				errs = append(errs, fmt.Sprintf("choice field %s.%s needs choices", e.Name, f.Name))
			}
		}
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

// Hash fingerprints the manifest bytes on disk.
func manifestHash(root string) string {
	data, err := os.ReadFile(filepath.Join(root, ManifestFile))
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:8])
}

// Slug turns a name into an id prefix.
func Slug(name string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(foldTurkish(name)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			dash = false
		case !dash && b.Len() > 0:
			b.WriteByte('-')
			dash = true
		}
		if b.Len() >= 32 {
			break
		}
	}
	s := strings.Trim(b.String(), "-")
	if len(s) < 2 {
		s = "app"
	}
	return s
}

func foldTurkish(s string) string {
	return strings.NewReplacer("ı", "i", "İ", "i", "ş", "s", "Ş", "s", "ğ", "g", "Ğ", "g", "ü", "u", "Ü", "u", "ö", "o", "Ö", "o", "ç", "c", "Ç", "c", "â", "a", "é", "e", "è", "e", "à", "a", "ä", "a", "ß", "ss").Replace(s)
}
