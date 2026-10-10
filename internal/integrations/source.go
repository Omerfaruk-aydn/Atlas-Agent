package integrations

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/tidwall/gjson"
)

// configFiles locates which loaded config file defines a key. Paths are in
// load order, lowest priority first, so the last file that defines a key is
// the one that wins.
type configFiles struct {
	paths      []string
	globalData string
	workspace  string
	read       func(string) ([]byte, error)
}

func newConfigFiles(loaded []string, globalData, workspace string) configFiles {
	return configFiles{paths: loaded, globalData: globalData, workspace: workspace, read: os.ReadFile}
}

// escapeKey escapes one JSON path segment for gjson/sjson.
func escapeKey(segment string) string {
	r := strings.NewReplacer(`\`, `\\`, ".", `\.`, "*", `\*`, "?", `\?`)
	return r.Replace(segment)
}

func isShellConfig(path string) bool {
	base := filepath.Base(path)
	return strings.HasSuffix(base, "rc") && !strings.HasSuffix(base, ".json")
}

func samePath(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	ca, cb := filepath.Clean(a), filepath.Clean(b)
	if filepath.Separator == '\\' {
		return strings.EqualFold(ca, cb)
	}
	return ca == cb
}

// locate reports the source of key. Atlas only edits the global data file
// and the workspace file, and only when no higher-priority shell config is
// loaded that could silently override the edit.
func (c configFiles) locate(key string) Source {
	definedAt := -1
	for i := len(c.paths) - 1; i >= 0; i-- {
		p := c.paths[i]
		if isShellConfig(p) {
			continue
		}
		data, err := c.read(p)
		if err != nil {
			continue
		}
		if gjson.GetBytes(data, key).Exists() {
			definedAt = i
			break
		}
	}
	shellAbove := func(idx int) string {
		for i := len(c.paths) - 1; i > idx; i-- {
			if isShellConfig(c.paths[i]) {
				return c.paths[i]
			}
		}
		return ""
	}
	if definedAt < 0 {
		if sh := shellAbove(-1); sh != "" {
			return Source{Origin: "atlasrc", Scope: scopeOf(sh, c), Path: sh}
		}
		return Source{}
	}
	path := c.paths[definedAt]
	src := Source{Origin: filepath.Base(path), Scope: scopeOf(path, c), Path: path}
	writable := samePath(path, c.globalData) || samePath(path, c.workspace)
	if writable && shellAbove(definedAt) == "" {
		src.Editable = true
		src.ConfigKey = key
	}
	return src
}

func scopeOf(path string, c configFiles) string {
	switch {
	case samePath(path, c.workspace):
		return "workspace"
	case samePath(path, c.globalData):
		return "global"
	}
	if c.workspace != "" {
		root := filepath.Dir(filepath.Dir(c.workspace))
		if rel, err := filepath.Rel(root, path); err == nil && !strings.HasPrefix(rel, "..") {
			return "workspace"
		}
	}
	return "global"
}
