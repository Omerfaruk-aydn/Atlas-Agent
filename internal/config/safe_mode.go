package config

// applySafeMode retains provider authentication and model selection while
// removing every executable or prompt-altering customization. It runs after
// defaults and workspace model state have been merged.
func (c *Config) applySafeMode() {
	c.MCP = nil
	c.LSP = nil
	c.Hooks = nil
	c.Env = nil
	c.Permissions = nil
	c.Tools = Tools{}
	if c.Options == nil {
		c.Options = &Options{}
	}
	c.Options.SafeMode = true
	c.Options.ContextPaths = nil
	c.Options.GlobalContextPaths = nil
	c.Options.SkillsPaths = nil
	c.Options.SubagentsPaths = nil
	c.Options.WorkflowPaths = nil
	c.Options.OutputStyle = ""
	c.Options.UsageProfile = ""
	c.Options.UsageProfiles = nil
	c.Options.ModelRoles = nil
	c.Options.ModelFallbacks = nil
	c.Options.AgentModels = nil
	c.Options.DisabledSkills = nil
	c.Options.DisabledTools = nil
	c.Options.Execution = nil
	c.Options.Memory = nil
}
