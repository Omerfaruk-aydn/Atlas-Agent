package engineering

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agentstate"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/interaction"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/lock"
)

type WorkflowTask struct {
	AcceptanceCriteria []string `json:"acceptance_criteria,omitempty"`
	Verification       string   `json:"verification,omitempty"`
	ID                 string   `json:"id"`
	Content            string   `json:"content"`
	Status             string   `json:"status"`
	Agent              string   `json:"agent,omitempty"`
	SpecFingerprint    string   `json:"spec_fingerprint"`
	DependsOn          []string `json:"depends_on,omitempty"`
	OwnedPaths         []string `json:"owned_paths,omitempty"`
}

type WorkflowSnapshot struct {
	PlatformJobs       []agentstate.Job    `json:"platform_jobs,omitempty"`
	PlatformTasks      []agentstate.Task   `json:"platform_tasks,omitempty"`
	SourceMemories     []agentstate.Memory `json:"source_memories,omitempty"`
	Interactions       interaction.State   `json:"interactions"`
	Batches            []AgentBatchReport  `json:"batches,omitempty"`
	Context            ContextManifest     `json:"context"`
	ContextPreferences ContextPreferences  `json:"context_preferences"`
	AgentLimit         int                 `json:"agent_limit"`
	Board              ControlBoard        `json:"board"`
	Runners            []LiveRunner        `json:"runners,omitempty"`
	SessionID          string              `json:"session_id"`
	Revision           string              `json:"revision"`
	Tasks              []WorkflowTask      `json:"tasks"`
	Usage              Usage               `json:"usage"`
	Limits             Limits              `json:"limits"`
	Stage              int                 `json:"stage"`
	Paused             bool                `json:"paused"`
	Busy               bool                `json:"busy"`
	Findings           []Finding           `json:"findings"`
	Checks             []Check             `json:"checks"`
	Executions         []RoleExecution     `json:"executions"`
	Checkpoints        []Checkpoint        `json:"checkpoints"`
	Operations         []Operation         `json:"operations"`
	Capabilities       map[string]string   `json:"capabilities"`
}

type WorkflowControl struct {
	OwnedPaths       []string `json:"owned_paths,omitempty"`
	CheckpointID     string   `json:"checkpoint_id,omitempty"`
	Text             string   `json:"text,omitempty"`
	DirectiveID      string   `json:"directive_id,omitempty"`
	DependsOn        []string `json:"depends_on,omitempty"`
	File             string   `json:"file,omitempty"`
	Line             int      `json:"line,omitempty"`
	MaxAgents        int      `json:"max_agents,omitempty"`
	Limits           *Limits  `json:"limits,omitempty"`
	TaskID           string   `json:"task_id,omitempty"`
	Action           string   `json:"action"`
	Agent            string   `json:"agent,omitempty"`
	ExpectedRevision string   `json:"expected_revision"`
}

func (s *Store) WorkflowLock(ctx context.Context, id string) (func(), error) {
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return lock.File(ctx, filepath.Join(s.dir, "workflow-"+Hash(id)+".lock"))
}

