package cmd

import (
	"fmt"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/marking"
	"github.com/spf13/cobra"
)

var markCmd = &cobra.Command{
	Use:   "mark",
	Short: "Mark a region or element on screen for Atlas (Esc cancels)",
	Long: `Shows a full-screen overlay over a frozen capture of your desktop. Drag
to mark a region or click to mark the accessibility element under the
pointer. Nothing you do in the overlay reaches the applications below.
The mark id can then be referenced in a prompt or read with the
screen_mark tool.`,
	RunE: func(cmd *cobra.Command, _ []string) error {
		note, _ := cmd.Flags().GetString("note")
		validate, _ := cmd.Flags().GetString("validate")
		store := marking.Store{Dir: config.GlobalMarksDir()}
		if validate != "" {
			m, _, err := store.Get(validate)
			if err != nil {
				return err
			}
			m.Validity = marking.Validate(m, marking.Desktop{}, time.Now())
			_ = store.Update(m)
			return printJSON(cmd.OutOrStdout(), m.Validity)
		}
		m, crop, err := marking.CaptureMark(cmd.Context(), note)
		if err != nil {
			return err
		}
		saved, err := store.Save(m, crop)
		if err != nil {
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), marking.Manifest(saved))
		return nil
	},
}

func init() {
	markCmd.Flags().String("note", "", "Short description of what you marked")
	markCmd.Flags().String("validate", "", "Validate an existing mark id against the live screen instead")
	rootCmd.AddCommand(markCmd)
}
