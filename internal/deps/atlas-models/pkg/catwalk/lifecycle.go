package catwalk

import (
	"net/url"
	"slices"
	"time"
)

// ModelLifecycle records provider-specific, sourced availability decisions.
// A missing public listing is deliberately not an availability decision.
type ModelLifecycle struct {
	Status         string `json:"status"`
	RetirementDate string `json:"retirement_date,omitempty"`
	Replacement    string `json:"replacement,omitempty"`
	SourceURL      string `json:"source_url"`
	VerifiedAt     string `json:"verified_at"`
	Note           string `json:"note,omitempty"`
}

// RetiredAt distinguishes scheduled retirement from an already closed route.
// Unsourced or malformed metadata cannot disable a model.
func (m Model) RetiredAt(now time.Time) bool {
	l := m.Lifecycle
	if l == nil || l.SourceURL == "" || l.VerifiedAt == "" {
		return false
	}
	if _, err := time.Parse(time.DateOnly, l.VerifiedAt); err != nil {
		return false
	}
	source, err := url.Parse(l.SourceURL)
	if err != nil || source.Scheme != "https" || source.Host == "" {
		return false
	}
	if l.Status == "retired" {
		return true
	}
	if l.Status != "deprecated" {
		return false
	}
	date, err := time.Parse(time.DateOnly, l.RetirementDate)
	return err == nil && !now.UTC().Before(date)
}

// SelectableAt omits confirmed retired routes without mutating the source.
// Other hosts of the same model remain independent. Defaults only migrate
// to a replacement that actually exists in this provider's catalog.
func (p Provider) SelectableAt(now time.Time) Provider {
	models := make([]Model, 0, len(p.Models))
	replacements := make(map[string]string)
	for _, m := range p.Models {
		if m.RetiredAt(now) {
			replacements[m.ID] = m.Lifecycle.Replacement
			continue
		}
		models = append(models, m)
	}
	p.Models = models
	resolve := func(id string) string {
		if _, retired := replacements[id]; !retired {
			return id
		}
		for range len(replacements) + 1 {
			if slices.ContainsFunc(models, func(m Model) bool { return m.ID == id }) {
				return id
			}
			next, ok := replacements[id]
			if !ok || next == "" || next == id {
				break
			}
			id = next
		}
		if len(models) > 0 {
			return models[0].ID
		}
		return ""
	}
	if p.DefaultLargeModelID != "" {
		p.DefaultLargeModelID = resolve(p.DefaultLargeModelID)
	}
	if p.DefaultSmallModelID != "" {
		p.DefaultSmallModelID = resolve(p.DefaultSmallModelID)
	}
	return p
}
