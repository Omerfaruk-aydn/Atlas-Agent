package tools

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/permission"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/subagents"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// Configuration is something the user says, not something they go and
// find. "Use the cheap model for summaries" is a sentence; making the
// user translate it into a dialog, a role name and a model id is work
// the agent is better placed to do, and work they have to redo from
// memory the next time.
//
// So this tool exists alongside the dialogs rather than replacing them:
// everything remains where it was for anyone who prefers clicking, and
// the conversation becomes a way in for everyone else.
//
// Writes go through ConfigStore, which validates nothing about meaning
// but does hold both an in-process mutex and a cross-process lock, and
// reloads in-memory state afterwards -- so a change takes effect on the
// next turn rather than the next restart. What meaning-level checking
// there is happens here: a role pointed at a provider the user has not
// configured is the mistake worth catching, because it fails much later
// and somewhere confusing.

const AtlasConfigToolName = "atlas_config"

//go:embed atlas_config.md
var atlasConfigDescription string

// AtlasConfigParams is the tool's input.
type AtlasConfigParams struct {
	Action       string `json:"action"`
	Scope        string `json:"scope,omitempty"`
	Role         string `json:"role,omitempty"`
	ModelType    string `json:"model_type,omitempty"`
	Provider     string `json:"provider,omitempty"`
	Model        string `json:"model,omitempty"`
	Tool         string `json:"tool,omitempty"`
	Key          string `json:"key,omitempty"`
	Value        any    `json:"value,omitempty"`
	Name         string `json:"name,omitempty"`
	Description  string `json:"description,omitempty"`
	Instructions string `json:"instructions,omitempty"`
}

// AtlasConfigPermissionParams is what the permission prompt shows.
type AtlasConfigPermissionParams struct {
	Action string `json:"action"`
	Scope  string `json:"scope,omitempty"`
	Detail string `json:"detail,omitempty"`
}

// atlasConfigReadOnly marks the actions that change nothing, so they do
// not interrupt the user for approval they have no reason to withhold.
var atlasConfigReadOnly = map[string]bool{
	"list":           true,
	"get_field":      true,
	"list_subagents": true,
}

var atlasConfigActions = []string{
	"list", "set_role", "set_model", "enable_tool", "disable_tool", "set_field", "get_field",
	"save_subagent", "delete_subagent", "list_subagents",
}

func NewAtlasConfigTool(permissions permission.Service, cfg *config.ConfigStore, workingDir string) fantasy.AgentTool {
	return fantasy.NewAgentTool(
		AtlasConfigToolName,
		atlasConfigDescription,
		func(ctx context.Context, params AtlasConfigParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			action := strings.ToLower(strings.TrimSpace(params.Action))
			if !slices.Contains(atlasConfigActions, action) {
				return fantasy.NewTextErrorResponse(fmt.Sprintf(
					"unknown action %q, must be one of: %s", params.Action, strings.Join(atlasConfigActions, ", "))), nil
			}

			scope, scopeName, err := atlasConfigScope(params.Scope)
			if err != nil {
				return fantasy.NewTextErrorResponse(err.Error()), nil
			}

			if !atlasConfigReadOnly[action] {
				sessionID := GetSessionFromContext(ctx)
				if sessionID == "" {
					return fantasy.ToolResponse{}, fmt.Errorf("session ID is required for changing configuration")
				}
				granted, err := permissions.Request(ctx, permission.CreatePermissionRequest{
					SessionID:   sessionID,
					Path:        workingDir,
					ToolCallID:  call.ID,
					ToolName:    AtlasConfigToolName,
					Action:      action,
					Description: atlasConfigDescribe(action, params, scopeName),
					Params: AtlasConfigPermissionParams{
						Action: action,
						Scope:  scopeName,
						Detail: atlasConfigDescribe(action, params, scopeName),
					},
				})
				if err != nil {
					return fantasy.ToolResponse{}, err
				}
				if !granted {
					return NewPermissionDeniedResponse(permissions), nil
				}
			}

			switch action {
			case "list":
				return fantasy.NewTextResponse(atlasConfigList(cfg)), nil
			case "get_field":
				return atlasConfigGetField(cfg, params)
			case "set_role":
				return atlasConfigSetRole(cfg, scope, scopeName, params)
			case "set_model":
				return atlasConfigSetModel(cfg, scope, scopeName, params)
			case "enable_tool", "disable_tool":
				return atlasConfigToggleTool(cfg, scope, scopeName, params, action == "disable_tool")
			case "save_subagent":
				return atlasConfigSaveSubagent(cfg, workingDir, scope, scopeName, params)
			case "delete_subagent":
				return atlasConfigDeleteSubagent(cfg, params)
			case "list_subagents":
				return fantasy.NewTextResponse(atlasConfigListSubagents(cfg)), nil
			default:
				return atlasConfigSetField(cfg, scope, scopeName, params)
			}
		},
	)
}

