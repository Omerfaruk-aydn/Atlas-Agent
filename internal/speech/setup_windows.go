//go:build windows

package speech

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"time"
)

//go:embed setup.ps1
var setupScript string

func setup(ctx context.Context, o SetupOptions) (SetupResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 16*time.Minute)
	defer cancel()
	data, err := runSpeechHelper(ctx, setupScript, o)
	if err != nil {
		return SetupResult{}, err
	}
	return decodeSetupResult(data, o.CheckOnly)
}

func decodeSetupResult(data []byte, checkOnly bool) (SetupResult, error) {
	var result SetupResult
	if err := json.Unmarshal(bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf}), &result); err != nil {
		return result, fmt.Errorf("invalid Windows speech setup response: %w", err)
	}
	switch result.State {
	case "available":
		if result.Available && result.Language != "" {
			return result, nil
		}
	case "needs_install":
		if checkOnly && !result.Available {
			return result, nil
		}
	case "restart_required", "requires_admin", "declined", "unsupported_language", "unavailable", "failed", "install_in_progress":
		if !result.Available {
			return result, fmt.Errorf("%w: %s", ErrSetupFailed, result.Message)
		}
	}
	return result, fmt.Errorf("invalid Windows speech setup state: %q", result.State)
}
