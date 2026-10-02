//go:build !windows && !linux

package terminal

import "github.com/Omerfaruk-aydn/Atlas-Agent/internal/execution"

func startProcess(execution.Request, execution.TerminalSize) (processTerminal, error) {
	return processTerminal{}, ErrUnavailable
}