// WorkflowRecords binds revisions to related manifests, not only usage state.
func (s *Store) WorkflowRecords(ctx context.Context, root, id string) (string, []Checkpoint, error) {
	release, err := s.artifactLock(ctx)
	if err != nil {
		return "", nil, err
	}
	defer release()
	entries, err := os.ReadDir(filepath.Join(s.dir, "artifact-index"))
	if err != nil {
		return "", nil, err
	}
	if len(entries) > 8192 {
		return "", nil, fmt.Errorf("workflow artifact index exceeds bounds")
	}
	contract, err := contractNamespace(root)
	if err != nil {
		return "", nil, err
	}
	cp, err := checkpointNamespace(root, id, "00000000-0000-0000-0000-000000000000")
	if err != nil {
		return "", nil, err
	}
	cp = strings.TrimSuffix(cp, "00")
	scenario := "scenario-runs-" + Hash(filepath.Clean(root)+"\x00"+id) + "-"
	var stamps []string
	var checkpoints []Checkpoint
	total := 0
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return "", nil, err
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data, err := boundedArtifactFile(filepath.Join(s.dir, "artifact-index", entry.Name()), maxStateBytes)
		if err != nil {
			return "", nil, err
		}
		var identity struct {
			Namespace string `json:"namespace"`
		}
		if err := json.Unmarshal(data, &identity); err != nil {
			return "", nil, err
		}
		ns := identity.Namespace
		if ns != findingNamespace(id) && ns != contract && !strings.HasPrefix(ns, cp) && !strings.HasPrefix(ns, scenario) {
			continue
		}
		total += len(data)
		if total > 16*1024*1024 {
			return "", nil, fmt.Errorf("workflow records exceed bounds")
		}
		manifest, err := s.readManifest(ns)
		if err != nil {
			return "", nil, err
		}
		stamps = append(stamps, ns+":"+Hash(string(data)))
		if strings.HasPrefix(ns, cp) {
			for _, record := range manifest.Records {
				payload, err := s.readArtifact(record.Ref)
				if err != nil {
					return "", nil, err
				}
				var checkpoint Checkpoint
				if err := json.Unmarshal(payload, &checkpoint); err != nil {
					return "", nil, err
				}
				if err := validCheckpoint(checkpoint); err != nil {
					return "", nil, err
				}
				if checkpoint.SessionID != id || filepath.Clean(checkpoint.Root) != filepath.Clean(root) {
					return "", nil, fmt.Errorf("checkpoint scope mismatch")
				}
				checkpoints = append(checkpoints, checkpoint)
				if len(checkpoints) > 128 {
					return "", nil, fmt.Errorf("workflow checkpoint collection exceeds bounds")
				}
			}
		}
	}
	sort.Strings(stamps)
	sort.Slice(checkpoints, func(i, j int) bool { return checkpoints[i].ID < checkpoints[j].ID })
	return Hash(strings.Join(stamps, "\n")), checkpoints, nil
}

func (s *Store) Snapshot(ctx context.Context, root, id string, tasks []WorkflowTask, todoRevision string, busy bool) (WorkflowSnapshot, error) {
	for range 3 {
		st, err := s.Read(ctx, id)
		if err != nil {
			return WorkflowSnapshot{}, err
		}
		stamp, checkpoints, err := s.WorkflowRecords(ctx, root, id)
		if err != nil {
			return WorkflowSnapshot{}, err
		}
		findings, err := s.TaskFindings(ctx, id, "")
		if err != nil {
			return WorkflowSnapshot{}, err
		}
		after, _, err := s.WorkflowRecords(ctx, root, id)
		if err != nil {
			return WorkflowSnapshot{}, err
		}
		current, err := s.Read(ctx, id)
		if err != nil {
			return WorkflowSnapshot{}, err
		}
		if current.Revision != st.Revision || after != stamp {
			continue
		}
		out := WorkflowSnapshot{SessionID: id, Tasks: tasks, Usage: st.Usage, Limits: st.Limits, Paused: st.Paused, Busy: busy, Findings: findings, Checks: st.Checks, Checkpoints: checkpoints, Operations: st.Operations, Capabilities: map[string]string{"controls": "available", "terminal": "requires platform PTY; isolated PTY depends on runner", "browser": "requires enabled installed browser"}}
		if st.Delivery != nil {
			out.Stage = st.Delivery.CurrentStage
		}
		ids := make([]string, 0, len(st.RoleExecutions))
		for key := range st.RoleExecutions {
			ids = append(ids, key)
		}
		sort.Strings(ids)
		times := map[string]int64{}
		for _, op := range st.Operations {
			times[op.ID] = op.StartedAt
		}
		sort.SliceStable(ids, func(i, j int) bool {
			return times[st.RoleExecutions[ids[i]].ExecutionID] < times[st.RoleExecutions[ids[j]].ExecutionID]
		})
		for _, key := range ids {
			value := st.RoleExecutions[key]
			value = ProjectExecution(value, len(out.Executions) >= max(0, len(ids)-32))
			out.Executions = append(out.Executions, value)
		}
		boardRecord, board, err := s.ReadControlBoard(ctx, id)
		if err != nil {
			return WorkflowSnapshot{}, err
		}
		out.Board, out.Runners = board, s.LiveRunners(id)
		out.AgentLimit = 16
		if board.MaxAgents > 0 {
			out.AgentLimit = min(out.AgentLimit, board.MaxAgents)
		}
		data, _ := json.Marshal([]any{st.Revision, todoRevision, stamp, busy, boardRecord.Revision, out.Runners})
		out.Revision = Hash(string(data))
		return out, nil
	}
	return WorkflowSnapshot{}, fmt.Errorf("workflow changed during snapshot; retry")
}
