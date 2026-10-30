package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/rehearsal"
	"github.com/spf13/cobra"
)

func rehearsalStore(cmd *cobra.Command, root string) (*rehearsal.Store, error) {
	dataDir, _ := cmd.Flags().GetString("data-dir")
	cfg, err := config.Load(root, dataDir, false)
	if err != nil {
		return nil, err
	}
	return rehearsal.NewStore(cfg.Config().Options.DataDirectory), nil
}

func printRehearsal(cmd *cobra.Command, report *rehearsal.Report) error {
	if asJSON, _ := cmd.Flags().GetBool("json"); asJSON {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(report)
	}
	_, err := fmt.Fprint(cmd.OutOrStdout(), rehearsal.Render(report))
	return err
}

// agentRehearsal makes the change by running a non-interactive agent in
// the disposable copy. The copy is the only tree it can edit, which is why
// auto-approving its tool calls is acceptable; it is still a normal process
// with the user's privileges, so this stays behind an explicit flag.
func agentRehearsal(task string) rehearsal.AgentFunc {
	return func(ctx context.Context, dir string) (string, error) {
		program, err := os.Executable()
		if err != nil {
			return "", err
		}
		run := exec.CommandContext(ctx, program, "run", "--cwd", dir, "--quiet", "--yolo", task)
		out, err := run.CombinedOutput()
		return string(out), err
	}
}

