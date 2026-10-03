package engineering

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
)

type (
	AgentBatchItem struct {
		ID    string `json:"id"`
		Input string `json:"input"`
	}
	AgentBatchRow struct {
		AgentBatchItem
		Status    string `json:"status"`
		Attempts  int    `json:"attempts"`
		Output    string `json:"output,omitempty"`
		UpdatedAt int64  `json:"updated_at"`
	}
)

type AgentBatchReport struct {
	Request     json.RawMessage `json:"request"`
	ID          string          `json:"id"`
	Fingerprint string          `json:"fingerprint"`
	Rows        []AgentBatchRow `json:"rows"`
}

func AgentBatchNamespace(parent string) string { return "agent-batches-" + Hash(parent) }

func (s *Store) AgentBatches(ctx context.Context, parent string) ([]AgentBatchReport, error) {
	ns := AgentBatchNamespace(parent)
	records, err := s.ListRecords(ctx, ns)
	if err != nil {
		return nil, err
	}
	if len(records) > 64 {
		return nil, fmt.Errorf("batch list exceeds 64 reports")
	}
	keys := make([]string, 0, len(records))
	for key := range records {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	var reports []AgentBatchReport
	for _, key := range keys {
		_, data, err := s.ReadRecord(ctx, ns, key)
		if err != nil {
			return nil, err
		}
		var report AgentBatchReport
		if err = json.Unmarshal(data, &report); err != nil {
			return nil, err
		}
		if len(report.Rows) > 128 {
			return nil, fmt.Errorf("batch row bounds exceeded")
		}
		reports = append(reports, report)
	}
	return reports, nil
}
