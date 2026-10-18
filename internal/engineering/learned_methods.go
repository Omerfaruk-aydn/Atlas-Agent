package engineering

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

var learnedMethodName = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// LearnedMethod is a bounded procedure candidate, not a universal instruction.
// Evidence is captured by the runtime; promotion uses the normal skill tool.
type LearnedMethod struct {
	ID           string            `json:"id"`
	Name         string            `json:"name"`
	Description  string            `json:"description"`
	Conditions   []string          `json:"conditions"`
	Versions     []string          `json:"versions"`
	Limits       []string          `json:"limits"`
	Steps        []string          `json:"steps"`
	OperationIDs []string          `json:"operation_ids"`
	Evidence     []EvidenceRef     `json:"evidence"`
	Sources      []SourceReference `json:"sources"`
	SessionID    string            `json:"session_id"`
	Generation   uint64            `json:"generation"`
	Status       string            `json:"status"`
	CreatedAt    int64             `json:"created_at"`
	SkillPath    string            `json:"skill_path,omitempty"`
}

func methodKey(root string) (string, error) {
	k, err := projectKey(root)
	return k + "-methods", err
}

func methodTexts(values []string, min, max int) bool {
	if len(values) < min || len(values) > max {
		return false
	}
	for _, value := range values {
		if strings.TrimSpace(value) == "" || len(value) > 4096 {
			return false
		}
	}
	return true
}

// ExtractMethod binds a reusable candidate to a successful observed sequence.
// Arguments and outputs are never copied automatically into instructions.
func (s *Store) ExtractMethod(ctx context.Context, root, sessionID string, in LearnedMethod) (LearnedMethod, error) {
	in.Name = strings.ToLower(strings.TrimSpace(in.Name))
	if !learnedMethodName.MatchString(in.Name) || len(in.Name) > 64 || len(in.Description) > 1024 || strings.TrimSpace(in.Description) == "" ||
		!methodTexts(in.Conditions, 1, 8) || !methodTexts(in.Versions, 1, 8) || !methodTexts(in.Limits, 1, 8) || !methodTexts(in.Steps, 0, 32) || len(in.OperationIDs) < 2 || len(in.OperationIDs) > 32 || len(in.Sources) > 16 {
		return LearnedMethod{}, errors.New("method needs a valid name, description, conditions, versions, limits and 2-32 observed operations")
	}
	st, err := s.Read(ctx, sessionID)
	if err != nil {
		return LearnedMethod{}, err
	}
	in.Evidence = nil
	seen := map[string]bool{}
	verified := false
	last := int64(0)
	for _, id := range in.OperationIDs {
		if seen[id] {
			return LearnedMethod{}, errors.New("duplicate method operation")
		}
		seen[id] = true
		var op *Operation
		for i := range st.Operations {
			if st.Operations[i].ID == id || st.Operations[i].CallID == id {
				op = &st.Operations[i]
				break
			}
		}
		if op == nil || op.Status != "completed" || !op.OutcomeObserved || op.Generation > st.Generation || op.FinishedAt < last {
			return LearnedMethod{}, errors.New("method evidence must be completed, observed, current and ordered")
		}
		last = op.FinishedAt
		if verificationOperation(st, *op) {
			verified = true
		}
		in.Evidence = append(in.Evidence, EvidenceRef{OperationID: op.ID, Tool: op.Tool, Generation: op.Generation, Passed: true, ObservedAt: op.FinishedAt})
	}
	if !verified {
		return LearnedMethod{}, errors.New("method requires a runtime-observed verification")
	}
	for i := range in.Sources {
		data, err := readProjectFile(ctx, root, in.Sources[i].Path, 256*1024)
		if err != nil {
			return LearnedMethod{}, err
		}
		in.Sources[i].Fingerprint = Hash(string(data))
	}
	if len(in.Steps) == 0 {
		for _, ev := range in.Evidence {
			in.Steps = append(in.Steps, fmt.Sprintf("Use %s on freshly observed targets; inspect its outcome before proceeding.", ev.Tool))
		}
	}
	in.SessionID, in.Generation, in.Status, in.CreatedAt, in.SkillPath = sessionID, st.Generation, "candidate", time.Now().UnixMilli(), ""
	encoded := workflowMethodIdentity(in)
	in.ID = "method-" + Hash(encoded)[:24]
	k, err := methodKey(root)
	if err != nil {
		return LearnedMethod{}, err
	}
	err = s.Update(ctx, k, func(st *State) error {
		for _, old := range st.LearnedMethods {
			if old.ID == in.ID {
				in = old
				return nil
			}
		}
		if len(st.LearnedMethods) >= 64 {
			return errors.New("method catalog full; remove an obsolete candidate first")
		}
		st.LearnedMethods = append(st.LearnedMethods, in)
		return nil
	})
	return in, err
}