// atlasConfigScope maps the requested scope onto the store's, defaulting
// to global: a setting the user asked for in conversation is one they
// will expect to find still applying tomorrow, in whatever directory
// they happen to open.
func atlasConfigScope(raw string) (config.Scope, string, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "global", "user":
		return config.ScopeGlobal, "global", nil
	case "workspace", "project", "local":
		return config.ScopeWorkspace, "workspace", nil
	default:
		return 0, "", fmt.Errorf("unknown scope %q, must be \"global\" or \"workspace\"", raw)
	}
}

// atlasConfigDescribe renders the change in the words the permission
// prompt should show, since "atlas_config set_role" tells the user
// nothing about what is about to happen to their setup.
func atlasConfigDescribe(action string, p AtlasConfigParams, scopeName string) string {
	switch action {
	case "set_role":
		if p.Provider == "" && p.Model == "" {
			return fmt.Sprintf("Clear the %q model role (%s)", p.Role, scopeName)
		}
		return fmt.Sprintf("Run the %q role on %s/%s (%s)", p.Role, p.Provider, p.Model, scopeName)
	case "set_model":
		return fmt.Sprintf("Switch the %s model to %s/%s (%s)", cmpOr(p.ModelType, "large"), p.Provider, p.Model, scopeName)
	case "enable_tool":
		return fmt.Sprintf("Enable the %q tool (%s)", p.Tool, scopeName)
	case "disable_tool":
		return fmt.Sprintf("Disable the %q tool (%s)", p.Tool, scopeName)
	case "set_field":
		return fmt.Sprintf("Set %s to %v (%s)", p.Key, p.Value, scopeName)
	case "save_subagent":
		return fmt.Sprintf("Save the %q subagent (%s)", p.Name, scopeName)
	case "delete_subagent":
		return fmt.Sprintf("Delete the %q subagent", p.Name)
	default:
		return action
	}
}

func cmpOr(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}

