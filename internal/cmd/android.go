package cmd

import (
	"fmt"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/android"
	"github.com/spf13/cobra"
)

var androidCmd = &cobra.Command{
	Use:   "android",
	Short: "Android devices and emulators (via adb)",
}

var androidDevicesCmd = &cobra.Command{
	Use:   "devices",
	Short: "List Android devices/emulators and their connection state",
	RunE: func(cmd *cobra.Command, _ []string) error {
		adbPath, _ := cmd.Flags().GetString("adb")
		asJSON, _ := cmd.Flags().GetBool("json")
		path, err := android.Locate(cmd.Context(), adbPath)
		if err != nil {
			return err
		}
		devices, err := (&android.ADB{Path: path}).Devices(cmd.Context())
		if err != nil {
			return err
		}
		if asJSON {
			return printJSON(cmd.OutOrStdout(), map[string]any{"adb": path, "devices": devices})
		}
		fmt.Fprintf(cmd.OutOrStdout(), "adb: %s\n", path)
		if len(devices) == 0 {
			fmt.Fprintln(cmd.OutOrStdout(), "No devices. Connect a phone with USB debugging enabled or start an emulator.")
		}
		for _, d := range devices {
			kind := "device"
			if d.Emulator {
				kind = "emulator"
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%-22s %-13s %-9s %s %s\n", d.Serial, d.State, kind, d.Model, d.Hint)
		}
		return nil
	},
}

func init() {
	androidDevicesCmd.Flags().String("adb", "", "Path to adb (default: ANDROID_SDK_ROOT / ANDROID_HOME platform-tools, then PATH)")
	androidDevicesCmd.Flags().Bool("json", false, "Print JSON")
	androidCmd.AddCommand(androidDevicesCmd)
	rootCmd.AddCommand(androidCmd)
}
