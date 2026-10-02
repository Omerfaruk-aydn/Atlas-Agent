package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agent"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/db"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/session"
	"github.com/spf13/cobra"
)

func newWorkflowControls() []*cobra.Command {
	var out []*cobra.Command
	for _, action := range []string{"snapshot", "control"} {
		args, use := cobra.ExactArgs(1), "snapshot <session-id>"
		if action == "control" {
			args, use = cobra.ExactArgs(2), "control <session-id> <pause|resume|stop|reassign>"
		}
		cmd := &cobra.Command{Use: use, Args: args, Short: "Inspect or control the shared revision-checked agent workflow", RunE: func(cmd *cobra.Command, args []string) error {
			var snapshot engineering.WorkflowSnapshot
			control := engineering.WorkflowControl{}
			if action == "control" {
				control.Action = args[1]
				control.TaskID, _ = cmd.Flags().GetString("task-id")
				control.Agent, _ = cmd.Flags().GetString("agent")
				control.ExpectedRevision, _ = cmd.Flags().GetString("revision")
			}
			if useClientServer() {
				client, ws, cleanup, err := connectToServer(cmd)
				if err != nil {
					return err
				}
				defer cleanup()
				ctx, cancel := context.WithCancel(cmd.Context())
				defer cancel()
				events, err := client.SubscribeEvents(ctx, ws.ID)
				if err != nil {
					return err
				}
				go func() {
					for range events {
					}
				}()
				if action == "control" {
					if err := client.WorkflowControl(ctx, ws.ID, args[0], control); err != nil {
						return err
					}
				}
				snapshot, err = client.WorkflowSnapshot(ctx, ws.ID, args[0])
				if err != nil {
					return err
				}
			} else {
				root, err := workflowRoot(cmd)
				if err != nil {
					return err
				}
				store := checkpointStore(cmd, root)
				if action == "snapshot" {
					conn, err := db.ConnectReadOnly(cmd.Context(), filepath.Join(filepath.Dir(store.Dir()), "atlas.db"))
					if err != nil {
						return err
					}
					defer conn.Close()
					snapshot, err = agent.ReadWorkflowSnapshot(cmd.Context(), store, session.NewService(db.New(conn), conn), root, args[0], false)
					if err != nil {
						return err
					}
				} else {
					if control.Action == "stop" {
						return fmt.Errorf("live stop requires the TUI or client/server mode; no process exit is inferred from local records")
					}
					dataDir, _ := cmd.Flags().GetString("data-dir")
					cfg, err := config.Load(root, dataDir, false)
					if err != nil {
						return err
					}
					store = engineering.NewStore(cfg.Config().Options.DataDirectory)
					if _, err := os.Stat(filepath.Join(cfg.Config().Options.DataDirectory, "atlas.db")); err != nil {
						return fmt.Errorf("existing session database required: %w", err)
					}
					conn, err := db.Connect(cmd.Context(), cfg.Config().Options.DataDirectory)
					if err != nil {
						return err
					}
					defer db.Release(cfg.Config().Options.DataDirectory)
					controller := agent.NewWorkflowController(cfg, session.NewService(db.New(conn), conn), store)
					if err := controller.WorkflowControl(cmd.Context(), args[0], control); err != nil {
						return err
					}
					snapshot, err = controller.WorkflowSnapshot(cmd.Context(), args[0])
					if err != nil {
						return err
					}
				}
			}
			return json.NewEncoder(cmd.OutOrStdout()).Encode(snapshot)
		}}
		if action == "control" {
			cmd.Flags().String("revision", "", "Required snapshot revision")
			cmd.Flags().String("task-id", "", "Pending task to reassign")
			cmd.Flags().String("agent", "", "Exact configured role name")
		}
		out = append(out, cmd)
	}
	return out
}
