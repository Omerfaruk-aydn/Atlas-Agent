package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/evaluation"
	"github.com/spf13/cobra"
)

func newEvalCommand() *cobra.Command {
	command := &cobra.Command{Use: "eval", Short: "List quality scenarios and score recorded agent evaluations"}
	command.AddCommand(&cobra.Command{
		Use: "recommend <policy.json> <results.json>", Short: "Select a role model from comparable live quality, cost and duration evidence", Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			var policy evaluation.SelectionPolicy
			var records []evaluation.LiveRecord
			if err := evaluation.ReadJSONFile(args[0], &policy); err != nil {
				return err
			}
			if err := evaluation.ReadJSONFile(args[1], &records); err != nil {
				return err
			}
			result, err := evaluation.Recommend(policy, records, time.Now())
			if err != nil {
				return err
			}
			encoder := json.NewEncoder(cmd.OutOrStdout())
			encoder.SetIndent("", "  ")
			return encoder.Encode(result)
		},
	})
	command.AddCommand(&cobra.Command{
		Use: "scenarios", Short: "Print engineering and UI/UX evaluation scenarios as JSON", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			scenarios, err := evaluation.Scenarios()
			if err != nil {
				return err
			}
			encoder := json.NewEncoder(cmd.OutOrStdout())
			encoder.SetIndent("", "  ")
			return encoder.Encode(scenarios)
		},
	})
	var allowTools bool
	live := &cobra.Command{Use: "run <manifest.json> <results.json>", Short: "Run live model comparisons in preserved disposable Git worktrees", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		file, err := os.Open(args[0])
		if err != nil {
			return err
		}
		defer file.Close()
		data, err := io.ReadAll(io.LimitReader(file, 1024*1024+1))
		if err != nil {
			return err
		}
		if len(data) > 1024*1024 {
			return fmt.Errorf("manifest exceeds 1 MiB")
		}
		var manifest evaluation.LiveManifest
		if err := json.Unmarshal(data, &manifest); err != nil {
			return err
		}
		program, err := os.Executable()
		if err != nil {
			return err
		}
		records, err := evaluation.RunLive(cmd.Context(), manifest, program, args[1], allowTools, nil)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(cmd.OutOrStdout(), "Recorded %d live runs in %s; worktrees preserved for inspection.\n", len(records), args[1])
		return err
	}}
	live.Flags().BoolVar(&allowTools, "allow-tools", false, "Allow the model to execute tools without prompts in disposable benchmark worktrees")
	command.AddCommand(live)
	command.AddCommand(&cobra.Command{
		Use: "score <results.json>", Short: "Score recorded criteria and metrics; does not execute agents", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			file, err := os.Open(args[0])
			if err != nil {
				return err
			}
			defer file.Close()
			data, err := io.ReadAll(io.LimitReader(file, 1024*1024+1))
			if err != nil {
				return err
			}
			if len(data) > 1024*1024 {
				return fmt.Errorf("evaluation results exceed 1 MiB")
			}
			var runs []evaluation.Run
			if err := json.Unmarshal(data, &runs); err != nil {
				return fmt.Errorf("decode evaluation records: %w", err)
			}
			scenarios, err := evaluation.Scenarios()
			if err != nil {
				return err
			}
			comparisons, err := evaluation.Compare(scenarios, runs)
			if err != nil {
				return err
			}
			encoder := json.NewEncoder(cmd.OutOrStdout())
			encoder.SetIndent("", "  ")
			if len(comparisons) > 1 {
				return encoder.Encode(comparisons)
			}
			if len(comparisons) == 1 {
				return encoder.Encode(comparisons[0].Report)
			}
			report, err := evaluation.Score(scenarios, runs)
			if err != nil {
				return err
			}
			return encoder.Encode(report)
		},
	})
	return command
}

func init() { rootCmd.AddCommand(newEvalCommand()) }
