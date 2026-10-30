package rehearsal

import (
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/codeintel"
)

// apiDiff compares the exported Go surface of the packages the change
// touched, before (the real tree) and after (the copy). It returns nil
// when no Go file changed.
func apiDiff(root, work string, changes []change) *APIDiff {
	dirs := map[string]bool{}
	for _, c := range changes {
		if strings.HasSuffix(c.Path, ".go") && !strings.HasSuffix(c.Path, "_test.go") {
			dirs[path.Dir(c.Path)] = true
		}
	}
	if len(dirs) == 0 {
		return nil
	}
	before := surface(root, dirs)
	after := surface(work, dirs)
	d := &APIDiff{}
	for key, sig := range after {
		old, ok := before[key]
		switch {
		case !ok:
			d.Added = append(d.Added, label(key, sig))
		case old != sig:
			d.Changed = append(d.Changed, label(key, old)+"  ->  "+sig)
		}
	}
	for key, sig := range before {
		if _, ok := after[key]; !ok {
			d.Removed = append(d.Removed, label(key, sig))
		}
	}
	sort.Strings(d.Added)
	sort.Strings(d.Removed)
	sort.Strings(d.Changed)
	return d
}

func label(key, sig string) string {
	dir, _, _ := strings.Cut(key, "|")
	return dir + ": " + sig
}

// surface maps "dir|kind|recv|name" to the declaration for the given
// package directories (slash-relative to root).
func surface(root string, dirs map[string]bool) map[string]string {
	out := map[string]string{}
	result, err := codeintel.APISurface(root, false)
	if err != nil {
		return out
	}
	for _, pkg := range result.Packages {
		rel, err := filepath.Rel(root, pkg.Dir)
		if err != nil || !dirs[filepath.ToSlash(rel)] {
			continue
		}
		for _, s := range pkg.Symbols {
			out[filepath.ToSlash(rel)+"|"+s.Kind+"|"+s.Recv+"|"+s.Name] = s.Signature
		}
	}
	return out
}
