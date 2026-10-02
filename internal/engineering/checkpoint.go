package engineering

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/google/uuid"
)

type Checkpoint struct {
	RecipeHash        string            `json:"recipe_hash,omitempty"`
	ParametersHash    string            `json:"parameters_hash,omitempty"`
	ID                string            `json:"id"`
	Root              string            `json:"root"`
	SessionID         string            `json:"session_id"`
	MessageID         string            `json:"message_id,omitempty"`
	PlanFingerprint   string            `json:"plan_fingerprint"`
	SourceFingerprint string            `json:"source_fingerprint"`
	Stage             int               `json:"stage"`
	TaskFingerprints  map[string]string `json:"task_fingerprints"`
	Workspaces        []string          `json:"workspaces"`
	Operations        []string          `json:"operations"`
}

type ResumePlan struct {
	CheckpointID         string   `json:"checkpoint_id"`
	Root                 string   `json:"root"`
	PlanFingerprint      string   `json:"plan_fingerprint"`
	SourceFingerprint    string   `json:"source_fingerprint"`
	TaskStateFingerprint string   `json:"task_state_fingerprint"`
	StateRevision        uint64   `json:"state_revision"`
	ReadyTasks           []string `json:"ready_tasks"`
	ReverifyTasks        []string `json:"reverify_tasks"`
	AmbiguousOperations  []string `json:"ambiguous_operations"`
}

type ResumeInput struct {
	Checkpoint            Checkpoint
	State                 State
	Source                string
	TaskSpecs, TaskStates map[string]string
}

func checkpointNamespace(root, session, id string) (string, error) {
	if _, err := uuid.Parse(id); err != nil || session == "" || !filepath.IsAbs(root) {
		return "", fmt.Errorf("checkpoint requires a UUID, session and absolute root")
	}
	root = filepath.Clean(root)
	if runtime.GOOS == "windows" {
		root = strings.ToLower(root)
	}
	return "checkpoints-" + Hash(root+"\x00"+session) + "-" + strings.ToLower(id[:2]), nil
}

func validCheckpoint(cp Checkpoint) error {
	if _, err := checkpointNamespace(cp.Root, cp.SessionID, cp.ID); err != nil {
		return err
	}
	if cp.RecipeHash != "" && !validArtifactHash(cp.RecipeHash) || cp.ParametersHash != "" && !validArtifactHash(cp.ParametersHash) || (cp.RecipeHash == "") != (cp.ParametersHash == "") {
		return fmt.Errorf("invalid checkpoint recipe binding")
	}
	if cp.Stage < 0 || cp.Stage > 512 || len(cp.Root) > 8192 || len(cp.SessionID) > 1024 || len(cp.MessageID) > 4096 || len(cp.TaskFingerprints) > 512 || len(cp.Operations) > 256 || len(cp.Workspaces) > 256 || !validArtifactHash(cp.SourceFingerprint) || !validArtifactHash(cp.PlanFingerprint) {
		return fmt.Errorf("invalid or oversized checkpoint")
	}
	for id, hash := range cp.TaskFingerprints {
		if id == "" || len(id) > 4096 || !validArtifactHash(hash) {
			return fmt.Errorf("invalid checkpoint task identity")
		}
	}
	for _, ids := range [][]string{cp.Workspaces, cp.Operations} {
		seen := map[string]bool{}
		for _, id := range ids {
			if id == "" || len(id) > 1024 || seen[id] {
				return fmt.Errorf("invalid or duplicated checkpoint reference")
			}
			seen[id] = true
		}
	}
	return nil
}

func validArtifactHash(hash string) bool {
	return (ArtifactRef{Kind: "record", Hash: hash, Version: 1}).validate() == nil
}

func (s *Store) SaveCheckpoint(ctx context.Context, cp Checkpoint) (Record, error) {
	ns, err := checkpointNamespace(cp.Root, cp.SessionID, cp.ID)
	if err != nil {
		return Record{}, err
	}
	if err := validCheckpoint(cp); err != nil {
		return Record{}, err
	}
	data, err := json.Marshal(cp)
	if err != nil {
		return Record{}, err
	}
	return s.PutRecord(ctx, ns, cp.ID, 0, data)
}

func (s *Store) ReadCheckpoint(ctx context.Context, root, session, id string) (Checkpoint, error) {
	ns, err := checkpointNamespace(root, session, id)
	if err != nil {
		return Checkpoint{}, err
	}
	_, data, err := s.ReadRecord(ctx, ns, id)
	if err != nil {
		return Checkpoint{}, err
	}
	var cp Checkpoint
	if err := json.Unmarshal(data, &cp); err != nil {
		return cp, err
	}
	storedNS, err := checkpointNamespace(cp.Root, cp.SessionID, cp.ID)
	if err != nil || ns != storedNS || cp.ID != id {
		return cp, fmt.Errorf("checkpoint identity mismatch")
	}
	return cp, validCheckpoint(cp)
}

