package engineering

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
)

type ContractCheck struct {
	Name      string `json:"name"`
	Tool      string `json:"tool"`
	InputJSON string `json:"input_json"`
}

type ContractRevision struct {
	ID              string            `json:"id"`
	Root            string            `json:"root"`
	OwnerTaskID     string            `json:"owner_task_id"`
	Description     string            `json:"description"`
	Invariants      string            `json:"invariants"`
	Revision        uint64            `json:"revision"`
	ConsumerTaskIDs []string          `json:"consumer_task_ids,omitempty"`
	Sources         []SourceReference `json:"sources"`
	Checks          []ContractCheck   `json:"checks"`
}

type ContractEvidence struct {
	Root              string `json:"root"`
	TaskID            string `json:"task_id"`
	TaskFingerprint   string `json:"task_fingerprint"`
	Contract          Record `json:"contract"`
	RunID             string `json:"run_id"`
	SourceFingerprint string `json:"source_fingerprint"`
}

func contractNamespace(root string) (string, error) {
	if !filepath.IsAbs(root) {
		return "", fmt.Errorf("contract root must be absolute")
	}
	root = filepath.Clean(root)
	if runtime.GOOS == "windows" {
		root = strings.ToLower(root)
	}
	return "contracts-" + Hash(root), nil
}

func (c ContractRevision) validate() error {
	if !validID(c.ID) || !validID(c.OwnerTaskID) || c.Revision == 0 || !boundedText(c.Description, 4096) || !boundedText(c.Invariants, 8192) || len(c.ConsumerTaskIDs) > 32 || len(c.Sources) == 0 || len(c.Sources) > 16 || len(c.Checks) == 0 || len(c.Checks) > 12 {
		return fmt.Errorf("invalid contract identity, description or collection bounds")
	}
	if _, err := contractNamespace(c.Root); err != nil {
		return err
	}
	seen := map[string]bool{c.OwnerTaskID: true}
	for _, task := range c.ConsumerTaskIDs {
		if !validID(task) || seen[task] {
			return fmt.Errorf("invalid or duplicate contract consumer")
		}
		seen[task] = true
	}
	seen = map[string]bool{}
	for _, check := range c.Checks {
		if !boundedText(check.Name, 128) || seen[check.Name] || len(check.InputJSON) > 16*1024 || !json.Valid([]byte(check.InputJSON)) || check.Tool != "bash" && check.Tool != "test_run" && check.Tool != "lint_run" {
			return fmt.Errorf("invalid or duplicate contract check")
		}
		seen[check.Name] = true
	}
	return nil
}

func contractSources(ctx context.Context, c ContractRevision) error {
	paths := make([]string, len(c.Sources))
	for i, source := range c.Sources {
		paths[i] = source.Path
	}
	actual, err := CaptureSources(ctx, c.Root, paths)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(actual, c.Sources) {
		return fmt.Errorf("contract source changed; publish a new revision")
	}
	return nil
}

func (s *Store) SaveContract(ctx context.Context, c ContractRevision, expected uint64) (Record, error) {
	if err := c.validate(); err != nil {
		return Record{}, err
	}
	if expected == ^uint64(0) || c.Revision != expected+1 {
		return Record{}, fmt.Errorf("contract revision must advance exactly once")
	}
	if err := contractSources(ctx, c); err != nil {
		return Record{}, err
	}
	c.Checks = slices.Clone(c.Checks)
	for i := range c.Checks {
		var compact bytes.Buffer
		if err := json.Compact(&compact, []byte(c.Checks[i].InputJSON)); err != nil {
			return Record{}, err
		}
		c.Checks[i].InputJSON = compact.String()
	}
	ns, _ := contractNamespace(c.Root)
	previous, _, err := s.ReadRecord(ctx, ns, c.ID)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return Record{}, err
	}
	if previous.Revision != expected {
		return Record{}, fmt.Errorf("contract revision conflict")
	}
	data, err := json.Marshal(c)
	if err != nil {
		return Record{}, err
	}
	linked := slices.Clone(previous.Linked)
	if previous.Revision > 0 {
		linked = append(linked, previous.Ref)
	}
	return s.PutRecordStrict(ctx, ns, c.ID, expected, data, linked...)
}

