package codemap

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Limits keep the map bounded on any project.
const (
	maxFiles     = 20000
	maxFileBytes = 1 << 20
)

// generatedDirs are skipped when a folder without git is walked; git's own
// ignore rules cover them otherwise.
var generatedDirs = map[string]bool{
	".git": true, ".atlas": true, ".atlas-env": true, ".venv": true, "venv": true, "env": true,
	"node_modules": true, "bower_components": true, "vendor": true, "dist": true, "build": true, "out": true, "target": true,
	"coverage": true, "test-results": true, "playwright-report": true, "bin": true, "obj": true,
	".next": true, ".nuxt": true, ".svelte-kit": true, ".cache": true, ".turbo": true, ".parcel-cache": true,
	"__pycache__": true, ".pytest_cache": true, ".mypy_cache": true, ".tox": true, ".gradle": true, ".idea": true, ".vscode": true,
	"Pods": true, "DerivedData": true, ".dart_tool": true, "_build": true, "deps": true, "zig-cache": true, "zig-out": true,
}

// cacheEntry is a file's extraction and the stat it was read at.
type cacheEntry struct {
	Size    int64 `json:"size"`
	ModTime int64 `json:"mod_time"`
	File    File  `json:"file"`
}

// cacheVersion changes when extraction changes, so old caches are not
// reused.
const cacheVersion = 4

type cacheFile struct {
	Version int                    `json:"version"`
	Entries map[string]*cacheEntry `json:"entries"`
}

// Builder keeps the latest map of each project in memory, so a query
// after a few edits re-reads only the edited files.
type Builder struct {
	// CacheDir holds the per-project extraction caches; empty keeps them
	// in memory only.
	CacheDir string

	mu       sync.Mutex
	projects map[string]*project
}

type project struct {
	mu      sync.Mutex
	entries map[string]*cacheEntry
	loaded  bool
	current *Map
	stamp   string

	historyMu sync.Mutex
	history   *cochange
}

// NewBuilder returns a builder that caches extractions under cacheDir.
func NewBuilder(cacheDir string) *Builder {
	return &Builder{CacheDir: cacheDir, projects: map[string]*project{}}
}

// Map returns the current map of the project at root, re-reading only the
// files that changed since the last call.
func (b *Builder) Map(ctx context.Context, root string) (*Map, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	b.mu.Lock()
	p := b.projects[root]
	if p == nil {
		p = &project{}
		b.projects[root] = p
	}
	b.mu.Unlock()

	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.loaded {
		p.entries = b.readCache(root)
		p.loaded = true
	}
	listed, others, truncated, err := listFiles(ctx, root)
	if err != nil {
		return nil, err
	}
	type statted struct {
		path string
		info fs.FileInfo
	}
	var files []statted
	var stamp strings.Builder
	for _, rel := range listed {
		info, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		files = append(files, statted{rel, info})
		stamp.WriteString(rel)
		stamp.WriteString(" " + strconv.FormatInt(info.Size(), 10) + " " + strconv.FormatInt(info.ModTime().UnixNano(), 10) + "\n")
	}
	for _, rel := range others {
		stamp.WriteString(rel + "\n")
	}
	if p.current != nil && p.stamp == stamp.String() {
		return p.current, nil
	}

	var skipped []string
	result := make([]*File, len(files))
	var work []int
	for i, f := range files {
		limit := int64(maxFileBytes)
		switch category := Category(LanguageOf(f.path)); {
		case category == CategoryAsset:
			limit = -1
		case LanguageOf(f.path) == "notebook":
			limit = 16 * maxFileBytes
		}
		if limit >= 0 && f.info.Size() > limit {
			skipped = append(skipped, f.path+" (larger than "+formatBytes(limit)+")")
			continue
		}
		if e := p.entries[f.path]; e != nil && e.Size == f.info.Size() && e.ModTime == f.info.ModTime().UnixNano() {
			file := e.File
			result[i] = &file
			continue
		}
		work = append(work, i)
	}
	var mu sync.Mutex
	changed := false
	jobs := make(chan int)
	var wg sync.WaitGroup
	for range max(1, min(runtime.NumCPU(), 8)) {
		wg.Go(func() {
			for i := range jobs {
				f := files[i]
				full := filepath.Join(root, filepath.FromSlash(f.path))
				if Category(LanguageOf(f.path)) == CategoryAsset {
					head, err := readHead(full, f.path)
					if err != nil {
						continue
					}
					extracted := ExtractAsset(f.path, head, f.info.Size())
					mu.Lock()
					p.entries[f.path] = &cacheEntry{Size: f.info.Size(), ModTime: f.info.ModTime().UnixNano(), File: extracted}
					result[i] = &extracted
					changed = true
					mu.Unlock()
					continue
				}
				data, err := os.ReadFile(full)
				if err != nil || bytes.IndexByte(data, 0) >= 0 {
					continue
				}
				content := string(data)
				if Category(LanguageOf(f.path)) == CategoryCode && LanguageOf(f.path) != "notebook" && minified(content) {
					mu.Lock()
					skipped = append(skipped, f.path+" (minified)")
					mu.Unlock()
					continue
				}
				hash := Hash(content)
				mu.Lock()
				e := p.entries[f.path]
				mu.Unlock()
				var extracted File
				if e != nil && e.File.Hash == hash {
					extracted = e.File
				} else {
					extracted = Extract(f.path, content)
				}
				mu.Lock()
				p.entries[f.path] = &cacheEntry{Size: f.info.Size(), ModTime: f.info.ModTime().UnixNano(), File: extracted}
				result[i] = &extracted
				changed = true
				mu.Unlock()
			}
		})
	}
	for _, i := range work {
		if ctx.Err() != nil {
			break
		}
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	kept := make([]*File, 0, len(result))
	present := map[string]bool{}
	for _, f := range result {
		if f != nil {
			kept = append(kept, f)
			present[f.Path] = true
		}
	}
	for rel := range p.entries {
		if !present[rel] {
			delete(p.entries, rel)
			changed = true
		}
	}
	if changed {
		b.writeCache(root, p.entries)
	}
	sort.Strings(skipped)
	m := newMap(root, kept, goModule(root), others)
	m.Truncated = truncated
	m.Skipped = skipped
	m.history = p
	p.current, p.stamp = m, stamp.String()
	return m, nil
}

// readHead reads the first bytes of an asset: all of a text format that
// links other files (SVG, glTF), the header of anything else.
func readHead(full, rel string) ([]byte, error) {
	f, err := os.Open(full)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	n := assetHeaderBytes
	switch strings.ToLower(filepath.Ext(rel)) {
	case ".svg", ".gltf":
		n = maxFileBytes
	}
	buf := make([]byte, n)
	read, err := io.ReadFull(f, buf)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return nil, err
	}
	return buf[:read], nil
}

