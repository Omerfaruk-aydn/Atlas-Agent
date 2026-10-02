package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agent"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agent/tools"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/execution"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/hooks"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/permission"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/scenarios"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

func readScenarioFile(ctx context.Context, path string) (scenarios.Scenario, error) {
	file, err := os.Open(path)
	if err != nil {
		return scenarios.Scenario{}, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return scenarios.Scenario{}, err
	}
	if !info.Mode().IsRegular() || info.Size() > 1024*1024 {
		return scenarios.Scenario{}, fmt.Errorf("scenario requires a regular JSON file up to 1 MiB")
	}
	data, err := io.ReadAll(io.LimitReader(file, 1024*1024+1))
	if err != nil {
		return scenarios.Scenario{}, err
	}
	return scenarios.Parse(ctx, data)
}

func newScenarioCommand() *cobra.Command {
	command := &cobra.Command{Use: "scenario", Short: "Validate, execute and inspect source-bound web/terminal scenarios without an LLM"}
	for _, action := range []string{"validate", "run", "report"} {
		child := &cobra.Command{Use: action + " <file-or-run-id>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			root, err := workflowRoot(cmd)
			if err != nil {
				return err
			}
			sid, _ := cmd.Flags().GetString("session-id")
			services := tools.ScenarioServices{Root: root, SessionID: sid, Store: checkpointStore(cmd, root)}
			if action == "report" {
				saved, _, err := tools.ReadScenario(cmd.Context(), services, args[0])
				if err != nil {
					return err
				}
				return json.NewEncoder(cmd.OutOrStdout()).Encode(saved)
			}
			path := args[0]
			if !filepath.IsAbs(path) {
				path = filepath.Join(root, path)
			}
			scenario, err := readScenarioFile(cmd.Context(), path)
			if err != nil {
				return err
			}
			if action == "validate" {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]any{"valid": true, "scenario_id": scenario.ID, "executed": false})
			}
			allow, _ := cmd.Flags().GetBool("allow-execution")
			if !allow || sid == "" {
				return fmt.Errorf("run requires --session-id and --allow-execution for the declared browser/terminal effects")
			}
			dataDir, _ := cmd.Flags().GetString("data-dir")
			cfg, err := config.Load(root, dataDir, false)
			if err != nil {
				return err
			}
			services.Store = engineering.NewStore(cfg.Config().Options.DataDirectory)
			permissions := permission.NewPermissionService(root, true, nil)
			pre := hooks.NewRunner(cfg.Config().Hooks[hooks.EventPreToolUse], root, root)
			post := hooks.NewRunner(cfg.Config().Hooks[hooks.EventPostToolUse], root, root)
			policy := tools.ExecutionPolicy(cfg.Config())
			if policy.Mode != "" && policy.Mode != "legacy" {
				binding := execution.Binding{Root: root, Store: services.Store, Factory: func(ctx context.Context) (execution.Runner, error) {
					return execution.NewRunner(ctx, policy, services.Store)
				}}
				pre = pre.WithExecution(binding)
				post = post.WithExecution(binding)
			}
			browser := agent.WithToolHooks(tools.WithExecution(tools.NewBrowserTool(permissions, root, cfg.Config().Tools.Browser), cfg, services.Store), pre, post)
			invoke := func(ctx context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
				if call.Name != tools.BrowserToolName {
					return fantasy.NewTextErrorResponse("unsupported scenario tool"), nil
				}
				if !cfg.Config().Tools.Browser.IsEnabled() || !slices.Contains(cfg.Config().Agents[config.AgentCoder].AllowedTools, tools.BrowserToolName) {
					return fantasy.NewTextErrorResponse("browser disabled by configuration"), nil
				}
				operation, err := services.Store.Begin(ctx, sid, call.ID, call.Name, call.Input, "")
				if err != nil {
					return fantasy.ToolResponse{}, err
				}
				response, runErr := browser.Run(ctx, call)
				cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
				defer cancel()
				passed := runErr == nil && !response.IsError && !response.StopTurn
				if err := services.Store.FinishObserved(cleanup, sid, operation, passed, false, engineering.Hash(response.Content), passed); err != nil {
					return response, err
				}
				return response, runErr
			}
			tool := agent.WithToolHooks(tools.WithExecution(tools.NewScenarioTool(root, services.Store, permissions, invoke, tools.NewCommandPolicy(cfg.Config())), cfg, services.Store), pre, post)
			input, err := json.Marshal(tools.ScenarioParams{Action: "run", Scenario: scenario})
			if err != nil {
				return err
			}
			ctx := context.WithValue(cmd.Context(), tools.SessionIDContextKey, sid)
			response, err := tool.Run(ctx, fantasy.ToolCall{ID: uuid.NewString(), Name: tools.ScenarioToolName, Input: string(input)})
			if err != nil {
				return err
			}
			if response.IsError || response.StopTurn {
				return fmt.Errorf("scenario failed: %s", response.Content)
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), response.Content)
			return err
		}}
		child.Flags().String("session-id", "", "Workflow accounting session; required for run and report")
		if action == "run" {
			child.Flags().Bool("allow-execution", false, "Approve declared scenario effects; preserve configured hooks, limits and execution backend")
		}
		command.AddCommand(child)
	}
	return command
}
