package workflows

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func recipeFixture() Recipe {
	return Recipe{ID: "feature", Version: 1, Parameters: []Parameter{{Name: "target", Type: "string", Required: true}}, Steps: []Step{{ID: "implement", Role: "backend", Prompt: "Implement ${target}", OwnedPaths: []string{"src"}, Criteria: []string{"Target works"}, Checks: []Check{{Name: "check", Directory: ".", Argv: []string{"check", "${target}"}}}}, {ID: "review", Role: "review", Prompt: "Inspect implementation", DependsOn: []string{"implement"}, OwnedPaths: []string{"src"}, Criteria: []string{"Independently reviewed"}}}, Requirements: []Requirement{{ID: "user", Description: "Working feature", StepIDs: []string{"implement", "review"}}}}
}

func TestRecipesStrictSchemaAndCycles(t *testing.T) {
	t.Parallel()
	roles := []string{"backend", "review"}
	r := recipeFixture()
	require.NoError(t, Validate(t.Context(), r, roles))
	r.Steps[0].DependsOn = []string{"review"}
	require.Error(t, Validate(t.Context(), r, roles))
	r = recipeFixture()
	r.Steps[1].Role = "disabled"
	require.Error(t, Validate(t.Context(), r, roles))
	r = recipeFixture()
	r.Version = 2
	require.Error(t, Validate(t.Context(), r, roles))
	r = recipeFixture()
	r.Steps[1].ID = r.Steps[0].ID
	require.Error(t, Validate(t.Context(), r, roles))
	r = recipeFixture()
	r.Requirements[0].StepIDs = []string{"missing"}
	require.Error(t, Validate(t.Context(), r, roles))
	r = recipeFixture()
	for range 63 {
		r.Steps = append(r.Steps, r.Steps[0])
	}
	require.Error(t, Validate(t.Context(), r, roles))
	root := t.TempDir()
	path := filepath.Join(root, "bad.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"id":"bad","version":1,"unknown":true}`), 0o600))
	_, err := Load(t.Context(), []string{path})
	require.Error(t, err)
	require.NoError(t, os.WriteFile(path, []byte(`{"id":"bad","id":"other","version":1}`), 0o600))
	_, err = Load(t.Context(), []string{path})
	require.Error(t, err, "duplicate JSON fields are ambiguous")
}

func TestRecipesLiteralArguments(t *testing.T) {
	t.Parallel()
	r := recipeFixture()
	literal := "$(touch escaped) ; echo injected"
	value, err := json.Marshal(literal)
	require.NoError(t, err)
	compiled, err := Compile(t.Context(), r, map[string]json.RawMessage{"target": value}, []string{"backend", "review"})
	require.NoError(t, err)
	require.Len(t, compiled.Tasks, 2)
	var found bool
	for _, checks := range compiled.Checks {
		for _, check := range checks {
			require.Equal(t, literal, check.Argv[1])
			found = true
		}
	}
	require.True(t, found)
	require.Contains(t, compiled.Tasks[0].Content, literal)
	_, err = Compile(t.Context(), r, map[string]json.RawMessage{"target": value, "unknown": value}, []string{"backend", "review"})
	require.Error(t, err)
	r.Steps[0].Checks[0].Argv = []string{"sh", "-c", "${target}"}
	_, err = Compile(t.Context(), r, map[string]json.RawMessage{"target": value}, []string{"backend", "review"})
	require.Error(t, err, "typed parameters cannot become interpreted shell source")
}

func TestRecipesTypedValuesAndInterpreterAliases(t *testing.T) {
	t.Parallel()
	for _, p := range []Parameter{{Name: "value", Type: "string"}, {Name: "value", Type: "integer"}, {Name: "value", Type: "boolean"}} {
		_, _, err := parameterValue(p, json.RawMessage(`null`))
		require.Error(t, err, p.Type)
	}
	_, _, err := parameterValue(Parameter{Name: "value", Type: "integer"}, json.RawMessage(`"123"`))
	require.Error(t, err, "quoted numbers are strings")
	for _, argv := range [][]string{{"bash", "-ec", "${target}"}, {"pwsh", "-Co", "${target}"}, {"python3.12", "-c", "${target}"}, {"nodejs", "--eval=${target}"}, {"busybox", "sh", "-c", "${target}"}, {"env", "sh", "-c", "${target}"}} {
		r := recipeFixture()
		r.Steps[0].Checks[0].Argv = argv
		_, err := Compile(t.Context(), r, map[string]json.RawMessage{"target": json.RawMessage(`"echo unsafe"`)}, []string{"backend", "review"})
		require.Error(t, err, argv)
	}
}

func TestRecipesSixtyFourUniqueSteps(t *testing.T) {
	t.Parallel()
	r := Recipe{ID: "bounded", Version: 1}
	for i := range 64 {
		step := Step{ID: fmt.Sprintf("step-%d", i), Role: "backend", Prompt: "Inspect bounded work", OwnedPaths: []string{"."}, Criteria: []string{"Observed result"}}
		if i > 0 {
			step.DependsOn = []string{r.Steps[i-1].ID}
		}
		r.Steps = append(r.Steps, step)
		if i%16 == 0 {
			r.Requirements = append(r.Requirements, Requirement{ID: fmt.Sprintf("req-%d", i/16), Description: "Bounded phase"})
		}
		index := len(r.Requirements) - 1
		r.Requirements[index].StepIDs = append(r.Requirements[index].StepIDs, step.ID)
	}
	compiled, err := Compile(t.Context(), r, nil, []string{"backend"})
	require.NoError(t, err)
	require.Len(t, compiled.Tasks, 64)
	require.Len(t, compiled.Plan.Stages, 32)
	r.Steps = append(r.Steps, Step{ID: "sixty-fifth", Role: "backend", Prompt: "Too much", OwnedPaths: []string{"."}, Criteria: []string{"Observed"}})
	require.Error(t, Validate(t.Context(), r, []string{"backend"}))
}
