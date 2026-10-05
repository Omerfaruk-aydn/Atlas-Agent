package model

import (
	"context"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/csync"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-models/pkg/catwalk"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/subagents"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/common"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/dialog"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/util"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/workspace"
	"github.com/stretchr/testify/require"
)

// modelManagementWorkspace is a minimal workspace.Workspace stub
// recording SetConfigField/SaveSubagent calls and serving a fixed
// config/subagent list for reads.
type modelManagementWorkspace struct {
	workspace.Workspace
	cfg *config.Config

	setKey   string
	setValue any
	setErr   error

	subagentsList []subagents.Subagent
	savedSubagent subagents.Subagent
	savedScope    bool
	saveErr       error

	updatedModels     map[config.SelectedModelType]config.SelectedModel
	updateErr         error
	updatedAgentModel bool
}

func (w *modelManagementWorkspace) Config() *config.Config { return w.cfg }

func (w *modelManagementWorkspace) SetConfigField(_ config.Scope, key string, value any) error {
	w.setKey, w.setValue = key, value
	return w.setErr
}

func (w *modelManagementWorkspace) ListSubagents(context.Context) ([]subagents.Subagent, error) {
	return w.subagentsList, nil
}

func (w *modelManagementWorkspace) UpdatePreferredModel(_ config.Scope, modelType config.SelectedModelType, model config.SelectedModel) error {
	if w.updatedModels == nil {
		w.updatedModels = map[config.SelectedModelType]config.SelectedModel{}
	}
	w.updatedModels[modelType] = model
	return w.updateErr
}

func (w *modelManagementWorkspace) UpdateAgentModel(context.Context) error {
	w.updatedAgentModel = true
	return nil
}

func (w *modelManagementWorkspace) SaveSubagent(_ context.Context, sub subagents.Subagent, userScope bool) (string, error) {
	w.savedSubagent, w.savedScope = sub, userScope
	return "/agents/" + sub.Name + ".md", w.saveErr
}

func newModelManagementTestUI(ws *modelManagementWorkspace) *UI {
	return &UI{
		com:    &common.Common{Workspace: ws},
		dialog: dialog.NewOverlay(),
	}
}

func TestHandleSaveModelRoleRequiresAName(t *testing.T) {
	ws := &modelManagementWorkspace{cfg: &config.Config{}}
	m := newModelManagementTestUI(ws)

	cmd := m.handleSaveModelRole(dialog.ActionSaveModelRole{Args: map[string]string{"provider": "openai", "model": "gpt-4o"}})
	require.NotNil(t, cmd)
	cmd() // must not touch the workspace
	require.Empty(t, ws.setKey)
}

func TestHandleSaveModelRoleWritesTheExpectedField(t *testing.T) {
	ws := &modelManagementWorkspace{cfg: &config.Config{}}
	m := newModelManagementTestUI(ws)

	cmd := m.handleSaveModelRole(dialog.ActionSaveModelRole{
		Args: map[string]string{"name": "research", "provider": "openai", "model": "o3"},
	})
	require.NotNil(t, cmd)
	msg := cmd().(modelRoleSavedMsg)
	require.NoError(t, msg.err)
	require.Equal(t, "options.model_roles.research", ws.setKey)
	require.Equal(t, config.SelectedModel{Provider: "openai", Model: "o3"}, ws.setValue)
}

func TestHandleSaveModelRoleIncludesReasoningEffortWhenGiven(t *testing.T) {
	ws := &modelManagementWorkspace{cfg: &config.Config{}}
	m := newModelManagementTestUI(ws)

	cmd := m.handleSaveModelRole(dialog.ActionSaveModelRole{
		Args: map[string]string{"name": "research", "provider": "openai", "model": "o3", "reasoning_effort": "high"},
	})
	msg := cmd().(modelRoleSavedMsg)
	require.NoError(t, msg.err)
	require.Equal(t, config.SelectedModel{Provider: "openai", Model: "o3", ReasoningEffort: "high"}, ws.setValue)
}

