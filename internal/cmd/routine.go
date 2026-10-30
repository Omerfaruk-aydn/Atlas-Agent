package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/appenv"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/routine"
	"github.com/spf13/cobra"
)

// routineExecutor is replaced by tests so no real agent process starts.
var routineExecutor = func(cmd *cobra.Command) routine.Executor {
	exec := routine.ProcessExecutor{}
	if dir, _ := cmd.Flags().GetString("data-dir"); dir != "" {
		if abs, err := filepath.Abs(dir); err == nil {
			dir = abs
		}
		exec.ExtraArgs = []string{"--data-dir", dir}
	}
	return exec
}

// routineStoreDir is where routines live: one store per user, because each
// routine carries its own working directory. ATLAS_AGENT_ROUTINES_DIR
// overrides it.
func routineStoreDir() string {
	if dir := appenv.Get("ROUTINES_DIR"); dir != "" {
		return dir
	}
	return filepath.Join(filepath.Dir(config.GlobalConfigData()), "routines")
}

func routineStore() *routine.Store { return routine.NewStore(routineStoreDir()) }

func init() {
	rootCmd.AddCommand(newRoutineCommand())
}

// newRoutineCommand builds the routine command tree. Tests build their own
// so flag values never leak between runs.
func newRoutineCommand() *cobra.Command {
	c := &cobra.Command{
		Use:   "routine",
		Short: "Run prompts on a cron schedule, unattended",
		Long: `Routines are prompts that run headlessly on a cron schedule, each in its
own working directory, with results kept in a log.

A routine only runs while something is driving the schedule. Either keep
"routine daemon" running, or have the OS scheduler (cron, Task Scheduler,
a systemd timer) call "routine run --due" every minute.

Routines never run in bypass mode. By default they keep your configured
permission mode and allowed-tools list, and deny any call that would need
an answer, because nobody is there to give one. See docs/routines.md.`,
	}
	c.AddCommand(
		newRoutineAddCommand(),
		newRoutineListCommand(),
		newRoutineRemoveCommand(),
		newRoutineRunCommand(),
		newRoutineLogsCommand(),
		newRoutineDaemonCommand(),
		newRoutineToggleCommand("enable", true),
		newRoutineToggleCommand("disable", false),
	)
	return c
}

func newRoutineAddCommand() *cobra.Command {
	c := &cobra.Command{
		Use:   "add <cron> <prompt...>",
		Short: "Create a routine",
		Long: `Create a routine. The first argument is a five-field cron expression
(quote it) or a macro such as @daily; the rest is the prompt.`,
		Example: `  atlas-agent routine add "0 9 * * 1-5" --name standup "Summarize yesterday's commits"
  atlas-agent routine add @daily --dir ~/src/app --model small --goal "Get the flaky tests green"`,
		Args: cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			name, _ := cmd.Flags().GetString("name")
			dir, _ := cmd.Flags().GetString("dir")
			model, _ := cmd.Flags().GetString("model")
			goal, _ := cmd.Flags().GetBool("goal")
			perms, _ := cmd.Flags().GetString("permissions")
			timeout, _ := cmd.Flags().GetDuration("timeout")
			maxLate, _ := cmd.Flags().GetDuration("max-late")
			disabled, _ := cmd.Flags().GetBool("disabled")
			if dir == "" {
				cwd, err := ResolveCwd(cmd)
				if err != nil {
					return err
				}
				dir = cwd
			}
			dir, err := filepath.Abs(dir)
			if err != nil {
				return err
			}
			if info, err := os.Stat(dir); err != nil || !info.IsDir() {
				return fmt.Errorf("working directory %q does not exist", dir)
			}
			r := routine.Routine{
				Name:           name,
				Schedule:       args[0],
				Prompt:         strings.Join(args[1:], " "),
				Dir:            dir,
				Model:          model,
				Goal:           goal,
				Permissions:    perms,
				TimeoutSeconds: int(timeout / time.Second),
				MaxLateSeconds: int(maxLate / time.Second),
				Enabled:        !disabled,
			}
			added, err := routineStore().Add(cmd.Context(), r)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "Added routine %s (%s)\n", added.Name, added.ID)
			if next, err := added.NextAfter(time.Now()); err == nil && !next.IsZero() && added.Enabled {
				fmt.Fprintf(out, "Next run: %s\n", next.Format("2006-01-02 15:04 MST"))
			}
			if added.Permissions == routine.PermissionsAuto {
				fmt.Fprintln(out, "Warning: this routine approves every tool call without asking.")
			}
			return nil
		},
	}
	c.Flags().String("name", "", "Routine name (default: generated)")
	c.Flags().String("dir", "", "Working directory (default: --cwd or the current directory)")
	c.Flags().StringP("model", "m", "", "Model override: 'model' or 'provider/model'")
	c.Flags().Bool("goal", false, "Run the prompt as a goal: keep working until a verifier confirms it is met")
	c.Flags().String("permissions", routine.PermissionsConfigured, "Permission policy: configured, readonly, edits or auto (approves everything)")
	c.Flags().Duration("timeout", routine.DefaultTimeout, "Per-run timeout")
	c.Flags().Duration("max-late", 0, "Skip an occurrence discovered later than this after it was due (0 never skips)")
	c.Flags().Bool("disabled", false, "Create the routine paused")
	return c
}

