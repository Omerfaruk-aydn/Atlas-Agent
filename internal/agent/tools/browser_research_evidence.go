package tools

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

func validResearchSource(source string) bool {
	u, err := url.Parse(strings.TrimSpace(source))
	return err == nil && u.Host != "" && u.User == nil && (u.Scheme == "http" || u.Scheme == "https") && len(source) <= 2048
}

func researchSourceKey(source string) string {
	u, err := url.Parse(strings.TrimSpace(source))
	if err != nil {
		return ""
	}
	u.Fragment = ""
	u.Scheme, u.Host = strings.ToLower(u.Scheme), strings.ToLower(u.Host)
	return u.String()
}

func evidenceContentHash(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

func validResearchEvidence(e browserEvidence) bool {
	return e.Op != "" && e.Action != "" && !e.At.IsZero() && validResearchSource(e.Source) && e.Subject != "" && e.Claim != "" && e.ContentHash != ""
}

func requirementEvidenceMatches(r browserRequirement, e browserEvidence) bool {
	switch r.Verify {
	case "observation":
		return e.Kind == "observation" && validResearchEvidence(e) && researchSourceKey(e.Source) == researchSourceKey(r.Source) && e.Subject == r.Subject && e.Claim == r.Expect
	case "delivery":
		return e.Kind == "delivery" && e.Action == "final_response" && e.Op != "" && !e.At.IsZero() && e.ContentHash != "" && e.Claim == r.Expect
	}
	return true
}

// researchSourceText selects one returned source, never the whole research
// batch: a subject on one page cannot verify a claim on another page.
func researchSourceText(op BrowserOp, source string) string {
	key := researchSourceKey(source)
	// Only the original response can establish an exact source URL. The
	// lowercase compatibility text loses path and query case distinctions.
	observed := op.ObservedRaw
	if observed == "" {
		return ""
	}
	if op.Action == "research" {
		var data struct {
			Sources []struct {
				URL       string `json:"final_url"`
				Title     string `json:"title"`
				Heading   string `json:"heading"`
				Excerpt   string `json:"excerpt"`
				Published string `json:"published"`
				Modified  string `json:"modified"`
				Error     string `json:"error"`
			} `json:"sources"`
		}
		if json.Unmarshal([]byte(observed), &data) != nil {
			return ""
		}
		for _, s := range data.Sources {
			if s.Error == "" && validResearchSource(s.URL) && researchSourceKey(s.URL) == key {
				return strings.Join([]string{s.Title, s.Heading, s.Excerpt, s.Published, s.Modified}, "\n")
			}
		}
		return ""
	}
	if op.Action == "observe" || op.Action == "snapshot" {
		for _, line := range strings.Split(observed, "\n") {
			if !strings.HasPrefix(line, "url=") {
				continue
			}
			raw := strings.TrimPrefix(line, "url=")
			if end := strings.Index(raw, " title="); end >= 0 {
				raw = raw[:end]
			}
			if validResearchSource(raw) && researchSourceKey(raw) == key {
				// Exclude the URL itself as evidence about the subject.
				return strings.Replace(observed, "url="+raw, "", 1)
			}
		}
	}
	if op.Action == "collect" {
		var data struct {
			Items []struct {
				SourceURL string `json:"source_url"`
				Text      string `json:"text"`
			} `json:"items"`
		}
		if json.Unmarshal([]byte(observed), &data) != nil {
			return ""
		}
		var text []string
		for _, row := range data.Items {
			if validResearchSource(row.SourceURL) && researchSourceKey(row.SourceURL) == key {
				text = append(text, row.Text)
			}
		}
		return strings.Join(text, "\n")
	}
	return ""
}

func researchEvidence(req browserRequirement, op BrowserOp) (browserEvidence, error) {
	text := strings.ToLower(researchSourceText(op, req.Source))
	if !op.OK || op.Epoch != browserProcessEpoch || text == "" || !strings.Contains(text, strings.ToLower(req.Subject)) || !strings.Contains(text, strings.ToLower(req.Expect)) {
		return browserEvidence{}, fmt.Errorf("evidence %s must return subject %q and claim %q from source %q together; a note or unrelated source cannot establish this observation", op.ID, req.Subject, req.Expect, req.Source)
	}
	return browserEvidence{Op: op.ID, Seq: op.Seq, Action: op.Action, Verification: op.Verification, Epoch: op.Epoch, Browser: op.Browser, Tab: op.Tab, Document: op.Document, At: op.At, Kind: "observation", Source: req.Source, Subject: req.Subject, Claim: req.Expect, ContentHash: evidenceContentHash(text)}, nil
}

// BrowserEvidenceSnapshot exposes provenance without live refs or values.
type BrowserEvidenceSnapshot struct {
	Op          string
	Kind        string
	Source      string
	Subject     string
	Claim       string
	ContentHash string
	At          time.Time
}

// BrowserRequirementSnapshot is the effective checkpoint requirement state.
type BrowserRequirementSnapshot struct {
	TaskID     string
	ID         string
	Text       string
	Verify     string
	Subject    string
	Source     string
	Expect     string
	Status     string
	Reason     string
	Blocker    string
	Evidence   []string
	Provenance []BrowserEvidenceSnapshot
}

// BrowserTaskRequirements returns the active requirements for reconciliation
// with the session's shared outcome ledger.
func BrowserTaskRequirements(root, sessionID string) []BrowserRequirementSnapshot {
	result, _ := BrowserTaskRequirementsChecked(root, sessionID)
	return result
}

// BrowserTaskRequirementsChecked preserves checkpoint errors for runtime guards.
func BrowserTaskRequirementsChecked(root, sessionID string) ([]BrowserRequirementSnapshot, error) {
	browserTaskMu.Lock()
	defer browserTaskMu.Unlock()
	file, err := loadBrowserTasks(root, sessionID)
	if err != nil || file == nil || file.Active == nil {
		return nil, err
	}
	var result []BrowserRequirementSnapshot
	for _, r := range file.Active.Requirements {
		status, reason := file.Active.effective(r)
		v := BrowserRequirementSnapshot{TaskID: file.Active.ID, ID: r.ID, Text: r.Text, Verify: r.Verify, Subject: r.Subject, Source: r.Source, Expect: r.Expect, Status: status, Reason: reason, Blocker: r.Blocker}
		for _, e := range r.Evidence {
			v.Evidence = append(v.Evidence, e.Op)
			v.Provenance = append(v.Provenance, BrowserEvidenceSnapshot{Op: e.Op, Kind: e.Kind, Source: e.Source, Subject: e.Subject, Claim: e.Claim, ContentHash: e.ContentHash, At: e.At})
		}
		result = append(result, v)
	}
	return result, nil
}

// RecordBrowserTaskDelivery records only a final assistant message already
// saved by the agent. Tool calls cannot invoke this delivery transition.
func RecordBrowserTaskDelivery(root, sessionID, finalMessageID, finalText string) error {
	if strings.TrimSpace(finalMessageID) == "" || strings.TrimSpace(finalText) == "" {
		return errors.New("delivery requires a saved final message identity and text")
	}
	browserTaskMu.Lock()
	defer browserTaskMu.Unlock()
	file, err := loadBrowserTasks(root, sessionID)
	if err != nil || file == nil || file.Active == nil {
		return err
	}
	changed := false
	for i := range file.Active.Requirements {
		r := &file.Active.Requirements[i]
		if r.Verify != "delivery" || r.Status == ReqBlocked || r.Expect == "" || !strings.Contains(strings.ToLower(finalText), strings.ToLower(r.Expect)) {
			continue
		}
		r.Status, r.Blocker, r.BlockerEvidence, r.BlockedEpoch = ReqVerified, "", nil, ""
		r.Note, r.Updated = "", time.Now()
		r.Evidence = []browserEvidence{{Op: finalMessageID, Kind: "delivery", Action: "final_response", Verification: "delivered", Claim: r.Expect, ContentHash: evidenceContentHash(finalText), At: r.Updated}}
		changed = true
	}
	if !changed {
		return nil
	}
	return saveBrowserTasks(root, sessionID, file)
}
