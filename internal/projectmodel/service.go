package projectmodel

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/codemap"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/factstore"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/gitx"
)

// Service builds, refreshes and stores the model of one project.
type Service struct {
	root    string
	dataDir string
	mapper  *codemap.Builder
	facts   *factstore.Store
}

// NewService returns a service for the project at root. mapper keeps the
// code map between calls and may be shared with the code map tool; nil
// creates a private one cached under dataDir. facts supplies recorded
// decisions and receives new ones; nil disables them.
func NewService(root, dataDir string, mapper *codemap.Builder, facts *factstore.Store) *Service {
	if abs, err := filepath.Abs(root); err == nil {
		root = abs
	}
	if !filepath.IsAbs(dataDir) {
		dataDir = filepath.Join(root, dataDir)
	}
	if mapper == nil {
		mapper = codemap.NewBuilder(filepath.Join(dataDir, "codemap"))
	}
	return &Service{root: root, dataDir: dataDir, mapper: mapper, facts: facts}
}

// Report says what a refresh did.
type Report struct {
	// Rebuilt is false when nothing the model depends on had changed.
	Rebuilt bool `json:"rebuilt"`
	First   bool `json:"first,omitempty"`
	Added   int  `json:"added"`
	Changed int  `json:"changed"`
	Removed int  `json:"removed"`
	// Reused names the expensive parts carried over from the previous
	// model because their inputs had not changed.
	Reused  []string      `json:"reused,omitempty"`
	Elapsed time.Duration `json:"elapsed_ns"`
}

// String is a one-line description.
func (r Report) String() string {
	switch {
	case !r.Rebuilt:
		return "Project model is up to date."
	case r.First:
		return fmt.Sprintf("Project model built in %s.", r.Elapsed.Round(time.Millisecond))
	}
	s := fmt.Sprintf("Project model refreshed in %s: %d added, %d changed, %d removed.",
		r.Elapsed.Round(time.Millisecond), r.Added, r.Changed, r.Removed)
	if len(r.Reused) > 0 {
		s += " Reused " + strings.Join(r.Reused, ", ") + "."
	}
	return s
}

// Root returns the project directory.
func (s *Service) Root() string { return s.root }

// DataDir returns where the model is stored.
func (s *Service) DataDir() string { return s.dataDir }

// Load returns the stored model without checking it against the project, or
// nil when there is none.
func (s *Service) Load() (*Model, error) { return Load(s.dataDir) }

// Refresh brings the model up to date and stores it. Only what changed is
// recomputed from scratch: the code map re-reads edited files, the history
// is read again only when the checked-out commit moved, and documents are
// indexed again only when a Markdown file changed. force rebuilds all of it.
func (s *Service) Refresh(ctx context.Context, force bool) (*Model, Report, error) {
	start := time.Now()
	prev, err := Load(s.dataDir)
	if err != nil {
		// A damaged file is replaced, not an obstacle.
		prev = nil
	}
	cm, err := s.mapper.Map(ctx, s.root)
	if err != nil {
		return nil, Report{}, fmt.Errorf("cannot map the project: %w", err)
	}

	files := make(map[string]string, len(cm.Files))
	for _, f := range cm.Files {
		if cat := codemap.Category(f.Language); cat == codemap.CategoryAsset || f.Language == "" {
			continue
		}
		files[f.Path] = shortHash(f.Hash)
	}
	head := repoHead(ctx, s.root)
	decisions := readDecisions(s.facts)
	fingerprint := fingerprintOf(files, head, decisions)

	if prev != nil && !force && prev.Fingerprint == fingerprint {
		return prev, Report{}, nil
	}

	report := Report{Rebuilt: true, First: prev == nil}
	if prev != nil {
		for p, h := range files {
			old, ok := prev.Files[p]
			switch {
			case !ok:
				report.Added++
			case old != h:
				report.Changed++
			}
		}
		for p := range prev.Files {
			if _, ok := files[p]; !ok {
				report.Removed++
			}
		}
	}

	d := derive(cm, s.root)
	m := &Model{
		Version:     Version,
		Head:        head,
		Fingerprint: fingerprint,
		GoModule:    codemap.ModulePath(s.root),
		Files:       files,
		Languages:   d.langs,
		Modules:     d.modules,
		Entries:     d.entries,
		Edges:       d.edges,
		TestMap:     d.testMap,
	}
	m.TestCommands = detectTestCommands(s.root, d.langs["python"] > 0)

	// Ownership: the history only moves when the commit does.
	switch {
	case head == "":
	case prev != nil && !force && prev.Head == head:
		carryOwners(m, prev)
		report.Reused = append(report.Reused, "ownership")
	default:
		if owners := readOwners(ctx, s.root, files, m.Modules); owners != nil {
			for i := range m.Modules {
				m.Modules[i].Owners = owners[m.Modules[i].Path]
			}
		}
	}

	// Documents: only when a Markdown file changed.
	if prev != nil && !force && !docsChanged(prev.Files, files) {
		m.Docs = prev.Docs
		report.Reused = append(report.Reused, "documents")
	} else {
		m.Docs = readDocs(s.root, files)
	}
	m.Decisions = append(adrDecisions(m.Docs), decisions...)

	m.BuiltAt = time.Now().UTC().Format(time.RFC3339)
	if err := Save(s.dataDir, m); err != nil {
		return nil, Report{}, fmt.Errorf("cannot store the project model: %w", err)
	}
	report.Elapsed = time.Since(start)
	return m, report, nil
}