// atlasConfigList reports what there is to choose from. Without it the
// agent guesses at provider and model ids, and a guess that is close but
// wrong fails at the next turn rather than at this one.
func atlasConfigList(store *config.ConfigStore) string {
	cfg := store.Config()
	var b strings.Builder

	b.WriteString("[providers and models]\n")
	if cfg.Providers == nil || cfg.Providers.Len() == 0 {
		b.WriteString("  none configured\n")
	} else {
		ids := make([]string, 0, cfg.Providers.Len())
		for id := range cfg.Providers.Seq2() {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			p, ok := cfg.Providers.Get(id)
			if !ok || p.Disable {
				continue
			}
			fmt.Fprintf(&b, "  %s\n", id)
			for _, m := range p.Models {
				fmt.Fprintf(&b, "    %s\n", m.ID)
			}
		}
	}

	b.WriteString("\n[session models]\n")
	for _, t := range []config.SelectedModelType{config.SelectedModelTypeLarge, config.SelectedModelTypeSmall} {
		if m, ok := cfg.Models[t]; ok {
			fmt.Fprintf(&b, "  %s: %s/%s\n", t, m.Provider, m.Model)
		} else {
			fmt.Fprintf(&b, "  %s: not set\n", t)
		}
	}

	b.WriteString("\n[model roles]\n")
	if cfg.Options == nil || len(cfg.Options.ModelRoles) == 0 {
		b.WriteString("  none assigned\n")
	} else {
		names := make([]string, 0, len(cfg.Options.ModelRoles))
		for name := range cfg.Options.ModelRoles {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			m := cfg.Options.ModelRoles[name]
			fmt.Fprintf(&b, "  %s: %s/%s\n", name, m.Provider, m.Model)
		}
	}

	b.WriteString("\n[disabled tools]\n")
	if cfg.Options == nil || len(cfg.Options.DisabledTools) == 0 {
		b.WriteString("  none -- every tool is available\n")
	} else {
		for _, t := range cfg.Options.DisabledTools {
			fmt.Fprintf(&b, "  %s\n", t)
		}
	}

	// Shown here too, not just under list_subagents: a subagent named in
	// a debate/orchestrate/delegate/agent call is one of these, and a
	// built-in one (review, security, ...) whose model role has nothing
	// assigned yet fails at call time with an error about a role nobody
	// asked about by name. Surfacing that gap in the first list call
	// this tool gets means it never has to be discovered by trial and
	// error against several providers.
	b.WriteString("\n[subagents]\n")
	for _, s := range subagents.Discover(subagentsPaths(store)) {
		roleNote := ""
		if s.Model != "" {
			if _, ok := cfg.ResolveRole(s.Model); !ok {
				roleNote = fmt.Sprintf(" (needs the %q model role assigned -- set_role -- before it can run)", config.StripRoleReference(s.Model))
			}
		}
		fmt.Fprintf(&b, "  %s: %s%s\n", s.Name, s.Description, roleNote)
	}
	return b.String()
}

// knownProviderModel reports whether the provider is configured and, when
// a model is named, whether that provider actually offers it.
func knownProviderModel(store *config.ConfigStore, provider, model string) error {
	cfg := store.Config()
	if cfg.Providers == nil {
		return fmt.Errorf("no providers are configured")
	}
	p, ok := cfg.Providers.Get(provider)
	if !ok || p.Disable {
		return fmt.Errorf("provider %q is not configured; call atlas_config with action \"list\" to see what is", provider)
	}
	if model == "" {
		return nil
	}
	for _, m := range p.Models {
		if m.ID == model {
			return nil
		}
	}
	return fmt.Errorf("provider %q has no model %q; call atlas_config with action \"list\" to see its models", provider, model)
}

func atlasConfigSetRole(store *config.ConfigStore, scope config.Scope, scopeName string, p AtlasConfigParams) (fantasy.ToolResponse, error) {
	role := strings.TrimSpace(p.Role)
	if role == "" {
		return fantasy.NewTextErrorResponse("role is required"), nil
	}

	key := "options.model_roles." + role
	if p.Provider == "" && p.Model == "" {
		if err := store.SetConfigField(scope, key, nil); err != nil {
			return fantasy.ToolResponse{}, err
		}
		return fantasy.NewTextResponse(fmt.Sprintf("Cleared the %q role (%s). It falls back to the session's own model.", role, scopeName)), nil
	}

	if err := knownProviderModel(store, p.Provider, p.Model); err != nil {
		return fantasy.NewTextErrorResponse(err.Error()), nil
	}
	value := map[string]any{"provider": p.Provider, "model": p.Model}
	if err := store.SetConfigField(scope, key, value); err != nil {
		return fantasy.ToolResponse{}, err
	}
	return fantasy.NewTextResponse(fmt.Sprintf("The %q role now runs on %s/%s (%s).", role, p.Provider, p.Model, scopeName)), nil
}

