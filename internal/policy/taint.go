package policy

import (
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// Taint remembers files that came from the network, and files copied,
// moved or extracted from them. Running a tainted file is refused until the
// user clears it, because the agent has not necessarily read what it
// fetched. State can be saved and restored so it survives compaction.
type Taint struct {
	mu    sync.Mutex
	paths map[string]bool
}

// NewTaint returns an empty tracker.
func NewTaint() *Taint { return &Taint{paths: map[string]bool{}} }

// Snapshot lists the tainted paths, sorted.
func (t *Taint) Snapshot() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]string, 0, len(t.paths))
	for p := range t.paths {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// Restore replaces the tracker's state.
func (t *Taint) Restore(paths []string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.paths = map[string]bool{}
	for _, p := range paths {
		t.paths[p] = true
	}
}

// Clear removes a path (and anything under it) after the user has
// reviewed it.
func (t *Taint) Clear(p, dir string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	p = norm(p, dir)
	for k := range t.paths {
		if k == p || strings.HasPrefix(k, p+"/") {
			delete(t.paths, k)
		}
	}
}

// Tainted reports whether p, or a directory containing it, is tainted.
func (t *Taint) Tainted(p, dir string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.taintedLocked(norm(p, dir))
}

func (t *Taint) taintedLocked(p string) bool {
	for k := range t.paths {
		if p == k || strings.HasPrefix(p, k+"/") {
			return true
		}
	}
	return false
}

func norm(p, dir string) string {
	p = strings.ReplaceAll(p, `\`, "/")
	if !path.IsAbs(p) && !(len(p) > 1 && p[1] == ':') && dir != "" {
		p = path.Join(strings.ReplaceAll(dir, `\`, "/"), p)
	}
	p = path.Clean(p)
	return strings.ToLower(filepath.ToSlash(p))
}

// observe records what a call fetches or derives. Callers hold the engine
// lock; the tracker has its own for direct use.
func (t *Taint) observe(c Call, dir string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	args := c.Args()
	switch c.Name() {
	case "curl":
		for i, a := range args {
			switch {
			case (a == "-o" || a == "--output") && i+1 < len(args):
				t.paths[norm(args[i+1], dir)] = true
			case a == "-O" || a == "--remote-name":
				for _, u := range args {
					if strings.HasPrefix(u, "http") {
						t.paths[norm(path.Base(trimQuery(u)), dir)] = true
					}
				}
			}
		}
	case "wget":
		out := ""
		for i, a := range args {
			if (a == "-O" || a == "--output-document") && i+1 < len(args) {
				out = args[i+1]
			}
		}
		if out != "" {
			t.paths[norm(out, dir)] = true
			return
		}
		for _, u := range args {
			if strings.HasPrefix(u, "http") {
				t.paths[norm(path.Base(trimQuery(u)), dir)] = true
			}
		}
	case "iwr", "invoke-webrequest", "irm", "invoke-restmethod":
		for i, a := range args {
			if strings.EqualFold(a, "-OutFile") && i+1 < len(args) {
				t.paths[norm(args[i+1], dir)] = true
			}
		}
	case "cp", "mv", "install", "copy", "move", "ln":
		files := positional(args)
		if len(files) >= 2 {
			dst := files[len(files)-1]
			for _, src := range files[:len(files)-1] {
				if t.taintedLocked(norm(src, dir)) {
					t.paths[norm(path.Join(dst, path.Base(src)), dir)] = true
					t.paths[norm(dst, dir)] = true
				}
			}
		}
	case "tar", "unzip", "7z", "7za", "gunzip", "expand-archive":
		files := positional(args)
		dest := dir
		for i, a := range args {
			if (a == "-C" || a == "-d" || a == "--directory" || strings.HasPrefix(a, "-o")) && i+1 < len(args) {
				dest = args[i+1]
				if strings.HasPrefix(a, "-o") && len(a) > 2 {
					dest = a[2:]
				}
			}
		}
		for _, f := range files {
			if t.taintedLocked(norm(f, dir)) {
				t.paths[norm(dest, dir)] = true
			}
		}
	case "chmod":
		// Making a tainted file executable is judged by check; no new taint.
	}
}

func trimQuery(u string) string {
	if i := strings.IndexAny(u, "?#"); i >= 0 {
		return u[:i]
	}
	return u
}

func positional(args []string) []string {
	var out []string
	for _, a := range args {
		if !strings.HasPrefix(a, "-") {
			out = append(out, a)
		}
	}
	return out
}

// check finds attempts to run or install a tainted file.
func (t *Taint) check(c Call, dir string) []Finding {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(c.Argv) == 0 {
		return nil
	}
	find := func(what, file string) Finding {
		return Finding{
			RuleID: "run-downloaded-file", Action: Deny, Enforced: true,
			Reason: what + " " + file + ", which came from the network and has not been reviewed.",
			Remedy: "Read the file first, then have the user clear it.", Command: strings.Join(c.Argv, " "),
		}
	}
	name := c.Argv[0]
	if (strings.ContainsAny(name, `/\`) || strings.HasPrefix(name, ".")) && t.taintedLocked(norm(name, dir)) {
		return []Finding{find("This runs", name)}
	}
	var out []Finding
	if interpreters[c.Name()] || c.Name() == "source" || c.Name() == "." || c.Name() == "start" || c.Name() == "powershell" || c.Name() == "pwsh" {
		for _, a := range c.Args() {
			if !strings.HasPrefix(a, "-") && t.taintedLocked(norm(a, dir)) {
				out = append(out, find("This runs", a))
				break
			}
		}
	}
	if c.Name() == "chmod" {
		a := c.Args()
		for _, x := range positional(a) {
			if strings.ContainsAny(x, "x") && !strings.ContainsAny(x, "/.") {
				continue
			}
			if t.taintedLocked(norm(x, dir)) && hasExecBit(a) {
				out = append(out, find("This makes executable", x))
			}
		}
	}
	return out
}

func hasExecBit(args []string) bool {
	for _, a := range args {
		if a == "+x" || a == "a+x" || a == "u+x" || a == "755" || a == "700" || a == "777" {
			return true
		}
	}
	return false
}
