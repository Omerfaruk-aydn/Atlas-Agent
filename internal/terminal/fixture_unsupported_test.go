//go:build !windows && !linux

package terminal

import "github.com/Omerfaruk-aydn/Atlas-Agent/internal/execution"

func fixtureIsTerminal() bool { return false }
func fixtureTerminalSize() (execution.TerminalSize, error) {
	return execution.TerminalSize{}, ErrUnavailable
}