func newRehearseCommand() *cobra.Command {
	command := &cobra.Command{
		Use:   "rehearse [task]",
		Short: "Preview a risky change in an isolated copy before applying it to the real tree",
		Long: `Copies the project to a temporary directory, runs the change there, runs your checks before and after, and prints a preview: files changed, checks that newly fail, exported Go API changes and every command executed. The real tree is only written by "rehearse apply <id>" (or --apply when the verdict is clean).

The change is given as shell commands (--run, repeatable) or, with --agent, performed by a non-interactive agent working inside the copy. Only file changes are applied; effects outside the tree (databases, installed packages) must be repeated for real.`,
		Example: `  atlas-agent rehearse "upgrade lodash" --run "npm update lodash" --check "npm test"
  atlas-agent rehearse "rename Foo to Bar" --run "gofmt -r 'Foo -> Bar' -w ." --check "go build ./..." --check "go test ./..."
  atlas-agent rehearse apply rh-20261030-101500-ab12cd`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			steps, _ := cmd.Flags().GetStringArray("run")
			checks, _ := cmd.Flags().GetStringArray("check")
			skip, _ := cmd.Flags().GetBool("skip-baseline")
			useAgent, _ := cmd.Flags().GetBool("agent")
			applyNow, _ := cmd.Flags().GetBool("apply")
			timeout, _ := cmd.Flags().GetDuration("timeout")
			task := strings.Join(args, " ")
			if len(steps) == 0 && !useAgent {
				return fmt.Errorf("supply the change with --run \"<command>\" (repeatable) or --agent")
			}
			if useAgent && task == "" {
				return fmt.Errorf("--agent needs the task as an argument")
			}
			root, err := workflowRoot(cmd)
			if err != nil {
				return err
			}
			store, err := rehearsalStore(cmd, root)
			if err != nil {
				return err
			}
			opts := rehearsal.Options{Root: root, Store: store, Task: task, Steps: steps, Checks: checks, SkipBaseline: skip, CommandTimeout: timeout}
			if useAgent {
				opts.Agent = agentRehearsal(task)
			}
			report, err := rehearsal.Run(cmd.Context(), opts)
			if err != nil {
				return err
			}
			if err := printRehearsal(cmd, report); err != nil {
				return err
			}
			if !applyNow {
				return nil
			}
			if report.Verdict == rehearsal.VerdictFailed || report.Verdict == rehearsal.VerdictBreakChecks {
				return fmt.Errorf("not applying: verdict is %s; review the report, then run \"rehearse apply %s --force\" if that is acceptable", report.Verdict, report.ID)
			}
			if report.Verdict == rehearsal.VerdictNoChanges {
				return nil
			}
			applied, err := store.Apply(cmd.Context(), report.ID, rehearsal.ApplyOptions{Root: root})
			if err != nil {
				return err
			}
			_, err = fmt.Fprintf(cmd.ErrOrStderr(), "Applied %d file(s) from %s.\n", len(applied.Files), applied.ID)
			return err
		},
	}
	command.Flags().StringArray("run", nil, "Shell command that makes the change, run inside the copy (repeatable, relative paths only)")
	command.Flags().StringArray("check", nil, "Check to run before and after the change, e.g. a test command (repeatable)")
	command.Flags().Bool("skip-baseline", false, "Do not run the checks on the untouched copy first")
	command.Flags().Bool("agent", false, "Let a non-interactive agent make the change inside the copy (it auto-approves its own tool calls there)")
	command.Flags().Bool("apply", false, "Apply immediately when the verdict is not failed or breaks_checks")
	command.Flags().Duration("timeout", 10*time.Minute, "Per-command timeout")
	command.Flags().Bool("json", false, "Print the report as JSON")

	command.AddCommand(&cobra.Command{Use: "list", Args: cobra.NoArgs, Short: "List stored rehearsals", RunE: func(cmd *cobra.Command, args []string) error {
		root, err := workflowRoot(cmd)
		if err != nil {
			return err
		}
		store, err := rehearsalStore(cmd, root)
		if err != nil {
			return err
		}
		reports, err := store.List()
		if err != nil {
			return err
		}
		for _, r := range reports {
			fmt.Fprintf(cmd.OutOrStdout(), "%s  %-9s %-13s %d file(s)  %s\n", r.ID, r.Status, r.Verdict, len(r.Files), r.Task)
		}
		return nil
	}})
	show := &cobra.Command{Use: "show <id>", Args: cobra.ExactArgs(1), Short: "Show a stored rehearsal report", RunE: func(cmd *cobra.Command, args []string) error {
		root, err := workflowRoot(cmd)
		if err != nil {
			return err
		}
		store, err := rehearsalStore(cmd, root)
		if err != nil {
			return err
		}
		report, err := store.Load(args[0])
		if err != nil {
			return err
		}
		if patch, _ := cmd.Flags().GetBool("patch"); patch {
			text, err := store.Patch(args[0])
			if err != nil {
				return err
			}
			_, err = fmt.Fprint(cmd.OutOrStdout(), text)
			return err
		}
		return printRehearsal(cmd, report)
	}}
	show.Flags().Bool("patch", false, "Print the unified diff instead of the report")
	show.Flags().Bool("json", false, "Print the report as JSON")
	command.AddCommand(show)

	apply := &cobra.Command{Use: "apply <id>", Args: cobra.ExactArgs(1), Short: "Apply a previewed rehearsal to the real project", RunE: func(cmd *cobra.Command, args []string) error {
		root, err := workflowRoot(cmd)
		if err != nil {
			return err
		}
		store, err := rehearsalStore(cmd, root)
		if err != nil {
			return err
		}
		force, _ := cmd.Flags().GetBool("force")
		applied, err := store.Apply(cmd.Context(), args[0], rehearsal.ApplyOptions{Root: root, Force: force})
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(cmd.OutOrStdout(), "Applied %d file(s) from %s.\n", len(applied.Files), applied.ID)
		return err
	}}
	apply.Flags().Bool("force", false, "Apply even if the verdict is failed or breaks_checks")
	command.AddCommand(apply)
	command.AddCommand(&cobra.Command{Use: "discard <id>", Args: cobra.ExactArgs(1), Short: "Discard a rehearsal", RunE: func(cmd *cobra.Command, args []string) error {
		root, err := workflowRoot(cmd)
		if err != nil {
			return err
		}
		store, err := rehearsalStore(cmd, root)
		if err != nil {
			return err
		}
		_, err = store.Discard(args[0])
		return err
	}})
	return command
}

func init() { rootCmd.AddCommand(newRehearseCommand()) }