func TestHandleSaveModelRoleEditingKeepsTheExistingName(t *testing.T) {
	ws := &modelManagementWorkspace{cfg: &config.Config{}}
	m := newModelManagementTestUI(ws)

	// A rogue "name" in Args (the field is omitted from the edit form,
	// but nothing stops a stale value lingering) must not override
	// ExistingName.
	cmd := m.handleSaveModelRole(dialog.ActionSaveModelRole{
		ExistingName: "research",
		Args:         map[string]string{"name": "other", "provider": "openai", "model": "o3"},
	})
	msg := cmd().(modelRoleSavedMsg)
	require.NoError(t, msg.err)
	require.Equal(t, "options.model_roles.research", ws.setKey)
}

func TestHandleSaveFallbackEntryAppendsToTheExistingChain(t *testing.T) {
	ws := &modelManagementWorkspace{cfg: &config.Config{
		Options: &config.Options{
			ModelFallbacks: map[config.SelectedModelType][]config.SelectedModel{
				config.SelectedModelTypeLarge: {{Provider: "openai", Model: "gpt-4o-mini"}},
			},
		},
	}}
	m := newModelManagementTestUI(ws)

	cmd := m.handleSaveFallbackEntry(dialog.ActionSaveFallbackEntry{
		ModelType: config.SelectedModelTypeLarge,
		Args:      map[string]string{"provider": "anthropic", "model": "claude-haiku-4-5"},
	})
	require.NotNil(t, cmd)
	msg := cmd().(fallbackEntrySavedMsg)
	require.NoError(t, msg.err)
	require.Equal(t, "options.model_fallbacks.large", ws.setKey)
	require.Equal(t, []config.SelectedModel{
		{Provider: "openai", Model: "gpt-4o-mini"},
		{Provider: "anthropic", Model: "claude-haiku-4-5"},
	}, ws.setValue)
}

func TestHandleSaveFallbackEntryOnAnEmptyChainStartsOne(t *testing.T) {
	ws := &modelManagementWorkspace{cfg: &config.Config{}}
	m := newModelManagementTestUI(ws)

	cmd := m.handleSaveFallbackEntry(dialog.ActionSaveFallbackEntry{
		ModelType: config.SelectedModelTypeSmall,
		Args:      map[string]string{"provider": "openai", "model": "gpt-4o-mini"},
	})
	msg := cmd().(fallbackEntrySavedMsg)
	require.NoError(t, msg.err)
	require.Equal(t, []config.SelectedModel{{Provider: "openai", Model: "gpt-4o-mini"}}, ws.setValue)
}

func TestHandleSaveFallbackCooldownRejectsNonNumeric(t *testing.T) {
	ws := &modelManagementWorkspace{cfg: &config.Config{}}
	m := newModelManagementTestUI(ws)

	cmd := m.handleSaveFallbackCooldown(dialog.ActionSaveFallbackCooldown{Args: map[string]string{"seconds": "soon"}})
	require.NotNil(t, cmd)
	cmd()
	require.Empty(t, ws.setKey, "an invalid cooldown must not reach the workspace")
}

func TestHandleSaveFallbackCooldownRejectsNegative(t *testing.T) {
	ws := &modelManagementWorkspace{cfg: &config.Config{}}
	m := newModelManagementTestUI(ws)

	cmd := m.handleSaveFallbackCooldown(dialog.ActionSaveFallbackCooldown{Args: map[string]string{"seconds": "-5"}})
	cmd()
	require.Empty(t, ws.setKey)
}

func TestHandleSaveFallbackCooldownWritesTheField(t *testing.T) {
	ws := &modelManagementWorkspace{cfg: &config.Config{}}
	m := newModelManagementTestUI(ws)

	cmd := m.handleSaveFallbackCooldown(dialog.ActionSaveFallbackCooldown{Args: map[string]string{"seconds": "300"}})
	msg := cmd().(fallbackEntrySavedMsg)
	require.NoError(t, msg.err)
	require.Equal(t, "options.fallback_cooldown", ws.setKey)
	require.Equal(t, 300, ws.setValue)
}

func TestHandleSaveAutoCompactThresholdRejectsNonNumeric(t *testing.T) {
	ws := &modelManagementWorkspace{cfg: &config.Config{}}
	m := newModelManagementTestUI(ws)

	cmd := m.handleSaveAutoCompactThreshold(dialog.ActionSaveAutoCompactThreshold{Args: map[string]string{"percent": "lots"}})
	require.NotNil(t, cmd)
	cmd()
	require.Empty(t, ws.setKey, "an invalid threshold must not reach the workspace")
}