// minified reports whether content is a bundle or minified file, whose
// lines are far longer than written code.
func minified(content string) bool {
	if len(content) < 4096 {
		return false
	}
	lines := strings.Count(content, "\n") + 1
	return len(content)/lines > 500
}

// maxOtherFiles bounds how many files the map knows of only by name.
const maxOtherFiles = 50000

// listFiles returns the files under root the map reads, and the names of
// the others, which links may still point at; all slash-separated and
// relative. Inside a git work tree it asks git, which applies every
// ignore rule; elsewhere it walks the folder, skipping generated ones.
func listFiles(ctx context.Context, root string) (out, others []string, truncated bool, err error) {
	keep := func(rel string) {
		switch {
		case inDependencyDir(rel):
		case LanguageOf(rel) != "":
			out = append(out, rel)
		case len(others) < maxOtherFiles:
			others = append(others, rel)
		}
	}
	if listed, gitErr := gitFiles(ctx, root); gitErr == nil {
		for _, rel := range listed {
			keep(rel)
		}
	} else {
		err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				if p == root {
					return err
				}
				return nil
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if d.IsDir() {
				if p != root && (generatedDirs[d.Name()] || strings.HasPrefix(d.Name(), ".") && d.Name() != ".github") {
					return filepath.SkipDir
				}
				return nil
			}
			if rel, err := filepath.Rel(root, p); err == nil {
				keep(filepath.ToSlash(rel))
			}
			if len(out) > maxFiles*2 {
				return fs.SkipAll
			}
			return nil
		})
		if err != nil {
			return nil, nil, false, err
		}
	}
	sort.Strings(out)
	truncated = len(out) > maxFiles
	if truncated {
		out = out[:maxFiles]
	}
	return out, others, truncated, nil
}

// dependencyDirs hold installed packages and caches. Git lists them when a
// project forgets to ignore them, but they are not the project's code.
var dependencyDirs = map[string]bool{
	"node_modules": true, "bower_components": true, ".venv": true, "venv": true, "__pycache__": true,
	".atlas": true, "Pods": true, ".dart_tool": true, ".next": true, ".nuxt": true, ".svelte-kit": true,
	".turbo": true, ".cache": true, ".gradle": true, "DerivedData": true, "site-packages": true,
}

func inDependencyDir(rel string) bool {
	for _, part := range strings.Split(rel, "/") {
		if dependencyDirs[part] {
			return true
		}
	}
	return false
}

// gitFiles lists tracked and untracked, not ignored files.
func gitFiles(ctx context.Context, root string) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	out, err := gitCommand(ctx, root, "ls-files", "-z", "--cached", "--others", "--exclude-standard")
	if err != nil {
		return nil, err
	}
	var files []string
	seen := map[string]bool{}
	for _, rel := range strings.Split(string(out), "\x00") {
		if rel != "" && !seen[rel] {
			seen[rel] = true
			files = append(files, rel)
		}
	}
	return files, nil
}

// gitCommand runs git in root without inheriting GIT_ variables, which
// could point it at another repository.
func gitCommand(ctx context.Context, root string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", root}, args...)...)
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "GIT_") {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, errors.New(strings.TrimSpace(err.Error() + ": " + stderr.String()))
	}
	return out, nil
}

// goModule reads the module path from root's go.mod.
func goModule(root string) string {
	data, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == "module" {
			return strings.Trim(fields[1], `"`)
		}
	}
	return ""
}

func (b *Builder) cachePath(root string) string {
	if b.CacheDir == "" {
		return ""
	}
	return filepath.Join(b.CacheDir, Hash(strings.ToLower(root))+".json")
}

func (b *Builder) readCache(root string) map[string]*cacheEntry {
	entries := map[string]*cacheEntry{}
	file := b.cachePath(root)
	if file == "" {
		return entries
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return entries
	}
	var cache cacheFile
	if json.Unmarshal(data, &cache) != nil || cache.Version != cacheVersion || cache.Entries == nil {
		return entries
	}
	return cache.Entries
}

// writeCache saves the extractions; a failure only costs a re-read next
// time.
func (b *Builder) writeCache(root string, entries map[string]*cacheEntry) {
	file := b.cachePath(root)
	if file == "" {
		return
	}
	data, err := json.Marshal(cacheFile{Version: cacheVersion, Entries: entries})
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return
	}
	tmp := file + ".tmp"
	if os.WriteFile(tmp, data, 0o644) != nil {
		return
	}
	if os.Rename(tmp, file) != nil {
		_ = os.Remove(tmp)
	}
}
