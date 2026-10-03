package cmd

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agentstate"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/db"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/permission"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/vault"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/workspace"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func platformStore(cmd *cobra.Command) (*agentstate.Store, string, string, func(), error) {
	root, err := workflowRoot(cmd)
	if err != nil {
		return nil, "", "", nil, err
	}
	dir, _ := cmd.Flags().GetString("data-dir")
	dataDir := dir
	if dataDir == "" {
		cfg, e := config.Load(root, dir, false)
		if e != nil {
			return nil, "", "", nil, e
		}
		dataDir = cfg.Config().Options.DataDirectory
	} else if !filepath.IsAbs(dataDir) {
		dataDir = filepath.Join(root, dataDir)
	}
	conn, err := db.Connect(cmd.Context(), dataDir)
	if err != nil {
		return nil, "", "", nil, err
	}
	h := sha256.Sum256([]byte(root))
	scope := "workspace-" + hex.EncodeToString(h[:16])
	return &agentstate.Store{DB: conn}, root, scope, func() { _ = db.Release(dataDir) }, nil
}

func newVaultCommand() *cobra.Command {
	command := &cobra.Command{Use: "vault", Short: "Manage origin-bound encrypted browser credentials without sending secrets to a model"}
	add := &cobra.Command{Use: "add ID", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		s, _, scope, close, err := platformStore(cmd)
		if err != nil {
			return err
		}
		defer close()
		origin, _ := cmd.Flags().GetString("origin")
		username, _ := cmd.Flags().GetString("username")
		if !term.IsTerminal(int(os.Stdin.Fd())) {
			return errors.New("vault add requires an interactive terminal; secrets are never accepted in command arguments")
		}
		fmt.Fprint(cmd.ErrOrStderr(), "Password (hidden): ")
		secret, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(cmd.ErrOrStderr())
		if err != nil {
			return err
		}
		defer clear(secret)
		err = (&vault.Store{State: s, Namespace: scope + "/vault"}).Add(cmd.Context(), args[0], origin, username, string(secret))
		if err != nil {
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), "Credential saved.")
		return nil
	}}
	add.Flags().String("origin", "", "Exact HTTPS origin")
	add.Flags().String("username", "", "Login identifier")
	_ = add.MarkFlagRequired("origin")
	_ = add.MarkFlagRequired("username")
	command.AddCommand(add)
	for _, action := range []string{"list", "remove"} {
		c := &cobra.Command{Use: action, Args: cobra.NoArgs}
		if action == "remove" {
			c.Use = "remove ID"
			c.Args = cobra.ExactArgs(1)
		}
		c.RunE = func(cmd *cobra.Command, args []string) error {
			s, _, scope, close, err := platformStore(cmd)
			if err != nil {
				return err
			}
			defer close()
			v := &vault.Store{State: s, Namespace: scope + "/vault"}
			if action == "remove" {
				return v.Remove(cmd.Context(), args[0])
			}
			items, err := v.List(cmd.Context())
			if err != nil {
				return err
			}
			return json.NewEncoder(cmd.OutOrStdout()).Encode(items)
		}
		command.AddCommand(c)
	}
	return command
}

