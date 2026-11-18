package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"sort"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/sidecar"
	"github.com/spf13/cobra"
)

func newSidecarCommand() *cobra.Command {
	c := &cobra.Command{
		Use:   "sidecar",
		Short: "Run helper processes that live in this project",
		Long: "A sidecar is a folder .atlas/sidecars/<id>/ with a sidecar.json (command, args, env, restart_policy). " +
			"It never starts until you enable it here; the agent cannot enable one. Running sidecars get " +
			"ATLAS_AGENTAPI_EXE and can start or continue conversations with `agentapi`, always read-only or " +
			"edits-only, never with every permission granted.",
	}
	c.AddCommand(
		&cobra.Command{Use: "list", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
			project, err := workflowRoot(cmd)
			if err != nil {
				return err
			}
			found, errs := sidecar.Discover(sidecar.Dir(project))
			type row struct {
				ID      string `json:"id"`
				Enabled bool   `json:"enabled"`
				Command string `json:"command"`
				Restart string `json:"restart_policy"`
			}
			rows := make([]row, 0, len(found))
			for _, s := range found {
				rows = append(rows, row{s.ID, s.Enabled, s.Def.Command, s.Def.Policy()})
			}
			for _, e := range errs {
				fmt.Fprintln(cmd.ErrOrStderr(), "warning:", e)
			}
			return json.NewEncoder(cmd.OutOrStdout()).Encode(rows)
		}},
		toggleSidecar("enable", true),
		toggleSidecar("disable", false),
		&cobra.Command{Use: "logs <id>", Args: cobra.ExactArgs(1), Short: "Print the newest log of a sidecar", RunE: func(cmd *cobra.Command, args []string) error {
			project, err := workflowRoot(cmd)
			if err != nil {
				return err
			}
			dir := filepath.Join(sidecar.RuntimeDir(project, args[0]), "logs")
			entries, err := os.ReadDir(dir)
			if err != nil || len(entries) == 0 {
				return fmt.Errorf("no logs for sidecar %q", args[0])
			}
			names := make([]string, 0, len(entries))
			for _, e := range entries {
				names = append(names, e.Name())
			}
			sort.Strings(names)
			f, err := os.Open(filepath.Join(dir, names[len(names)-1]))
			if err != nil {
				return err
			}
			defer f.Close()
			_, err = io.Copy(cmd.OutOrStdout(), f)
			return err
		}},
		&cobra.Command{Use: "run", Args: cobra.NoArgs, Short: "Supervise the enabled sidecars until interrupted", RunE: func(cmd *cobra.Command, args []string) error {
			project, err := workflowRoot(cmd)
			if err != nil {
				return err
			}
			exe, err := os.Executable()
			if err != nil {
				return err
			}
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt)
			defer stop()
			m := &sidecar.Manager{Project: project, Exe: exe}
			go reportSidecars(ctx, cmd.ErrOrStderr(), m)
			return m.Run(ctx)
		}},
	)
	return c
}

func toggleSidecar(name string, enabled bool) *cobra.Command {
	return &cobra.Command{Use: name + " <id>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		project, err := workflowRoot(cmd)
		if err != nil {
			return err
		}
		return sidecar.SetEnabled(sidecar.Dir(project), args[0], enabled)
	}}
}

// reportSidecars prints status changes while the supervisor runs.
func reportSidecars(ctx context.Context, w io.Writer, m *sidecar.Manager) {
	last := map[string]sidecar.Status{}
	ticker := newTicker()
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			for _, i := range m.Status() {
				if last[i.ID] != i.Status {
					last[i.ID] = i.Status
					fmt.Fprintf(w, "sidecar %s: %s %s\n", i.ID, i.Status, i.LastErr)
				}
			}
		}
	}
}

func init() { rootCmd.AddCommand(newSidecarCommand()) }
