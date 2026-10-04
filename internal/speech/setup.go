package speech

import (
	"context"
	"errors"
)

var ErrSetupFailed = errors.New("offline Windows speech setup did not make a recognizer available")

// SetupOptions controls Windows component installation, never audio capture.
type SetupOptions struct {
	Language       string `json:"language,omitempty"`
	CheckOnly      bool   `json:"check_only"`
	AllowElevation bool   `json:"allow_elevation"`
}

// SetupResult distinguishes an installed component from a usable recognizer.
type SetupResult struct {
	Status
	Available       bool     `json:"available"`
	State           string   `json:"state"`
	Capabilities    []string `json:"capabilities,omitempty"`
	RestartRequired bool     `json:"restart_required"`
	Fallback        bool     `json:"fallback"`
	Message         string   `json:"message"`
}

// Setup installs missing Windows capabilities from Windows Update. CheckOnly
// never installs components, requests elevation or opens the microphone.
func Setup(ctx context.Context, o SetupOptions) (SetupResult, error) {
	if err := (DictationOptions{Language: o.Language}).Validate(); err != nil {
		return SetupResult{}, err
	}
	if err := ctx.Err(); err != nil {
		return SetupResult{}, err
	}
	return setup(ctx, o)
}