func workflowMethodIdentity(m LearnedMethod) string {
	sources := ""
	for _, source := range m.Sources {
		sources += source.Path + ":" + source.Fingerprint + "\n"
	}
	return fmt.Sprintf("%s\n%d\n", m.SessionID, m.Generation) + m.Name + "\n" + m.Description + "\n" + strings.Join(m.OperationIDs, "\n") + "\n" + strings.Join(m.Steps, "\n") + "\n" + strings.Join(m.Conditions, "\n") + "\n" + strings.Join(m.Versions, "\n") + "\n" + strings.Join(m.Limits, "\n") + "\n" + sources
}

func (s *Store) LearnedMethods(ctx context.Context, root string) ([]LearnedMethod, error) {
	k, err := methodKey(root)
	if err != nil {
		return nil, err
	}
	st, err := s.Read(ctx, k)
	return st.LearnedMethods, err
}

// CurrentMethod rechecks original evidence and explicitly scoped sources.
func (s *Store) CurrentMethod(ctx context.Context, root, id string) (LearnedMethod, error) {
	methods, err := s.LearnedMethods(ctx, root)
	if err != nil {
		return LearnedMethod{}, err
	}
	for _, m := range methods {
		if m.ID != id {
			continue
		}
		st, err := s.Read(ctx, m.SessionID)
		if err != nil {
			return m, err
		}
		if st.Generation != m.Generation {
			return m, errors.New("method evidence stale; re-extract after re-verification")
		}
		for _, ev := range m.Evidence {
			found := false
			for _, op := range st.Operations {
				if op.ID == ev.OperationID && op.Status == "completed" && op.OutcomeObserved && op.Generation == ev.Generation && op.FinishedAt == ev.ObservedAt {
					found = true
				}
			}
			if !found {
				return m, errors.New("method evidence unavailable")
			}
		}
		for _, src := range m.Sources {
			data, err := readProjectFile(ctx, root, src.Path, 256*1024)
			if err != nil || Hash(string(data)) != src.Fingerprint {
				return m, errors.New("method source changed or unavailable")
			}
		}
		return m, nil
	}
	return LearnedMethod{}, errors.New("unknown learned method")
}

// MethodInstructions compiles an evidence-scoped candidate into skill prose.
func MethodInstructions(m LearnedMethod) string {
	var b strings.Builder
	b.WriteString("This procedure is evidence-scoped. Project instructions, permissions and current observations take precedence. Never reuse stored target identities or credentials.\n\n")
	for _, section := range []struct {
		title  string
		values []string
	}{{"Use when", m.Conditions}, {"Required versions", m.Versions}, {"Validity limits", m.Limits}, {"Procedure", m.Steps}} {
		fmt.Fprintf(&b, "## %s\n\n", section.title)
		for _, v := range section.values {
			fmt.Fprintf(&b, "- %s\n", v)
		}
		b.WriteString("\n")
	}
	fmt.Fprintf(&b, "Evidence: %s, generation %d. Verification is scoped to the recorded sequence; a single success is not a universal guarantee. Recheck versions, source and targets before reuse.\n", m.ID, m.Generation)
	return b.String()
}

func (s *Store) MarkMethodExported(ctx context.Context, root, id, path string) error {
	k, err := methodKey(root)
	if err != nil {
		return err
	}
	return s.Update(ctx, k, func(st *State) error {
		for i := range st.LearnedMethods {
			if st.LearnedMethods[i].ID == id {
				st.LearnedMethods[i].Status = "exported"
				st.LearnedMethods[i].SkillPath = path
				return nil
			}
		}
		return errors.New("unknown learned method")
	})
}

// ForgetMethod removes a candidate record, never an exported skill file.
func (s *Store) ForgetMethod(ctx context.Context, root, id string) error {
	k, err := methodKey(root)
	if err != nil {
		return err
	}
	return s.Update(ctx, k, func(st *State) error {
		for i, m := range st.LearnedMethods {
			if m.ID == id {
				st.LearnedMethods = append(st.LearnedMethods[:i], st.LearnedMethods[i+1:]...)
				return nil
			}
		}
		return errors.New("unknown learned method")
	})
}
