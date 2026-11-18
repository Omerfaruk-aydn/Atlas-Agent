package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/importer"
	"github.com/spf13/cobra"
)

func newImportCommand() *cobra.Command {
	command := &cobra.Command{
		Use:   "import <" + strings.Join(importer.Sources, "|") + ">",
		Short: "Import another agent's commands, skills and instructions as a disabled plugin",
		Long: "Reads the customizations another coding agent keeps in your home directory and this project " +
			"and installs them as one plugin named imported-<source>. The plugin starts disabled; enable it with " +
			"`plugin enable` and remove it with `plugin remove`. Files that look like they hold credentials are " +
			"skipped, and MCP servers are listed by name only.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			home, err := os.UserHomeDir()
			if err != nil {
				return err
			}
			project, err := workflowRoot(cmd)
			if err != nil {
				return err
			}
			root, err := pluginRoot(cmd)
			if err != nil {
				return err
			}
			dryRun, _ := cmd.Flags().GetBool("dry-run")
			result, err := importer.Import(args[0], home, project, root, dryRun)
			if err != nil {
				return err
			}
			if dryRun {
				fmt.Fprintln(cmd.ErrOrStderr(), "Dry run: nothing was written.")
			}
			return json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]any{
				"plugin":   result.Plugin,
				"imported": result.Imported,
				"skipped":  result.Skipped,
				"notes":    result.Notes,
				"enabled":  false,
			})
		},
	}
	command.Flags().Bool("dry-run", false, "List what would be imported without writing anything")
	return command
}

func init() { rootCmd.AddCommand(newImportCommand()) }
