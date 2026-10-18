package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/evaluation"
	"github.com/spf13/cobra"
)

func newEvalBenchCommand() *cobra.Command {
	bench := &cobra.Command{
		Use:   "bench",
		Short: "Measure harness changes on real tasks judged by hidden tests",
		Long: "A bench suite is a directory of small tasks, each with starting files, a prompt and hidden tests. " +
			"The hidden tests are copied over the workspace after the agent finishes, so only working code passes. " +
			"Variants run the same tasks with different Atlas configuration so a harness change can be compared with a baseline.",
	}

	var allowTools bool
	run := &cobra.Command{
		Use:   "run <manifest.json> <results.json>",
		Short: "Run every task under every variant and print the comparison",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			var manifest evaluation.BenchManifest
			if err := evaluation.ReadJSONFile(args[0], &manifest); err != nil {
				return err
			}
			// A relative suite path is relative to the manifest, so a
			// manifest works from any directory.
			if !filepath.IsAbs(manifest.Suite) {
				manifest.Suite = filepath.Join(filepath.Dir(args[0]), manifest.Suite)
			}
			program, err := os.Executable()
			if err != nil {
				return err
			}
			records, err := evaluation.RunBench(cmd.Context(), manifest, program, args[1], allowTools, nil)
			if len(records) > 0 {
				fmt.Fprint(cmd.OutOrStdout(), evaluation.FormatBenchReport(evaluation.SummarizeBench(records)))
				fmt.Fprintf(cmd.OutOrStdout(), "Recorded %d runs in %s; workspaces preserved for inspection.\n", len(records), args[1])
			}
			return err
		},
	}
	run.Flags().BoolVar(&allowTools, "allow-tools", false, "Allow the model to execute tools without prompts in the disposable bench workspaces")
	bench.AddCommand(run)

	bench.AddCommand(&cobra.Command{
		Use:   "validate <suite-dir>",
		Short: "Check that each task fails at the start and passes with its reference solution",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			tasks, err := evaluation.LoadBenchSuite(args[0], nil)
			if err != nil {
				return err
			}
			scratch, err := os.MkdirTemp("", "atlas-bench-validate-")
			if err != nil {
				return err
			}
			defer os.RemoveAll(scratch)
			failed := 0
			for _, task := range tasks {
				if err := evaluation.ValidateBenchTask(cmd.Context(), task, scratch, nil); err != nil {
					failed++
					fmt.Fprintf(cmd.OutOrStdout(), "FAIL %s: %v\n", task.ID, err)
					continue
				}
				fmt.Fprintf(cmd.OutOrStdout(), "ok   %s\n", task.ID)
			}
			if failed > 0 {
				return fmt.Errorf("%d of %d tasks are not valid", failed, len(tasks))
			}
			return nil
		},
	})

	rescore := &cobra.Command{
		Use:   "rescore <suite-dir> <results.json> <rescored.json>",
		Short: "Re-run the checks on preserved workspaces after fixing hidden tests; runs no agents",
		Args:  cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			var records []evaluation.BenchRecord
			if err := evaluation.ReadJSONFile(args[1], &records); err != nil {
				return err
			}
			rescored, err := evaluation.RescoreBench(cmd.Context(), args[0], records, nil)
			if err != nil {
				return err
			}
			encoded, err := json.MarshalIndent(rescored, "", "  ")
			if err != nil {
				return err
			}
			if err := os.WriteFile(args[2], encoded, 0o600); err != nil {
				return err
			}
			changed := 0
			for i := range records {
				if records[i].Passed != rescored[i].Passed {
					changed++
				}
			}
			fmt.Fprint(cmd.OutOrStdout(), evaluation.FormatBenchReport(evaluation.SummarizeBench(rescored)))
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "%d of %d verdicts changed; written to %s.\n", changed, len(records), args[2])
			return err
		},
	}
	bench.AddCommand(rescore)

	var asJSON bool
	report := &cobra.Command{
		Use:   "report <results.json>",
		Short: "Print the comparison for recorded bench results; does not run agents",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var records []evaluation.BenchRecord
			if err := evaluation.ReadJSONFile(args[0], &records); err != nil {
				return err
			}
			summaries := evaluation.SummarizeBench(records)
			if asJSON {
				encoder := json.NewEncoder(cmd.OutOrStdout())
				encoder.SetIndent("", "  ")
				return encoder.Encode(summaries)
			}
			_, err := fmt.Fprint(cmd.OutOrStdout(), evaluation.FormatBenchReport(summaries))
			return err
		},
	}
	report.Flags().BoolVar(&asJSON, "json", false, "Print the summaries as JSON")
	bench.AddCommand(report)
	return bench
}
