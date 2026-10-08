package tools

import (
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

// Learned browser knowledge records which verified method worked for a
// kind of control on an origin, with dates and evidence. It is a hint for
// ordering attempts, never a substitute for observing the current page:
// every use is still verified by readback, and stale or contested entries
// are ignored. Page text is never stored, so nothing on a page can become
// an instruction through this store.

const (
	knowledgeStaleAfter = 45 * 24 * time.Hour
	maxKnowledgeEntries = 500
)

type knowledgeEntry struct {
	Origin    string    `json:"origin"`
	Kind      string    `json:"kind"`
	Label     string    `json:"label"`
	Method    string    `json:"method"`
	Success   int       `json:"success"`
	Failure   int       `json:"failure"`
	Platform  string    `json:"platform"`
	FirstSeen time.Time `json:"first_seen"`
	LastSeen  time.Time `json:"last_seen"`
	Evidence  []string  `json:"evidence,omitempty"`
}

var (
	knowledgeMu     sync.Mutex
	knowledgeDigits = regexp.MustCompile(`\d{4,}`)
	knowledgeEmail  = regexp.MustCompile(`[^\s@]+@[^\s@]+`)
)

func knowledgePath() string { return filepath.Join(".atlas", "browser", "knowledge.json") }

// sanitizeKnowledgeLabel keeps a control label short and free of personal
// identifiers before it is stored.
func sanitizeKnowledgeLabel(label string) string {
	label = knowledgeEmail.ReplaceAllString(label, "[email]")
	label = knowledgeDigits.ReplaceAllString(label, "#")
	return truncateRunes(strings.ToLower(strings.Join(strings.Fields(label), " ")), 40)
}

func knowledgeOrigin(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return ""
	}
	return u.Scheme + "://" + u.Host
}

func loadKnowledge(root string) ([]knowledgeEntry, error) {
	if root == "" {
		return nil, nil
	}
	fs, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	defer fs.Close()
	f, err := fs.Open(knowledgePath())
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, 1024*1024+1))
	if err != nil {
		return nil, err
	}
	if len(data) > 1024*1024 {
		return nil, errors.New("browser knowledge exceeds size limit")
	}
	var entries []knowledgeEntry
	return entries, json.Unmarshal(data, &entries)
}

func saveKnowledge(root string, entries []knowledgeEntry) error {
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].LastSeen.After(entries[j].LastSeen) })
	if len(entries) > maxKnowledgeEntries {
		entries = entries[:maxKnowledgeEntries]
	}
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	fs, err := os.OpenRoot(root)
	if err != nil {
		return err
	}
	defer fs.Close()
	if err := fs.MkdirAll(filepath.Dir(knowledgePath()), 0o700); err != nil {
		return err
	}
	tmp := knowledgePath() + ".tmp"
	if err := fs.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return fs.Rename(tmp, knowledgePath())
}

// learnFieldCommit records the outcome of each commit method tried on a
// field. Methods tried before the one that worked count as failures.
func learnFieldCommit(root, pageURL, kind, label string, attempts []string, succeeded bool, op string) {
	origin := knowledgeOrigin(pageURL)
	label = sanitizeKnowledgeLabel(label)
	if root == "" || origin == "" || label == "" || len(attempts) == 0 {
		return
	}
	knowledgeMu.Lock()
	defer knowledgeMu.Unlock()
	entries, err := loadKnowledge(root)
	if err != nil {
		return
	}
	now := time.Now()
	for i, method := range attempts {
		ok := succeeded && i == len(attempts)-1
		found := false
		for j := range entries {
			e := &entries[j]
			if e.Origin == origin && e.Kind == "field_commit:"+kind && e.Label == label && e.Method == method {
				if ok {
					e.Success++
					e.Evidence = append(e.Evidence, op)
					if len(e.Evidence) > 3 {
						e.Evidence = e.Evidence[len(e.Evidence)-3:]
					}
				} else {
					e.Failure++
				}
				e.LastSeen, e.Platform, found = now, runtime.GOOS, true
			}
		}
		if !found {
			e := knowledgeEntry{Origin: origin, Kind: "field_commit:" + kind, Label: label, Method: method, Platform: runtime.GOOS, FirstSeen: now, LastSeen: now}
			if ok {
				e.Success, e.Evidence = 1, []string{op}
			} else {
				e.Failure = 1
			}
			entries = append(entries, e)
		}
	}
	_ = saveKnowledge(root, entries)
}

func knowledgeStatus(e knowledgeEntry, now time.Time) string {
	switch {
	case now.Sub(e.LastSeen) > knowledgeStaleAfter:
		return "stale"
	case e.Success > 0 && e.Failure > 0 && e.Failure*2 >= e.Success:
		return "contested"
	case e.Success == 0:
		return "failed"
	}
	return "verified"
}

// learnedFieldCommit returns a method that verified on this origin and
// label and has not gone stale or contested since.
func learnedFieldCommit(root, pageURL, kind, label string) string {
	origin := knowledgeOrigin(pageURL)
	label = sanitizeKnowledgeLabel(label)
	if root == "" || origin == "" || label == "" {
		return ""
	}
	knowledgeMu.Lock()
	entries, err := loadKnowledge(root)
	knowledgeMu.Unlock()
	if err != nil {
		return ""
	}
	now, best, bestScore := time.Now(), "", 0
	for _, e := range entries {
		if e.Origin != origin || e.Kind != "field_commit:"+kind || e.Label != label || knowledgeStatus(e, now) != "verified" {
			continue
		}
		if score := e.Success - e.Failure; score > bestScore {
			best, bestScore = e.Method, score
		}
	}
	return best
}

// browserKnowledgeReport lists entries for the current origin with their
// standing, for the knowledge action.
func browserKnowledgeReport(root, pageURL string) (string, error) {
	origin := knowledgeOrigin(pageURL)
	knowledgeMu.Lock()
	entries, err := loadKnowledge(root)
	knowledgeMu.Unlock()
	if err != nil {
		return "", err
	}
	type view struct {
		knowledgeEntry
		Status string `json:"status"`
	}
	var out []view
	now := time.Now()
	for _, e := range entries {
		if origin == "" || e.Origin == origin {
			out = append(out, view{e, knowledgeStatus(e, now)})
		}
	}
	data, _ := json.Marshal(map[string]any{"origin": origin, "entries": out, "note": "Learned methods only order attempts; the current page and readback decide. Stale and contested entries are not used."})
	return string(data), nil
}