func newAgentJobsCommand() *cobra.Command {
	command := &cobra.Command{Use: "agent-jobs", Short: "Manage durable heartbeat and isolated agent cron jobs"}
	add := &cobra.Command{Use: "add ID", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		s, root, scope, close, err := platformStore(cmd)
		if err != nil {
			return err
		}
		defer close()
		prompt, _ := cmd.Flags().GetString("prompt")
		kind, _ := cmd.Flags().GetString("kind")
		session, _ := cmd.Flags().GetString("session")
		every, _ := cmd.Flags().GetDuration("every")
		timeout, _ := cmd.Flags().GetDuration("timeout")
		runs, _ := cmd.Flags().GetInt("max-runs")
		enabled, _ := cmd.Flags().GetBool("enabled")
		if session != "" {
			if _, err := db.New(s.DB).GetSessionByID(cmd.Context(), session); err != nil {
				return errors.New("heartbeat session not found; use its full ID")
			}
		}
		job := agentstate.Job{ID: args[0], Kind: kind, Root: root, SessionID: session, Prompt: prompt, EverySeconds: int64(every / time.Second), TimeoutSeconds: int64(timeout / time.Second), MaxRuns: runs, NextAt: time.Now().Add(every).Unix(), Paused: !enabled}
		if err := agentstate.SaveJob(cmd.Context(), s, scope+"/agent_jobs", job, 0); err != nil {
			return err
		}
		return json.NewEncoder(cmd.OutOrStdout()).Encode(job)
	}}
	add.Flags().String("prompt", "", "Explicit recurring instruction")
	_ = add.MarkFlagRequired("prompt")
	add.Flags().String("kind", "cron", "cron or heartbeat")
	add.Flags().String("session", "", "Full session ID for heartbeat")
	add.Flags().Duration("every", time.Hour, "Repeat interval, at least 60s")
	add.Flags().Duration("timeout", 5*time.Minute, "Per-run timeout")
	add.Flags().Int("max-runs", 10, "Maximum paid runs")
	add.Flags().Bool("enabled", false, "Enable the explicitly supplied job")
	command.AddCommand(add)
	for _, action := range []string{"list", "pause", "resume", "clear", "recover"} {
		c := &cobra.Command{Use: action, Args: cobra.NoArgs}
		if action != "list" {
			c.Use = action + " ID"
			c.Args = cobra.ExactArgs(1)
		}
		c.RunE = func(cmd *cobra.Command, args []string) error {
			s, _, scope, close, err := platformStore(cmd)
			if err != nil {
				return err
			}
			defer close()
			if action != "list" {
				return agentstate.ControlJob(cmd.Context(), s, scope+"/agent_jobs", args[0], action)
			}
			jobs, err := agentstate.Jobs(cmd.Context(), s, scope+"/agent_jobs")
			if err != nil {
				return err
			}
			return json.NewEncoder(cmd.OutOrStdout()).Encode(jobs)
		}
		command.AddCommand(c)
	}
	worker := &cobra.Command{Use: "worker", Short: "Run explicitly enabled jobs while this process remains active", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		ws, close, err := setupLocalWorkspace(cmd)
		if err != nil {
			return err
		}
		defer close()
		local := ws.(*workspace.AppWorkspace)
		if err := local.App().InitCoderAgentNonInteractive(cmd.Context()); err != nil {
			return err
		}
		coordinator := local.App().AgentCoordinator
		if coordinator == nil {
			return errors.New("configure an agent model before starting the worker")
		}
		once, _ := cmd.Flags().GetBool("once")
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		runner, ok := coordinator.(interface{ AutomationTick(context.Context) error })
		if !ok {
			return errors.New("automation unavailable")
		}
		for {
			if err := runner.AutomationTick(cmd.Context()); err != nil {
				return err
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
	}}
	worker.Flags().Bool("once", false, "Check for one due job and exit")
	command.AddCommand(worker)
	return command
}

func newTaskBoardCommand() *cobra.Command {
	command := &cobra.Command{Use: "task-board", Short: "Inspect and control durable tasks, worker leases and evidence review"}
	add := &cobra.Command{Use: "add ID", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		s, root, scope, close, err := platformStore(cmd)
		if err != nil {
			return err
		}
		defer close()
		title, _ := cmd.Flags().GetString("title")
		prompt, _ := cmd.Flags().GetString("prompt")
		criteria, _ := cmd.Flags().GetStringSlice("acceptance")
		deps, _ := cmd.Flags().GetStringSlice("depends-on")
		return agentstate.AddTask(cmd.Context(), s, scope+"/task_board", agentstate.Task{ID: args[0], Root: root, Title: title, Prompt: prompt, Acceptance: criteria, Dependencies: deps})
	}}
	add.Flags().String("title", "", "Task title")
	add.Flags().String("prompt", "", "Implementation instruction")
	add.Flags().StringSlice("acceptance", nil, "Acceptance criteria")
	add.Flags().StringSlice("depends-on", nil, "Existing prerequisite task IDs")
	for _, f := range []string{"title", "prompt", "acceptance"} {
		_ = add.MarkFlagRequired(f)
	}
	command.AddCommand(add)
	command.AddCommand(&cobra.Command{Use: "list", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		s, _, scope, close, err := platformStore(cmd)
		if err != nil {
			return err
		}
		defer close()
		tasks, err := agentstate.Tasks(cmd.Context(), s, scope+"/task_board")
		if err != nil {
			return err
		}
		return json.NewEncoder(cmd.OutOrStdout()).Encode(tasks)
	}})
	for _, action := range []string{"claim", "renew", "submit", "block", "accept", "retry", "recover"} {
		c := &cobra.Command{Use: action + " ID", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			s, _, scope, close, err := platformStore(cmd)
			if err != nil {
				return err
			}
			defer close()
			ns := scope + "/task_board"
			worker, _ := cmd.Flags().GetString("worker")
			attempt, _ := cmd.Flags().GetString("attempt")
			evidence, _ := cmd.Flags().GetStringSlice("evidence")
			result, _ := cmd.Flags().GetString("result")
			if action == "claim" {
				task, err := agentstate.ClaimTask(cmd.Context(), s, ns, args[0], worker, 300)
				if err != nil {
					return err
				}
				return json.NewEncoder(cmd.OutOrStdout()).Encode(task)
			}
			return agentstate.TransitionTask(cmd.Context(), s, ns, args[0], action, attempt, evidence, result)
		}}
		c.Flags().String("worker", "", "Explicit worker identity")
		c.Flags().String("attempt", "", "Claim attempt token")
		c.Flags().StringSlice("evidence", nil, "Project-relative evidence files")
		c.Flags().String("result", "", "Bounded report")
		command.AddCommand(c)
	}
	command.AddCommand(newTaskWorkerCommand())
	return command
}

