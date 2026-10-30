package workplan

// RedEvidence pairs an observed failing check with its later passing run.
// Sequence numbers are request-local and meaningful only in this session state.
type RedEvidence struct {
	Command        string   `json:"command"`
	Directory      string   `json:"directory,omitempty"`
	RequirementIDs []string `json:"requirement_ids,omitempty"`
	RedSequence    int      `json:"red_sequence"`
	EditSequence   int      `json:"edit_sequence"`
	GreenSequence  int      `json:"green_sequence,omitempty"`
}
