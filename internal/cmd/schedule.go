package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agent/tools"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/execution"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/lock"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/maintenance"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/shell"
	"github.com/spf13/cobra"
)

func scheduleEnabled(ctx context.Context, store *engineering.Store, set *bool) (bool, error) {
	record, data, err := store.ReadRecord(ctx, "maintenance-worker", "enabled")
	enabled := false
	if err == nil {
		if err := json.Unmarshal(data, &enabled); err != nil {
			return false, err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	if set != nil {
		encoded, err := json.Marshal(*set)
		if err != nil {
			return false, err
		}
		_, err = store.PutRecordStrict(ctx, "maintenance-worker", "enabled", record.Revision, encoded)
		return *set, err
	}
	return enabled, nil
}

func newScheduleCommand() *cobra.Command {
	command := &cobra.Command{Use: "schedule", Short: "Manage opt-in durable maintenance commands"}
	add := &cobra.Command{Use: "add <id>", Args: cobra.ExactArgs(1), Short: "Save an explicit argv command; paused unless --enabled", RunE: func(cmd *cobra.Command, args []string) error {
		store, err := workflowStore(cmd)
		if err != nil {
			return err
		}
		jobs, err := maintenance.List(cmd.Context(), store)
		if err != nil {
			return err
		}
		if len(jobs) >= 64 {
			return fmt.Errorf("at most 64 maintenance jobs")
		}
		root, err := workflowRoot(cmd)
		if err != nil {
			return err
		}
		encoded, _ := cmd.Flags().GetString("argv-json")
		var argv []string
		if err := json.Unmarshal([]byte(encoded), &argv); err != nil {
			return err
		}
		period, _ := cmd.Flags().GetDuration("every")
		timeout, _ := cmd.Flags().GetDuration("timeout")
		enabled, _ := cmd.Flags().GetBool("enabled")
		job := maintenance.Job{ID: args[0], Root: root, Argv: argv, PeriodMS: period.Milliseconds(), TimeoutMS: timeout.Milliseconds(), NextAt: time.Now().Add(period).UnixMilli(), Paused: !enabled}
		if err := maintenance.Save(cmd.Context(), store, job, 0); err != nil {
			return err
		}
		return json.NewEncoder(cmd.OutOrStdout()).Encode(job)
	}}
	add.Flags().String("argv-json", "", "Required JSON array of literal command arguments")
	add.Flags().Duration("every", time.Hour, "Repeat interval, minimum 10s")
	add.Flags().Duration("timeout", 2*time.Minute, "Per-run timeout, 1s-10m")
	add.Flags().Bool("enabled", false, "Enable this explicitly supplied recurring command")
	_ = add.MarkFlagRequired("argv-json")
	command.AddCommand(add, &cobra.Command{Use: "list", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		store, err := workflowStore(cmd)
		if err != nil {
			return err
		}
		jobs, err := maintenance.List(cmd.Context(), store)
		if err != nil {
			return err
		}
		return json.NewEncoder(cmd.OutOrStdout()).Encode(jobs)
	}})
	for _, action := range []string{"pause", "resume", "recover"} {
		command.AddCommand(&cobra.Command{Use: action + " <id>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			store, err := workflowStore(cmd)
			if err != nil {
				return err
			}
			record, data, err := store.ReadRecord(cmd.Context(), "maintenance-jobs", args[0])
			if err != nil {
				return err
			}
			var job maintenance.Job
			if err := json.Unmarshal(data, &job); err != nil {
				return err
			}
			if action == "resume" && job.LeaseUntil != 0 {
				return fmt.Errorf("job has an unresolved run; inspect its effects and use schedule recover %s", job.ID)
			}
			if action == "recover" {
				if job.LeaseUntil > time.Now().UnixMilli() {
					return fmt.Errorf("job may still be running; recovery requires an expired lease")
				}
				job.LeaseUntil = 0
				job.Paused = true
			} else {
				job.Paused = action == "pause"
			}
			return maintenance.Save(cmd.Context(), store, job, record.Revision)
		}})
	}
	command.AddCommand(&cobra.Command{Use: "stop", Args: cobra.NoArgs, Short: "Ask the worker to stop at the next poll; does not kill unrelated processes", RunE: func(cmd *cobra.Command, args []string) error {
		store, err := workflowStore(cmd)
		if err != nil {
			return err
		}
		enabled := false
		_, err = scheduleEnabled(cmd.Context(), store, &enabled)
		return err
	}})
	command.AddCommand(&cobra.Command{Use: "start", Args: cobra.NoArgs, Short: "Start a detached maintenance worker with logs in the data directory", RunE: func(cmd *cobra.Command, args []string) error {
		store, err := workflowStore(cmd)
		if err != nil {
			return err
		}
		root, err := workflowRoot(cmd)
		if err != nil {
			return err
		}
		executable, err := os.Executable()
		if err != nil {
			return err
		}
		enabled := true
		if _, err := scheduleEnabled(cmd.Context(), store, &enabled); err != nil {
			return err
		}
		logFile, err := os.OpenFile(filepath.Join(store.Dir(), "maintenance.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			return err
		}
		defer logFile.Close()
		child := exec.CommandContext(context.Background(), executable, "schedule", "worker", "--cwd", root, "--data-dir", filepath.Dir(store.Dir()))
		detachProcess(child)
		child.Stdout = logFile
		child.Stderr = logFile
		if err := child.Start(); err != nil {
			return err
		}
		pid := child.Process.Pid
		if err := child.Process.Release(); err != nil {
			return err
		}
		return json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]any{"spawned_pid": pid, "log": logFile.Name()})
	}})
	worker := &cobra.Command{Use: "worker", Args: cobra.NoArgs, Short: "Run the maintenance worker in the foreground", RunE: runScheduleWorker}
	worker.Flags().Bool("once", false, "Run one due-job poll and exit")
	command.AddCommand(worker)
	return command
}