func newTaskWorkerCommand() *cobra.Command {
	c := &cobra.Command{Use: "run ID", Short: "Claim and execute one task with the configured Atlas agent; submit for review", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		state, _, scope, closeState, err := platformStore(cmd)
		if err != nil {
			return err
		}
		defer closeState()
		worker, _ := cmd.Flags().GetString("worker")
		evidence, _ := cmd.Flags().GetStringSlice("evidence")
		if len(evidence) == 0 {
			return errors.New("specify --evidence paths the agent must produce")
		}
		task, err := agentstate.ClaimTask(cmd.Context(), state, scope+"/task_board", args[0], worker, 3600)
		if err != nil {
			return err
		}
		defer func() {
			cleanup, stop := context.WithTimeout(context.WithoutCancel(cmd.Context()), 5*time.Second)
			defer stop()
			var latest agentstate.Task
			if _, err := state.Get(cleanup, scope+"/task_board", task.ID, &latest); err == nil && latest.Status == "running" && latest.Attempt == task.Attempt {
				_ = agentstate.TransitionTask(cleanup, state, scope+"/task_board", task.ID, "block", task.Attempt, nil, "Worker exited before evidence submission; inspect changes and retry.")
			}
		}()
		ws, close, err := setupLocalWorkspace(cmd)
		if err != nil {
			_ = agentstate.TransitionTask(cmd.Context(), state, scope+"/task_board", task.ID, "block", task.Attempt, nil, "Worker setup failed.")
			return err
		}
		defer close()
		local := ws.(*workspace.AppWorkspace)
		if err := local.App().InitCoderAgentNonInteractive(cmd.Context()); err != nil {
			return err
		}
		coordinator := local.App().AgentCoordinator
		if coordinator == nil {
			return errors.New("agent unavailable")
		}
		ctx, cancel := context.WithTimeout(cmd.Context(), time.Hour)
		ctx = permission.WithoutPrompts(ctx)
		defer cancel()
		sess, err := ws.CreateSession(ctx, "Board: "+task.Title)
		if err != nil {
			return err
		}
		_ = agentstate.Update[agentstate.Task](ctx, state, scope+"/task_board", task.ID, func(t *agentstate.Task) error {
			if t.Attempt != task.Attempt {
				return agentstate.ErrConflict
			}
			t.SessionID = sess.ID
			return nil
		})
		result, runErr := coordinator.Run(ctx, sess.ID, fmt.Sprintf("%s\nAcceptance: %v\nProduce evidence files: %v\nReport limitations honestly; this task will be reviewed.", task.Prompt, task.Acceptance, evidence))
		report := ""
		if result != nil {
			report = result.Response.Content.Text()
		}
		if len(report) > 8000 {
			report = report[:8000]
		}
		cleanup, stop := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer stop()
		action := "submit"
		if runErr != nil {
			action = "block"
			evidence = nil
		}
		transition := agentstate.TransitionTask(cleanup, state, scope+"/task_board", task.ID, action, task.Attempt, evidence, report)
		return errors.Join(runErr, transition)
	}}
	c.Flags().String("worker", "atlas", "Worker identity")
	c.Flags().StringSlice("evidence", nil, "Evidence paths to produce")
	return c
}

func newSourceMemoryCommand() *cobra.Command {
	c := &cobra.Command{Use: "source-memory [query]", Short: "Inspect provenance, expiry and source freshness without an LLM", Args: cobra.MaximumNArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		s, root, scope, close, err := platformStore(cmd)
		if err != nil {
			return err
		}
		defer close()
		q := ""
		if len(args) > 0 {
			q = args[0]
		}
		m, err := agentstate.Memories(cmd.Context(), s, scope+"/source_memory", root, q)
		if err != nil {
			return err
		}
		return json.NewEncoder(cmd.OutOrStdout()).Encode(m)
	}}
	return c
}

func init() {
	rootCmd.AddCommand(newVaultCommand(), newAgentJobsCommand(), newTaskBoardCommand(), newSourceMemoryCommand())
}
