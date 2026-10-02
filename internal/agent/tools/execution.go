package tools

import (
	"context"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/execution"
)

func ExecutionPolicy(cfg *config.Config) execution.ExecutionPolicy {
	if cfg == nil || cfg.Options == nil || cfg.Options.Execution == nil {
		return execution.ExecutionPolicy{Mode: "legacy"}
	}
	p := cfg.Options.Execution
	return execution.ExecutionPolicy{Mode: p.Mode, RuntimePath: p.RuntimePath, Image: p.Image, Network: p.Network, ReadOnly: p.ReadOnly, CPUs: p.CPUs, MemoryBytes: p.MemoryBytes, MaxProcesses: p.MaxProcesses, TimeoutMS: p.TimeoutMS, EnvironmentKeys: append([]string(nil), p.EnvironmentKeys...)}
}

type executionTool struct {
	fantasy.AgentTool
	cfg   *config.ConfigStore
	store *engineering.Store
}

func WithExecution(tool fantasy.AgentTool, cfg *config.ConfigStore, store *engineering.Store) fantasy.AgentTool {
	return &executionTool{AgentTool: tool, cfg: cfg, store: store}
}

func (t *executionTool) Run(ctx context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
	policy := ExecutionPolicy(t.cfg.Config())
	if execution.IsReadOnly(ctx) {
		policy.ReadOnly = true
	}
	if policy.Mode != "" && policy.Mode != "legacy" {
		root := t.cfg.WorkingDir()
		if scope := engineering.GetScope(ctx, GetSessionFromContext(ctx)); scope.WriteRoot != "" {
			root = scope.WriteRoot
		}
		ctx = execution.WithBinding(ctx, execution.Binding{Root: root, Store: t.store, Factory: func(ctx context.Context) (execution.Runner, error) { return execution.NewRunner(ctx, policy, t.store) }})
	}
	return t.AgentTool.Run(ctx, call)
}
