//go:build !windows

package support

import "github.com/Omerfaruk-aydn/Atlas-Agent/internal/nodes"

func computerRootCandidates() ([]nodes.Root, error) {
	return []nodes.Root{{Label: "System", Path: "/"}}, nil
}
