package agent

import "context"

// MaxSubAgentDepth is how many sub-agents may sit inside one another. A
// sub-agent that lists the spawning tools in its own front matter could
// otherwise delegate to itself without end.
const MaxSubAgentDepth = 3

type subAgentDepthKey struct{}

// subAgentDepth reports how many sub-agents the context is already inside.
func subAgentDepth(ctx context.Context) int {
	depth, _ := ctx.Value(subAgentDepthKey{}).(int)
	return depth
}

// withDeeperSubAgent marks the context as one level further inside.
func withDeeperSubAgent(ctx context.Context) context.Context {
	return context.WithValue(ctx, subAgentDepthKey{}, subAgentDepth(ctx)+1)
}
