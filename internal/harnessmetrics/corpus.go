package harnessmetrics

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// The failure corpus keeps the requests that ended badly in real use: the
// verification contract still unmet after the round limit, or evaluator
// findings left unresolved. Each case holds what is needed to replay the
// request as a bench task (see internal/evaluation): the request, the end
// state of the project as a git commit kept under refs/atlas/corpus/, the
// files the request changed as they were before it, and the checks that
// judged it. Benchmarks built from these cases measure a harness change
// against the work it actually struggles with, not against invented tasks.

const (
	// CorpusDir holds the cases under the harness directory.
	CorpusDir = "corpus"
	// CorpusRef prefixes the git refs that keep each case's commit.
	CorpusRef = "refs/atlas/corpus/"

	maxCases       = 200
	maxCaseRequest = 16 << 10
)

// CaseFile is one file the request changed, as it was before the request.
type CaseFile struct {
	Path    string `json:"path"`
	Before  string `json:"before,omitempty"`
	Existed bool   `json:"existed"`
	// Unknown marks a file whose earlier content was too large to keep;
	// a replay keeps its end state.
	Unknown bool `json:"unknown,omitempty"`
}

// Case is one saved request.
type Case struct {
	ID         string     `json:"id"`
	Time       int64      `json:"time"`
	Reason     string     `json:"reason"`
	Request    string     `json:"request"`
	WorkingDir string     `json:"working_dir"`
	Commit     string     `json:"commit,omitempty"`
	Checks     []string   `json:"checks,omitempty"`
	Findings   string     `json:"findings,omitempty"`
	Files      []CaseFile `json:"files,omitempty"`
}

// NewCaseID returns a sortable id for a case saved now.
func NewCaseID(now time.Time, session string) string {
	session = strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, strings.ToLower(session))
	return now.UTC().Format("20060102-150405") + "-" + session[:min(len(session), 8)]
}

// SaveCase writes c under root, dropping the oldest cases beyond the limit.
func SaveCase(root string, c Case) error {
	if root == "" || c.ID == "" {
		return errors.New("corpus case needs a data directory and an id")
	}
	if len(c.Request) > maxCaseRequest {
		c.Request = c.Request[:maxCaseRequest]
	}
	dir := filepath.Join(root, Dir, CorpusDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, c.ID+".json"), data, 0o644); err != nil {
		return err
	}
	names, err := caseFiles(dir)
	if err != nil {
		return nil //nolint:nilerr // The case is saved; pruning is best effort.
	}
	for _, name := range names[:max(0, len(names)-maxCases)] {
		_ = os.Remove(filepath.Join(dir, name))
	}
	return nil
}

func caseFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") {
			names = append(names, e.Name())
		}
	}
	slices.Sort(names)
	return names, nil
}

// LoadCases reads the saved cases, oldest first. Unreadable files are
// skipped.
func LoadCases(root string) ([]Case, error) {
	dir := filepath.Join(root, Dir, CorpusDir)
	names, err := caseFiles(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var cases []Case
	for _, name := range names {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		var c Case
		if json.Unmarshal(data, &c) == nil && c.ID != "" {
			cases = append(cases, c)
		}
	}
	return cases, nil
}

// LoadCase reads one case by id.
func LoadCase(root, id string) (Case, error) {
	if strings.ContainsAny(id, `/\`) || strings.Contains(id, "..") {
		return Case{}, fmt.Errorf("invalid case id %q", id)
	}
	data, err := os.ReadFile(filepath.Join(root, Dir, CorpusDir, id+".json"))
	if err != nil {
		return Case{}, err
	}
	var c Case
	if err := json.Unmarshal(data, &c); err != nil {
		return Case{}, fmt.Errorf("case %s: %w", id, err)
	}
	return c, nil
}
