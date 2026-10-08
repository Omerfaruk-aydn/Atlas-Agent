package main

import (
	"fmt"
	"os"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/mobilerelay"
)

func main() {
	command := mobilerelay.NewCommand()
	command.Use = "atlas-mobile-relay"
	command.SilenceErrors = true
	command.SilenceUsage = true
	if err := command.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