func atlasConfigSetModel(store *config.ConfigStore, scope config.Scope, scopeName string, p AtlasConfigParams) (fantasy.ToolResponse, error) {
	modelType := strings.ToLower(strings.TrimSpace(cmpOr(p.ModelType, "large")))
	if modelType != "large" && modelType != "small" {
		return fantasy.NewTextErrorResponse("model_type must be \"large\" or \"small\""), nil
	}
	if p.Provider == "" || p.Model == "" {
		return fantasy.NewTextErrorResponse("provider and model are both required"), nil
	}
	if err := knownProviderModel(store, p.Provider, p.Model); err != nil {
		return fantasy.NewTextErrorResponse(err.Error()), nil
	}

	value := map[string]any{"provider": p.Provider, "model": p.Model}
	if err := store.SetConfigField(scope, "models."+modelType, value); err != nil {
		return fantasy.ToolResponse{}, err
	}
	return fantasy.NewTextResponse(fmt.Sprintf(
		"The %s model is now %s/%s (%s). It applies from the next turn.", modelType, p.Provider, p.Model, scopeName)), nil
}

func atlasConfigToggleTool(store *config.ConfigStore, scope config.Scope, scopeName string, p AtlasConfigParams, disable bool) (fantasy.ToolResponse, error) {
	tool := strings.TrimSpace(p.Tool)
	if tool == "" {
		return fantasy.NewTextErrorResponse("tool is required"), nil
	}

	// Availability is expressed as a list of what is off, so both
	// directions are edits to that one list.
	var disabled []string
	if opts := store.Config().Options; opts != nil {
		disabled = append(disabled, opts.DisabledTools...)
	}
	has := slices.Contains(disabled, tool)

	switch {
	case disable && has:
		return fantasy.NewTextResponse(fmt.Sprintf("The %q tool was already off.", tool)), nil
	case !disable && !has:
		return fantasy.NewTextResponse(fmt.Sprintf("The %q tool was already on.", tool)), nil
	case disable:
		disabled = append(disabled, tool)
	default:
		disabled = slices.DeleteFunc(disabled, func(t string) bool { return t == tool })
	}
	sort.Strings(disabled)

	if err := store.SetConfigField(scope, "options.disabled_tools", disabled); err != nil {
		return fantasy.ToolResponse{}, err
	}
	state := "on"
	if disable {
		state = "off"
	}
	return fantasy.NewTextResponse(fmt.Sprintf("The %q tool is now %s (%s).", tool, state, scopeName)), nil
}

func atlasConfigSetField(store *config.ConfigStore, scope config.Scope, scopeName string, p AtlasConfigParams) (fantasy.ToolResponse, error) {
	key := strings.TrimSpace(p.Key)
	if key == "" {
		return fantasy.NewTextErrorResponse("key is required"), nil
	}
	if err := checkConfigValue(store.Config(), key, p.Value); err != nil {
		return fantasy.NewTextErrorResponse(err.Error()), nil
	}
	if err := store.SetConfigField(scope, key, p.Value); err != nil {
		return fantasy.ToolResponse{}, err
	}
	return fantasy.NewTextResponse(fmt.Sprintf("Set %s to %v (%s).", key, p.Value, scopeName)), nil
}

// checkConfigValue rejects a value the config cannot actually hold,
// before it is written rather than after.
//
// This is not fussiness. A field written with the wrong type -- a string
// "0.3" where a number belongs, which is exactly what a model produces
// when asked for thirty percent -- makes the whole file fail to parse,
// and the store logs that failure as a warning and carries on. The user
// is then left with a configuration that silently will not load and no
// idea which edit did it.
//
// Rather than encoding what every one of the config's several hundred
// fields accepts, apply the change to a copy and see whether the result
// still unmarshals. That catches every type mistake, including in fields
// added after this was written.
func checkConfigValue(cfg *config.Config, key string, value any) error {
	current, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("could not read the current configuration: %w", err)
	}
	updated, err := sjson.Set(string(current), key, value)
	if err != nil {
		return fmt.Errorf("%s cannot be set to %v: %w", key, value, err)
	}
	var probe config.Config
	if err := json.Unmarshal([]byte(updated), &probe); err != nil {
		return fmt.Errorf(
			"%s does not accept %#v -- the configuration would no longer load (%v). Check the type: numbers are numbers, not quoted strings",
			key, value, err)
	}
	return nil
}

