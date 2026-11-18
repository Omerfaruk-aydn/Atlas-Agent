package cmd

import (
	"encoding/json"
	"path/filepath"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/routine"
	"github.com/spf13/cobra"
)

func newRoutineSyncCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "sync",
		Short: "Create, update and remove routines from .atlas/automations/*.md",
		Long: "Reads one routine per markdown file in .atlas/automations (front matter: schedule, " +
			"permissions, model, goal, timeout, max_late; body: the prompt). The schedule may start " +
			"with CRON_TZ=<zone>. Routines from files are named auto-<file> and start disabled: read " +
			"them, then `routine enable auto-<file>`. A file cannot ask for the auto permission policy.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			project, err := workflowRoot(cmd)
			if err != nil {
				return err
			}
			report, err := routine.Sync(cmd.Context(), routineStore(), filepath.Join(project, ".atlas", "automations"), project)
			if err != nil {
				return err
			}
			return json.NewEncoder(cmd.OutOrStdout()).Encode(report)
		},
	}
}
