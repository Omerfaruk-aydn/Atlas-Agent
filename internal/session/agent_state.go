package session

import "github.com/Omerfaruk-aydn/Atlas-Agent/internal/agentstate"

// AgentState exposes the shared SQLite runtime store without widening mocks.
func (s *service) AgentState() *agentstate.Store {
	if s.db == nil {
		return nil
	}
	return &agentstate.Store{DB: s.db}
}