func (s *Store) contractRecords(ctx context.Context, root, taskID string) ([]ContractRevision, []Record, error) {
	ns, err := contractNamespace(root)
	if err != nil {
		return nil, nil, err
	}
	if !validID(taskID) {
		return nil, nil, fmt.Errorf("invalid contract task")
	}
	entries, err := s.ListRecords(ctx, ns)
	if err != nil {
		return nil, nil, err
	}
	ids := make([]string, 0, len(entries))
	for id := range entries {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	var contracts []ContractRevision
	var refs []Record
	for _, id := range ids {
		r, data, err := s.ReadRecord(ctx, ns, id)
		if err != nil {
			return nil, nil, err
		}
		var c ContractRevision
		if err := json.Unmarshal(data, &c); err != nil {
			return nil, nil, err
		}
		if err := c.validate(); err != nil {
			return nil, nil, err
		}
		otherNS, err := contractNamespace(c.Root)
		if err != nil || otherNS != ns || c.ID != id || c.Revision != r.Revision {
			return nil, nil, fmt.Errorf("contract record identity mismatch")
		}
		if c.OwnerTaskID == taskID || slices.Contains(c.ConsumerTaskIDs, taskID) {
			contracts = append(contracts, c)
			refs = append(refs, r)
		}
	}
	return contracts, refs, nil
}

func (s *Store) ContractsForTask(ctx context.Context, root, taskID string) ([]ContractRevision, error) {
	contracts, _, err := s.contractRecords(ctx, root, taskID)
	return contracts, err
}

// ContractSnapshotForTask returns definitions and references without certifying
// their source freshness, so a changed source cannot prevent corrective work.
func (s *Store) ContractSnapshotForTask(ctx context.Context, root, taskID string) ([]ContractRevision, []Record, error) {
	return s.contractRecords(ctx, root, taskID)
}

func (s *Store) ContractRefsForTask(ctx context.Context, root, taskID string) ([]Record, error) {
	contracts, refs, err := s.contractRecords(ctx, root, taskID)
	if err != nil {
		return nil, err
	}
	for _, c := range contracts {
		if err := contractSources(ctx, c); err != nil {
			return nil, err
		}
	}
	return refs, nil
}

func (s *Store) ValidateContractRefs(ctx context.Context, root, taskID string, refs []Record) error {
	actual, err := s.ContractRefsForTask(ctx, root, taskID)
	if err != nil {
		return err
	}
	if len(actual) != len(refs) {
		return fmt.Errorf("contract set changed; accept and check its current revisions")
	}
	for i := range actual {
		if !reflect.DeepEqual(actual[i], refs[i]) {
			return fmt.Errorf("contract revision changed; accept and check its current revisions")
		}
	}
	return nil
}

func contractEvidenceKey(session, root, task, contract string) (string, string) {
	ns, _ := contractNamespace(root)
	key := Hash(task + "\x00" + contract)
	return "contract-checks-" + Hash(session+"\x00"+ns) + "-" + key[:2], key
}

func (s *Store) validContractEvidence(ctx context.Context, session string, st State, c ContractRevision, p ContractEvidence) error {
	op, checks, err := s.VerificationObservation(ctx, session, p.RunID, p.TaskID, st)
	if err != nil {
		return err
	}
	observed := false
	for _, op := range []Operation{op} {
		if op.CallID == p.RunID && op.TaskID == p.TaskID && op.Tool == "verify" && op.Status == "completed" && op.OutcomeObserved && op.EvidenceHash != "" {
			observed = true
		}
	}
	if !observed {
		return fmt.Errorf("contract requires an observed completed verification operation")
	}
	if len(checks) != len(c.Checks) {
		return fmt.Errorf("contract check set differs from actual verification")
	}
	for i, check := range checks {
		want := c.Checks[i]
		if !check.Passed || check.Evidence == "" || check.ContractHash != p.Contract.Ref.Hash || check.Name != want.Name || check.Tool != want.Tool || check.InputHash != Hash(want.InputJSON) {
			return fmt.Errorf("contract lacks matching successful check inputs")
		}
	}
	return nil
}

func (s *Store) SaveContractEvidence(ctx context.Context, session, contractID string, p ContractEvidence) error {
	if session == "" || p.TaskFingerprint == "" || p.RunID == "" {
		return fmt.Errorf("contract evidence identity is required")
	}
	contracts, refs, err := s.contractRecords(ctx, p.Root, p.TaskID)
	if err != nil {
		return err
	}
	index := -1
	for i, c := range contracts {
		if c.ID == contractID {
			index = i
		}
	}
	if index < 0 || !reflect.DeepEqual(refs[index], p.Contract) {
		return fmt.Errorf("contract evidence revision is stale")
	}
	if err := contractSources(ctx, contracts[index]); err != nil {
		return err
	}
	fp, err := SourceFingerprint(ctx, p.Root, s.Dir())
	if err != nil {
		return err
	}
	if fp != p.SourceFingerprint {
		return fmt.Errorf("source changed during contract verification")
	}
	st, err := s.Read(ctx, session)
	if err != nil {
		return err
	}
	if err := s.validContractEvidence(ctx, session, st, contracts[index], p); err != nil {
		return err
	}
	ns, key := contractEvidenceKey(session, p.Root, p.TaskID, contractID)
	previous, _, err := s.ReadRecord(ctx, ns, key)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	data, err := json.Marshal(p)
	if err != nil {
		return err
	}
	linked := slices.Clone(previous.Linked)
	if previous.Revision > 0 {
		linked = append(linked, previous.Ref)
	}
	if !slices.Contains(linked, p.Contract.Ref) {
		linked = append(linked, p.Contract.Ref)
	}
	_, err = s.PutRecordStrict(ctx, ns, key, previous.Revision, data, linked...)
	return err
}

// ValidateTaskContracts requires current revisions and source-bound journal proof.
func (s *Store) ValidateTaskContracts(ctx context.Context, session, root, task, taskFP string, refs []Record) error {
	if err := s.ValidateContractRefs(ctx, root, task, refs); err != nil {
		return err
	}
	contracts, actual, err := s.contractRecords(ctx, root, task)
	if err != nil || len(contracts) == 0 {
		return err
	}
	fp, err := SourceFingerprint(ctx, root, s.Dir())
	if err != nil {
		return err
	}
	st, err := s.Read(ctx, session)
	if err != nil {
		return err
	}
	for i, c := range contracts {
		ns, key := contractEvidenceKey(session, root, task, c.ID)
		_, data, err := s.ReadRecord(ctx, ns, key)
		if err != nil {
			return fmt.Errorf("contract %s requires current checks: %w", c.ID, err)
		}
		var proof ContractEvidence
		if err := json.Unmarshal(data, &proof); err != nil {
			return err
		}
		proofNS, err := contractNamespace(proof.Root)
		rootNS, _ := contractNamespace(root)
		if err != nil || proofNS != rootNS || proof.TaskID != task || proof.TaskFingerprint != taskFP || proof.SourceFingerprint != fp || !reflect.DeepEqual(proof.Contract, actual[i]) {
			return fmt.Errorf("contract %s check evidence is stale", c.ID)
		}
		if err := s.validContractEvidence(ctx, session, st, c, proof); err != nil {
			return err
		}
	}
	return nil
}