func TestHandleSaveAutoCompactThresholdRejectsOutOfRange(t *testing.T) {
	ws := &modelManagementWorkspace{cfg: &config.Config{}}
	m := newModelManagementTestUI(ws)

	for _, percent := range []string{"0", "100", "-10", "150"} {
		cmd := m.handleSaveAutoCompactThreshold(dialog.ActionSaveAutoCompactThreshold{Args: map[string]string{"percent": percent}})
		cmd()
		require.Empty(t, ws.setKey, "percent %q must not reach the workspace", percent)
	}
}

func TestHandleSaveAutoCompactThresholdWritesTheField(t *testing.T) {
	ws := &modelManagementWorkspace{cfg: &config.Config{}}
	m := newModelManagementTestUI(ws)

	cmd := m.handleSaveAutoCompactThreshold(dialog.ActionSaveAutoCompactThreshold{Args: map[string]string{"percent": "80"}})
	cmd()
	require.Equal(t, "options.auto_summarize_at", ws.setKey)
	require.Equal(t, 0.8, ws.setValue)
}

func TestHandleSaveAutoCompactThresholdEmptyResetsToBuiltIn(t *testing.T) {
	ws := &modelManagementWorkspace{cfg: &config.Config{}}
	m := newModelManagementTestUI(ws)

	cmd := m.handleSaveAutoCompactThreshold(dialog.ActionSaveAutoCompactThreshold{Args: map[string]string{"percent": ""}})
	cmd()
	require.Equal(t, "options.auto_summarize_at", ws.setKey)
	require.Equal(t, 0.0, ws.setValue)
}

func TestHandleSaveSubagentMetaRequiresNameAndDescription(t *testing.T) {
	ws := &modelManagementWorkspace{}
	m := newModelManagementTestUI(ws)

	cmd := m.handleSaveSubagentMeta(dialog.ActionSaveSubagentMeta{Args: map[string]string{"description": "d"}})
	require.NotNil(t, cmd)
	cmd()
	require.Empty(t, ws.savedSubagent.Name, "missing name must not reach the workspace")

	cmd = m.handleSaveSubagentMeta(dialog.ActionSaveSubagentMeta{Args: map[string]string{"name": "research"}})
	cmd()
	require.Empty(t, ws.savedSubagent.Name, "missing description must not reach the workspace")
}

func TestHandleSaveSubagentMetaNewGetsThePlaceholderTemplate(t *testing.T) {
	ws := &modelManagementWorkspace{}
	m := newModelManagementTestUI(ws)

	cmd := m.handleSaveSubagentMeta(dialog.ActionSaveSubagentMeta{
		UserScope: true,
		Args:      map[string]string{"name": "research", "description": "Deep research.", "model": "@research"},
	})
	msg := cmd().(subagentSavedMsg)
	require.NoError(t, msg.err)
	require.Equal(t, "research", ws.savedSubagent.Name)
	require.Equal(t, "Deep research.", ws.savedSubagent.Description)
	require.Equal(t, "@research", ws.savedSubagent.Model)
	require.Contains(t, ws.savedSubagent.Instructions, "TODO")
	require.True(t, ws.savedScope)
}

// TestHandleSaveSubagentMetaEditingPreservesInstructions is the
// riskiest bit of this handler: editing a subagent's metadata must not
// silently blank out (or reset to the placeholder) instructions the
// user already wrote for it.
func TestHandleSaveSubagentMetaEditingPreservesInstructions(t *testing.T) {
	ws := &modelManagementWorkspace{subagentsList: []subagents.Subagent{
		{Name: "research", Description: "old description", Instructions: "Dig deep before answering.", PreferredSkills: []string{"research-evidence"}, InheritModel: true},
	}}
	m := newModelManagementTestUI(ws)

	cmd := m.handleSaveSubagentMeta(dialog.ActionSaveSubagentMeta{
		ExistingName: "research",
		Args:         map[string]string{"description": "new description"},
	})
	msg := cmd().(subagentSavedMsg)
	require.NoError(t, msg.err)
	require.Equal(t, "research", ws.savedSubagent.Name)
	require.Equal(t, "new description", ws.savedSubagent.Description)
	require.Equal(t, "Dig deep before answering.", ws.savedSubagent.Instructions,
		"editing metadata must not touch the instructions body")
	require.Equal(t, []string{"research-evidence"}, ws.savedSubagent.PreferredSkills)
	require.True(t, ws.savedSubagent.InheritModel)
}

