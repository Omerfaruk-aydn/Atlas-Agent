package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/isolation"
	"github.com/spf13/cobra"
)

var isolationCmd = &cobra.Command{
	Use:   "isolation",
	Short: "Isolated desktop environments (Windows Sandbox)",
	Long: `Isolated environments let Atlas use desktop applications without touching
your own desktop session. "probe" shows which backends this machine supports
and exactly why others are not available.`,
}

var isolationProbeCmd = &cobra.Command{
	Use:   "probe",
	Short: "Show available isolation backends and their prerequisites",
	RunE: func(cmd *cobra.Command, _ []string) error {
		asJSON, _ := cmd.Flags().GetBool("json")
		caps := newIsolationManager().Capabilities(cmd.Context())
		if asJSON {
			return printJSON(cmd.OutOrStdout(), caps)
		}
		for _, c := range caps {
			state := "unavailable"
			if c.Available {
				state = "available"
			}
			iso := "not isolated"
			if c.Isolated {
				iso = "isolated"
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%-16s %-12s %-13s %s\n", c.Backend, state, iso, c.Reason)
			if !c.Available && len(c.Prerequisites) > 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "%16s prerequisites: %s\n", "", strings.Join(c.Prerequisites, "; "))
			}
		}
		return nil
	},
}

var isolationStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start an Atlas-controlled Windows Sandbox",
	RunE: func(cmd *cobra.Command, _ []string) error {
		mem, _ := cmd.Flags().GetInt("memory-mb")
		st, err := newIsolationManager().Start(cmd.Context(), isolation.StartOptions{MemoryMB: mem})
		if err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Sandbox %s running; guest agent at %s registered as node %q.\n", st.Lease.ID, st.Lease.Address, st.Lease.NodeName)
		return nil
	},
}

var isolationStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "List Atlas isolated environments",
	RunE: func(cmd *cobra.Command, _ []string) error {
		list, err := newIsolationManager().List()
		if err != nil {
			return err
		}
		return printJSON(cmd.OutOrStdout(), list)
	},
}

var isolationStopCmd = &cobra.Command{
	Use:   "stop <lease-id>",
	Short: "Stop an Atlas sandbox (only processes Atlas started)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		st, err := newIsolationManager().Stop(args[0])
		if err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Stopped %s (%s).\n", st.Lease.ID, st.Record.State)
		return nil
	},
}

func newIsolationManager() *isolation.Manager {
	exe, _ := os.Executable()
	return isolation.NewManager(config.GlobalDevicesDir(), exe, nil)
}

func init() {
	isolationProbeCmd.Flags().Bool("json", false, "Print JSON")
	isolationStartCmd.Flags().Int("memory-mb", 0, "Guest memory in MiB")
	isolationCmd.AddCommand(isolationProbeCmd, isolationStartCmd, isolationStatusCmd, isolationStopCmd)
	rootCmd.AddCommand(isolationCmd)
}
