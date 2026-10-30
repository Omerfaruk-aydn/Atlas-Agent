package workplan

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
)

// SpecStage is a reviewable specification milestone.
type SpecStage string

const (
	SpecRequirements SpecStage = "requirements"
	SpecDesign       SpecStage = "design"
	SpecTasks        SpecStage = "tasks"
)

var (
	specMu       sync.Mutex
	taskLine     = regexp.MustCompile(`^- \[([ xX])\] ([A-Za-z0-9][A-Za-z0-9_.-]*) — (.+)$`)
	verification = regexp.MustCompile("^  Verification: `([^`]+)`$")
)

// SpecSnapshot captures the revisions a user reviewed or edited. Contract
// Digest protects the JSON source of truth; document digests protect each
// approval from stale content.
type SpecSnapshot struct {
	ContractDigest string               `json:"contract_digest"`
	Documents      map[string]string    `json:"documents,omitempty"`
	Approved       map[SpecStage]string `json:"approved,omitempty"`
}

// LoadSpec reads the human-facing specification and its approval state.
func LoadSpec(root string) (SpecSnapshot, error) {
	plan, _, err := Load(root)
	if err != nil {
		return SpecSnapshot{}, err
	}
	snapshot := SpecSnapshot{ContractDigest: planDigest(plan), Documents: map[string]string{}, Approved: map[SpecStage]string{}}
	for _, stage := range []SpecStage{SpecRequirements, SpecDesign, SpecTasks} {
		data, err := os.ReadFile(specPath(root, stage))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return SpecSnapshot{}, err
		}
		snapshot.Documents[string(stage)] = digest(data)
	}
	data, err := os.ReadFile(filepath.Join(root, Dir, "spec", "state.json"))
	if err == nil {
		if err := json.Unmarshal(data, &snapshot.Approved); err != nil {
			return SpecSnapshot{}, fmt.Errorf("invalid spec approval state: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return SpecSnapshot{}, err
	}
	return snapshot, nil
}

// RenderSpec returns the three human-facing views of the contract. tasks.md
// is the only view whose checkbox edits synchronize back to work.json.
func RenderSpec(plan *Plan) map[SpecStage]string {
	requirements := "# Requirements\n\n## User story\n\n" + strings.TrimSpace(plan.Goal) + "\n\n## Acceptance criteria\n"
	for _, feature := range plan.Features {
		requirements += fmt.Sprintf("\n- **%s**: %s", feature.ID, feature.Title)
		if feature.Acceptance != "" {
			requirements += " — " + feature.Acceptance
		}
	}
	design := "# Design\n\n## Architecture\n\nSee the acceptance-linked implementation tasks below.\n\n## Data flow\n\nRequirements → implementation → verification.\n\n## Risks\n\nChanges must preserve existing behavior outside the declared feature scope.\n"
	tasks := RenderTasks(plan)
	return map[SpecStage]string{SpecRequirements: requirements + "\n", SpecDesign: design, SpecTasks: tasks}
}

// RenderTasks produces stable, checkable Markdown without inventing new task
// definitions. work.json remains authoritative for IDs, titles and commands.
func RenderTasks(plan *Plan) string {
	var b strings.Builder
	b.WriteString("# Tasks\n\n<!-- work.json is authoritative; edit checkboxes, then synchronize. -->\n")
	for _, feature := range plan.Features {
		checked := " "
		if feature.Done {
			checked = "x"
		}
		fmt.Fprintf(&b, "\n- [%s] %s — %s\n", checked, feature.ID, strings.ReplaceAll(feature.Title, "\n", " "))
		if feature.Verify != "" {
			fmt.Fprintf(&b, "  Verification: `%s`\n", strings.ReplaceAll(feature.Verify, "`", "'"))
		}
	}
	return b.String()
}

// WriteSpecStage writes a stage only when the contract has not changed since
// the supplied snapshot. Every stage starts unapproved and must be approved
// explicitly with ApproveSpecStage.
func WriteSpecStage(root string, snapshot SpecSnapshot, stage SpecStage, content string) error {
	specMu.Lock()
	defer specMu.Unlock()
	if err := checkContract(root, snapshot.ContractDigest); err != nil {
		return err
	}
	if stage != SpecRequirements && stage != SpecDesign && stage != SpecTasks {
		return fmt.Errorf("unknown spec stage %q", stage)
	}
	if stage == SpecTasks {
		return updateTasksLocked(root, snapshot.ContractDigest, content)
	}
	if err := os.MkdirAll(filepath.Dir(specPath(root, stage)), 0o755); err != nil {
		return err
	}
	return os.WriteFile(specPath(root, stage), []byte(content), 0o644)
}

// ApproveSpecStage records the exact reviewed document digest. It never
// approves a later, unreviewed edit implicitly.
func ApproveSpecStage(root string, stage SpecStage, documentDigest string) error {
	specMu.Lock()
	defer specMu.Unlock()
	data, err := os.ReadFile(specPath(root, stage))
	if err != nil {
		return err
	}
	if digest(data) != documentDigest {
		return errors.New("spec document changed after review; reload and approve again")
	}
	snapshot, err := LoadSpec(root)
	if err != nil {
		return err
	}
	var previous SpecStage
	switch stage {
	case SpecRequirements:
	case SpecDesign:
		previous = SpecRequirements
	case SpecTasks:
		previous = SpecDesign
	default:
		return fmt.Errorf("unknown spec stage %q", stage)
	}
	if previous != "" && snapshot.Approved[previous] != snapshot.Documents[string(previous)] {
		return fmt.Errorf("approve current %s before %s", previous, stage)
	}
	snapshot.Approved[stage] = documentDigest
	data, err = json.MarshalIndent(snapshot.Approved, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(root, Dir, "spec", "state.json"), append(data, '\n'), 0o644)
}

// UpdateTasks synchronizes checkbox changes only. Unknown IDs, duplicate IDs,
// edited titles, and removed tasks are rejected rather than weakening the
// verification contract.
func UpdateTasks(root, expectedContractDigest, markdown string) error {
	specMu.Lock()
	defer specMu.Unlock()
	return updateTasksLocked(root, expectedContractDigest, markdown)
}

func updateTasksLocked(root, expectedContractDigest, markdown string) error {
	if err := checkContract(root, expectedContractDigest); err != nil {
		return err
	}
	plan, _, err := Load(root)
	if err != nil {
		return err
	}
	if plan == nil {
		return errors.New("cannot synchronize tasks without a work contract")
	}
	done, err := parseTasks(markdown, plan)
	if err != nil {
		return err
	}
	for i := range plan.Features {
		plan.Features[i].Done = done[plan.Features[i].ID]
	}
	if err := saveAndRender(root, plan); err != nil {
		return err
	}
	return nil
}

// CompleteFeature atomically marks one feature and refreshes tasks.md while
// preserving user-authored requirements.md and design.md.
func CompleteFeature(root, id string, done bool) error {
	specMu.Lock()
	defer specMu.Unlock()
	plan, _, err := Load(root)
	if err != nil {
		return err
	}
	if plan == nil {
		return errors.New("cannot complete a task without a work contract")
	}
	found := false
	for i := range plan.Features {
		if plan.Features[i].ID == id {
			plan.Features[i].Done = done
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("unknown task %q", id)
	}
	return saveAndRender(root, plan)
}

func parseTasks(markdown string, plan *Plan) (map[string]bool, error) {
	done := map[string]bool{}
	var current string
	for _, line := range strings.Split(markdown, "\n") {
		if match := taskLine.FindStringSubmatch(line); match != nil {
			id := match[2]
			feature, ok := plan.Feature(id)
			if !ok {
				return nil, fmt.Errorf("task %q is not in work.json", id)
			}
			if _, duplicate := done[id]; duplicate {
				return nil, fmt.Errorf("duplicate task %q", id)
			}
			if strings.TrimSpace(match[3]) != strings.TrimSpace(feature.Title) {
				return nil, fmt.Errorf("task %q title differs from work.json", id)
			}
			done[id] = match[1] != " "
			current = id
			continue
		}
		if match := verification.FindStringSubmatch(line); match != nil {
			if current == "" {
				return nil, errors.New("verification command has no task")
			}
			feature, _ := plan.Feature(current)
			if match[1] != feature.Verify {
				return nil, fmt.Errorf("task %q verification differs from work.json", current)
			}
		}
	}
	if len(done) != len(plan.Features) {
		return nil, errors.New("tasks.md must contain every work.json task exactly once")
	}
	return done, nil
}

func saveAndRender(root string, plan *Plan) error {
	data, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(root, Dir, "spec"), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(root, Dir, PlanFile), append(data, '\n'), 0o644); err != nil {
		return err
	}
	return os.WriteFile(specPath(root, SpecTasks), []byte(RenderTasks(plan)), 0o644)
}

func checkContract(root, expected string) error {
	plan, _, err := Load(root)
	if err != nil {
		return err
	}
	if planDigest(plan) != expected {
		return errors.New("work contract changed after the spec was loaded; reload before synchronizing")
	}
	return nil
}

func planDigest(plan *Plan) string {
	if plan == nil {
		return ""
	}
	data, _ := json.Marshal(plan)
	return digest(data)
}

func digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func specPath(root string, stage SpecStage) string {
	return filepath.Join(root, Dir, "spec", string(stage)+".md")
}

// ApprovedStages returns approved stage names in deterministic order.
func (s SpecSnapshot) ApprovedStages() []SpecStage {
	stages := make([]SpecStage, 0, len(s.Approved))
	for stage := range s.Approved {
		stages = append(stages, stage)
	}
	sort.Slice(stages, func(i, j int) bool { return stages[i] < stages[j] })
	return stages
}
