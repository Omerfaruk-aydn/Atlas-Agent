package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/workflows"
	"github.com/spf13/cobra"
)

func newCommandsCommand() *cobra.Command {
	command := &cobra.Command{Use: "commands", Short: "Share named project workflows as validated JSON recipes"}
	for _, child := range newRecipeCommands() {
		if child.Name() == "recipes" {
			child.Use = "list"
		}
		command.AddCommand(child)
	}
	command.AddCommand(&cobra.Command{Use: "export <id>", Args: cobra.ExactArgs(1), Short: "Print a recipe for sharing without executing it", RunE: func(cmd *cobra.Command, args []string) error {
		root, err := workflowRoot(cmd)
		if err != nil {
			return err
		}
		paths, _ := recipeCLISources(cmd, root)
		recipes, err := workflows.Load(cmd.Context(), paths)
		if err != nil {
			return err
		}
		for _, recipe := range recipes {
			if recipe.ID == args[0] {
				encoder := json.NewEncoder(cmd.OutOrStdout())
				encoder.SetIndent("", "  ")
				return encoder.Encode(recipe)
			}
		}
		return fmt.Errorf("recipe not found: %s", args[0])
	}})
	importCommand := &cobra.Command{Use: "import <file>", Args: cobra.ExactArgs(1), Short: "Validate and install a new workspace-relative JSON recipe without overwriting", RunE: func(cmd *cobra.Command, args []string) error {
		root, err := workflowRoot(cmd)
		if err != nil {
			return err
		}
		data, err := engineering.ReadProjectEvidence(cmd.Context(), root, args[0])
		if err != nil {
			return err
		}
		recipe, err := workflows.Decode(data)
		if err != nil {
			return err
		}
		paths, roles := recipeCLISources(cmd, root)
		if err := workflows.Validate(cmd.Context(), recipe, roles); err != nil {
			return err
		}
		catalog, err := workflows.Load(cmd.Context(), paths)
		if err != nil {
			return err
		}
		for _, existing := range catalog {
			if existing.ID == recipe.ID {
				return fmt.Errorf("recipe ID already exists: %s", recipe.ID)
			}
		}
		parent := root
		for _, segment := range []string{".atlas", "workflows"} {
			parent = filepath.Join(parent, segment)
			if err := os.Mkdir(parent, 0o700); err != nil && !os.IsExist(err) {
				return err
			}
			info, err := os.Lstat(parent)
			if err != nil {
				return err
			}
			if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("workflow directory must not be a symlink")
			}
		}
		path := filepath.Join(parent, recipe.ID+".json")
		file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			return err
		}
		encoder := json.NewEncoder(file)
		encoder.SetIndent("", "  ")
		writeErr := encoder.Encode(recipe)
		closeErr := file.Close()
		if writeErr != nil {
			_ = os.Remove(path)
			return writeErr
		}
		if closeErr != nil {
			_ = os.Remove(path)
			return closeErr
		}
		_, err = fmt.Fprintln(cmd.OutOrStdout(), path)
		return err
	}}
	command.AddCommand(importCommand)
	return command
}

func init() { rootCmd.AddCommand(newCommandsCommand()) }
