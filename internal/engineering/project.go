package engineering

import (
	"context"
	"encoding/json"
	"fmt"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type ProjectFile struct {
	Path        string   `json:"path"`
	Fingerprint string   `json:"fingerprint"`
	Size        int64    `json:"size"`
	Package     string   `json:"package,omitempty"`
	Imports     []string `json:"imports,omitempty"`
	Test        bool     `json:"test,omitempty"`
	Entry       bool     `json:"entry,omitempty"`
}

type ProjectMap struct {
	Root      string        `json:"root"`
	Files     []ProjectFile `json:"files"`
	Truncated bool          `json:"truncated"`
	Changed   int           `json:"changed"`
	Removed   int           `json:"removed"`
}

func (s *Store) Project(ctx context.Context, root string) (ProjectMap, error) {
	var m ProjectMap
	data, err := os.ReadFile(filepath.Join(s.dir, "project-"+Hash(root)+".json"))
	if err != nil {
		return m, err
	}
	if len(data) > maxStateBytes {
		return m, fmt.Errorf("project map exceeds size limit")
	}
	if err := ctx.Err(); err != nil {
		return m, err
	}
	err = json.Unmarshal(data, &m)
	return m, err
}

func (s *Store) RefreshProject(ctx context.Context, root string) (ProjectMap, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return ProjectMap{}, err
	}
	previous, _ := s.Project(ctx, root)
	old := map[string]string{}
	for _, f := range previous.Files {
		old[f.Path] = f.Fingerprint
	}
	m := ProjectMap{Root: root, Files: []ProjectFile{}}
	entries := 0
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			return walkErr
		}
		entries++
		if entries > 8192 || len(m.Files) >= 4096 {
			m.Truncated = true
			return fs.SkipAll
		}
		if d.Type()&os.ModeSymlink != 0 {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			if filepath.Clean(p) == filepath.Clean(s.dir) {
				return fs.SkipDir
			}
			if p != root {
				switch d.Name() {
				case ".git", ".atlas", ".atlas-env", ".venv", "node_modules", "vendor", "dist", "build", ".next", "coverage", "__pycache__":
					return fs.SkipDir
				}
			}
			return nil
		}
		name := d.Name()
		if strings.HasPrefix(name, ".env") || strings.HasSuffix(name, ".pem") || strings.HasSuffix(name, ".key") {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(name))
		switch ext {
		case ".go", ".mod", ".work", ".ts", ".tsx", ".js", ".jsx", ".py", ".rs", ".java", ".cs", ".json", ".toml", ".yaml", ".yml", ".md", ".sql", ".tpl", ".tmpl", ".proto", ".swift", ".kt", ".css", ".scss", ".html", ".vue", ".svelte", ".tf":
		default:
			if name != "Makefile" && name != "Dockerfile" && name != "atlasrc" {
				return nil
			}
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.Size() > 64*1024 {
			m.Truncated = true
			return nil
		}
		f, err := os.Open(p)
		if err != nil {
			return err
		}
		data, readErr := io.ReadAll(io.LimitReader(f, 64*1024+1))
		f.Close()
		if readErr != nil {
			return readErr
		}
		if len(data) > 64*1024 || strings.ContainsRune(string(data), 0) {
			m.Truncated = true
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		node := ProjectFile{Path: rel, Fingerprint: Hash(string(data)), Size: info.Size(), Test: strings.Contains(name, "test") || strings.Contains(rel, "/tests/"), Entry: name == "main.go" || name == "package.json" || name == "go.mod" || name == "pyproject.toml" || name == "Cargo.toml"}
		if ext == ".go" {
			parsed, err := parser.ParseFile(token.NewFileSet(), p, data, parser.ImportsOnly)
			if err == nil {
				node.Package = parsed.Name.Name
				for _, imp := range parsed.Imports {
					value, err := strconv.Unquote(imp.Path.Value)
					if err == nil {
						node.Imports = append(node.Imports, value)
					}
				}
			}
		}
		if old[rel] != node.Fingerprint {
			m.Changed++
		}
		delete(old, rel)
		m.Files = append(m.Files, node)
		return nil
	})
	if err != nil {
		return m, err
	}
	if !m.Truncated {
		m.Removed = len(old)
	}
	sort.Slice(m.Files, func(i, j int) bool { return m.Files[i].Path < m.Files[j].Path })
	data, err := json.Marshal(m)
	if err != nil {
		return m, err
	}
	for len(data) > maxStateBytes && len(m.Files) > 0 {
		m.Files = m.Files[:len(m.Files)-1]
		m.Truncated = true
		data, err = json.Marshal(m)
		if err != nil {
			return m, err
		}
	}
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return m, err
	}
	err = AtomicWrite(filepath.Join(s.dir, "project-"+Hash(root)+".json"), data)
	return m, err
}
