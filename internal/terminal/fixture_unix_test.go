//go:build linux

package terminal

import (
	"os"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-term"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/execution"
	"github.com/creack/pty"
)

func fixtureIsTerminal() bool { return term.IsTerminal(os.Stdin.Fd()) }
func fixtureTerminalSize() (execution.TerminalSize, error) {
	size, err := pty.GetsizeFull(os.Stdout)
	if err != nil {
		return execution.TerminalSize{}, err
	}
	return execution.TerminalSize{Width: int(size.Cols), Height: int(size.Rows)}, nil
}
