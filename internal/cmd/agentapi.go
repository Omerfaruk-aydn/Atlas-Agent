package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/sidecar"
	"github.com/spf13/cobra"
)

// newTicker is the status-report cadence of `sidecar run`.
func newTicker() *time.Ticker { return time.NewTicker(time.Second) }

func newAgentAPICommand() *cobra.Command {
	build := func(cmd *cobra.Command) (sidecar.AgentAPI, error) {
		project := os.Getenv("ATLAS_PROJECT_DIR")
		if project == "" {
			var err error
			if project, err = workflowRoot(cmd); err != nil {
				return sidecar.AgentAPI{}, err
			}
		}
		events := os.Getenv("ATLAS_SIDECAR_EVENTS_DIR")
		if events == "" {
			events = filepath.Join(project, ".atlas", "agentapi-events")
		}
		perm, _ := cmd.Flags().GetString("permissions")
		model, _ := cmd.Flags().GetString("model")
		return sidecar.AgentAPI{Project: project, EventsDir: events, Permissions: perm, Model: model}, nil
	}
	c := &cobra.Command{
		Use:   "agentapi",
		Short: "Let a sidecar start and continue agent conversations",
		Long: "Runs the agent headlessly in the project and prints one JSON object. Conversations run read-only " +
			"unless --permissions edits or configured is given; the unrestricted auto policy is refused. " +
			"Each call is recorded under the sidecar's events folder.",
	}
	c.PersistentFlags().String("permissions", "", "readonly (default), edits or configured")
	c.PersistentFlags().String("model", "", "Model to use")
	c.AddCommand(
		&cobra.Command{Use: "new-conversation <prompt>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			api, err := build(cmd)
			if err != nil {
				return err
			}
			conv, err := api.NewConversation(cmd.Context(), args[0])
			if encErr := json.NewEncoder(cmd.OutOrStdout()).Encode(conv); encErr != nil {
				return encErr
			}
			return err
		}},
		&cobra.Command{Use: "send-message <conversation-id> <text>", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
			api, err := build(cmd)
			if err != nil {
				return err
			}
			conv, err := api.SendMessage(cmd.Context(), args[0], args[1])
			if encErr := json.NewEncoder(cmd.OutOrStdout()).Encode(conv); encErr != nil {
				return encErr
			}
			return err
		}},
		&cobra.Command{Use: "get-conversation-metadata <conversation-id>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			api, err := build(cmd)
			if err != nil {
				return err
			}
			meta, err := api.GetMetadata(args[0])
			if err != nil {
				return err
			}
			return json.NewEncoder(cmd.OutOrStdout()).Encode(meta)
		}},
	)
	return c
}

func init() { rootCmd.AddCommand(newAgentAPICommand()) }
