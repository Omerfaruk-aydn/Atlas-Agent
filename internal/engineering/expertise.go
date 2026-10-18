package engineering

// ExpertiseRecord tracks the provenance and lifecycle of an on-demand guide.
type ExpertiseRecord struct {
	Name   string `json:"name"`
	Source string `json:"source"`
	Hash   string `json:"hash"`
	Group  string `json:"group,omitempty"`
	CallID string `json:"call_id"`
	Active bool   `json:"active"`
}
