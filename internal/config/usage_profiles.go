package config

import (
	"fmt"
	"slices"
)

// UsageProfile binds execution limits, model roles and output expectations.
// Models are selected from configured roles, never a hardcoded catalog ID.
type UsageProfile struct {
	Description  string   `json:"description,omitempty"`
	ModelRole    string   `json:"model_role,omitempty"`
	ReadOnly     bool     `json:"read_only,omitempty"`
	AllowedTools []string `json:"allowed_tools,omitempty"`
	MaxSteps     int      `json:"max_steps"`
	MaxAgents    int      `json:"max_agents"`
	Instructions string   `json:"instructions"`
}

func BuiltinUsageProfiles() map[string]UsageProfile {
	return map[string]UsageProfile{
		"economical":     {Description: "Bounded everyday work with minimal context and concise output", MaxSteps: 24, MaxAgents: 2, Instructions: "Use the smallest sufficient scope. Prefer targeted reads and checks; avoid redundant tools. Report concise changes and verification."},
		"research":       {Description: "Read-only investigation and evidence gathering", ReadOnly: true, MaxSteps: 48, MaxAgents: 4, Instructions: "Investigate without modifying files or executing commands. Separate observations from inferences, cite source paths and report unknowns."},
		"implementation": {Description: "Implementation with relevant verification", MaxSteps: 96, MaxAgents: 4, Instructions: "Implement coherent changes following project conventions. Verify changed behavior with relevant checks and report remaining limitations."},
		"review":         {Description: "Independent read-only review", ReadOnly: true, MaxSteps: 48, MaxAgents: 3, Instructions: "Review actual code and report actionable findings with source locations and concrete failure scenarios. Do not make changes or execute commands."},
		"ci":             {Description: "Bounded reproducible automation with structured reporting", MaxSteps: 64, MaxAgents: 2, Instructions: "Run only work requested by the automation task. Produce a concise structured report containing status, changes, observed checks and blockers. Do not broaden scope."},
	}
}

func (c *Config) UsageProfiles() map[string]UsageProfile {
	profiles := BuiltinUsageProfiles()
	if c.Options != nil {
		for name, profile := range c.Options.UsageProfiles {
			profiles[name] = profile
		}
	}
	return profiles
}

func (c *Config) ApplyUsageProfile(name string) error {
	profile, ok := c.UsageProfiles()[name]
	if !ok {
		return fmt.Errorf("unknown usage profile %q", name)
	}
	if profile.MaxSteps < 1 || profile.MaxSteps > 512 || profile.MaxAgents < 1 || profile.MaxAgents > 16 || len(profile.Instructions) > 8192 || len(profile.AllowedTools) > 128 {
		return fmt.Errorf("invalid usage profile bounds for %q", name)
	}
	var selected SelectedModel
	if profile.ModelRole != "" {
		var ok bool
		selected, ok = c.ResolveRole(profile.ModelRole)
		if !ok {
			return fmt.Errorf("profile %q references unknown model role %q", name, profile.ModelRole)
		}
	}
	if c.Options == nil {
		c.Options = &Options{}
	}
	c.Options.UsageProfile = name
	c.Options.MaxStepsPerTurn = profile.MaxSteps
	c.Options.MaxConcurrentSubAgents = profile.MaxAgents
	if profile.ModelRole != "" {
		if c.Models == nil {
			c.Models = map[SelectedModelType]SelectedModel{}
		}
		c.Models[SelectedModelTypeLarge] = selected
	}
	return nil
}

func (c *Config) profileTools(allowed []string) []string {
	profile, ok := c.UsageProfiles()[c.Options.UsageProfile]
	if !ok {
		return allowed
	}
	if profile.ReadOnly {
		allowed = resolveReadOnlyTools(allowed)
	}
	if len(profile.AllowedTools) > 0 {
		out := []string{}
		for _, name := range allowed {
			if slices.Contains(profile.AllowedTools, name) {
				out = append(out, name)
			}
		}
		allowed = out
	}
	return allowed
}

// OverrideUsageProfile applies an ephemeral CLI selection before agents start.
func (s *ConfigStore) OverrideUsageProfile(name string) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	candidate := s.Config().cloneForWrite()
	if err := candidate.ApplyUsageProfile(name); err != nil {
		return err
	}
	candidate.SetupAgents()
	s.overrides.UsageProfile = &name
	s.setConfig(candidate)
	return nil
}
