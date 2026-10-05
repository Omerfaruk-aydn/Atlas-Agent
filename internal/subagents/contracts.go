package subagents

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// RoleContract separates responsibility and routing from provider/model choice.
type RoleContract struct {
	TaskTypes         []string `yaml:"task_types" json:"task_types"`
	Responsibilities  []string `yaml:"responsibilities" json:"responsibilities"`
	Inputs            []string `yaml:"inputs" json:"inputs"`
	Outputs           []string `yaml:"outputs" json:"outputs"`
	Completion        []string `yaml:"completion" json:"completion"`
	DecisionRights    []string `yaml:"decision_rights,omitempty" json:"decision_rights,omitempty"`
	OutOfScope        []string `yaml:"out_of_scope,omitempty" json:"out_of_scope,omitempty"`
	StopConditions    []string `yaml:"stop_conditions,omitempty" json:"stop_conditions,omitempty"`
	EvidenceRequired  []string `yaml:"evidence_required,omitempty" json:"evidence_required,omitempty"`
	RequiredTools     []string `yaml:"required_tools,omitempty" json:"required_tools,omitempty"`
	IndependentReview bool     `yaml:"independent_review,omitempty" json:"independent_review,omitempty"`
}

func (r *RoleContract) Validate() error {
	if r == nil {
		return nil
	}
	for _, list := range [][]string{r.TaskTypes, r.Responsibilities, r.Inputs, r.Outputs, r.Completion} {
		if len(list) == 0 || len(list) > 16 {
			return fmt.Errorf("role contract lists require 1-16 entries")
		}
		for _, entry := range list {
			if strings.TrimSpace(entry) == "" || len(entry) > 512 {
				return fmt.Errorf("invalid role contract entry")
			}
		}
	}
	if len(r.RequiredTools) > 32 {
		return fmt.Errorf("too many required tools")
	}
	for _, list := range [][]string{r.DecisionRights, r.OutOfScope, r.StopConditions, r.EvidenceRequired} {
		if len(list) > 16 {
			return fmt.Errorf("role policy lists require at most 16 entries")
		}
		for _, entry := range list {
			if strings.TrimSpace(entry) == "" || len(entry) > 512 {
				return fmt.Errorf("invalid role policy entry")
			}
		}
	}
	for _, tool := range r.RequiredTools {
		if !namePattern.MatchString(strings.ReplaceAll(tool, "_", "-")) {
			return fmt.Errorf("invalid required tool %q", tool)
		}
	}
	return nil
}

func (s *Subagent) RolePrompt() string {
	if s.Contract == nil {
		return ""
	}
	data, _ := json.Marshal(s.Contract)
	bindings, _ := json.Marshal(s.PreferredSkills)
	return "\n\n<role_contract>\n" + string(data) + "\nPreferred skill names: " + string(bindings) + ". Use selected role guidance when applicable; unavailable skills do not grant substitute capabilities.\nUse the assignment's owned paths and acceptance criteria as the task boundary. " +
		"Missing inputs must be investigated or reported as blocked. Report actual checks and unresolved risks; do not infer successful execution. " +
		"Decision rights describe routine choices inside the assignment; they do not grant new tool permissions or publication authority. Stop only the affected dependency and continue independent authorized work. " +
		"For workflow assignments return only the requested JSON handoff. The coordinator independently integrates and verifies completion.\n</role_contract>"
}

// RouteRequest supplies explicit task requirements; text inference is a fallback.
type RouteRequest struct {
	Prompt        string   `json:"prompt"`
	TaskType      string   `json:"task_type,omitempty"`
	RequiredTools []string `json:"required_tools,omitempty"`
	Output        string   `json:"output,omitempty"`
}

type RouteResult struct {
	Agent  *Subagent `json:"-"`
	Name   string    `json:"agent"`
	Reason string    `json:"reason"`
}

