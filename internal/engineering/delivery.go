package engineering

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

type WorkProfile struct {
	Name     string   `json:"name"`
	Workflow []string `json:"workflow"`
}

func Profiles() []WorkProfile {
	return []WorkProfile{
		{"small_fix", []string{"Inspect affected path", "Make a focused reversible change", "Run a relevant regression or behavior check"}},
		{"feature", []string{"Map architecture and requirements", "Define contracts", "Implement an integrated vertical slice", "Expand in dependency order", "Review and verify integrated behavior"}},
		{"migration", []string{"Inspect existing data and coexisting versions", "Record compatibility decision and recovery limits", "Plan resumable stages", "Verify each stage before switching authoritative behavior"}},
		{"ui", []string{"Record design brief and user flow", "Implement real state and interactions", "Capture narrow and wide evidence", "Critique visual and interaction criteria", "Repair findings and capture fresh evidence"}},
		{"research", []string{"Define the question and decision", "Inspect primary evidence", "Separate observation and inference", "Report bounded unknowns without implementation"}},
	}
}

func ValidProfile(name string) bool {
	for _, p := range Profiles() {
		if p.Name == name {
			return true
		}
	}
	return false
}

// InferProfile is a disclosed deterministic hint, never a scope or permission.
func InferProfile(prompt string) string {
	p := strings.ToLower(prompt)
	for _, group := range []struct {
		name  string
		words []string
	}{
		{"migration", []string{"migrat", "schema", "şema", "veri taşı"}},
		{"ui", []string{"frontend", "arayüz", "ui/ux", "tasarım", "responsive"}},
		{"research", []string{"research", "araştır", "sadece analiz", "investigate"}},
		{"small_fix", []string{"bug", "hata düzelt", "fix", "regression"}},
	} {
		for _, word := range group.words {
			if strings.Contains(p, word) {
				return group.name
			}
		}
	}
	return "feature"
}

type Requirement struct {
	ID          string   `json:"id"`
	Description string   `json:"description"`
	TaskIDs     []string `json:"task_ids"`
}

type Stage struct {
	ID                string   `json:"id"`
	Title             string   `json:"title"`
	TaskIDs           []string `json:"task_ids"`
	Checks            []Check  `json:"checks,omitempty"`
	SourceFingerprint string   `json:"source_fingerprint,omitempty"`
	Passed            bool     `json:"passed"`
}

type DesignBrief struct {
	Target          string       `json:"target"`
	Audience        string       `json:"audience"`
	PrimaryFlow     string       `json:"primary_flow"`
	VisualDirection string       `json:"visual_direction"`
	States          []string     `json:"states"`
	Criteria        []string     `json:"criteria"`
	TaskIDs         []string     `json:"task_ids"`
	MinimumWidth    int          `json:"minimum_width"`
	WideWidth       int          `json:"wide_width"`
	Critiques       []UICritique `json:"critiques,omitempty"`
}

type UICritique struct {
	States            []string        `json:"states"`
	ArtifactID        string          `json:"artifact_id"`
	SourceFingerprint string          `json:"source_fingerprint"`
	Findings          []DesignFinding `json:"findings"`
	CheckedAt         int64           `json:"checked_at"`
}

type DesignFinding struct {
	Criterion string `json:"criterion"`
	Passed    bool   `json:"passed"`
	Evidence  string `json:"evidence"`
}

type UIEvidence struct {
	ScenarioStore     string       `json:"scenario_store,omitempty"`
	ScenarioProof     *ArtifactRef `json:"scenario_proof,omitempty"`
	ID                string       `json:"id"`
	TaskID            string       `json:"task_id,omitempty"`
	Path              string       `json:"path"`
	Hash              string       `json:"hash"`
	Width             int          `json:"width"`
	Height            int          `json:"height"`
	Target            string       `json:"target"`
	SourceFingerprint string       `json:"source_fingerprint"`
	AssertionsPassed  bool         `json:"assertions_passed"`
	Origin            string       `json:"origin"`
	RecordedAt        int64        `json:"recorded_at"`
}

type DeliveryPlan struct {
	TaskContractRefs map[string][]Record `json:"task_contract_refs,omitempty"`
	Root             string              `json:"root"`
	Profile          string              `json:"profile"`
	Requirements     []Requirement       `json:"requirements"`
	Stages           []Stage             `json:"stages"`
	CurrentStage     int                 `json:"current_stage"`
	TaskFingerprints map[string]string   `json:"task_fingerprints"`
	Design           *DesignBrief        `json:"design,omitempty"`
}

func boundedText(s string, max int) bool { return strings.TrimSpace(s) != "" && len(s) <= max }

func validID(s string) bool {
	if !boundedText(s, 64) {
		return false
	}
	for _, r := range s {
		allowed := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_'
		if !allowed {
			return false
		}
	}
	return true
}

