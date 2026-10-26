package cmd

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agentstate"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/db"
	"github.com/spf13/cobra"
)

func newGoalCommand() *cobra.Command {
	command := &cobra.Command{Use: "goal", Short: "Inspect or stop an autonomous session goal"}
	status := &cobra.Command{Use: "status <session-id>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		ctx, svc, cleanup, err := sessionSetup(cmd)
		if err != nil {
			return err
		}
		defer cleanup()
		sess, err := resolveSessionID(ctx, svc.sessions, args[0])
		if err != nil {
			return err
		}
		dataDir, _ := cmd.Flags().GetString("data-dir")
		if dataDir == "" {
			dataDir = svc.cfg.Config().Options.DataDirectory
		}
		connection, err := db.Connect(ctx, dataDir)
		if err != nil {
			return err
		}
		var goal agentstate.Goal
		revision, err := (&agentstate.Store{DB: connection}).Get(ctx, "goals", sess.ID, &goal)
		if err != nil {
			return err
		}
		return json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]any{
			"session_id": sess.ID, "goal": sess.Goal, "used": goal.Used, "budget": goal.Budget,
			"claimed": goal.Claimed, "done": goal.Done, "stopped": goal.Stopped, "reason": goal.Reason,
			"updated_at": time.UnixMilli(goal.UpdatedAt).UTC(), "persisted": revision > 0,
		})
	}}
	stop := &cobra.Command{Use: "stop <session-id>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if err := selectSessionWorkspace(cmd); err != nil {
			return err
		}
		ws, cleanup, err := setupWorkspaceWithProgressBar(cmd)
		if err != nil {
			return err
		}
		defer cleanup()
		sess, err := resolveWorkspaceSessionID(cmd.Context(), ws, args[0])
		if err != nil {
			return err
		}
		ws.AgentCancel(sess.ID)
		if err := ws.AgentSetGoal(cmd.Context(), sess.ID, ""); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Goal stopped for session %s\n", sess.ID)
		return nil
	}}
	command.AddCommand(status, stop)
	return command
}

func init() { rootCmd.AddCommand(newGoalCommand()) }
