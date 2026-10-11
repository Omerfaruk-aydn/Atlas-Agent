package support

import (
	"fmt"
	"os"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/nodes"
)

// ComputerFileRoots discovers accessible filesystem roots for the /support
// menu. The owner grants file capabilities separately when accepting support.
func ComputerFileRoots() ([]nodes.Root, error) {
	candidates, err := computerRootCandidates()
	if err != nil {
		return nil, fmt.Errorf("discover computer drives: %w", err)
	}
	var roots []nodes.Root
	for _, root := range candidates {
		if info, err := os.Stat(root.Path); err == nil && info.IsDir() {
			roots = append(roots, root)
		}
	}
	if len(roots) == 0 {
		return nil, fmt.Errorf("no accessible computer drives were found")
	}
	return roots, nil
}
