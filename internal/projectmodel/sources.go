package projectmodel

import (
	"context"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/docindex"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/factstore"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/gitx"
)

// Limits on what is read from outside the code map.
const (
	maxHistoryCommits = 2000
	maxOwnersPerMod   = 3
	maxDocs           = 60
	maxDocDepth       = 3
	maxADRs           = 30
	ownerTimeout      = 20 * time.Second
	// DecisionTag marks a fact as a project decision.
	DecisionTag = "decision"
)

// repoHead returns the commit the working tree is at, or "".
func repoHead(ctx context.Context, root string) string {
	out, err := gitx.Run(ctx, root, "rev-parse", "HEAD")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// readOwners returns each module's most frequent committers, from one pass
// over recent history. Files that no longer exist are ignored.
func readOwners(ctx context.Context, root string, files map[string]string, modules []Module) map[string][]Owner {
	ctx, cancel := context.WithTimeout(ctx, ownerTimeout)
	defer cancel()
	out, err := gitx.Run(ctx, root, "-c", "core.quotepath=false", "log", "--no-merges", "--relative",
		"-n", strconv.Itoa(maxHistoryCommits), "--name-only", "--format=%x1e%aN")
	if err != nil || out == "" {
		return nil
	}
	moduleAt := make(map[string]bool, len(modules))
	for _, mod := range modules {
		moduleAt[mod.Path] = true
	}
	moduleOf := func(file string) string {
		dir := slashDir(file)
		for {
			if moduleAt[dir] {
				return dir
			}
			if dir == "." || dir == "" || dir == "/" {
				return ""
			}
			dir = slashDir(dir)
		}
	}
	counts := map[string]map[string]int{}
	totals := map[string]int{}
	for _, record := range strings.Split(out, "\x1e") {
		lines := strings.Split(strings.ReplaceAll(record, "\r", ""), "\n")
		if len(lines) < 2 || strings.TrimSpace(lines[0]) == "" {
			continue
		}
		author := strings.TrimSpace(lines[0])
		touched := map[string]bool{}
		for _, file := range lines[1:] {
			file = strings.TrimSpace(file)
			if file == "" {
				continue
			}
			if _, ok := files[file]; !ok {
				continue
			}
			if mod := moduleOf(file); mod != "" {
				touched[mod] = true
			}
		}
		for mod := range touched {
			if counts[mod] == nil {
				counts[mod] = map[string]int{}
			}
			counts[mod][author]++
			totals[mod]++
		}
	}
	owners := make(map[string][]Owner, len(counts))
	for mod, byAuthor := range counts {
		list := make([]Owner, 0, len(byAuthor))
		for name, n := range byAuthor {
			list = append(list, Owner{Name: name, Commits: n, Share: n * 100 / totals[mod]})
		}
		sort.Slice(list, func(i, j int) bool {
			if list[i].Commits != list[j].Commits {
				return list[i].Commits > list[j].Commits
			}
			return list[i].Name < list[j].Name
		})
		if len(list) > maxOwnersPerMod {
			list = list[:maxOwnersPerMod]
		}
		owners[mod] = list
	}
	return owners
}

// readDocs indexes the project's Markdown documents. Only documents the
// code map listed are kept, so ignored and generated folders stay out.
func readDocs(root string, files map[string]string) []Doc {
	res, err := docindex.Build(root, docindex.Options{MaxDepth: 1})
	if err != nil {
		return nil
	}
	var docs []Doc
	for _, f := range res.Files {
		rel, err := filepath.Rel(root, f.Path)
		if err != nil {
			continue
		}
		rel = filepath.ToSlash(rel)
		if _, ok := files[rel]; !ok {
			continue
		}
		adr := isADR(rel)
		if !adr && strings.Count(rel, "/") >= maxDocDepth {
			continue
		}
		docs = append(docs, Doc{Path: rel, Title: f.Title, ADR: adr})
	}
	sort.Slice(docs, func(i, j int) bool { return docs[i].Path < docs[j].Path })
	if len(docs) > maxDocs {
		docs = docs[:maxDocs]
	}
	return docs
}

// isADR reports whether a document path looks like a decision record.
func isADR(rel string) bool {
	lower := strings.ToLower(rel)
	base := path.Base(lower)
	for _, seg := range strings.Split(path.Dir(lower), "/") {
		if seg == "adr" || seg == "adrs" || seg == "decisions" || seg == "decision-records" {
			return base != "readme.md" && base != "index.md"
		}
	}
	return strings.HasPrefix(base, "adr-")
}

// docsChanged reports whether the set of Markdown files, or any of their
// contents, differs between two file maps.
func docsChanged(prev, cur map[string]string) bool {
	count := 0
	for p, h := range cur {
		if !isMarkdown(p) {
			continue
		}
		count++
		if prev[p] != h {
			return true
		}
	}
	for p := range prev {
		if isMarkdown(p) {
			count--
		}
	}
	return count != 0
}

func isMarkdown(p string) bool {
	lower := strings.ToLower(p)
	return strings.HasSuffix(lower, ".md") || strings.HasSuffix(lower, ".mdx")
}

var (
	decisionFilesRe = regexp.MustCompile(`\s*\[files: ([^\]]*)\]\s*$`)
	decisionWhySep  = " -- why: "
)

// FormatDecision renders a decision the way it is stored in the fact store,
// where it stays readable to the facts tool.
func FormatDecision(text, rationale string, files []string) string {
	var b strings.Builder
	b.WriteString(strings.TrimSpace(text))
	if r := strings.TrimSpace(rationale); r != "" {
		b.WriteString(decisionWhySep + r)
	}
	if len(files) > 0 {
		b.WriteString(" [files: " + strings.Join(files, ", ") + "]")
	}
	return b.String()
}

// parseDecision reverses FormatDecision. Text recorded by other means is a
// decision with only text.
func parseDecision(id, raw string) Decision {
	d := Decision{ID: id, Source: "fact"}
	if m := decisionFilesRe.FindStringSubmatchIndex(raw); m != nil {
		for _, f := range strings.Split(raw[m[2]:m[3]], ",") {
			if f = strings.TrimSpace(f); f != "" {
				d.Files = append(d.Files, filepath.ToSlash(f))
			}
		}
		raw = raw[:m[0]]
	}
	text, why, _ := strings.Cut(raw, decisionWhySep)
	d.Text, d.Rationale = strings.TrimSpace(text), strings.TrimSpace(why)
	return d
}

// readDecisions returns the decisions recorded in the fact store, oldest
// first.
func readDecisions(store *factstore.Store) []Decision {
	if store == nil {
		return nil
	}
	facts, err := store.Recall(DecisionTag, 10000)
	if err != nil {
		return nil
	}
	var kept []factstore.Fact
	for _, f := range facts {
		for _, tag := range f.Tags {
			if tag == DecisionTag {
				kept = append(kept, f.Fact)
				break
			}
		}
	}
	sort.SliceStable(kept, func(i, j int) bool {
		if !kept[i].CreatedAt.Equal(kept[j].CreatedAt) {
			return kept[i].CreatedAt.Before(kept[j].CreatedAt)
		}
		return kept[i].ID < kept[j].ID
	})
	out := make([]Decision, 0, len(kept))
	for _, f := range kept {
		out = append(out, parseDecision(f.ID, f.Text))
	}
	return out
}

// adrDecisions turns decision record documents into decisions.
func adrDecisions(docs []Doc) []Decision {
	var out []Decision
	for _, d := range docs {
		if d.ADR && len(out) < maxADRs {
			out = append(out, Decision{Text: d.Title, Source: "adr", Path: d.Path})
		}
	}
	return out
}
