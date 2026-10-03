package engineering

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"reflect"
	"slices"
	"strings"
)

type Finding struct {
	ID                        string          `json:"id"`
	Root                      string          `json:"root"`
	TaskID                    string          `json:"task_id"`
	TaskFingerprint           string          `json:"task_fingerprint"`
	ReviewerExecutionID       string          `json:"reviewer_execution_id"`
	ImplementerExecutionID    string          `json:"implementer_execution_id"`
	SourceFingerprint         string          `json:"source_fingerprint"`
	Status                    string          `json:"status"`
	Path                      string          `json:"path"`
	Issue                     string          `json:"issue"`
	Expected                  string          `json:"expected"`
	Evidence                  string          `json:"evidence"`
	RemediationTaskID         string          `json:"remediation_task_id,omitempty"`
	WaiverReason              string          `json:"waiver_reason,omitempty"`
	WaiverSourceFingerprint   string          `json:"waiver_source_fingerprint,omitempty"`
	WaiverProvenance          string          `json:"waiver_provenance,omitempty"`
	StartLine                 int             `json:"start_line"`
	EndLine                   int             `json:"end_line"`
	Severity                  int             `json:"severity"`
	Checks                    []ContractCheck `json:"checks,omitempty"`
	VerificationRunIDs        []string        `json:"verification_run_ids,omitempty"`
	VerificationReviewerID    string          `json:"verification_reviewer_id,omitempty"`
	VerifiedSourceFingerprint string          `json:"verified_source_fingerprint,omitempty"`
	VerifiedTaskFingerprint   string          `json:"verified_task_fingerprint,omitempty"`
}

