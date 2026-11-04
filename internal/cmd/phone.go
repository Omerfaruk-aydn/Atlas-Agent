package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agent"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/connections"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/voice"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/workspace"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(newPhoneCommand())
}

func newPhoneCommand() *cobra.Command {
	c := &cobra.Command{
		Use:   "phone",
		Short: "Run the phone voice assistant and inspect its calls",
		Long: `The voice assistant lets the owner talk to Atlas by phone and lets Atlas
call the owner (call_owner tool, routines, site monitoring).

It is configured under tools.voice_calls and is off by default. Credentials
are referenced through environment variables, never written into the
configuration. See docs/voice-calls.md.`,
	}
	c.AddCommand(newPhoneServeCommand(), newPhoneCheckCommand(), newPhoneStatusCommand(), newPhoneTestCallCommand())
	return c
}

func newPhoneServeCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Serve the call webhooks, media and the local call API",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt)
			defer stop()
			ws, closeWS, err := setupLocalWorkspace(cmd)
			if err != nil {
				return err
			}
			defer closeWS()
			local := ws.(*workspace.AppWorkspace)
			if err := local.App().InitCoderAgentNonInteractive(ctx); err != nil {
				return err
			}
			fmt.Fprintln(cmd.ErrOrStderr(), "Voice service starting. Press Ctrl+C to stop.")
			err = local.App().RunVoiceService(ctx)
			if errors.Is(err, voice.ErrServiceDisabled) {
				return errors.New("the voice assistant is disabled; set tools.voice_calls.enabled to true")
			}
			return err
		},
	}
}

func newPhoneCheckCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "check",
		Short: "Validate the voice configuration without contacting any service",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ws, closeWS, err := setupLocalWorkspace(cmd)
			if err != nil {
				return err
			}
			defer closeWS()
			cfg := voice.Config(ws.(*workspace.AppWorkspace).App().Config().Tools.VoiceCalls)
			if err := cfg.Validate(connections.Env(filepath.Dir(config.GlobalConfigData()))); err != nil {
				fmt.Fprintln(cmd.OutOrStdout(), "Voice configuration has problems:")
				fmt.Fprintln(cmd.OutOrStdout(), err)
				return errors.New("voice configuration is invalid")
			}
			if !cfg.Enabled {
				fmt.Fprintln(cmd.OutOrStdout(), "Voice configuration is well-formed but disabled.")
				return nil
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Voice configuration is complete.")
			return nil
		},
	}
}

func newPhoneStatusCommand() *cobra.Command {
	c := &cobra.Command{
		Use:   "status",
		Short: "Show the running voice service and recent calls",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := voice.DialControl(agent.VoiceDataDir())
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
			defer cancel()
			st, err := client.Status(ctx)
			if err != nil {
				return err
			}
			limit, _ := cmd.Flags().GetInt("limit")
			calls, err := client.Calls(ctx, limit)
			if err != nil {
				return err
			}
			enc := json.NewEncoder(cmd.OutOrStdout())
			enc.SetIndent("", "  ")
			return enc.Encode(map[string]any{"status": st, "calls": calls})
		},
	}
	c.Flags().Int("limit", 10, "Number of recent calls to show")
	return c
}

func newPhoneTestCallCommand() *cobra.Command {
	c := &cobra.Command{
		Use:   "test-call",
		Short: "Place one real, billed test call to the configured owner number",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if yes, _ := cmd.Flags().GetBool("yes"); !yes {
				return errors.New("a test call rings the owner's phone and is billed by the provider; pass --yes to place it")
			}
			client, err := voice.DialControl(agent.VoiceDataDir())
			if err != nil {
				return err
			}
			res, err := client.CallOwner(cmd.Context(), voice.OwnerCallRequest{
				Reason:  "This is a test call from Atlas.",
				EventID: "test:" + time.Now().UTC().Format(time.RFC3339),
			})
			if err != nil {
				return err
			}
			enc := json.NewEncoder(cmd.OutOrStdout())
			enc.SetIndent("", "  ")
			return enc.Encode(res)
		},
	}
	c.Flags().Bool("yes", false, "Confirm placing a real, billed call")
	return c
}
