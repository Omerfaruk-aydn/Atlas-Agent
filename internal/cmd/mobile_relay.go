package cmd

import (
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/mobilerelay"
	"github.com/spf13/cobra"
)

func init() { rootCmd.AddCommand(newMobileRelayCommand()) }

func newMobileRelayCommand() *cobra.Command { return mobilerelay.NewCommand() }
