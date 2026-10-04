package cmd

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/speech"
	"github.com/spf13/cobra"
)

var voiceCmd = &cobra.Command{
	Use:   "voice",
	Short: "Inspect offline Vosk models or install optional Windows recognition",
	Args:  cobra.NoArgs,
}

func init() {
	status := &cobra.Command{Use: "status", Short: "Check offline speech models without opening the microphone", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		language, _ := cmd.Flags().GetString("language")
		backend, _ := cmd.Flags().GetString("backend")
		modelDir, _ := cmd.Flags().GetString("model-dir")
		runtimeDir, _ := cmd.Flags().GetString("runtime-dir")
		info, err := speech.Check(cmd.Context(), speech.DictationOptions{Language: language, Backend: backend, ModelDir: modelDir, RuntimeDir: runtimeDir})
		if err != nil && !errors.Is(err, speech.ErrNoRecognizer) && !errors.Is(err, speech.ErrUnsupported) {
			return err
		}
		payload := struct {
			Available bool `json:"available"`
			speech.Status
			Error string `json:"error,omitempty"`
		}{Available: err == nil, Status: info}
		if err != nil {
			payload.Error = err.Error()
		}
		data, err := json.MarshalIndent(payload, "", "  ")
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(cmd.OutOrStdout(), string(data))
		return err
	}}
	status.Flags().String("language", "", "Optional installed language code, such as en-US")
	status.Flags().String("backend", "vosk", "Offline recognizer: vosk or windows")
	status.Flags().String("model-dir", "", "Vosk models directory")
	status.Flags().String("runtime-dir", "", "Trusted Vosk runtime directory")
	install := &cobra.Command{Use: "install", Short: "Download verified offline Vosk models and runtime for Windows x64", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		language, _ := cmd.Flags().GetString("language")
		modelDir, _ := cmd.Flags().GetString("model-dir")
		runtimeDir, _ := cmd.Flags().GetString("runtime-dir")
		automatic, _ := cmd.Flags().GetBool("automatic")
		info, err := speech.InstallVosk(cmd.Context(), speech.DictationOptions{Language: language, ModelDir: modelDir, RuntimeDir: runtimeDir}, func(progress string) { fmt.Fprintln(cmd.OutOrStdout(), progress) })
		if err != nil {
			if automatic {
				fmt.Fprintln(cmd.ErrOrStderr(), "Atlas is installed; offline speech setup failed:", err, "; retry atlas-agent voice install.")
				return nil
			}
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), "Offline speech is ready:", info.Installed)
		return nil
	}}
	install.Flags().String("language", "", "Language to verify after installing all four models")
	install.Flags().String("model-dir", "", "Vosk models destination")
	install.Flags().String("runtime-dir", "", "Vosk runtime destination")
	install.Flags().Bool("automatic", false, "Keep CLI installation usable when optional speech downloads fail")
	_ = install.Flags().MarkHidden("automatic")
	setup := &cobra.Command{
		Use:   "setup",
		Short: "Install missing offline speech components through Windows Update",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			language, _ := cmd.Flags().GetString("language")
			checkOnly, _ := cmd.Flags().GetBool("check-only")
			noElevate, _ := cmd.Flags().GetBool("no-elevate")
			automatic, _ := cmd.Flags().GetBool("automatic")
			asJSON, _ := cmd.Flags().GetBool("json")
			if !checkOnly && !asJSON {
				fmt.Fprintln(cmd.OutOrStdout(), "Checking Windows offline speech. Missing components will be downloaded from Windows Update; Windows may request administrator approval.")
			}
			result, setupErr := speech.Setup(cmd.Context(), speech.SetupOptions{
				Language: language, CheckOnly: checkOnly, AllowElevation: !noElevate,
			})
			if setupErr != nil && result.Message == "" {
				result.State = "failed"
				result.Message = setupErr.Error()
			}
			var outputErr error
			if asJSON {
				outputErr = json.NewEncoder(cmd.OutOrStdout()).Encode(result)
			} else {
				_, outputErr = fmt.Fprintln(cmd.OutOrStdout(), result.Message)
			}
			if outputErr != nil {
				return outputErr
			}
			if automatic && (errors.Is(setupErr, speech.ErrSetupFailed) || errors.Is(setupErr, speech.ErrUnsupported)) {
				fmt.Fprintln(cmd.ErrOrStderr(), "Atlas installation can continue. Offline dictation is not ready; retry atlas-agent voice setup when Windows speech components are available.")
				return nil
			}
			return setupErr
		},
	}
	setup.Flags().String("language", "", "Dictation language; installed/system language, otherwise en-US. Turkish is unsupported")
	setup.Flags().Bool("check-only", false, "Inspect the installation plan without changing Windows or requesting UAC")
	setup.Flags().Bool("no-elevate", false, "Do not request Windows administrator approval")
	setup.Flags().Bool("json", false, "Print structured setup diagnostics")
	setup.Flags().Bool("automatic", false, "Keep CLI installation successful when optional Windows speech setup fails")
	_ = setup.Flags().MarkHidden("automatic")
	voiceCmd.AddCommand(status, setup, install)
}
