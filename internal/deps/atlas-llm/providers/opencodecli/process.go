package opencodecli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"golang.org/x/mod/semver"
)

type cliResult struct {
	text  string
	usage fantasy.Usage
}

func executable(path string) (string, error) {
	if path != "" {
		resolved, err := exec.LookPath(path)
		if err != nil {
			return "", fmt.Errorf("opencode-cli: configured executable unavailable: %w", err)
		}
		if runtime.GOOS == "windows" && !strings.EqualFold(filepath.Ext(resolved), ".exe") {
			return "", fmt.Errorf("opencode-cli: configure the native opencode.exe, not a shell wrapper")
		}
		return resolved, nil
	}
	if resolved, err := exec.LookPath("opencode"); err == nil && (runtime.GOOS != "windows" || strings.EqualFold(filepath.Ext(resolved), ".exe")) {
		return resolved, nil
	}
	if runtime.GOOS == "windows" {
		// npm on Windows installs shell shims beside the native executable.
		for _, directory := range filepath.SplitList(os.Getenv("PATH")) {
			candidate := filepath.Join(directory, "node_modules", "opencode-ai", "bin", "opencode.exe")
			if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
				return candidate, nil
			}
		}
	}
	return "", fmt.Errorf("opencode-cli: OpenCode is not installed; install the official CLI with npm install -g opencode-ai")
}

