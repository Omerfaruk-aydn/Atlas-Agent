// Package errmemory remembers how errors in a project were fixed. When a
// command fails and, after some edits, the same command passes, the failure's
// normalized signature is stored with a summary of those edits. When the same
// signature shows up again, in this session or a later one, the fix that
// worked last time is shown next to the failure, so the agent does not
// rediscover it from scratch.
//
// The store is a small JSON file in the project's .atlas directory. It is
// best effort: a missing or corrupt file is an empty memory, and nothing
// here is ever a reason for a command or a turn to fail.
package errmemory

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const (
	// File is the store's name inside the .atlas directory.
	File = "errors.json"
	dir  = ".atlas"

	maxEntries     = 100
	maxFixes       = 3
	maxFixChars    = 200
	maxSignature   = 200
	maxExample     = 240
	maxScanLines   = 400
	maxCommandChar = 200
)

// Entry is one remembered error and the changes that fixed it.
type Entry struct {
	Signature string   `json:"signature"`
	Example   string   `json:"example"`
	Command   string   `json:"command,omitempty"`
	Fixes     []string `json:"fixes"`
	Files     []string `json:"files,omitempty"`
	Fixed     int      `json:"fixed"`
	Last      int64    `json:"last"`
}

// mu serializes read-modify-write cycles of the store within the process.
var mu sync.Mutex

var (
	strongError = regexp.MustCompile(`(?i)(^|[\s:\[(])(error|err|fatal|panic|exception|traceback|failed|failure|undefined|segmentation fault|cannot|unexpected|unresolved|invalid|not found|no such file|permission denied|connection refused|syntaxerror|typeerror|referenceerror|valueerror|keyerror|importerror|modulenotfounderror|attributeerror|nameerror)\b[:\s\]]`)
	noise       = regexp.MustCompile(`(?i)^\s*(at |\d+ error|found \d+ error|npm err! (a complete log|this is probably)|--- fail|fail\s*$|exit status \d+|make: \*\*\*|error: process completed|command exited|\[?full output saved)`)
	pathPattern = regexp.MustCompile(`(?:[A-Za-z]:)?(?:[\w.~-]*[/\\])+[\w.-]+(?::\d+)*`)
	hexPattern  = regexp.MustCompile(`\b0x[0-9a-fA-F]+\b|\b[0-9a-f]{12,}\b`)
	numPattern  = regexp.MustCompile(`\b\d+(\.\d+)*\b`)
	timePattern = regexp.MustCompile(`(?i)\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}:\d{2}(\.\d+)?Z?|\d{2}:\d{2}:\d{2}(\.\d+)?`)
	spaces      = regexp.MustCompile(`\s+`)
)

// Signature finds the first error line in a failed command's output and
// returns it normalized (paths, numbers, addresses and times replaced), so
// the same error matches across runs, together with the line as printed.
// ok is false when the output holds no recognizable error line.
func Signature(output string) (signature, line string, ok bool) {
	lines := strings.Split(output, "\n")
	for _, raw := range lines[:min(len(lines), maxScanLines)] {
		text := strings.TrimSpace(strings.TrimRight(raw, "\r"))
		if text == "" || noise.MatchString(text) || !strongError.MatchString(" "+text) {
			continue
		}
		sig := strings.ToLower(text)
		sig = timePattern.ReplaceAllString(sig, "<time>")
		sig = pathPattern.ReplaceAllString(sig, "<path>")
		sig = hexPattern.ReplaceAllString(sig, "<hex>")
		sig = numPattern.ReplaceAllString(sig, "<n>")
		sig = strings.TrimSpace(spaces.ReplaceAllString(sig, " "))
		if len(sig) < 8 {
			continue
		}
		return clip(sig, maxSignature), clip(text, maxExample), true
	}
	return "", "", false
}

func path(root string) string { return filepath.Join(root, dir, File) }

func load(root string) []Entry {
	data, err := os.ReadFile(path(root))
	if err != nil {
		return nil
	}
	var entries []Entry
	if json.Unmarshal(data, &entries) != nil {
		return nil
	}
	return entries
}

func save(root string, entries []Entry) error {
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil && !errors.Is(err, fs.ErrExist) {
		return err
	}
	tmp := path(root) + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path(root))
}

// Lookup returns the remembered fix for signature.
func Lookup(root, signature string) (Entry, bool) {
	if root == "" || signature == "" {
		return Entry{}, false
	}
	mu.Lock()
	defer mu.Unlock()
	for _, e := range load(root) {
		if e.Signature == signature && len(e.Fixes) > 0 {
			return e, true
		}
	}
	return Entry{}, false
}

// Learn records that the edits summarized in fixes, to files, fixed the
// error with signature. The newest fix comes first; the least recently
// seen entries are dropped beyond the store's limit.
func Learn(root, signature, example, command string, fixes, files []string) error {
	if root == "" || signature == "" || len(fixes) == 0 {
		return nil
	}
	mu.Lock()
	defer mu.Unlock()
	entries := load(root)
	i := slices.IndexFunc(entries, func(e Entry) bool { return e.Signature == signature })
	if i < 0 {
		entries = append(entries, Entry{Signature: signature})
		i = len(entries) - 1
	}
	e := &entries[i]
	e.Example = clip(example, maxExample)
	e.Command = clip(command, maxCommandChar)
	clipped := make([]string, 0, len(fixes))
	for _, fix := range fixes[:min(len(fixes), maxFixes)] {
		clipped = append(clipped, clip(fix, maxFixChars))
	}
	e.Fixes = clipped
	e.Files = files[:min(len(files), 8)]
	e.Fixed++
	e.Last = time.Now().UnixMilli()
	slices.SortFunc(entries, func(a, b Entry) int {
		switch {
		case a.Last > b.Last:
			return -1
		case a.Last < b.Last:
			return 1
		default:
			return 0
		}
	})
	return save(root, entries[:min(len(entries), maxEntries)])
}

// Note renders a remembered fix for the agent.
func Note(e Entry) string {
	var b strings.Builder
	b.WriteString("Seen before in this project")
	if e.Fixed > 1 {
		b.WriteString(" (fixed " + strconv.Itoa(e.Fixed) + " times)")
	}
	b.WriteString(": `" + e.Example + "`")
	if e.Command != "" {
		b.WriteString(" from `" + e.Command + "`")
	}
	b.WriteString(". What fixed it last time:\n")
	for _, fix := range e.Fixes {
		b.WriteString("- " + fix + "\n")
	}
	b.WriteString("Check whether the same cause applies before trying something new.")
	return b.String()
}

func clip(s string, limit int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= limit {
		return s
	}
	cut := limit
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "…"
}
