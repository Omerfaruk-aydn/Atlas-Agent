// Package screens keeps the outside material a design file draws on: store
// screenshots of shipped apps, licensed photographs, generated pictures and
// the written platform and UX rules.
package screens

import (
	"path/filepath"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/design"
)

// Dir is where a design file's references and assets live.
func Dir(workDir, file string) string {
	return filepath.Join(workDir, ".atlas", "design", "screens", design.Slug(file))
}