// carryOwners copies ownership of modules that exist in both models.
func carryOwners(m, prev *Model) {
	byPath := make(map[string][]Owner, len(prev.Modules))
	for _, mod := range prev.Modules {
		byPath[mod.Path] = mod.Owners
	}
	for i := range m.Modules {
		m.Modules[i].Owners = byPath[m.Modules[i].Path]
	}
}

// Current returns a model that matches the project as it is now.
func (s *Service) Current(ctx context.Context) (*Model, error) {
	m, _, err := s.Refresh(ctx, false)
	return m, err
}

// RecordDecision stores a decision so the model, and the facts tool, can
// find it. files scopes it to paths; a directory scopes it to everything
// under it.
func (s *Service) RecordDecision(text, rationale string, files []string) (Decision, error) {
	if s.facts == nil {
		return Decision{}, errors.New("decisions cannot be recorded: no fact store is available")
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return Decision{}, errors.New("a decision needs text")
	}
	var scoped []string
	for _, f := range files {
		if f = strings.TrimSpace(filepath.ToSlash(f)); f != "" {
			scoped = append(scoped, strings.TrimPrefix(f, "./"))
		}
	}
	fact, err := s.facts.Retain(FormatDecision(text, rationale, scoped), []string{DecisionTag})
	if err != nil {
		return Decision{}, err
	}
	return parseDecision(fact.ID, fact.Text), nil
}

// ChangedFiles lists the files with uncommitted changes, relative to the
// project root, or nil without a repository.
func (s *Service) ChangedFiles(ctx context.Context) []string {
	st, err := gitx.GetStatus(ctx, s.root)
	if err != nil {
		return nil
	}
	prefix := repoPrefix(ctx, s.root)
	// The model's own data is never a change to the project.
	own := ""
	if rel, err := filepath.Rel(s.root, s.dataDir); err == nil && !strings.HasPrefix(rel, "..") {
		own = filepath.ToSlash(rel)
	}
	seen := map[string]bool{}
	var out []string
	for _, f := range st.Files {
		p := filepath.ToSlash(f.Path)
		if prefix != "" {
			if !strings.HasPrefix(p, prefix) {
				continue
			}
			p = strings.TrimPrefix(p, prefix)
		}
		if f.Unstaged == "deleted" || f.Staged == "deleted" {
			continue
		}
		if own != "" && (strings.TrimSuffix(p, "/") == own || strings.HasPrefix(p, own+"/")) {
			continue
		}
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out
}

// repoPrefix is the project directory's path inside its repository, with a
// trailing slash, or "" when it is the repository root.
func repoPrefix(ctx context.Context, root string) string {
	out, err := gitx.Run(ctx, root, "rev-parse", "--show-prefix")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

func shortHash(h string) string {
	if len(h) > 12 {
		return h[:12]
	}
	return h
}

func fingerprintOf(files map[string]string, head string, decisions []Decision) string {
	lines := make([]string, 0, len(files)+len(decisions)+2)
	for p, h := range files {
		lines = append(lines, p+"\x00"+h)
	}
	sort.Strings(lines)
	lines = append(lines, "head\x00"+head, fmt.Sprintf("version\x00%d", Version))
	for _, d := range decisions {
		lines = append(lines, "decision\x00"+d.ID+"\x00"+d.Text+"\x00"+d.Rationale+"\x00"+strings.Join(d.Files, ","))
	}
	sum := sha256.Sum256([]byte(strings.Join(lines, "\n")))
	return hex.EncodeToString(sum[:])
}
