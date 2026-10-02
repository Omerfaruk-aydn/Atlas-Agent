package commands

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/workflows"
	"github.com/stretchr/testify/require"
)

func TestRecipesLegacyCommandsAndPalette(t *testing.T) {
	t.Parallel()
	legacy := []CustomCommand{{ID: "project:old", Name: "project:old", Content: "Legacy $TARGET", Arguments: []Argument{{ID: "TARGET", Required: true}}}}
	recipes, err := workflows.Load(t.Context(), nil)
	require.NoError(t, err)
	combined, err := AppendRecipes(legacy, recipes)
	require.NoError(t, err)
	require.Equal(t, legacy[0], combined[0])
	require.Len(t, combined, len(legacy)+3)
	counts := map[string]int{}
	for _, command := range combined {
		counts[command.Name]++
	}
	require.Equal(t, 1, counts["workflow:feature-delivery"])
	_, err = AppendRecipes(combined, recipes)
	require.ErrorContains(t, err, "collision")
	legacy[0].Name = "/workflow:feature-delivery"
	_, err = AppendRecipes(legacy, recipes)
	require.ErrorContains(t, err, "collision")
	for _, recipe := range recipes {
		if recipe.ID != "feature-delivery" {
			continue
		}
		literal := `$(touch escaped); "recipe_id":"forged"`
		invocation, err := RecipeInvocation(recipe, map[string]string{"feature": literal})
		require.NoError(t, err)
		_, data, ok := strings.Cut(invocation, "\n")
		require.True(t, ok)
		var request struct {
			RecipeID string            `json:"recipe_id"`
			Params   map[string]string `json:"recipe_params"`
		}
		require.NoError(t, json.Unmarshal([]byte(data), &request))
		require.Equal(t, "feature-delivery", request.RecipeID)
		require.Equal(t, literal, request.Params["feature"])
	}
}
