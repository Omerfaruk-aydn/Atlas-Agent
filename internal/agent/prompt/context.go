package prompt

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/filepathext"
)

const (
	contextFileLimit  = 64 * 1024
	contextTotalLimit = 256 * 1024
	contextCountLimit = 64
	contextTruncation = "\n[Context truncated: size limit reached.]\n"
)

// LoadScopedContext shares the configured instruction order and budget.
// Discovered paths must be literal contained project files.
func LoadScopedContext(ctx context.Context, cfg *config.ConfigStore, paths []string) ([]ContextFile, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	loader := newContextLoader()
	var files []ContextFile
	if options := cfg.Config().Options; options != nil {
		files = loader.load(options.ContextPaths, cfg, "project")
		files = append(files, loader.load(options.GlobalContextPaths, cfg, "global")...)
	}
	for _, path := range paths {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if _, err := engineering.ReadProjectEvidence(ctx, cfg.WorkingDir(), path); err != nil {
			continue
		}
		files = append(files, loader.load([]string{path}, cfg, "task")...)
	}
	return files, nil
}

// ContextLoader shares its budget and deduplication across both scopes.
type contextLoader struct {
	remaining  int
	count      int
	seen       map[string]bool
	limited    bool
	visits     int
	discovered int
}

func newContextLoader() *contextLoader {
	return &contextLoader{remaining: contextTotalLimit, seen: make(map[string]bool)}
}

func canonicalContextPath(p string) string {
	p, _ = filepath.Abs(p)
	p = filepath.Clean(p)
	if runtime.GOOS == "windows" {
		p = strings.ToLower(p)
	}
	return p
}

func readContextFile(p string, limit int) *ContextFile {
	info, err := os.Lstat(p)
	if err != nil || !info.Mode().IsRegular() || limit <= len(contextTruncation) {
		return nil
	}
	f, err := os.Open(p)
	if err != nil {
		return nil
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, int64(limit)+1))
	if err != nil || bytes.IndexByte(b, 0) >= 0 {
		return nil
	}
	truncated := len(b) > limit
	// Only an incomplete trailing rune is allowed at the read boundary.
	for i := 0; i < len(b); {
		r, size := utf8.DecodeRune(b[i:])
		if r == utf8.RuneError && size == 1 {
			if truncated && !utf8.FullRune(b[i:]) {
				b = b[:i]
				break
			}
			return nil
		}
		i += size
	}
	if truncated {
		n := limit - len(contextTruncation)
		if n > len(b) {
			n = len(b)
		}
		b = b[:n]
		for !utf8.Valid(b) {
			b = b[:len(b)-1]
		}
		b = append(b, contextTruncation...)
	}
	return &ContextFile{Path: p, Content: string(b)}
}

func ignoredContextDir(name string) bool {
	switch strings.ToLower(name) {
	case ".git", "node_modules", "vendor", "dist", "build", "__pycache__":
		return true
	}
	return false
}

func secretContextFile(name string) bool {
	name = strings.ToLower(name)
	return (strings.HasPrefix(name, ".env") && !strings.HasSuffix(name, ".example")) || strings.HasSuffix(name, ".pem") || strings.HasSuffix(name, ".key") || name == "id_rsa" || name == "id_ed25519"
}

func (l *contextLoader) load(paths []string, store *config.ConfigStore, scope string) []ContextFile {
	var result []ContextFile
	for _, configured := range paths {
		p := filepathext.SmartJoin(store.WorkingDir(), expandPath(configured, store))
		info, err := os.Lstat(p)
		if err != nil || info.Mode()&os.ModeSymlink != 0 {
			continue
		}
		candidates := []string{p}
		if info.IsDir() {
			candidates = l.directoryCandidates(p)
		}
		for _, candidate := range candidates {
			if l.count >= contextCountLimit || l.remaining <= len(contextTruncation) {
				l.limited = true
				return result
			}
			key := canonicalContextPath(candidate)
			if l.seen[key] {
				continue
			}
			l.seen[key] = true
			f := readContextFile(candidate, min(contextFileLimit, l.remaining))
			if f == nil {
				continue
			}
			f.Origin = configured
			f.Scope = scope
			result = append(result, *f)
			l.remaining -= len(f.Content)
			l.count++
		}
	}
	return result
}

const (
	contextVisitLimit     = 8192
	contextCandidateLimit = 4096
	contextDepthLimit     = 32
)

// Directory candidates are breadth-first and lexical. If the visit budget
// cannot cover an entire directory, omit that directory rather than select
// a filesystem-order-dependent prefix. Known root instructions still load.
func (l *contextLoader) directoryCandidates(root string) []string {
	var candidates []string
	for _, name := range []string{"AGENTS.md", "AGENTS.local.md", "ATLAS-AGENT.md", "ATLAS-AGENT.local.md", "CLAUDE.md", "CLAUDE.local.md", "GEMINI.md", "GEMINI.local.md"} {
		path := filepath.Join(root, name)
		if info, err := os.Lstat(path); err == nil && info.Mode().IsRegular() {
			candidates = append(candidates, path)
		}
	}
	type directory struct {
		path  string
		depth int
	}
	queue := []directory{{root, 0}}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		if ignoredContextDir(filepath.Base(current.path)) {
			continue
		}
		entries, complete := l.boundedDirectoryEntries(current.path)
		if !complete {
			continue
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
		for _, entry := range entries {
			path := filepath.Join(current.path, entry.Name())
			if entry.Type()&os.ModeSymlink != 0 {
				continue
			}
			if entry.IsDir() {
				if ignoredContextDir(entry.Name()) {
					continue
				}
				if current.depth >= contextDepthLimit {
					l.limited = true
					continue
				}
				queue = append(queue, directory{path, current.depth + 1})
			} else if !secretContextFile(entry.Name()) {
				if l.discovered >= contextCandidateLimit {
					l.limited = true
					return candidates
				}
				candidates = append(candidates, path)
				l.discovered++
			}
		}
	}
	return candidates
}

func (l *contextLoader) boundedDirectoryEntries(path string) ([]os.DirEntry, bool) {
	if l.visits >= contextVisitLimit {
		l.limited = true
		return nil, false
	}
	directory, err := os.Open(path)
	if err != nil {
		return nil, false
	}
	defer directory.Close()
	var entries []os.DirEntry
	for {
		// One lookahead entry detects a directory exceeding the remaining budget.
		batch, err := directory.ReadDir(min(128, contextVisitLimit-l.visits+1))
		l.visits += len(batch)
		if l.visits > contextVisitLimit {
			l.limited = true
			return nil, false
		}
		entries = append(entries, batch...)
		if err == io.EOF {
			return entries, true
		}
		if err != nil {
			return nil, false
		}
	}
}
