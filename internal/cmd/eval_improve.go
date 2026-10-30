package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/evaluation"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/harnessmetrics"
	"github.com/spf13/cobra"
)

// improveRunner replaces process execution in tests, so the loop can run
// with mock agents and checks without a model or a built binary.
var improveRunner evaluation.CommandRunner

type improveFlags struct {
	suite, model, candidate, baseline string
	proposal, results                 string
	repeats, timeout                  int
	tasks, check                      []string
	mine, plan, allowTools            bool
	minHardness, maxTasks             int
}

func newEvalImproveCommand() *cobra.Command {
	var f improveFlags
	command := &cobra.Command{
		Use:   "improve",
		Short: "Mine failures into hard bench tasks and A/B test a harness variant against the baseline",
		Long: `The self-improving quality loop.

1. With --mine, the failure corpus (requests that ended with unmet checks or
   unresolved findings) is mined into hard bench tasks: multi-file changes,
   loosely specified requests and errors the error memory keeps seeing. Each
   task is kept only if its check fails on the starting files, every time.
2. The baseline and the candidate harness variant (an Atlas config fragment:
   a prompt, gate or option set) run on the suite with repeats.
3. The result is reported with intervals, run-to-run variance and warnings
   about ceilings, flaky tasks and small samples, and written to a proposal
   file.

Atlas never applies a change. The proposal says what the evidence supports;
you decide and edit your configuration yourself. Running the variants spends
real model tokens. Use --plan to mine and validate without running any agent.`,
		Example: `  atlas eval improve --suite bench/mined --mine --plan
  atlas eval improve --suite bench/mined --mine --model anthropic/claude-sonnet-4 \
      --candidate-config variants/strict-gate.json --repeats 3 --allow-tools`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error { return runEvalImprove(cmd, f) },
	}
	flags := command.Flags()
	flags.StringVar(&f.suite, "suite", "", "Bench suite directory; mined tasks are added to it")
	flags.StringVar(&f.model, "model", "", "Explicit model ID to run both variants with")
	flags.StringVar(&f.candidate, "candidate-config", "", "JSON file with the Atlas config fragment of the candidate variant")
	flags.StringVar(&f.baseline, "baseline-config", "", "JSON file with the Atlas config fragment of the baseline (default: none)")
	flags.IntVar(&f.repeats, "repeats", evaluation.DefaultImproveRepeats, "Attempts per task and variant; fewer than 3 hides variance")
	flags.IntVar(&f.timeout, "timeout", 0, "Per-attempt timeout in seconds (default: 600)")
	flags.StringSliceVar(&f.tasks, "task", nil, "Only run these task IDs")
	flags.BoolVar(&f.mine, "mine", false, "Mine the failure corpus into the suite first")
	flags.IntVar(&f.minHardness, "min-hardness", evaluation.DefaultMinHardness, "Lowest hardness score a corpus case needs to be mined")
	flags.IntVar(&f.maxTasks, "max-tasks", 0, "Most tasks to mine (default: the bench limit)")
	flags.StringSliceVar(&f.check, "check", nil, "Judge command as comma-separated argv, replacing the recorded checks of mined cases")
	flags.BoolVar(&f.plan, "plan", false, "Only mine and validate the suite; run no agent and spend no tokens")
	flags.StringVar(&f.proposal, "proposal", "improve-proposal.json", "Where to write the proposal")
	flags.StringVar(&f.results, "results", "improve-results.json", "Where to write the raw bench records")
	flags.BoolVar(&f.allowTools, "allow-tools", false, "Allow the model to execute tools without prompts in the disposable bench workspaces")
	return command
}

func runEvalImprove(cmd *cobra.Command, f improveFlags) error {
	out := cmd.OutOrStdout()
	if f.suite == "" {
		return errors.New("--suite is required")
	}
	var mined []evaluation.MinedTask
	if f.mine {
		var err error
		if mined, err = mineIntoSuite(cmd, f); err != nil {
			return err
		}
		fmt.Fprint(out, evaluation.FormatMined(mined))
	}
	if f.plan {
		if !f.mine {
			tasks, err := evaluation.LoadBenchSuite(f.suite, f.tasks)
			if err != nil {
				return err
			}
			fmt.Fprintf(out, "Suite %s holds %d tasks.\n", f.suite, len(tasks))
		}
		fmt.Fprintln(out, "Plan only: no agent was run.")
		return nil
	}
	if f.model == "" {
		return errors.New("--model is required to run the variants")
	}
	if f.candidate == "" {
		return errors.New("--candidate-config is required: the candidate must differ from the baseline")
	}
	options := evaluation.ImproveOptions{
		Suite: f.suite, Model: f.model, Tasks: f.tasks, Repeats: f.repeats,
		TimeoutSeconds: f.timeout, AllowTools: f.allowTools, Output: f.results, Runner: improveRunner,
	}
	var err error
	if options.CandidateConfig, err = readConfigFragment(f.candidate); err != nil {
		return err
	}
	if f.baseline != "" {
		if options.BaselineConfig, err = readConfigFragment(f.baseline); err != nil {
			return err
		}
	}
	if improveRunner == nil {
		if options.Program, err = os.Executable(); err != nil {
			return err
		}
	} else {
		options.Program = "atlas"
	}
	// A relative suite path is used by the bench from other directories.
	if options.Suite, err = filepath.Abs(f.suite); err != nil {
		return err
	}
	result, runErr := evaluation.RunImprove(cmd.Context(), options)
	if len(result.Records) == 0 {
		return runErr
	}
	proposal := evaluation.BuildProposal(options, mined, result, time.Now())
	if err := evaluation.WriteProposal(f.proposal, proposal); err != nil {
		return errors.Join(runErr, err)
	}
	fmt.Fprint(out, evaluation.FormatProposal(proposal))
	fmt.Fprintf(out, "Proposal written to %s; raw records in %s.\n", f.proposal, f.results)
	return runErr
}

func mineIntoSuite(cmd *cobra.Command, f improveFlags) ([]evaluation.MinedTask, error) {
	dataDir, err := corpusDataDir(cmd)
	if err != nil {
		return nil, err
	}
	cases, err := harnessmetrics.LoadCases(dataDir)
	if err != nil {
		return nil, err
	}
	if len(cases) == 0 {
		return nil, errors.New("the failure corpus is empty: nothing to mine yet")
	}
	recurring := map[string]bool{}
	roots := map[string]bool{}
	if cwd, err := os.Getwd(); err == nil {
		roots[cwd] = true
	}
	for _, c := range cases {
		if c.WorkingDir != "" {
			roots[c.WorkingDir] = true
		}
	}
	for root := range roots {
		for signature := range evaluation.RecurringSignatures(root) {
			recurring[signature] = true
		}
	}
	return evaluation.MineCorpus(cmd.Context(), cases, f.suite, evaluation.MineOptions{
		MinHardness: f.minHardness, MaxTasks: f.maxTasks, Check: f.check, Recurring: recurring, Runner: improveRunner,
	})
}

// readConfigFragment reads a variant's config and checks it is a JSON object.
func readConfigFragment(path string) (json.RawMessage, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		return nil, fmt.Errorf("%s must hold a JSON object: %w", path, err)
	}
	return data, nil
}
