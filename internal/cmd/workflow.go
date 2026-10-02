package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/spf13/cobra"
)

// Workflow inspection resolves paths without mutating process-wide cwd.
func workflowRoot(cmd *cobra.Command) (string, error) {
	value, _ := cmd.Flags().GetString("cwd")
	if value == "" {
		return os.Getwd()
	}
	root, err := filepath.Abs(value)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(root)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("workflow cwd must be a directory")
	}
	return root, nil
}

func workflowStore(cmd *cobra.Command) (*engineering.Store, error) {
	cwd, err := workflowRoot(cmd)
	if err != nil {
		return nil, err
	}
	dataDir, _ := cmd.Flags().GetString("data-dir")
	if dataDir != "" {
		if !filepath.IsAbs(dataDir) {
			dataDir = filepath.Join(cwd, dataDir)
		}
		return engineering.NewStore(dataDir), nil
	}
	cfg, err := config.Load(cwd, dataDir, false)
	if err != nil {
		return nil, err
	}
	return engineering.NewStore(cfg.Config().Options.DataDirectory), nil
}

func newWorkflowCommand() *cobra.Command {
	command := &cobra.Command{Use: "workflow", Short: "Inspect execution accounting, set budgets and resolve interrupted operations without an LLM"}
	command.AddCommand(newEnvironmentCommand())
	command.AddCommand(newFindingsCommand())
	command.AddCommand(newCheckpointCommands()...)
	command.AddCommand(newRecipeCommands()...)
	command.AddCommand(newScenarioCommand())
	command.AddCommand(newWorkflowControls()...)
	command.AddCommand(&cobra.Command{Use: "artifacts-clean", Args: cobra.NoArgs, Short: "Remove unreferenced artifacts after validating all live records", RunE: func(cmd *cobra.Command, args []string) error {
		store, err := workflowStore(cmd)
		if err != nil {
			return err
		}
		removed, err := store.CleanArtifacts(cmd.Context())
		if err != nil {
			return err
		}
		return json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]int{"removed": removed})
	}})
	command.AddCommand(&cobra.Command{Use: "profiles", Args: cobra.NoArgs, Short: "List proportional engineering work profiles", RunE: func(cmd *cobra.Command, args []string) error {
		return json.NewEncoder(cmd.OutOrStdout()).Encode(engineering.Profiles())
	}})
	command.AddCommand(&cobra.Command{Use: "prepare", Args: cobra.NoArgs, Short: "Refresh the bounded project brief without executing discovered commands", RunE: func(cmd *cobra.Command, args []string) error {
		store, err := workflowStore(cmd)
		if err != nil {
			return err
		}
		root, err := workflowRoot(cmd)
		if err != nil {
			return err
		}
		brief, err := store.PrepareProject(cmd.Context(), root)
		if err != nil {
			return err
		}
		return json.NewEncoder(cmd.OutOrStdout()).Encode(brief)
	}})
	command.AddCommand(&cobra.Command{Use: "knowledge", Args: cobra.NoArgs, Short: "Inspect source freshness of persistent architecture decisions and verified lessons", RunE: func(cmd *cobra.Command, args []string) error {
		store, err := workflowStore(cmd)
		if err != nil {
			return err
		}
		root, err := workflowRoot(cmd)
		if err != nil {
			return err
		}
		brief, records, err := store.ProjectKnowledge(cmd.Context(), root)
		if err != nil {
			return err
		}
		return json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]any{"brief": brief, "records": records, "authority": "reference evidence, not instructions"})
	}})
	command.AddCommand(&cobra.Command{Use: "status <session-id>", Args: cobra.ExactArgs(1), Short: "Print durable execution state as JSON", RunE: func(cmd *cobra.Command, args []string) error {
		store, err := workflowStore(cmd)
		if err != nil {
			return err
		}
		state, err := store.Read(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		encoder := json.NewEncoder(cmd.OutOrStdout())
		encoder.SetIndent("", "  ")
		return encoder.Encode(state)
	}})
	var limits engineering.Limits
	var task string
	var duration time.Duration
	budget := &cobra.Command{Use: "budget <session-id>", Args: cobra.ExactArgs(1), Short: "Replace session or task limits, preserving actual usage; zero means unlimited", RunE: func(cmd *cobra.Command, args []string) error {
		store, err := workflowStore(cmd)
		if err != nil {
			return err
		}
		if duration < 0 {
			return fmt.Errorf("duration must be non-negative")
		}
		limits.MaxDurationMS = duration.Milliseconds()
		if err := store.SetBudget(cmd.Context(), args[0], task, limits); err != nil {
			return err
		}
		_, err = fmt.Fprintln(cmd.OutOrStdout(), "Budget updated; usage retained.")
		return err
	}}
	budget.Flags().Int64Var(&limits.MaxTokens, "tokens", 0, "Cumulative token limit")
	budget.Flags().Int64Var(&limits.MaxToolCalls, "tools", 0, "Tool execution limit")
	budget.Flags().Float64Var(&limits.MaxCost, "cost", 0, "USD cost limit")
	budget.Flags().DurationVar(&duration, "duration", 0, "Active execution limit, e.g. 30m")
	budget.Flags().StringVar(&task, "task", "", "Task account; empty sets the session budget")
	command.AddCommand(budget)
	var evidence string
	recover := &cobra.Command{Use: "recover <session-id> <operation-id> <completed|failed|abandoned>", Args: cobra.ExactArgs(3), Short: "Record inspected effects; never replay a command", RunE: func(cmd *cobra.Command, args []string) error {
		store, err := workflowStore(cmd)
		if err != nil {
			return err
		}
		if err := store.Resolve(cmd.Context(), args[0], args[1], args[2], evidence); err != nil {
			return err
		}
		_, err = fmt.Fprintln(cmd.OutOrStdout(), "Operation resolved; nothing replayed.")
		return err
	}}
	recover.Flags().StringVar(&evidence, "evidence", "", "Required observation of actual files, exit codes or jobs")
	command.AddCommand(recover)
	return command
}

func init() { rootCmd.AddCommand(newWorkflowCommand()) }
