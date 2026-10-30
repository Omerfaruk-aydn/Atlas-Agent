package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/attempt"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// attemptEnv is the repository and configuration an attempt command works
// against.
type attemptEnv struct {
	root     string
	stateDir string
	cfg      *config.Config
}

func newAttemptEnv(cmd *cobra.Command) (attemptEnv, error) {
	root, err := workflowRoot(cmd)
	if err != nil {
		return attemptEnv{}, err
	}
	dataDir, _ := cmd.Flags().GetString("data-dir")
	store, err := config.Load(root, dataDir, false)
	if err != nil {
		return attemptEnv{}, fmt.Errorf("failed to load config: %w", err)
	}
	cfg := store.Config()
	state := cfg.Options.DataDirectory
	if state == "" {
		state = ".atlas"
	}
	if !filepath.IsAbs(state) {
		state = filepath.Join(root, state)
	}
	return attemptEnv{root: root, stateDir: filepath.Join(state, "attempts"), cfg: cfg}, nil
}

func newAttemptCommand() *cobra.Command {
	command := &cobra.Command{
		Use:   "attempt [flags] <task>",
		Short: "Run N independent attempts at a task in isolated worktrees and pick the best",
		Long: `Run the same task several times, each attempt in its own disposable git
worktree (optionally with a different model), then score every attempt with
deterministic checks and an independent judge model and rank the results.

Nothing touches your working tree while attempts run. The ranked report names
a recommended winner; its patch is applied only when you approve it, either at
the prompt, with --apply, or later with "atlas-agent attempts apply".

Scoring: verify commands (from --verify, else .atlas/work.json, else the
detected test command) and lint decide first. A failing verify command is never
outranked by a judge preference. The judge sees anonymous diffs and check
results, and is chosen with --judge (default: the "judge" or "evaluator" model
role, else the default model). Models and the judge accept role names.`,
		Example: `  atlas-agent attempt "make the parser reject empty input"
  atlas-agent attempt --n 3 --models fast,fast,large --judge large "fix the flaky retry test"
  atlas-agent attempt --verify "go test ./..." --lint "go vet ./..." --apply "add a --dry-run flag"`,
		Args: cobra.MinimumNArgs(1),
		RunE: runAttempt,
	}
	flags := command.Flags()
	flags.IntP("n", "n", 3, fmt.Sprintf("Number of attempts (1-%d)", attempt.MaxAttempts))
	flags.StringSlice("models", nil, "Models or model roles for the attempts, comma separated; cycled when fewer than --n")
	flags.String("judge", "", "Judge model or role (default: the judge or evaluator role, else the default model)")
	flags.Bool("no-judge", false, "Rank by deterministic checks only")
	flags.StringArray("verify", nil, "Command that must pass for an attempt to count (repeatable)")
	flags.StringArray("lint", nil, "Lint command (repeatable)")
	flags.String("setup", "", "Command run once in each fresh worktree before the agent, e.g. to install dependencies")
	flags.Int("parallel", 3, "Attempts running at the same time")
	flags.Duration("timeout", 20*time.Minute, "Time limit for each attempt's agent run")
	flags.BoolP("yolo", "y", false, "Auto-approve tool permissions inside the attempts. They run in disposable worktrees, but shell commands can still reach outside them")
	flags.Bool("keep", false, "Keep the worktrees after the run")
	flags.Bool("apply", false, "Apply the recommended winner without asking")
	flags.Bool("json", false, "Print the report as JSON")
	return command
}

func runAttempt(cmd *cobra.Command, args []string) error {
	env, err := newAttemptEnv(cmd)
	if err != nil {
		return err
	}
	n, _ := cmd.Flags().GetInt("n")
	models, _ := cmd.Flags().GetStringSlice("models")
	judge, _ := cmd.Flags().GetString("judge")
	noJudge, _ := cmd.Flags().GetBool("no-judge")
	verify, _ := cmd.Flags().GetStringArray("verify")
	lint, _ := cmd.Flags().GetStringArray("lint")
	setup, _ := cmd.Flags().GetString("setup")
	parallel, _ := cmd.Flags().GetInt("parallel")
	timeout, _ := cmd.Flags().GetDuration("timeout")
	yolo, _ := cmd.Flags().GetBool("yolo")
	keep, _ := cmd.Flags().GetBool("keep")
	autoApply, _ := cmd.Flags().GetBool("apply")
	asJSON, _ := cmd.Flags().GetBool("json")

	if judge == "" {
		judge = attempt.DefaultJudge(env.cfg)
	} else {
		judge = attempt.ResolveModel(env.cfg, judge)
	}

	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt)
	defer stop()

	progress := func(msg string) { fmt.Fprintln(cmd.ErrOrStderr(), msg) }
	if asJSON {
		progress = func(string) {}
	}
	report, err := attempt.Run(ctx, attempt.Options{
		Root:         env.root,
		StateDir:     env.stateDir,
		Task:         strings.Join(args, " "),
		N:            n,
		Models:       attempt.ResolveModels(env.cfg, models),
		Judge:        judge,
		NoJudge:      noJudge,
		Verify:       verify,
		Lint:         lint,
		Setup:        setup,
		Parallel:     parallel,
		Timeout:      timeout,
		CheckTimeout: timeout,
		Yolo:         yolo,
		Keep:         keep,
		Progress:     progress,
	})
	if err != nil {
		return err
	}

	if asJSON {
		if autoApply && report.Winner != 0 {
			if report, _, err = attempt.Apply(cmd.Context(), env.stateDir, report.ID, 0); err != nil {
				return err
			}
		}
		return printJSON(cmd.OutOrStdout(), report)
	}
	fmt.Fprint(cmd.OutOrStdout(), attempt.Format(report))
	if report.Winner == 0 {
		return nil
	}
	if !autoApply && !confirmApply(cmd, report) {
		return nil
	}
	_, applied, err := attempt.Apply(cmd.Context(), env.stateDir, report.ID, 0)
	if err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Applied attempt %s (%d file(s), +%d/-%d) to %s. Review it with git diff.\n",
		applied.Label, len(applied.Files), applied.Additions, applied.Deletions, env.root)
	return nil
}

