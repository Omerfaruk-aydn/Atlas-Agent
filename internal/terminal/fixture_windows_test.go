package terminal

import (
	"os"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/execution"
	"golang.org/x/sys/windows"
)

func fixtureIsTerminal() bool {
	var mode uint32
	return windows.GetConsoleMode(windows.Handle(os.Stdin.Fd()), &mode) == nil
}

func fixtureTerminalSize() (execution.TerminalSize, error) {
	var info windows.ConsoleScreenBufferInfo
	err := windows.GetConsoleScreenBufferInfo(windows.Handle(os.Stdout.Fd()), &info)
	return execution.TerminalSize{Width: int(info.Window.Right-info.Window.Left) + 1, Height: int(info.Window.Bottom-info.Window.Top) + 1}, err
}
