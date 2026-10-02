package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/db"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/session"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/subagents"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/workflows"
	"github.com/spf13/cobra"
)

func recipeCLISources(cmd *cobra.Command, root string) ([]string, []string) {
	paths, _ := cmd.Flags().GetStringArray("workflow-path")
	rolePaths, _ := cmd.Flags().GetStringArray("role-path")
	rolePaths = append([]string{filepath.Join(root, ".atlas", "agents")}, rolePaths...)
	for i, source := range rolePaths {
		if !filepath.IsAbs(source) {
			rolePaths[i] = filepath.Join(root, source)
		}
	}
	var roles []string
	for _, role := range subagents.Discover(rolePaths) {
		roles = append(roles, role.Name)
	}
	return workflows.Paths(root, paths), roles
}

// Recipe CLI commands deliberately avoid executable configuration and models.
func newRecipeCommands() []*cobra.Command {
	var commands []*cobra.Command
	for _, action := range []string{"recipes", "validate", "run"} {
		use, args := "recipes", cobra.NoArgs
		if action != "recipes" {
			use, args = action+" <recipe-id>", cobra.ExactArgs(1)
		}
		command := &cobra.Command{Use: use, Args: args, Short: "Inspect or admit a versioned workflow recipe without an LLM", RunE: func(cmd *cobra.Command, args []string) error {
			root, err := workflowRoot(cmd)
			if err != nil {
				return err
			}
			paths, roles := recipeCLISources(cmd, root)
			catalog, err := workflows.Load(cmd.Context(), paths)
			if err != nil {
				return err
			}
			if action == "recipes" {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(catalog)
			}
			for _, recipe := range catalog {
				if recipe.ID != args[0] {
					continue
				}
				if err := workflows.Validate(cmd.Context(), recipe, roles); err != nil {
					return err
				}
				if action == "validate" {
					return json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]any{"valid": true, "recipe_id": recipe.ID, "executed": false})
				}
				values, _ := cmd.Flags().GetStringArray("param")
				if len(values) > 32 {
					return fmt.Errorf("at most 32 recipe parameters")
				}
				params := map[string]json.RawMessage{}
				for _, value := range values {
					name, raw, ok := strings.Cut(value, "=")
					if !ok || name == "" || len(raw) > 16*1024 || params[name] != nil || !json.Valid([]byte(raw)) {
						return fmt.Errorf("parameters require unique NAME=JSON values")
					}
					params[name] = json.RawMessage(raw)
				}
				compiled, err := workflows.Compile(cmd.Context(), recipe, params, roles)
				if err != nil {
					return err
				}
				compiled.Plan.Root = root
				planOnly, _ := cmd.Flags().GetBool("plan-only")
				if planOnly {
					return json.NewEncoder(cmd.OutOrStdout()).Encode(compiled)
				}
				sessionID, _ := cmd.Flags().GetString("session-id")
				if sessionID == "" {
					return fmt.Errorf("--session-id is required; use --plan-only for a preview")
				}
				store := checkpointStore(cmd, root)
				dataDir := filepath.Dir(store.Dir())
				if _, err := os.Stat(filepath.Join(dataDir, "atlas.db")); err != nil {
					return fmt.Errorf("recipe run requires an existing session database: %w", err)
				}
				if err := store.Check(cmd.Context(), sessionID, ""); err != nil {
					return err
				}
				conn, err := db.Connect(cmd.Context(), dataDir)
				if err != nil {
					return err
				}
				defer db.Release(dataDir)
				run, err := workflows.Install(cmd.Context(), store, session.NewService(db.New(conn), conn), sessionID, root, compiled)
				if err != nil {
					return err
				}
				return json.NewEncoder(cmd.OutOrStdout()).Encode(run)
			}
			return fmt.Errorf("unknown recipe ID %s", args[0])
		}}
		command.Flags().StringArray("workflow-path", nil, "Recipe directory or JSON file; repeat for multiple paths")
		command.Flags().StringArray("role-path", nil, "Named role directory; repeat for multiple paths")
		if action == "run" {
			command.Flags().StringArray("param", nil, "Typed NAME=JSON parameter; repeat for multiple parameters")
			command.Flags().Bool("plan-only", false, "Print compiled tasks and stages without session mutation or execution")
			command.Flags().String("session-id", "", "Existing empty session receiving the recipe")
		}
		commands = append(commands, command)
	}
	return commands
}