func (p DeliveryPlan) Validate() error {
	if !ValidProfile(p.Profile) || !boundedText(p.Root, 4096) || len(p.Requirements) == 0 || len(p.Requirements) > 128 || len(p.Stages) == 0 || len(p.Stages) > 32 || p.CurrentStage < 0 || p.CurrentStage > len(p.Stages) {
		return fmt.Errorf("invalid delivery profile, root or collection size")
	}
	tasks, stages, requirements := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for index, stage := range p.Stages {
		if !validID(stage.ID) || stages[stage.ID] || !boundedText(stage.Title, 512) || len(stage.TaskIDs) == 0 || len(stage.TaskIDs) > 256 {
			return fmt.Errorf("invalid or duplicate stage")
		}
		stages[stage.ID] = true
		if index < p.CurrentStage {
			if !stage.Passed || stage.SourceFingerprint == "" || len(stage.Checks) == 0 {
				return fmt.Errorf("advanced stage requires observed checks and a source snapshot")
			}
			for _, check := range stage.Checks {
				if !check.Passed || check.Evidence == "" || check.RunID == "" || check.TaskID != "stage:"+stage.ID {
					return fmt.Errorf("invalid certified stage check")
				}
			}
		} else if stage.Passed || stage.SourceFingerprint != "" || len(stage.Checks) > 0 {
			return fmt.Errorf("future stage cannot contain certification")
		}
		for _, id := range stage.TaskIDs {
			if !validID(id) || tasks[id] || p.TaskFingerprints[id] == "" {
				return fmt.Errorf("each task must appear in exactly one stage with a fingerprint")
			}
			tasks[id] = true
		}
	}
	for _, r := range p.Requirements {
		if !validID(r.ID) || requirements[r.ID] || !boundedText(r.Description, 2048) || len(r.TaskIDs) == 0 || len(r.TaskIDs) > 32 {
			return fmt.Errorf("invalid requirement")
		}
		requirements[r.ID] = true
		seen := map[string]bool{}
		for _, id := range r.TaskIDs {
			if !tasks[id] || seen[id] {
				return fmt.Errorf("requirement references unknown or duplicate task")
			}
			seen[id] = true
		}
	}
	for task, refs := range p.TaskContractRefs {
		if !tasks[task] || len(refs) > 128 {
			return fmt.Errorf("invalid task contract references")
		}
		for _, ref := range refs {
			if ref.Revision == 0 || ref.Ref.Kind != "record" {
				return fmt.Errorf("invalid contract revision reference")
			}
			if err := ref.Ref.validate(); err != nil {
				return err
			}
		}
	}
	if p.Design != nil {
		return p.Design.Validate(tasks)
	}
	return nil
}

func (d DesignBrief) Validate(tasks map[string]bool) error {
	if d.Target != "web" && d.Target != "tui" {
		return fmt.Errorf("design target must be web or tui")
	}
	if !boundedText(d.Audience, 1024) || !boundedText(d.PrimaryFlow, 2048) || !boundedText(d.VisualDirection, 2048) || len(d.States) == 0 || len(d.States) > 16 || len(d.Criteria) == 0 || len(d.Criteria) > 16 || len(d.TaskIDs) == 0 || len(d.TaskIDs) > 32 || d.MinimumWidth < 20 || d.WideWidth <= d.MinimumWidth || d.WideWidth > 3840 {
		return fmt.Errorf("invalid design brief or viewport range")
	}
	for _, list := range [][]string{d.States, d.Criteria} {
		seen := map[string]bool{}
		for _, value := range list {
			if !boundedText(value, 512) || seen[value] {
				return fmt.Errorf("invalid or duplicate design state/criterion")
			}
			seen[value] = true
		}
	}
	seenTasks := map[string]bool{}
	for _, id := range d.TaskIDs {
		if !tasks[id] || seenTasks[id] {
			return fmt.Errorf("design references an unknown task")
		}
		seenTasks[id] = true
	}
	if d.Target == "web" && d.MinimumWidth < 240 || d.Target == "tui" && d.WideWidth > 1000 {
		return fmt.Errorf("design widths must match target capture limits")
	}
	return nil
}

func (p DeliveryPlan) Fingerprint() string { data, _ := json.Marshal(p); return Hash(string(data)) }

func (p DeliveryPlan) AllowsTask(id string) bool {
	return p.CurrentStage >= 0 && p.CurrentStage < len(p.Stages) && slices.Contains(p.Stages[p.CurrentStage].TaskIDs, id)
}

// DesignReady requires fresh reported critiques linked to actual artifacts.
// Reported visual judgments remain model/user evidence, not machine proof.
func (p DeliveryPlan) DesignReady(ctx context.Context, st State, source string, stage Stage) error {
	d := p.Design
	if d == nil {
		return nil
	}
	needed := false
	for _, id := range stage.TaskIDs {
		if slices.Contains(d.TaskIDs, id) {
			needed = true
		}
	}
	if !needed {
		return nil
	}
	narrow, wide := false, false
	observedStates := map[string]bool{}
	for _, critique := range d.Critiques {
		if critique.SourceFingerprint != source {
			continue
		}
		for _, artifact := range st.UIEvidence {
			if artifact.ID != critique.ArtifactID || artifact.SourceFingerprint != source || artifact.Target != d.Target || !artifact.AssertionsPassed {
				continue
			}
			if err := ValidateUIArtifact(ctx, artifact); err != nil {
				return err
			}
			passed := true
			for _, finding := range critique.Findings {
				if !finding.Passed {
					return fmt.Errorf("unresolved design finding: %s", finding.Criterion)
				}
			}
			for _, criterion := range d.Criteria {
				found := false
				for _, finding := range critique.Findings {
					if finding.Criterion == criterion && finding.Passed && boundedText(finding.Evidence, 2048) {
						found = true
					}
				}
				passed = passed && found
			}
			if passed {
				for _, state := range critique.States {
					observedStates[state] = true
				}
				narrow = narrow || artifact.Width <= d.MinimumWidth
				wide = wide || artifact.Width >= d.WideWidth
			}
		}
	}
	if !narrow || !wide {
		return fmt.Errorf("design requires fresh passing critiques with actual narrow and wide artifacts")
	}
	for _, state := range d.States {
		if !observedStates[state] {
			return fmt.Errorf("design state has no fresh critique evidence: %s", state)
		}
	}
	return nil
}
