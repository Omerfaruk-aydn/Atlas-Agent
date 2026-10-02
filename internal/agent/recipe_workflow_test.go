package agent

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agent/tools"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/permission"
	"github.com/stretchr/testify/require"
)

func TestRecipesPermissionAndPlanOnly(t *testing.T) {
	env := testEnv(t)
	root := t.TempDir()
	sess, err := env.sessions.Create(t.Context(), "recipe")
	require.NoError(t, err)
	c := &coordinator{cfg: config.NewTestStore(&config.Config{Options: &config.Options{}}).Scoped(root), sessions: env.sessions, engineering: engineering.NewStore(t.TempDir()), permissions: env.permissions}
	ctx := context.WithValue(t.Context(), tools.SessionIDContextKey, sess.ID)
	p := WorkflowParams{RecipeID: "feature-delivery", RecipeAction: "plan", RecipeParams: map[string]json.RawMessage{"feature": json.RawMessage(`"widget"`)}}
	invoke := func(context.Context, fantasy.ToolCall) (fantasy.ToolResponse, error) {
		t.Fatal("planning or admission must not dispatch or execute checks")
		return fantasy.ToolResponse{}, nil
	}
	resp, err := c.recipeWorkflow(ctx, p, fantasy.ToolCall{ID: "preview"}, invoke)
	require.NoError(t, err)
	require.False(t, resp.IsError, resp.Content)
	latest, err := env.sessions.Get(ctx, sess.ID)
	require.NoError(t, err)
	require.Empty(t, latest.Todos)
	env.permissions.SetMode(permission.ModePlan)
	p.RecipeAction = "run"
	resp, err = c.recipeWorkflow(ctx, p, fantasy.ToolCall{ID: "denied"}, invoke)
	require.NoError(t, err)
	require.True(t, resp.IsError)
	state, err := c.engineering.Read(ctx, sess.ID)
	require.NoError(t, err)
	require.Nil(t, state.Recipe)
	env.permissions.SetMode(permission.ModeBypass)
	resp, err = c.recipeWorkflow(ctx, p, fantasy.ToolCall{ID: "admit"}, invoke)
	require.NoError(t, err)
	require.False(t, resp.IsError, resp.Content)
	require.NoError(t, c.currentRecipe(ctx, sess.ID))
	p.RecipeParams["feature"] = json.RawMessage(`"changed"`)
	resp, err = c.recipeWorkflow(ctx, p, fantasy.ToolCall{ID: "overwrite"}, invoke)
	require.NoError(t, err)
	require.True(t, resp.IsError)
}