func TestHandleSaveSubagentMetaSaveErrorIsReported(t *testing.T) {
	ws := &modelManagementWorkspace{saveErr: context.DeadlineExceeded}
	m := newModelManagementTestUI(ws)

	cmd := m.handleSaveSubagentMeta(dialog.ActionSaveSubagentMeta{
		Args: map[string]string{"name": "research", "description": "d"},
	})
	msg := cmd().(subagentSavedMsg)
	require.Error(t, msg.err)
}

func testModeSwitchConfig() *config.Config {
	providers := csync.NewMap[string, config.ProviderConfig]()
	providers.Set("openai", config.ProviderConfig{
		ID:     "openai",
		Models: []catwalk.Model{{ID: "o3-mini", ReasoningLevels: []string{"low", "medium", "high"}}},
	})
	providers.Set("anthropic", config.ProviderConfig{
		ID:     "anthropic",
		Models: []catwalk.Model{{ID: "claude-sonnet-5", ReasoningLevels: []string{"low", "high"}}},
	})
	return &config.Config{
		Models: map[config.SelectedModelType]config.SelectedModel{
			config.SelectedModelTypeLarge: {Provider: "anthropic", Model: "claude-sonnet-5"},
			config.SelectedModelTypeSmall: {Provider: "openai", Model: "o3-mini"},
		},
		Providers: providers,
	}
}

func TestHandleSetModeRejectsUnknownMode(t *testing.T) {
	ws := &modelManagementWorkspace{cfg: testModeSwitchConfig()}
	m := newModelManagementTestUI(ws)

	cmd := m.handleSetMode("turbo")
	require.NotNil(t, cmd)
	cmd()
	require.Empty(t, ws.setKey, "an unknown mode must not touch the workspace")
}

func TestHandleSetModeFastSwitchesToSmallAtLowestReasoning(t *testing.T) {
	ws := &modelManagementWorkspace{cfg: testModeSwitchConfig()}
	m := newModelManagementTestUI(ws)

	msg := m.applyMode("fast")
	info, ok := msg.(util.InfoMsg)
	require.True(t, ok, "expected an info message, got %T", msg)
	require.Contains(t, info.Msg, "Fast")
	require.Contains(t, info.Msg, "low")

	require.Equal(t, "options.agent_models.coder", ws.setKey)
	require.Equal(t, config.SelectedModelTypeSmall, ws.setValue)

	updated, ok := ws.updatedModels[config.SelectedModelTypeSmall]
	require.True(t, ok, "the small model's reasoning effort must be updated")
	require.Equal(t, "low", updated.ReasoningEffort)
	require.True(t, ws.updatedAgentModel, "the live agent must be rebuilt after a mode switch")
}

func TestHandleSetModeQualitySwitchesToLargeAtHighestReasoning(t *testing.T) {
	ws := &modelManagementWorkspace{cfg: testModeSwitchConfig()}
	m := newModelManagementTestUI(ws)

	msg := m.applyMode("quality")
	info := msg.(util.InfoMsg)
	require.Contains(t, info.Msg, "Quality")
	require.Contains(t, info.Msg, "high")

	require.Equal(t, "options.agent_models.coder", ws.setKey)
	require.Equal(t, config.SelectedModelTypeLarge, ws.setValue)

	updated, ok := ws.updatedModels[config.SelectedModelTypeLarge]
	require.True(t, ok)
	require.Equal(t, "high", updated.ReasoningEffort)
}

func TestHandleSetModeAgentModelWriteErrorIsReported(t *testing.T) {
	ws := &modelManagementWorkspace{cfg: testModeSwitchConfig(), setErr: context.DeadlineExceeded}
	m := newModelManagementTestUI(ws)

	msg := m.applyMode("fast")
	info, ok := msg.(util.InfoMsg)
	require.True(t, ok, "expected an info message, got %T", msg)
	require.Equal(t, util.InfoTypeError, info.Type, "a SetConfigField failure must be reported as an error, not success")
}