// Route filters incompatible capabilities before ranking task and output matches.
func Route(all []*Subagent, req RouteRequest, available []string) (RouteResult, bool) {
	taskType := strings.ToLower(strings.TrimSpace(req.TaskType))
	if taskType == "" {
		taskType = inferTaskType(req.Prompt)
	}
	var best *Subagent
	bestScore := 0
	for _, s := range all {
		required := append([]string{}, req.RequiredTools...)
		if s.Contract != nil {
			required = append(required, s.Contract.RequiredTools...)
		}
		compatible := true
		for _, tool := range required {
			if !s.SupportsTool(tool) || (available != nil && !slices.Contains(available, tool)) {
				compatible = false
				break
			}
		}
		if !compatible {
			continue
		}
		score := overlapCount(matchWords(req.Prompt), matchWords(s.Name+" "+s.Description))
		if s.Contract != nil && taskType != "" {
			if !slices.Contains(s.Contract.TaskTypes, taskType) {
				continue
			}
			score += 100
		} else if req.TaskType != "" {
			continue
		}
		if req.Output != "" {
			if s.Contract == nil || !slices.Contains(s.Contract.Outputs, req.Output) {
				continue
			}
			score += 50
		}
		if score > bestScore {
			best, bestScore = s, score
		}
	}
	if best == nil {
		return RouteResult{}, false
	}
	return RouteResult{Agent: best, Name: best.Name, Reason: fmt.Sprintf("task_type=%s; compatible tools; score=%d", taskType, bestScore)}, true
}

// SupportsTool matches the restricted specialist policy without granting tools.
func (s *Subagent) SupportsTool(tool string) bool {
	if len(s.Tools) > 0 && !slices.Contains(s.Tools, tool) {
		return false
	}
	if s.ReadOnly {
		if slices.Contains(CommandTools, tool) {
			return s.AllowCommands
		}
		return slices.Contains(ReadTools, tool)
	}
	if len(s.Tools) == 0 && slices.Contains([]string{"agent", "delegate", "orchestrate", "debate", "workflow", "worktree"}, tool) {
		return false
	}
	return true
}

var (
	CommandTools = []string{"bash", "test_run", "lint_run", "verify", "job_output", "job_kill"}
	ReadTools    = []string{"view", "glob", "grep", "ls", "lsp_symbols", "lsp_definition", "lsp_call_hierarchy", "lsp_references", "lsp_edit_plan", "git_status", "git_diff", "git_log", "git_blame", "import_graph", "impact_analysis", "api_surface", "design_search", "project_map", "sourcegraph", "web_search", "fetch", "usage", "session_search"}
)

func inferTaskType(prompt string) string {
	words := strings.FieldsFunc(strings.ToLower(prompt), func(r rune) bool { return (r < 'a' || r > 'z') && r <= 127 })
	if (slices.Contains(words, "visual") || slices.Contains(words, "görsel")) && (slices.Contains(words, "review") || slices.Contains(words, "kontrol") || slices.Contains(words, "incele")) {
		return "visual-quality"
	}
	// Review and diagnosis take precedence over the subject being inspected.
	for _, group := range []struct {
		kind  string
		words []string
	}{
		{"review", []string{"review", "incele", "denetle"}},
		{"debug", []string{"debug", "diagnose", "reproduce", "hata"}},
		{"test", []string{"test", "tests", "testing", "regression"}},
		{"security", []string{"security", "vulnerability", "güvenlik"}},
		{"architecture", []string{"architecture", "architect", "mimari"}},
		{"planning", []string{"plan", "planner", "planning", "planla"}},
		{"template", []string{"template", "templates", "şablon", "şablonu"}},
		{"presentation", []string{"presentation", "presentations", "slides", "pptx", "sunum", "slayt"}},
		{"document", []string{"docx", "pdf", "document", "belge"}},
		{"data-analysis", []string{"spreadsheet", "xlsx", "csv", "analytics", "istatistik", "tablo"}},
		{"desktop", []string{"desktop", "masaüstü", "computer"}},
		{"browser", []string{"browser", "tarayıcı", "tarayıcıda"}},
		{"integration", []string{"mcp", "connector", "integration", "entegrasyon"}},
		{"operations", []string{"slack", "calendar", "takvim", "toplantı", "scheduling"}},
		{"motion", []string{"motion", "animation", "animasyon", "animasyonu"}},
		{"product-design", []string{"figma", "tasarla", "tasarımı", "designer"}},
		{"frontend", []string{"frontend", "ui", "ux", "responsive", "arayüz"}},
		{"backend", []string{"backend", "api", "database", "veritabanı"}},
		{"docs", []string{"documentation", "docs", "dokümantasyon"}},
		{"refactor", []string{"refactor", "refactoring"}},
		{"research", []string{"research", "investigate", "araştır"}},
	} {
		for _, word := range group.words {
			if slices.Contains(words, word) {
				return group.kind
			}
		}
	}
	return ""
}