func runScheduleWorker(cmd *cobra.Command, args []string) error {
	store, err := workflowStore(cmd)
	if err != nil {
		return err
	}
	root, err := workflowRoot(cmd)
	if err != nil {
		return err
	}
	lockCtx, cancel := context.WithTimeout(cmd.Context(), 2*time.Second)
	release, err := lock.File(lockCtx, filepath.Join(store.Dir(), "maintenance-worker.lock"))
	cancel()
	if err != nil {
		return err
	}
	defer release()
	cfg, err := config.Load(root, filepath.Dir(store.Dir()), false)
	if err != nil {
		return err
	}
	policy := tools.ExecutionPolicy(cfg.Config())
	runner := func(ctx context.Context, job maintenance.Job) (int, string, error) {
		if filepath.Clean(job.Root) != filepath.Clean(root) {
			return -1, "", fmt.Errorf("job belongs to another workspace")
		}
		if policy.Mode != "" && policy.Mode != "legacy" {
			ctx = execution.WithBinding(ctx, execution.Binding{Root: root, Store: store, Factory: func(ctx context.Context) (execution.Runner, error) { return execution.NewRunner(ctx, policy, store) }})
		}
		sh := shell.NewShell(&shell.Options{WorkingDir: root})
		stdout, stderr, err := sh.ExecArgv(ctx, job.Argv)
		if !shell.ObservedExit(err) {
			return -1, "", err
		}
		return shell.ExitCode(err), stdout + stderr, nil
	}
	once, _ := cmd.Flags().GetBool("once")
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		if !once {
			enabled, err := scheduleEnabled(cmd.Context(), store, nil)
			if err != nil {
				return err
			}
			if !enabled {
				return nil
			}
		}
		events, err := maintenance.Tick(cmd.Context(), store, time.Now(), runner)
		if err != nil {
			return err
		}
		for _, event := range events {
			if err := json.NewEncoder(cmd.OutOrStdout()).Encode(event); err != nil {
				return err
			}
		}
		if once {
			return nil
		}
		select {
		case <-cmd.Context().Done():
			return cmd.Context().Err()
		case <-ticker.C:
		}
	}
}

func init() { rootCmd.AddCommand(newScheduleCommand()) }
