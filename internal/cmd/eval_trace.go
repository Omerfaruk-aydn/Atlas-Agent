package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/harnessmetrics"
	"github.com/spf13/cobra"
)

func newEvalTraceCommand() *cobra.Command {
	var turn int
	var asJSON bool
	cmd := &cobra.Command{Use: "trace <session-id>", Short: "Read retained local turn metadata without running a model", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		root, _ := cmd.Flags().GetString("data-dir")
		if root == "" {
			cwd, _ := cmd.Flags().GetString("cwd")
			cfg, err := config.Load(cwd, "", false)
			if err != nil {
				return err
			}
			root = cfg.Config().Options.DataDirectory
		}
		events, err := harnessmetrics.LoadTrace(root, args[0], turn)
		if err != nil {
			return err
		}
		if asJSON {
			enc := json.NewEncoder(cmd.OutOrStdout())
			enc.SetIndent("", "  ")
			return enc.Encode(events)
		}
		_, err = fmt.Fprint(cmd.OutOrStdout(), harnessmetrics.FormatTrace(events))
		return err
	}}
	cmd.Flags().IntVar(&turn, "turn", 0, "Filter a retained turn number; zero reads all retained turns")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Print versioned metadata as JSON")
	return cmd
}
