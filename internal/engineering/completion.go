package engineering

import (
	"context"
	"encoding/json"
	"slices"
)

// CompletionCheckpoint is the runtime's aggregate decision. Item verification
// remains historical evidence; only a current complete checkpoint authorizes
// delivery or treating the whole task as finished.
type CompletionCheckpoint struct {
	Verdict            string   `json:"verdict"`
	Reasons            []string `json:"reasons,omitempty"`
	Review             string   `json:"review,omitempty"`
	ContractGaps       []string `json:"contract_gaps,omitempty"`
	Generation         uint64   `json:"generation"`
	Revision           uint64   `json:"revision"`
	Root               string   `json:"root"`
	RecordsFingerprint string   `json:"records_fingerprint"`
	SourceFingerprint  string   `json:"source_fingerprint"`
	TasksFingerprint   string   `json:"tasks_fingerprint"`
	WorkFingerprint    string   `json:"work_fingerprint"`
	BoardRevision      uint64   `json:"board_revision"`
}

// WorkContractFingerprint binds a review to requested work rather than
// progress bookkeeping. Delivery and evidence updates do not change it.
func WorkContractFingerprint(w *WorkState) string {
	if w == nil {
		return Hash("")
	}
	var contract []any
	for _, turn := range w.Turns {
		if turn.Kind != "continuation" {
			contract = append(contract, []any{turn.Seq, turn.Fingerprint, turn.Text})
		}
	}
	for _, item := range w.Items {
		contract = append(contract, []any{item.ID, item.Kind, item.Text, item.Origin, item.SourceTurns, item.AcceptanceCheck, item.Status == ItemSuperseded || item.Status == ItemDeclined})
	}
	data, _ := json.Marshal(contract)
	return Hash(string(data))
}

// CompleteAt checks the state-local portion of the binding. Callers relying
// on completion must also use CompletionStatus to check external records and
// the project tree, and compare the current task specification fingerprint.
func (c *CompletionCheckpoint) CompleteAt(st State) bool {
	return c != nil && c.Verdict == "complete" && c.Revision == st.Revision && c.Generation == st.Generation
}

// CompletionStatus projects the persisted decision against current evidence.
// Failure to read any dependency withholds completion, including after restart.
func (s *Store) CompletionStatus(ctx context.Context, id string) (*CompletionCheckpoint, error) {
	st, err := s.Read(ctx, id)
	if err != nil || st.Completion == nil {
		return nil, err
	}
	c := *st.Completion
	c.Reasons = slices.Clone(c.Reasons)
	if c.Verdict != "complete" {
		return &c, nil
	}
	stale := !c.CompleteAt(st)
	stamp, _, stampErr := s.WorkflowRecords(ctx, c.Root, id)
	board, _, boardErr := s.ReadControlBoard(ctx, id)
	source, sourceErr := ProjectFingerprint(ctx, c.Root, s.Dir())
	after, readErr := s.Read(ctx, id)
	if stale || stampErr != nil || boardErr != nil || sourceErr != nil || readErr != nil ||
		stamp != c.RecordsFingerprint || board.Revision != c.BoardRevision || source != c.SourceFingerprint || after.Revision != st.Revision {
		c.Verdict = "unverified"
		c.Reasons = append(c.Reasons, "the saved completion decision no longer matches the current task state, source or workflow records")
	}
	return &c, nil
}
