package agent

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
)

// transientFallbackEligible limits opt-in failover to temporary failures. It
// never classifies authentication, validation, or caller cancellation as a
// reason to send the request to another provider.
func (c *modelChain) transientFallbackEligible(err *fantasy.ProviderError) bool {
	if c.policy == nil || !c.policy.OnTransient || err == nil || err.AuthError {
		return false
	}
	if c.transientUsed >= c.policy.MaxProvidersOrDefault() {
		return false
	}
	if errors.Is(err.Cause, context.Canceled) || errors.Is(err.Cause, context.DeadlineExceeded) {
		return false
	}
	if err.StatusCode == http.StatusRequestTimeout || (err.StatusCode >= 500 && err.StatusCode <= 599) {
		return true
	}
	if err.StatusCode != 0 {
		return false
	}
	if err.TransientError || errors.Is(err.Cause, io.ErrUnexpectedEOF) || fantasy.IsTransportError(err.Cause) {
		return true
	}
	var netErr net.Error
	return errors.As(err.Cause, &netErr) && netErr.Timeout()
}

// fallbackCompatible refuses candidates that cannot represent the original
// request. Context fitting still runs immediately before every model attempt.
func fallbackCompatible(primary, candidate Model) bool {
	if candidate.Model == nil {
		return false
	}
	if primary.CatwalkCfg.SupportsImages && !candidate.CatwalkCfg.SupportsImages {
		return false
	}
	if (primary.ModelCfg.Think || primary.ModelCfg.ReasoningEffort != "") && !candidate.CatwalkCfg.CanReason {
		return false
	}
	primaryWindow := primary.CatwalkCfg.ContextWindow
	candidateWindow := candidate.CatwalkCfg.ContextWindow
	return primaryWindow <= 0 || candidateWindow <= 0 || candidateWindow >= primaryWindow
}