func FindingID(f Finding) string {
	ns, _ := contractNamespace(f.Root)
	text := func(s string) string { return strings.Join(strings.Fields(strings.ToLower(s)), " ") }
	return Hash(fmt.Sprintf("%s\x00%s\x00%s\x00%s\x00%d\x00%s\x00%s", ns, f.TaskID, f.TaskFingerprint, path.Clean(strings.ReplaceAll(f.Path, `\`, "/")), f.StartLine, text(f.Issue), text(f.Expected)))
}

func (f Finding) RemediationID() string {
	if len(f.ID) != 64 {
		return ""
	}
	return "repair-" + f.ID[:32]
}

func (f Finding) VerificationBinding() string {
	return Hash(f.ID + "\x00" + f.VerifiedSourceFingerprint + "\x00" + f.VerificationReviewerID)
}

func (f Finding) validate() error {
	if _, err := contractNamespace(f.Root); err != nil {
		return err
	}
	file := strings.ReplaceAll(f.Path, `\`, "/")
	if f.ID != FindingID(f) || !validID(f.TaskID) || len(f.TaskFingerprint) != 64 || len(f.SourceFingerprint) != 64 || !boundedText(f.ReviewerExecutionID, 128) || !boundedText(f.ImplementerExecutionID, 128) || f.ReviewerExecutionID == f.ImplementerExecutionID || f.StartLine < 1 || f.EndLine < f.StartLine || f.EndLine > 1000000 || f.Severity < 0 || f.Severity > 3 || file == "" || len(file) > 512 || strings.HasPrefix(file, "/") || strings.ContainsAny(file, "*?:") || path.Clean(file) == "." || path.Clean(file) == ".." || strings.HasPrefix(path.Clean(file), "../") || len(f.Checks) > 12 || len(f.VerificationRunIDs) > 12 {
		return fmt.Errorf("invalid finding identity, source, path or bounds")
	}
	for _, text := range []string{f.Issue, f.Expected, f.Evidence} {
		if !boundedText(text, 4096) {
			return fmt.Errorf("invalid finding text")
		}
	}
	if f.RemediationTaskID != "" && f.RemediationTaskID != f.RemediationID() {
		return fmt.Errorf("remediation identity must be derived from its finding")
	}
	seen := map[string]bool{}
	for _, check := range f.Checks {
		if !boundedText(check.Name, 128) || seen[check.Name] || !json.Valid([]byte(check.InputJSON)) || len(check.InputJSON) > 16*1024 || check.Tool != "bash" && check.Tool != "test_run" && check.Tool != "lint_run" {
			return fmt.Errorf("invalid finding check")
		}
		seen[check.Name] = true
	}
	switch f.Status {
	case "open", "assigned", "fixed", "verified", "waived", "stale":
	default:
		return fmt.Errorf("invalid finding status")
	}
	return nil
}

func findingNamespace(namespace string) string { return "findings-" + Hash(namespace) }

func (s *Store) ReadFinding(ctx context.Context, namespace, id string) (Finding, Record, error) {
	ref, data, err := s.ReadRecord(ctx, findingNamespace(namespace), id)
	if err != nil {
		return Finding{}, ref, err
	}
	var f Finding
	if err := json.Unmarshal(data, &f); err != nil {
		return f, ref, err
	}
	if err := f.validate(); err != nil {
		return f, ref, err
	}
	if f.ID != id {
		return f, ref, fmt.Errorf("finding record identity mismatch")
	}
	return f, ref, nil
}

func (s *Store) TaskFindings(ctx context.Context, namespace, taskID string) ([]Finding, error) {
	records, err := s.ListRecords(ctx, findingNamespace(namespace))
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(records))
	for id := range records {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	var findings []Finding
	for _, id := range ids {
		f, _, err := s.ReadFinding(ctx, namespace, id)
		if err != nil {
			return nil, err
		}
		if taskID == "" || f.TaskID == taskID || f.RemediationTaskID == taskID {
			findings = append(findings, f)
		}
	}
	return findings, nil
}

func findOperation(st State, id string) (Operation, bool) {
	for _, op := range st.Operations {
		if op.ID == id {
			return op, true
		}
	}
	return Operation{}, false
}

func (s *Store) SaveFinding(ctx context.Context, namespace string, f Finding, expected uint64) (Record, error) {
	return s.saveFinding(ctx, namespace, f, expected, false)
}

func (s *Store) validateFindingReport(ctx context.Context, namespace string, st State, f Finding) error {
	reviewer, ok := s.CompletedAgentOperation(ctx, namespace, f.ReviewerExecutionID, st)
	implementer, implOK := s.CompletedAgentOperation(ctx, namespace, f.ImplementerExecutionID, st)
	if !ok || !implOK || reviewer.Tool != "agent" || reviewer.Status != "completed" || reviewer.AgentName != "review" && reviewer.AgentName != "test" || reviewer.TaskID != f.TaskID || implementer.Tool != "agent" || implementer.TaskID != f.TaskID || implementer.Status != "completed" {
		return fmt.Errorf("finding requires distinct completed runtime review and implementation identities")
	}
	fp, err := SourceFingerprint(ctx, f.Root, s.Dir())
	if err != nil {
		return err
	}
	if fp != f.SourceFingerprint {
		return fmt.Errorf("finding source changed during review")
	}
	data, err := ReadProjectEvidence(ctx, f.Root, f.Path)
	if err != nil {
		return err
	}
	if f.EndLine > bytes.Count(data, []byte("\n"))+1 {
		return fmt.Errorf("finding line range exceeds the inspected source")
	}
	return nil
}

func (s *Store) saveFinding(ctx context.Context, namespace string, f Finding, expected uint64, human bool) (Record, error) {
	if namespace == "" {
		return Record{}, fmt.Errorf("finding session identity is required")
	}
	if err := f.validate(); err != nil {
		return Record{}, err
	}
	f.Checks = slices.Clone(f.Checks)
	for i := range f.Checks {
		var compact bytes.Buffer
		if err := json.Compact(&compact, []byte(f.Checks[i].InputJSON)); err != nil {
			return Record{}, err
		}
		f.Checks[i].InputJSON = compact.String()
	}
	st, err := s.Read(ctx, namespace)
	if err != nil {
		return Record{}, err
	}
	if expected == 0 {
		if f.Status != "open" {
			return Record{}, fmt.Errorf("new reports must be open")
		}
		if err := s.validateFindingReport(ctx, namespace, st, f); err != nil {
			return Record{}, err
		}
	}
	old, previous, err := s.ReadFinding(ctx, namespace, f.ID)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return Record{}, err
	}
	if err == nil && expected == 0 {
		escalated := f.Severity < old.Severity
		if escalated {
			old.Severity = f.Severity
		}
		if old.Status == "verified" || old.Status == "waived" && (old.WaiverSourceFingerprint != f.SourceFingerprint || escalated) {
			old.Status = "stale"
		}
		if escalated || old.Status == "stale" {
			return s.saveFinding(ctx, namespace, old, previous.Revision, false)
		}
		return previous, nil
	}
	if previous.Revision != expected {
		return Record{}, fmt.Errorf("finding revision conflict")
	}
	if expected == 0 && f.Status != "open" {
		return Record{}, fmt.Errorf("new findings must be open")
	}
	if expected > 0 {
		if f.Root != old.Root || f.TaskID != old.TaskID || f.TaskFingerprint != old.TaskFingerprint || f.ReviewerExecutionID != old.ReviewerExecutionID || f.ImplementerExecutionID != old.ImplementerExecutionID || f.SourceFingerprint != old.SourceFingerprint || f.Issue != old.Issue || f.Expected != old.Expected || f.Path != old.Path || f.StartLine != old.StartLine || f.EndLine != old.EndLine || f.Severity > old.Severity {
			return Record{}, fmt.Errorf("finding identity and original evidence cannot be replaced or downgraded")
		}
		if old.Status == "verified" || old.Status == "waived" {
			if f.Status != "stale" {
				return Record{}, fmt.Errorf("closed findings require explicit stale reinspection")
			}
		}
	}
	preservesStaleWaiver := f.Status == "stale" && f.WaiverProvenance == old.WaiverProvenance && f.WaiverReason == old.WaiverReason && f.WaiverSourceFingerprint == old.WaiverSourceFingerprint
	if f.Status == "waived" {
		if !human || f.WaiverProvenance != "interactive-user" || !boundedText(f.WaiverReason, 2048) || f.WaiverSourceFingerprint == "" {
			return Record{}, fmt.Errorf("waiver requires an explicit interactive user decision and reason")
		}
	} else if (f.WaiverProvenance != "" || f.WaiverReason != "" || f.WaiverSourceFingerprint != "") && !preservesStaleWaiver {
		return Record{}, fmt.Errorf("waiver provenance is runtime-managed")
	}
	if f.Status == "verified" {
		if len(f.VerifiedTaskFingerprint) != 64 {
			return Record{}, fmt.Errorf("finding requires its freshly reviewed task specification")
		}
		fp, err := SourceFingerprint(ctx, f.Root, s.Dir())
		if err != nil {
			return Record{}, err
		}
		if fp != f.VerifiedSourceFingerprint || f.VerifiedSourceFingerprint == f.SourceFingerprint || f.VerificationReviewerID == f.ImplementerExecutionID || f.VerificationReviewerID == f.ReviewerExecutionID {
			return Record{}, fmt.Errorf("finding requires fresh source and a new independent review")
		}
		reviewer, ok := s.CompletedAgentOperation(ctx, namespace, f.VerificationReviewerID, st)
		if !ok || reviewer.Tool != "agent" || reviewer.Status != "completed" || reviewer.AgentName != "review" || reviewer.TaskID != f.TaskID {
			return Record{}, fmt.Errorf("finding requires a completed independent reviewer execution")
		}
		if len(f.Checks) == 0 || len(f.VerificationRunIDs) != 1 {
			return Record{}, fmt.Errorf("finding requires one fresh run of its complete check set")
		}
		observedOp, proof, err := s.VerificationObservation(ctx, namespace, f.VerificationRunIDs[0], f.TaskID, st)
		if err != nil {
			return Record{}, err
		}
		if len(proof) != len(f.Checks) {
			return Record{}, fmt.Errorf("finding verification check set mismatch")
		}
		observed := false
		for _, op := range []Operation{observedOp} {
			if op.CallID == f.VerificationRunIDs[0] && op.Tool == "verify" && op.TaskID == f.TaskID && op.Status == "completed" && op.OutcomeObserved && op.EvidenceHash != "" {
				observed = true
			}
		}
		if !observed {
			return Record{}, fmt.Errorf("finding requires actual observed verification")
		}
		for i, check := range proof {
			want := f.Checks[i]
			if !check.Passed || check.Evidence == "" || check.FindingID != f.ID || check.FindingReview != f.VerificationBinding() || check.Name != want.Name || check.Tool != want.Tool || check.InputHash != Hash(want.InputJSON) {
				return Record{}, fmt.Errorf("finding lacks new matching machine checks")
			}
		}
	}
	data, err := json.Marshal(f)
	if err != nil {
		return Record{}, err
	}
	linked := slices.Clone(previous.Linked)
	if previous.Revision > 0 {
		linked = append(linked, previous.Ref)
	}
	ref, err := s.PutRecordStrict(ctx, findingNamespace(namespace), f.ID, expected, data, linked...)
	if err != nil && expected == 0 {
		other, existing, readErr := s.ReadFinding(ctx, namespace, f.ID)
		if readErr == nil && reflect.DeepEqual(other, f) {
			return existing, nil
		}
	}
	return ref, err
}

func (s *Store) FindingBlocks(ctx context.Context, f Finding, source string) bool {
	if ctx.Err() != nil || f.validate() != nil {
		return true
	}
	if f.Severity == 3 {
		return false
	}
	if f.Status == "verified" {
		return f.VerifiedSourceFingerprint != source
	}
	if f.Status == "waived" {
		return f.WaiverProvenance != "interactive-user" || !boundedText(f.WaiverReason, 2048) || f.WaiverSourceFingerprint != source
	}
	return true
}

func (s *Store) ValidateTaskFindings(ctx context.Context, namespace, root, taskID string, taskFingerprint ...string) error {
	findings, err := s.TaskFindings(ctx, namespace, taskID)
	if err != nil || len(findings) == 0 {
		return err
	}
	fp, err := SourceFingerprint(ctx, root, s.Dir())
	if err != nil {
		return err
	}
	rootNS, err := contractNamespace(root)
	if err != nil {
		return err
	}
	for _, f := range findings {
		findingNS, err := contractNamespace(f.Root)
		if err != nil || findingNS != rootNS {
			return fmt.Errorf("finding belongs to another project root")
		}
		if s.FindingBlocks(ctx, f, fp) {
			return fmt.Errorf("task has unresolved or stale blocking finding %s", f.ID)
		}
		if f.Severity < 3 && f.Status == "verified" && f.TaskID == taskID && len(taskFingerprint) > 0 && f.VerifiedTaskFingerprint != taskFingerprint[0] {
			return fmt.Errorf("finding %s requires reinspection for the changed task specification", f.ID)
		}
	}
	return nil
}