// confirmApply asks on an interactive terminal; without one, nothing is
// applied and the report says how to apply later.
func confirmApply(cmd *cobra.Command, report *attempt.Report) bool {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return false
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Apply attempt %s to your working tree? [y/N] ", string(rune('A'+report.Winner-1)))
	line, err := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
	if err != nil && line == "" {
		return false
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes"
}

func newAttemptsCommand() *cobra.Command {
	command := &cobra.Command{
		Use:   "attempts",
		Short: "List, inspect, apply and clean up stored attempt runs",
	}

	list := &cobra.Command{
		Use:   "list",
		Short: "List stored attempt runs, newest first",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			env, err := newAttemptEnv(cmd)
			if err != nil {
				return err
			}
			reports, err := attempt.List(env.stateDir)
			if err != nil {
				return err
			}
			if asJSON, _ := cmd.Flags().GetBool("json"); asJSON {
				return printJSON(cmd.OutOrStdout(), reports)
			}
			if len(reports) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No attempt runs.")
				return nil
			}
			for _, r := range reports {
				state := "not applied"
				if r.Applied != 0 {
					state = "applied " + string(rune('A'+r.Applied-1))
				}
				winner := "no winner"
				if r.Winner != 0 {
					winner = "winner " + string(rune('A'+r.Winner-1)) + " (" + r.Confidence + ")"
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s  %d attempts  %s  %s  %s\n", r.ID, len(r.Attempts), winner, state, oneLineTask(r.Task))
			}
			return nil
		},
	}
	list.Flags().Bool("json", false, "Print JSON")

	show := &cobra.Command{
		Use:   "show <run-id>",
		Short: "Show the ranked report of a stored run",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			env, err := newAttemptEnv(cmd)
			if err != nil {
				return err
			}
			report, err := attempt.Load(env.stateDir, args[0])
			if err != nil {
				return err
			}
			if asJSON, _ := cmd.Flags().GetBool("json"); asJSON {
				return printJSON(cmd.OutOrStdout(), report)
			}
			fmt.Fprint(cmd.OutOrStdout(), attempt.Format(report))
			return nil
		},
	}
	show.Flags().Bool("json", false, "Print JSON")

	apply := &cobra.Command{
		Use:   "apply <run-id> [--attempt N]",
		Short: "Apply the winner (or a chosen attempt) of a stored run to the working tree",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			env, err := newAttemptEnv(cmd)
			if err != nil {
				return err
			}
			pick := 0
			if value, _ := cmd.Flags().GetString("attempt"); value != "" {
				if pick, err = parseAttemptPick(value); err != nil {
					return err
				}
			}
			report, applied, err := attempt.Apply(cmd.Context(), env.stateDir, args[0], pick)
			if err != nil {
				return err
			}
			if applied.Status != attempt.StatusCompleted {
				fmt.Fprintf(cmd.ErrOrStderr(), "Note: attempt %s ended with status %s; review the change carefully.\n", applied.Label, applied.Status)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Applied attempt %s (%d file(s), +%d/-%d) of run %s to %s.\n",
				applied.Label, len(applied.Files), applied.Additions, applied.Deletions, report.ID, report.Root)
			return nil
		},
	}
	apply.Flags().String("attempt", "", "Attempt label (A, B, ...) or number to apply instead of the winner")

	clean := &cobra.Command{
		Use:   "clean",
		Short: "Delete stored attempt runs",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			env, err := newAttemptEnv(cmd)
			if err != nil {
				return err
			}
			olderThan, _ := cmd.Flags().GetDuration("older-than")
			removed, err := attempt.Clean(env.stateDir, olderThan)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Removed %d attempt run(s).\n", len(removed))
			return nil
		},
	}
	clean.Flags().Duration("older-than", 0, "Only remove runs older than this (default: all)")

	command.AddCommand(list, show, apply, clean)
	return command
}

// parseAttemptPick accepts "B" or "2".
func parseAttemptPick(value string) (int, error) {
	value = strings.TrimSpace(value)
	if n, err := strconv.Atoi(value); err == nil {
		if n < 1 {
			return 0, errors.New("attempt numbers start at 1")
		}
		return n, nil
	}
	if len(value) == 1 {
		c := strings.ToUpper(value)[0]
		if c >= 'A' && c <= 'Z' {
			return int(c-'A') + 1, nil
		}
	}
	return 0, fmt.Errorf("invalid attempt %q: use a label such as B or a number", value)
}

func oneLineTask(task string) string {
	task = strings.Join(strings.Fields(task), " ")
	if len(task) > 70 {
		task = task[:70] + "..."
	}
	return task
}

func init() {
	rootCmd.AddCommand(newAttemptCommand(), newAttemptsCommand())
}
