package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/evaluation"
	"github.com/spf13/cobra"
)

func newPromptComparisonCommand() *cobra.Command {
	var minimum int
	command := &cobra.Command{Use: "prompt-compare <before.json> <after.json>", Short: "Compare matched prompt evaluations without executing agents", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		sets := [2][]evaluation.LiveRecord{}
		for i, path := range args {
			f, err := os.Open(path)
			if err != nil {
				return err
			}
			data, err := io.ReadAll(io.LimitReader(f, 1024*1024+1))
			f.Close()
			if err != nil {
				return err
			}
			if len(data) > 1024*1024 {
				return fmt.Errorf("prompt evaluation input exceeds 1 MiB")
			}
			if err := json.Unmarshal(data, &sets[i]); err != nil {
				return err
			}
		}
		scenarios, err := evaluation.Scenarios()
		if err != nil {
			return err
		}
		report, err := evaluation.ComparePromptVersions(scenarios, sets[0], sets[1], minimum)
		if err != nil {
			return err
		}
		encoder := json.NewEncoder(cmd.OutOrStdout())
		encoder.SetIndent("", "  ")
		return encoder.Encode(report)
	}}
	command.Flags().IntVar(&minimum, "min-samples", 3, "Required equal repeats per matching model, role and case")
	return command
}
