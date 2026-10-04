//go:build windows

package speech

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"runtime"
	"strings"
	"time"
)

//go:embed vosk.ps1
var voskScript string

func runVosk(ctx context.Context, path string, o DictationOptions) (Status, string, error) {
	if err := ctx.Err(); err != nil {
		return Status{}, "", err
	}
	if runtime.GOARCH != "amd64" {
		return Status{Backend: "vosk"}, "", ErrUnsupported
	}
	status, model, runtimeDir, err := findVosk(o)
	if err != nil {
		return status, "", err
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	data, err := runSpeechHelper(ctx, voskScript, map[string]string{"path": path, "model": model, "runtime": runtimeDir})
	if err != nil {
		return status, "", err
	}
	var result struct {
		Text  string `json:"text"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return status, "", errors.New("invalid Vosk speech helper response")
	}
	if result.Error != "" {
		return status, "", errors.New("vosk: " + result.Error)
	}
	return status, strings.TrimSpace(result.Text), nil
}

func checkVosk(ctx context.Context, o DictationOptions) (Status, error) {
	status, _, err := runVosk(ctx, "", o)
	return status, err
}

func transcribeVosk(ctx context.Context, path string, o DictationOptions) (string, error) {
	_, text, err := runVosk(ctx, path, o)
	return text, err
}