func newRoutineListCommand() *cobra.Command {
	c := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List routines",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			routines, err := routineStore().List(cmd.Context())
			if err != nil {
				return err
			}
			if asJSON, _ := cmd.Flags().GetBool("json"); asJSON {
				if routines == nil {
					routines = []routine.Routine{}
				}
				return writeJSON(cmd.OutOrStdout(), routines)
			}
			if len(routines) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No routines. Create one with: routine add <cron> <prompt>")
				return nil
			}
			tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintln(tw, "NAME\tID\tSCHEDULE\tNEXT\tLAST\tPERMISSIONS\tDIR")
			now := time.Now()
			for _, r := range routines {
				next := "paused"
				if r.Enabled {
					base := r.LastSlot
					if base.IsZero() {
						base = r.CreatedAt
					}
					if n, err := r.NextAfter(base); err == nil && !n.IsZero() {
						next = n.Format("01-02 15:04")
						if !n.After(now) {
							next += " (due)"
						}
					} else {
						next = "never"
					}
				}
				last := "-"
				if r.LastStatus != "" {
					last = r.LastStatus
				}
				if r.Running(now) {
					last = "running"
				}
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", r.Name, r.ID, r.Schedule, next, last, r.Permissions, r.Dir)
			}
			return tw.Flush()
		},
	}
	c.Flags().Bool("json", false, "Print JSON")
	return c
}

func newRoutineRemoveCommand() *cobra.Command {
	return &cobra.Command{
		Use:     "remove <name|id>",
		Aliases: []string{"rm"},
		Short:   "Delete a routine and its log",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := routineStore().Remove(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Removed routine %s (%s)\n", r.Name, r.ID)
			return nil
		},
	}
}

func newRoutineToggleCommand(verb string, enabled bool) *cobra.Command {
	return &cobra.Command{
		Use:   verb + " <name|id>",
		Short: strings.ToUpper(verb[:1]) + verb[1:] + " a routine's schedule",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := routineStore().SetEnabled(cmd.Context(), args[0], enabled)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Routine %s is now %sd\n", r.Name, verb)
			return nil
		},
	}
}

