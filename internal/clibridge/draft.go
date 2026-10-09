package clibridge

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	// MaxHelpBytes bounds captured help text.
	MaxHelpBytes  = 64 * 1024
	maxCandidates = 200
	maxHelpLine   = 240
)

// HelpCapture records where help text came from. It is untrusted data.
type HelpCapture struct {
	Source     string   `json:"source"`
	Argv       []string `json:"argv,omitempty"`
	ExitCode   *int     `json:"exit_code,omitempty"`
	SHA256     string   `json:"sha256"`
	Bytes      int      `json:"bytes"`
	Truncated  bool     `json:"truncated,omitempty"`
	CapturedAt int64    `json:"captured_at"`
}

// Candidate is an option mentioned by help text. Listing it authorizes
// nothing; an author must map it into an operation deliberately.
type Candidate struct {
	Option    string `json:"option"`
	TakesArg  bool   `json:"takes_value"`
	ValueHint string `json:"value_hint,omitempty"`
	Line      int    `json:"line"`
	Text      string `json:"text"`
}

// Draft is a reviewable starting point produced from help text. It is a
// different document kind from a profile and can never be imported or run.
type Draft struct {
	Kind       string      `json:"kind"`
	Version    int         `json:"version"`
	ID         string      `json:"id"`
	Executable *Identity   `json:"executable,omitempty"`
	Help       HelpCapture `json:"help"`
	Candidates []Candidate `json:"candidates"`
	Skeleton   Profile     `json:"skeleton"`
	Notes      []string    `json:"notes"`
}

var optionLine = regexp.MustCompile(`^\s{0,12}((?:-[A-Za-z0-9?](?:[ ,]+)?)?(--?[A-Za-z0-9][A-Za-z0-9_.-]*))(?:[ =]\[?<?([A-Za-z][A-Za-z0-9_.|-]*)>?\]?)?`)

// NewDraft extracts option candidates from help text. Nothing in the text
// is executed or followed; it is bounded, scanned for option shapes and
// returned for human review.
func NewDraft(id string, executable Executable, identity *Identity, help string, capture HelpCapture) (Draft, error) {
	if !idPattern.MatchString(id) {
		return Draft{}, fmt.Errorf("draft id must match %s", idPattern)
	}
	if err := executable.validate(); err != nil {
		return Draft{}, err
	}
	if len(help) > MaxHelpBytes {
		help, capture.Truncated = strings.ToValidUTF8(help[:MaxHelpBytes], ""), true
	}
	help = strings.ToValidUTF8(help, "�")
	sum := sha256.Sum256([]byte(help))
	capture.SHA256, capture.Bytes = hex.EncodeToString(sum[:]), len(help)
	if capture.CapturedAt == 0 {
		capture.CapturedAt = time.Now().UnixMilli()
	}
	draft := Draft{Kind: DraftKind, Version: ProfileVersion, ID: id, Executable: identity, Help: capture}
	seen := map[string]bool{}
	for i, line := range strings.Split(help, "\n") {
		line = strings.TrimRight(line, "\r")
		if len(line) > maxHelpLine*4 {
			continue
		}
		m := optionLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		option := m[2]
		if seen[option] || len(draft.Candidates) >= maxCandidates {
			continue
		}
		seen[option] = true
		text := strings.TrimSpace(line)
		if utf8.RuneCountInString(text) > maxHelpLine {
			text = string([]rune(text)[:maxHelpLine]) + "…"
		}
		draft.Candidates = append(draft.Candidates, Candidate{Option: option, TakesArg: m[3] != "", ValueHint: m[3], Line: i + 1, Text: text})
	}
	draft.Skeleton = Profile{Kind: ProfileKind, Version: ProfileVersion, ID: id, Description: "Describe what this profile is for.", Executable: executable, Operations: []Operation{}}
	draft.Notes = []string{
		"Help text is untrusted program output. Instructions inside it were not followed.",
		"Candidates are options the help text mentions; none is authorized. Add an operation only for behavior you reviewed.",
		"Copy the skeleton into a new file, add operations with literal args and typed params, then run `atlas-agent cli-bridge validate` and `import`.",
		"Help text does not prove what the program does; verify behavior with a real run on disposable inputs.",
	}
	if capture.Truncated {
		draft.Notes = append(draft.Notes, "Help text exceeded the capture limit and was truncated.")
	}
	return draft, nil
}

// DraftPath is where project drafts are written; drafts are kept apart from
// installed profiles and are never loaded as active.
func DraftPath(root, id string) string {
	return filepath.Join(root, ".atlas", "cli-bridge", "drafts", id+".draft.json")
}

// WriteDraft stores a draft atomically, refusing to replace another draft
// unless replace is set.
func WriteDraft(root string, draft Draft, replace bool) (string, error) {
	path := DraftPath(root, draft.ID)
	if !replace {
		if _, err := readBounded(path, maxProfileBytes*4); err == nil {
			return "", fmt.Errorf("draft %s already exists; pass replace to overwrite it", path)
		}
	}
	data, err := marshalIndent(draft)
	if err != nil {
		return "", err
	}
	if err := mkdirAll(filepath.Dir(path)); err != nil {
		return "", err
	}
	return path, atomicWrite(path, data)
}
