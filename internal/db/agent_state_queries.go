package db

import (
	_ "embed"
	"strings"
)

//go:embed sql/agent_state.sql
var agentStateSQL string

// AgentStateQuery returns a query from the persisted-state SQL source.
func AgentStateQuery(name string) string {
	parts := strings.Split(agentStateSQL, "-- "+name+"\n")
	if len(parts) != 2 {
		panic("unknown agent state query")
	}
	return strings.TrimSpace(strings.Split(parts[1], "\n-- ")[0])
}
