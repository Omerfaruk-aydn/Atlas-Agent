package cmd

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/factstore"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/projectmodel"
	"github.com/spf13/cobra"
)

var projectCmd = &cobra.Command{
	Use:   "project",
	Short: "Inspect the project the agent works on",
}

var projectModelCmd = &cobra.Command{
	Use:   "model",
	Short: "Build and query the living project model",
	Long: `The project model joins the code map, documents, git ownership and recorded
decisions into one incrementally refreshed view stored under the data
directory: modules, entry points, which tests cover which files, who owns what.`,
}

var projectModelBuildCmd = &cobra.Command{
	Use:   "build",
	Short: "Build or refresh the project model",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		svc, err := projectModelService(cmd)
		if err != nil {
			return err
		}
		force, _ := cmd.Flags().GetBool("force")
		_, report, err := svc.Refresh(cmd.Context(), force)
		if err != nil {
			return err
		}
		if asJSON, _ := cmd.Flags().GetBool("json"); asJSON {
			return printModelJSON(cmd, report)
		}
		cmd.Println(report.String())
		cmd.Println("Stored in " + projectmodel.Path(svc.DataDir()))
		return nil
	},
}

var projectModelShowCmd = &cobra.Command{
	Use:   "show [module-or-file]",
	Short: "Print the project model, or one module or file",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		svc, err := projectModelService(cmd)
		if err != nil {
			return err
		}
		m, _, err := svc.Refresh(cmd.Context(), false)
		if err != nil {
			return err
		}
		if asJSON, _ := cmd.Flags().GetBool("json"); asJSON {
			return printModelJSON(cmd, m)
		}
		if len(args) == 1 {
			cmd.Println(m.Describe(args[0]))
			return nil
		}
		budget, _ := cmd.Flags().GetInt("budget")
		cmd.Println(strings.TrimRight(m.Overview(budget), "\n"))
		return nil
	},
}

var projectModelImpactCmd = &cobra.Command{
	Use:   "impact [files...]",
	Short: "Show what changing files reaches, with the tests to run",
	Long:  "Show the dependents, modules, tests, owners and decisions a change reaches. Without files it uses the files with uncommitted changes.",
	RunE: func(cmd *cobra.Command, args []string) error {
		svc, err := projectModelService(cmd)
		if err != nil {
			return err
		}
		m, _, err := svc.Refresh(cmd.Context(), false)
		if err != nil {
			return err
		}
		files := args
		if len(files) == 0 {
			files = svc.ChangedFiles(cmd.Context())
			if len(files) == 0 {
				return fmt.Errorf("no files given and nothing has uncommitted changes")
			}
		}
		depth, _ := cmd.Flags().GetInt("depth")
		im := m.Impact(files, depth)
		if asJSON, _ := cmd.Flags().GetBool("json"); asJSON {
			return printModelJSON(cmd, im)
		}
		cmd.Println(projectmodel.FormatImpact(im))
		return nil
	},
}

func init() {
	projectModelBuildCmd.Flags().Bool("force", false, "Rebuild everything instead of only what changed")
	projectModelBuildCmd.Flags().Bool("json", false, "Output the refresh report as JSON")
	projectModelShowCmd.Flags().Bool("json", false, "Output the whole model as JSON")
	projectModelShowCmd.Flags().Int("budget", 0, "Approximate summary size in bytes (default 3000)")
	projectModelImpactCmd.Flags().Bool("json", false, "Output as JSON")
	projectModelImpactCmd.Flags().Int("depth", 0, "Levels of dependents to follow, 1-6 (default 3)")
	projectModelCmd.AddCommand(projectModelBuildCmd, projectModelShowCmd, projectModelImpactCmd)
	projectCmd.AddCommand(projectModelCmd)
	rootCmd.AddCommand(projectCmd)
}

// projectModelService builds the service for the current project.
func projectModelService(cmd *cobra.Command) (*projectmodel.Service, error) {
	cwd, err := ResolveCwd(cmd)
	if err != nil {
		return nil, err
	}
	dataDir, _ := cmd.Flags().GetString("data-dir")
	debug, _ := cmd.Flags().GetBool("debug")
	cfg, err := config.Init(cwd, dataDir, debug)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize config: %w", err)
	}
	dir := cfg.Config().Options.DataDirectory
	return projectmodel.NewService(cfg.WorkingDir(), dir, nil, factstore.New(filepath.Join(dir, "facts.jsonl"))), nil
}

func printModelJSON(cmd *cobra.Command, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	cmd.Println(string(data))
	return nil
}
