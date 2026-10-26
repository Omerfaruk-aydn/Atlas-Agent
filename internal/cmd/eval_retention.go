package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/evaluation"
	"github.com/spf13/cobra"
)

func newEvalRetentionCommand() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{Use: "retention [case-id summary.txt]", Short: "Score recorded summary facts against synthetic long-session evidence; no model calls", Args: func(cmd *cobra.Command, args []string) error {
		if len(args) != 0 && len(args) != 2 {
			return fmt.Errorf("provide no arguments for reference fixtures, or case-id and summary file")
		}
		return nil
	}, RunE: func(cmd *cobra.Command, args []string) error {
		cases, err := evaluation.RetentionCases()
		if err != nil {
			return err
		}
		var reports []evaluation.RetentionReport
		if len(args) == 2 {
			info, err := os.Stat(args[1])
			if err != nil {
				return err
			}
			if info.Size() > 1<<20 {
				return fmt.Errorf("summary exceeds 1 MiB")
			}
			data, err := os.ReadFile(args[1])
			if err != nil {
				return err
			}
			for _, c := range cases {
				if c.ID == args[0] {
					reports = append(reports, evaluation.ScoreRetention(c, string(data)))
				}
			}
			if len(reports) == 0 {
				return fmt.Errorf("unknown retention case %q", args[0])
			}
		} else {
			for _, c := range cases {
				reports = append(reports, evaluation.ScoreRetention(c, c.Summary), evaluation.ScoreRetention(c, c.Handoff))
			}
		}
		if asJSON {
			return json.NewEncoder(cmd.OutOrStdout()).Encode(reports)
		}
		for _, r := range reports {
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%s: %d/%d retained; missing %v (%s)\n", r.Case, r.Passed, r.Total, r.Missing, r.Method); err != nil {
				return err
			}
		}
		return nil
	}}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Print factual retention reports as JSON")
	return cmd
}