// PlanResume reconciles observations without executing or restoring anything.
func PlanResume(ctx context.Context, input ResumeInput) (ResumePlan, error) {
	cp := input.Checkpoint
	if err := ctx.Err(); err != nil {
		return ResumePlan{}, err
	}
	if err := validCheckpoint(cp); err != nil {
		return ResumePlan{}, err
	}
	if !validArtifactHash(input.Source) || len(input.TaskSpecs) > 512 || len(input.TaskStates) != len(input.TaskSpecs) || len(input.State.Operations) > 256 || len(input.State.Workspaces) > 256 {
		return ResumePlan{}, fmt.Errorf("invalid resume source or task bounds")
	}
	identity, err := json.Marshal([]map[string]string{input.TaskSpecs, input.TaskStates})
	if err != nil {
		return ResumePlan{}, err
	}
	plan := ResumePlan{CheckpointID: cp.ID, Root: cp.Root, PlanFingerprint: cp.PlanFingerprint, SourceFingerprint: input.Source, TaskStateFingerprint: Hash(string(identity)), StateRevision: input.State.Revision, ReadyTasks: []string{}, ReverifyTasks: []string{}, AmbiguousOperations: []string{}}
	for id, edit := range input.State.SemanticEdits {
		if SemanticEditPending(edit.Status) {
			plan.AmbiguousOperations = append(plan.AmbiguousOperations, "semantic-edit:"+id)
		}
	}
	if input.State.Recipe != nil {
		if input.State.Recipe.Status != "active" || input.State.Recipe.RecipeHash != cp.RecipeHash || input.State.Recipe.ParametersHash != cp.ParametersHash {
			return plan, fmt.Errorf("recipe binding changed or admission is incomplete")
		}
	} else if cp.RecipeHash != "" {
		return plan, fmt.Errorf("checkpoint recipe binding is no longer recorded")
	}
	if input.State.Delivery != nil && input.State.Delivery.Fingerprint() != cp.PlanFingerprint {
		return plan, fmt.Errorf("delivery plan changed; create a current checkpoint")
	}
	workspaces := map[string]bool{}
	for _, work := range input.State.Workspaces {
		workspaces[work.ID] = true
	}
	for _, id := range cp.Workspaces {
		if !workspaces[id] {
			return plan, fmt.Errorf("checkpoint workspace is no longer recorded; inspect it before creating a current checkpoint")
		}
	}
	staleEvidence := map[string]bool{}
	if input.State.Delivery != nil {
		for _, stage := range input.State.Delivery.Stages {
			if stage.Passed && stage.SourceFingerprint != input.Source {
				for _, id := range stage.TaskIDs {
					staleEvidence[id] = true
				}
			}
		}
	}
	ops := map[string]bool{}
	for _, op := range input.State.Operations {
		ops[op.ID] = true
		if op.Status != "completed" && op.Status != "failed" && op.Status != "abandoned" {
			plan.AmbiguousOperations = append(plan.AmbiguousOperations, op.ID)
		}
	}
	for _, id := range cp.Operations {
		if !ops[id] {
			plan.AmbiguousOperations = append(plan.AmbiguousOperations, id)
		}
	}
	for id, spec := range input.TaskSpecs {
		if err := ctx.Err(); err != nil {
			return plan, err
		}
		status := input.TaskStates[id]
		if !validArtifactHash(spec) || status != "pending" && status != "in_progress" && status != "completed" {
			return plan, fmt.Errorf("invalid current task identity or status")
		}
		if run, ok := input.State.RoleExecutions[id]; ok && run.SourceFingerprint != "" && run.SourceFingerprint != input.Source {
			staleEvidence[id] = true
		}
		if cp.TaskFingerprints[id] != spec || status == "in_progress" || status == "completed" && (cp.SourceFingerprint != input.Source || staleEvidence[id]) {
			plan.ReverifyTasks = append(plan.ReverifyTasks, id)
		} else if status == "pending" {
			plan.ReadyTasks = append(plan.ReadyTasks, id)
		}
	}
	for id := range cp.TaskFingerprints {
		if _, ok := input.TaskSpecs[id]; !ok {
			plan.ReverifyTasks = append(plan.ReverifyTasks, id)
		}
	}
	for _, list := range [][]string{plan.ReadyTasks, plan.ReverifyTasks, plan.AmbiguousOperations} {
		sort.Strings(list)
	}
	if len(plan.AmbiguousOperations) > 0 || len(plan.ReverifyTasks) > 0 {
		plan.ReadyTasks = []string{}
	}
	return plan, nil
}

// UpdateRevision applies an observed plan only to its original state revision.
func (s *Store) UpdateRevision(ctx context.Context, id string, expected uint64, fn func(*State) error) error {
	return s.Update(ctx, id, func(st *State) error {
		if st.Revision != expected {
			return fmt.Errorf("execution state changed; inspect a fresh resume plan")
		}
		return fn(st)
	})
}
