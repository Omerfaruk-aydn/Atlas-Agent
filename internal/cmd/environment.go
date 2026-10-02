package cmd

import (
	"encoding/json"
	"runtime"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/environment"
	"github.com/spf13/cobra"
)

func newEnvironmentCommand() *cobra.Command {
	command := &cobra.Command{Use: "environment", Short: "Inspect project-local tool and dependency plans without running commands or an LLM"}
	command.PersistentFlags().String("execution-os", runtime.GOOS, "Execution OS to plan for; use linux for the OCI command backend")
	for _, action := range []string{"inspect", "plan"} {
		command.AddCommand(&cobra.Command{Use: action, Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
			root, err := workflowRoot(cmd)
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			osName, _ := cmd.Flags().GetString("execution-os")
			ctx = environment.WithExecutionOS(ctx, osName)
			plan, err := environment.Inspect(ctx, root)
			if err != nil {
				return err
			}
			return json.NewEncoder(cmd.OutOrStdout()).Encode(plan)
		}})
	}
	return command
}