func newRoutineRunCommand() *cobra.Command {
	c := &cobra.Command{
		Use:   "run [name|id]",
		Short: "Run a routine now, or every due routine with --due",
		Long: `Run one routine immediately, ignoring its schedule, or with --due run
every routine whose schedule has come due since it last ran and exit. --due
is what an OS scheduler should call every minute; it exits non-zero when a
run failed.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			due, _ := cmd.Flags().GetBool("due")
			if due == (len(args) == 1) {
				return errors.New("give either a routine name or --due")
			}
			ctx, cancel := signal.NotifyContext(cmd.Context(), os.Interrupt)
			defer cancel()
			rn := &routine.Runner{Store: routineStore(), Exec: routineExecutor(cmd)}
			out := cmd.OutOrStdout()
			if due {
				entries, err := rn.RunDue(ctx)
				for _, e := range entries {
					printRoutineEntry(out, e, false)
				}
				if err != nil {
					return err
				}
				return failedRuns(entries)
			}
			entry, err := rn.RunOne(ctx, args[0])
			if err != nil {
				return err
			}
			printRoutineEntry(out, entry, true)
			return failedRuns([]routine.LogEntry{entry})
		},
	}
	c.Flags().Bool("due", false, "Run all due routines once and exit")
	return c
}

func failedRuns(entries []routine.LogEntry) error {
	failed := 0
	for _, e := range entries {
		if e.Status == routine.StatusFailed || e.Status == routine.StatusTimedOut {
			failed++
		}
	}
	if failed > 0 {
		return fmt.Errorf("%d routine run(s) failed", failed)
	}
	return nil
}

func newRoutineLogsCommand() *cobra.Command {
	c := &cobra.Command{
		Use:   "logs <name|id>",
		Short: "Show a routine's recent runs",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			limit, _ := cmd.Flags().GetInt("limit")
			full, _ := cmd.Flags().GetBool("output")
			entries, err := routineStore().Logs(cmd.Context(), args[0], limit)
			if err != nil {
				return err
			}
			if asJSON, _ := cmd.Flags().GetBool("json"); asJSON {
				if entries == nil {
					entries = []routine.LogEntry{}
				}
				return writeJSON(cmd.OutOrStdout(), entries)
			}
			if len(entries) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No runs recorded yet.")
				return nil
			}
			for _, e := range entries {
				printRoutineEntry(cmd.OutOrStdout(), e, full)
			}
			return nil
		},
	}
	c.Flags().IntP("limit", "n", 5, "Number of runs to show (0 for all kept)")
	c.Flags().Bool("output", false, "Show the full output of each run")
	c.Flags().Bool("json", false, "Print JSON")
	return c
}

func newRoutineDaemonCommand() *cobra.Command {
	c := &cobra.Command{
		Use:   "daemon",
		Short: "Run the scheduler loop in the foreground",
		Long: `Run due routines as their schedules come up, until interrupted. Only one
daemon per user runs at a time. Run it under a service manager, or use
"routine run --due" from the OS scheduler instead.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			interval, _ := cmd.Flags().GetDuration("interval")
			ctx, cancel := signal.NotifyContext(cmd.Context(), os.Interrupt)
			defer cancel()
			out := cmd.OutOrStdout()
			rn := &routine.Runner{Store: routineStore(), Exec: routineExecutor(cmd)}
			fmt.Fprintf(out, "Routine daemon started (checking every %s). Press Ctrl+C to stop.\n", interval)
			return rn.Daemon(ctx, routine.DaemonOptions{
				Interval: interval,
				OnRun:    func(e routine.LogEntry) { printRoutineEntry(out, e, false) },
			})
		},
	}
	c.Flags().Duration("interval", 30*time.Second, "How often to check for due routines")
	return c
}

func printRoutineEntry(out io.Writer, e routine.LogEntry, withOutput bool) {
	line := fmt.Sprintf("%s  %s  %s  %s (%s)", e.StartedAt.Format("2006-01-02 15:04:05"), e.RoutineName, e.Status, e.Trigger, e.Duration().Round(time.Second))
	if e.SessionID != "" {
		line += "  session " + e.SessionID
	}
	if e.Cost > 0 {
		line += fmt.Sprintf("  cost $%.4f", e.Cost)
	}
	fmt.Fprintln(out, line)
	if e.Error != "" {
		fmt.Fprintf(out, "  error: %s\n", e.Error)
	}
	if withOutput && strings.TrimSpace(e.Output) != "" {
		fmt.Fprintln(out, indentLines(strings.TrimSpace(e.Output), "  | "))
	}
}

func indentLines(s, prefix string) string {
	return prefix + strings.ReplaceAll(s, "\n", "\n"+prefix)
}

func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