// subagentsPaths reads the search directories the same way
// AppWorkspace.subagentsPaths does -- there is no exported accessor on
// the store for it, and this tool only needs it to find and save
// subagent files.
func subagentsPaths(store *config.ConfigStore) []string {
	opts := store.Config().Options
	if opts == nil {
		return nil
	}
	return opts.SubagentsPaths
}

// atlasConfigSaveSubagent creates or updates a subagent definition file,
// the same write the "new subagent" dialog performs. Editing an existing
// one keeps it in whatever directory it already lives in --
// subagents.SaveNamed does that -- so scope only decides where a
// genuinely new subagent is created: global here means user-scoped
// (~/.config/atlas/agents), matching every other atlas_config action's
// use of "global" as "applies everywhere this user runs Atlas".
func atlasConfigSaveSubagent(store *config.ConfigStore, workingDir string, scope config.Scope, scopeName string, p AtlasConfigParams) (fantasy.ToolResponse, error) {
	name := strings.TrimSpace(p.Name)
	if name == "" {
		return fantasy.NewTextErrorResponse("name is required"), nil
	}
	description := strings.TrimSpace(p.Description)
	if description == "" {
		return fantasy.NewTextErrorResponse("description is required -- it is what routes work to this subagent, whether picked by name or by auto-matching"), nil
	}
	instructions := strings.TrimSpace(p.Instructions)
	if instructions == "" {
		instructions = fmt.Sprintf("You are the %s subagent. %s", name, description)
	}

	sub := subagents.Subagent{
		Name:         name,
		Description:  description,
		Model:        strings.TrimSpace(p.Model),
		Instructions: instructions,
	}
	path, err := subagents.SaveNamed(subagentsPaths(store), workingDir, sub, scope == config.ScopeGlobal)
	if err != nil {
		return fantasy.NewTextErrorResponse(err.Error()), nil
	}
	return fantasy.NewTextResponse(fmt.Sprintf("Saved the %q subagent to %s (%s).", name, path, scopeName)), nil
}

func atlasConfigDeleteSubagent(store *config.ConfigStore, p AtlasConfigParams) (fantasy.ToolResponse, error) {
	name := strings.TrimSpace(p.Name)
	if name == "" {
		return fantasy.NewTextErrorResponse("name is required"), nil
	}
	if err := subagents.DeleteNamed(subagentsPaths(store), name); err != nil {
		return fantasy.NewTextErrorResponse(err.Error()), nil
	}
	return fantasy.NewTextResponse(fmt.Sprintf("Deleted the %q subagent.", name)), nil
}

// atlasConfigListSubagents reports what is actually configured, the same
// way atlasConfigList does for providers and roles -- so a request to
// use "the review agent" can be checked against reality instead of
// guessed at.
func atlasConfigListSubagents(store *config.ConfigStore) string {
	discovered := subagents.Discover(subagentsPaths(store))
	if len(discovered) == 0 {
		return "No subagents are configured."
	}
	var b strings.Builder
	b.WriteString("[subagents]\n")
	for _, s := range discovered {
		fmt.Fprintf(&b, "  %s: %s\n", s.Name, s.Description)
	}
	return b.String()
}

func atlasConfigGetField(store *config.ConfigStore, p AtlasConfigParams) (fantasy.ToolResponse, error) {
	key := strings.TrimSpace(p.Key)
	if key == "" {
		return fantasy.NewTextErrorResponse("key is required"), nil
	}
	raw, err := json.Marshal(store.Config())
	if err != nil {
		return fantasy.ToolResponse{}, err
	}
	result := gjson.Get(string(raw), key)
	if !result.Exists() {
		return fantasy.NewTextResponse(fmt.Sprintf("%s is not set.", key)), nil
	}
	return fantasy.NewTextResponse(fmt.Sprintf("%s = %s", key, result.Raw)), nil
}
