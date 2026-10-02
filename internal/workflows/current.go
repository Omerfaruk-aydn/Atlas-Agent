package workflows

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
)

// Paths resolves explicitly configured paths and the project recipe directory.
func Paths(root string, configured []string) []string {
	paths := []string{filepath.Join(root, ".atlas", "workflows")}
	for _, source := range configured {
		if !filepath.IsAbs(source) {
			source = filepath.Join(root, source)
		}
		source = filepath.Clean(source)
		found := false
		for _, existing := range paths {
			if existing == source {
				found = true
			}
		}
		if !found {
			paths = append(paths, source)
		}
	}
	return paths
}

func Current(ctx context.Context, store *engineering.Store, sessionID string, paths, roles []string) error {
	state, err := store.Read(ctx, sessionID)
	if err != nil || state.Recipe == nil {
		return err
	}
	if state.Recipe.SessionID != sessionID {
		return fmt.Errorf("recipe session binding mismatch")
	}
	recipes, err := Load(ctx, paths)
	if err != nil {
		return err
	}
	for _, recipe := range recipes {
		if recipe.ID != state.Recipe.RecipeID {
			continue
		}
		if err := Validate(ctx, recipe, roles); err != nil {
			return err
		}
		data, err := json.Marshal(recipe)
		if err != nil {
			return err
		}
		return ValidateBinding(ctx, store, sessionID, engineering.Hash(string(data)), state.Recipe.ParametersHash)
	}
	return fmt.Errorf("admitted recipe is no longer available")
}