func runCLI(ctx context.Context, options Options, modelID, prompt string) (cliResult, error) {
	path, err := executable(options.Executable)
	if err != nil {
		return cliResult{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	versionCmd := exec.CommandContext(ctx, path, "--version")
	hideWindow(versionCmd)
	versionOutput, err := versionCmd.Output()
	if err != nil {
		return cliResult{}, fmt.Errorf("opencode-cli: check CLI version: %w", err)
	}
	version := "v" + strings.TrimPrefix(strings.TrimSpace(string(versionOutput)), "v")
	if !semver.IsValid(version) || semver.Compare(version, "v1.18.34") < 0 {
		return cliResult{}, fmt.Errorf("opencode-cli: OpenCode 1.18.34 or newer is required for the tested non-interactive permission behavior")
	}
	directory, err := os.MkdirTemp("", "atlas-opencode-")
	if err != nil {
		return cliResult{}, fmt.Errorf("opencode-cli: create isolated directory: %w", err)
	}
	defer os.RemoveAll(directory)
	config := bridgeConfig()
	configPath := filepath.Join(directory, "opencode.json")
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		return cliResult{}, fmt.Errorf("opencode-cli: write isolated config: %w", err)
	}
	args := []string{"run", "--pure", "--agent", "build", "--model", "opencode/" + modelID, "--format", "json", "--title", "Atlas inference"}
	if options.Variant != "" {
		args = append(args, "--variant", options.Variant)
	}
	cmd := exec.CommandContext(ctx, path, args...)
	hideWindow(cmd)
	cmd.Dir = directory
	cmd.Env = bridgeEnvironment(os.Environ(), directory, configPath, config)
	cmd.Stdin = strings.NewReader(prompt)
	stderr := &boundedWriter{limit: 4096}
	cmd.Stderr = stderr
	cmd.WaitDelay = 3 * time.Second
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return cliResult{}, fmt.Errorf("opencode-cli: open stdout: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return cliResult{}, fmt.Errorf("opencode-cli: launch OpenCode: %w", err)
	}
	result, parseErr := parseEvents(stdout)
	requestErr := ctx.Err()
	if parseErr != nil {
		cancel()
	}
	waitErr := cmd.Wait()
	if requestErr != nil {
		return cliResult{}, fmt.Errorf("opencode-cli: request interrupted: %w", requestErr)
	}
	if parseErr != nil {
		return cliResult{}, parseErr
	}
	if ctx.Err() != nil {
		return cliResult{}, fmt.Errorf("opencode-cli: request interrupted: %w", ctx.Err())
	}
	if waitErr != nil {
		return cliResult{}, fmt.Errorf("opencode-cli: process failed: %w (%s)", waitErr, strings.TrimSpace(stderr.buffer.String()))
	}
	return result, nil
}

func bridgeConfig() string {
	// The genuine build agent keeps its own tool catalog. In non-interactive
	// run mode, without --auto, all permission requests are auto-rejected.
	// Any native tool event still aborts the bridge before accepting a reply.
	data, _ := json.Marshal(map[string]any{
		"autoupdate": false, "share": "disabled", "snapshot": false,
		"permission": map[string]string{"*": "ask"},
	})
	return string(data)
}

func bridgeEnvironment(env []string, directory, configPath, config string) []string {
	overrides := map[string]string{
		"OPENCODE_CONFIG": configPath, "OPENCODE_CONFIG_CONTENT": config,
		"OPENCODE_CONFIG_DIR": directory, "XDG_CONFIG_HOME": directory,
		"OPENCODE_DISABLE_PROJECT_CONFIG": "true", "OPENCODE_DISABLE_CLAUDE_CODE": "true",
		"OPENCODE_DISABLE_AUTOUPDATE": "true", "OPENCODE_DISABLE_AUTO_SHARE": "true",
		"OPENCODE_PERMISSION": `{"*":"ask"}`,
	}
	result := make([]string, 0, len(env)+len(overrides))
	for _, entry := range env {
		key, _, _ := strings.Cut(entry, "=")
		if _, override := overrides[strings.ToUpper(key)]; !override {
			result = append(result, entry)
		}
	}
	for key, value := range overrides {
		result = append(result, key+"="+value)
	}
	return result
}

func parseEvents(reader io.Reader) (cliResult, error) {
	limited := &io.LimitedReader{R: reader, N: 32 * 1024 * 1024}
	scanner := bufio.NewScanner(limited)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	var result cliResult
	var text strings.Builder
	finished := false
	for scanner.Scan() {
		if len(bytes.TrimSpace(scanner.Bytes())) == 0 {
			continue
		}
		var event struct {
			Type  string          `json:"type"`
			Error json.RawMessage `json:"error"`
			Part  struct {
				Text   string `json:"text"`
				Reason string `json:"reason"`
				Tokens struct {
					Input     int64 `json:"input"`
					Output    int64 `json:"output"`
					Total     int64 `json:"total"`
					Reasoning int64 `json:"reasoning"`
					Cache     struct {
						Read  int64 `json:"read"`
						Write int64 `json:"write"`
					} `json:"cache"`
				} `json:"tokens"`
			} `json:"part"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			return cliResult{}, fmt.Errorf("opencode-cli: invalid event JSON: %w", err)
		}
		switch event.Type {
		case "text":
			text.WriteString(event.Part.Text)
		case "tool_use":
			return cliResult{}, fmt.Errorf("opencode-cli: unexpected native OpenCode tool execution; request aborted")
		case "error":
			return cliResult{}, fmt.Errorf("opencode-cli: OpenCode returned an error: %s", event.Error)
		case "step_finish":
			if event.Part.Reason != "stop" {
				return cliResult{}, fmt.Errorf("opencode-cli: incomplete model response (%s)", event.Part.Reason)
			}
			finished = true
			t := event.Part.Tokens
			result.usage = fantasy.Usage{InputTokens: t.Input, OutputTokens: t.Output, TotalTokens: t.Total, ReasoningTokens: t.Reasoning, CacheReadTokens: t.Cache.Read, CacheCreationTokens: t.Cache.Write}
		}
	}
	if err := scanner.Err(); err != nil {
		return cliResult{}, fmt.Errorf("opencode-cli: read events: %w", err)
	}
	if limited.N == 0 || !finished || text.Len() == 0 {
		return cliResult{}, fmt.Errorf("opencode-cli: oversized, empty or interrupted event stream; no tools executed")
	}
	result.text = text.String()
	return result, nil
}

type boundedWriter struct {
	buffer bytes.Buffer
	limit  int
}

func (w *boundedWriter) Write(data []byte) (int, error) {
	n := len(data)
	if remaining := w.limit - w.buffer.Len(); remaining > 0 {
		_, _ = w.buffer.Write(data[:min(remaining, len(data))])
	}
	return n, nil
}
