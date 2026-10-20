// Package embedded provides access to all providers in a embedded manner.
// This basically means offline access to the providers.
package embedded

import (
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-models/internal/providers"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-models/pkg/catwalk"
)

// GetAll returns all embedded providers.
func GetAll() []catwalk.Provider {
	return providers.GetAll()
}

// ApplyLifecycle applies built-in availability rules to merged saved models.
func ApplyLifecycle(p catwalk.Provider, now time.Time) catwalk.Provider {
	return providers.ApplyLifecycle(p, now)
}
