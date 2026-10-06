package tools

import (
	"bytes"
	"cmp"
	"context"
	_ "embed"
	"fmt"
	"html/template"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ansi"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/execution"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/fsext"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/permission"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/shell"
)

type BashParams struct {
	Description         string   `json:"description" description:"A brief description of what the command does, try to keep it under 30 characters or so"`
	Command             string   `json:"command,omitempty" description:"Shell source to execute; mutually exclusive with argv"`
	Argv                []string `json:"argv,omitempty" description:"Literal executable and arguments, without shell expansion; mutually exclusive with command; foreground only"`
	WorkingDir          string   `json:"working_dir,omitempty" description:"The working directory to execute the command in (defaults to current directory)"`
	RunInBackground     bool     `json:"run_in_background,omitempty" description:"Set to true (boolean) to run this command in the background. Use job_output to read the output later."`
	AutoBackgroundAfter int      `json:"auto_background_after,omitempty" description:"Seconds to wait before automatically moving the command to a background job (default: 60)"`
}

type BashPermissionsParams struct {
	Description         string   `json:"description"`
	Command             string   `json:"command"`
	Argv                []string `json:"argv"`
	WorkingDir          string   `json:"working_dir"`
	RunInBackground     bool     `json:"run_in_background"`
	AutoBackgroundAfter int      `json:"auto_background_after"`
}

type BashResponseMetadata struct {
	Execution        *execution.Result `json:"execution,omitempty"`
	ExitCode         *int              `json:"exit_code,omitempty"`
	StartTime        int64             `json:"start_time"`
	EndTime          int64             `json:"end_time"`
	Output           string            `json:"output"`
	Description      string            `json:"description"`
	WorkingDirectory string            `json:"working_directory"`
	Background       bool              `json:"background,omitempty"`
	ShellID          string            `json:"shell_id,omitempty"`
	// Status separates "the command ran" from "the requested work is
	// done": succeeded, exited_nonzero, command_not_found, not_executable
	// or interrupted. Only the last three set is_error; an ordinary
	// non-zero exit keeps its caller-defined meaning (grep 1, diff 1).
	Status string `json:"status,omitempty"`
	// Executed is true when the process started and exited; the command
	// text is never rewritten or retried by the tool.
	Executed bool `json:"executed,omitempty"`
}

const (
	BashToolName = "bash"

	DefaultAutoBackgroundAfter = 60 // Commands taking longer automatically become background jobs
	MaxOutputLength            = 30000
	BashNoOutput               = "no output"
)

//go:embed bash.md.tpl
var bashDescriptionTmpl []byte

var bashDescriptionTpl = template.Must(
	template.New("bashDescription").
		Parse(string(bashDescriptionTmpl)),
)

type bashDescriptionData struct {
	BannedCommands  string
	MaxOutputLength int
	Attribution     config.Attribution
	ModelID         string
	RgAvailable     bool
	GhAvailable     bool
}

var bannedCommands = []string{
	// Network/Download tools
	"alias",
	"aria2c",
	"axel",
	"chrome",
	"curl",
	"curlie",
	"firefox",
	"http-prompt",
	"httpie",
	"links",
	"lynx",
	"nc",
	"safari",
	"scp",
	"ssh",
	"telnet",
	"w3m",
	"wget",
	"xh",

	// System administration
	"doas",
	"su",
	"sudo",

	// Package managers
	"apk",
	"apt",
	"apt-cache",
	"apt-get",
	"dnf",
	"dpkg",
	"emerge",
	"home-manager",
	"makepkg",
	"opkg",
	"pacman",
	"paru",
	"pkg",
	"pkg_add",
	"pkg_delete",
	"portage",
	"rpm",
	"yay",
	"yum",
	"zypper",

	// System modification
	"at",
	"batch",
	"chkconfig",
	"crontab",
	"fdisk",
	"mkfs",
	"mount",
	"parted",
	"service",
	"systemctl",
	"umount",

	// Network configuration
	"firewall-cmd",
	"ifconfig",
	"ip",
	"iptables",
	"netstat",
	"pfctl",
	"route",
	"ufw",
}

func bashDescription(attribution *config.Attribution, modelID string, policy CommandPolicy, limits BashLimits) string {
	bannedCommandsStr := strings.Join(policy.banned(), ", ")
	var out bytes.Buffer
	if err := bashDescriptionTpl.Execute(&out, bashDescriptionData{
		BannedCommands:  bannedCommandsStr,
		MaxOutputLength: cmp.Or(limits.MaxOutputLength, MaxOutputLength),
		Attribution:     *attribution,
		ModelID:         modelID,
		RgAvailable:     getRg() != "",
		GhAvailable:     ghAvailable,
	}); err != nil {
		// this should never happen.
		panic("failed to execute bash description template: " + err.Error())
	}
	return out.String()
}

func NewBashTool(permissions permission.Service, workingDir string, attribution *config.Attribution, modelID string, policy CommandPolicy, limits BashLimits) fantasy.AgentTool {
	blocks := policy.blockFuncs()
	run := func(ctx context.Context, params BashParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
		if (params.Command == "") == (len(params.Argv) == 0) {
			return fantasy.NewTextErrorResponse("supply exactly one of command or argv"), nil
		}
		if len(params.Argv) > 0 && params.RunInBackground {
			return fantasy.NewTextErrorResponse("literal argv checks require foreground execution"), nil
		}

		// Determine working directory
		execWorkingDir := cmp.Or(params.WorkingDir, workingDir)
		if len(params.Argv) > 0 && !filepath.IsAbs(execWorkingDir) {
			execWorkingDir = filepath.Join(workingDir, execWorkingDir)
		}

		isSafeReadOnly := false
		cmdLower := strings.ToLower(params.Command)

		if !containsCommandChaining(params.Command) {
			for _, safe := range safeCommands {
				if strings.HasPrefix(cmdLower, safe) {
					if len(cmdLower) == len(safe) || cmdLower[len(safe)] == ' ' || cmdLower[len(safe)] == '-' {
						isSafeReadOnly = true
						break
					}
				}
			}
		}

		sessionID := GetSessionFromContext(ctx)
		if sessionID == "" {
			return fantasy.ToolResponse{}, fmt.Errorf("session ID is required for executing shell command")
		}
		// Always request: the safe-command short-circuit now lives
		// inside the permission service (Safe: true), so ModePlan can
		// still deny a nominally-safe command instead of it silently
		// bypassing the check entirely.
		p, err := permissions.Request(
			ctx,
			permission.CreatePermissionRequest{
				SessionID:   sessionID,
				Path:        execWorkingDir,
				ToolCallID:  call.ID,
				ToolName:    BashToolName,
				Action:      "execute",
				Description: fmt.Sprintf("Execute command: %s", cmp.Or(params.Command, fmt.Sprint(params.Argv))),
				Params:      BashPermissionsParams(params),
				Safe:        isSafeReadOnly,
			},
		)
		if err != nil {
			return fantasy.ToolResponse{}, err
		}
		if !p {
			return NewPermissionDeniedResponse(permissions), nil
		}
		if execution.HasBinding(ctx) {
			return runIsolatedBash(ctx, params, call, execWorkingDir, blocks, limits)
		}
		if len(params.Argv) > 0 {
			start := time.Now()
			sh := shell.NewShell(&shell.Options{WorkingDir: execWorkingDir, BlockFuncs: blocks})
			stdout, stderr, err := sh.ExecArgv(ctx, params.Argv)
			if !shell.ObservedExit(err) {
				return fantasy.ToolResponse{}, err
			}
			exit := shell.ExitCode(err)
			output := formatOutput(stdout, stderr, err, limits.MaxOutputLength)
			metadata := BashResponseMetadata{ExitCode: &exit, StartTime: start.UnixMilli(), EndTime: time.Now().UnixMilli(), Output: output, Description: params.Description, WorkingDirectory: execWorkingDir}
			return fantasy.WithResponseMetadata(fantasy.NewTextResponse(cmp.Or(output, BashNoOutput)), metadata), nil
		}

		// If explicitly requested as background, start immediately with detached context
		if params.RunInBackground {
			startTime := time.Now()
			bgManager := shell.GetBackgroundShellManager()
			bgManager.Cleanup()
			// Use background context so it continues after tool returns
			bgShell, err := bgManager.Start(context.WithoutCancel(ctx), execWorkingDir, blocks, params.Command, params.Description)
			if err != nil {
				return fantasy.ToolResponse{}, fmt.Errorf("error starting background shell: %w", err)
			}

			// Wait a short time to detect fast failures (blocked commands, syntax errors, etc.)
			time.Sleep(1 * time.Second)
			stdout, stderr, done, execErr := bgShell.GetOutput()

			if done {
				// Command failed or completed very quickly
				bgManager.Remove(bgShell.ID)

				interrupted := shell.IsInterrupt(execErr)
				exitCode := shell.ExitCode(execErr)
				if exitCode == 0 && !interrupted && execErr != nil {
					return fantasy.ToolResponse{}, fmt.Errorf("[Job %s] error executing command: %w", bgShell.ID, execErr)
				}

				stdout = formatOutput(stdout, stderr, execErr, limits.MaxOutputLength)

				metadata := BashResponseMetadata{
					ExitCode:         &exitCode,
					StartTime:        startTime.UnixMilli(),
					EndTime:          time.Now().UnixMilli(),
					Output:           stdout,
					Description:      params.Description,
					Background:       params.RunInBackground,
					WorkingDirectory: bgShell.WorkingDir,
				}
				if stdout == "" {
					return fantasy.WithResponseMetadata(fantasy.NewTextResponse(BashNoOutput), metadata), nil
				}
				stdout += fmt.Sprintf("\n\n<cwd>%s</cwd>", normalizeWorkingDir(bgShell.WorkingDir))
				return fantasy.WithResponseMetadata(fantasy.NewTextResponse(stdout), metadata), nil
			}

			// Still running after fast-failure check - return as background job
			metadata := BashResponseMetadata{
				StartTime:        startTime.UnixMilli(),
				EndTime:          time.Now().UnixMilli(),
				Description:      params.Description,
				WorkingDirectory: bgShell.WorkingDir,
				Background:       true,
				ShellID:          bgShell.ID,
			}
			response := fmt.Sprintf("Background shell started with ID: %s\n\nUse job_output tool to view output or job_kill to terminate.", bgShell.ID)
			return fantasy.WithResponseMetadata(fantasy.NewTextResponse(response), metadata), nil
		}

		// Start synchronous execution with auto-background support
		startTime := time.Now()

		// Start with detached context so it can survive if moved to background
		bgManager := shell.GetBackgroundShellManager()
		bgManager.Cleanup()
		bgShell, err := bgManager.Start(context.WithoutCancel(ctx), execWorkingDir, blocks, params.Command, params.Description)
		if err != nil {
			return fantasy.ToolResponse{}, fmt.Errorf("error starting shell: %w", err)
		}

		// Wait for either completion, auto-background threshold, or context cancellation
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()

		autoBackgroundAfter := limits.autoBackgroundAfter(params.AutoBackgroundAfter)
		autoBackgroundThreshold := time.Duration(autoBackgroundAfter) * time.Second
		timeout := time.After(autoBackgroundThreshold)

		var stdout, stderr string
		var done bool
		var execErr error

	waitLoop:
		for {
			select {
			case <-ticker.C:
				stdout, stderr, done, execErr = bgShell.GetOutput()
				if done {
					break waitLoop
				}
			case <-timeout:
				stdout, stderr, done, execErr = bgShell.GetOutput()
				break waitLoop
			case <-ctx.Done():
				// Incoming context was cancelled before we moved to background
				// Kill the shell and return error
				bgManager.Kill(bgShell.ID)
				return fantasy.ToolResponse{}, ctx.Err()
			}
		}

		if done {
			// Command completed within threshold - return synchronously
			// Remove from background manager since we're returning directly
			// Don't call Kill() as it cancels the context and corrupts the exit code
			bgManager.Remove(bgShell.ID)

			interrupted := shell.IsInterrupt(execErr)
			exitCode := shell.ExitCode(execErr)
			if exitCode == 0 && !interrupted && execErr != nil {
				return fantasy.ToolResponse{}, fmt.Errorf("[Job %s] error executing command: %w", bgShell.ID, execErr)
			}

			stdout = formatOutput(stdout, stderr, execErr, limits.MaxOutputLength)

			metadata := BashResponseMetadata{
				ExitCode:         &exitCode,
				StartTime:        startTime.UnixMilli(),
				EndTime:          time.Now().UnixMilli(),
				Output:           stdout,
				Description:      params.Description,
				Background:       params.RunInBackground,
				WorkingDirectory: bgShell.WorkingDir,
			}
			if stdout == "" {
				return fantasy.WithResponseMetadata(fantasy.NewTextResponse(BashNoOutput), metadata), nil
			}
			stdout += fmt.Sprintf("\n\n<cwd>%s</cwd>", normalizeWorkingDir(bgShell.WorkingDir))
			return fantasy.WithResponseMetadata(fantasy.NewTextResponse(stdout), metadata), nil
		}

		// Still running - keep as background job
		metadata := BashResponseMetadata{
			StartTime:        startTime.UnixMilli(),
			EndTime:          time.Now().UnixMilli(),
			Description:      params.Description,
			WorkingDirectory: bgShell.WorkingDir,
			Background:       true,
			ShellID:          bgShell.ID,
		}
		response := fmt.Sprintf("Command is taking longer than expected and has been moved to background.\n\nBackground shell ID: %s\n\nUse job_output tool to view output or job_kill to terminate.", bgShell.ID)
		return fantasy.WithResponseMetadata(fantasy.NewTextResponse(response), metadata), nil
	}
	return fantasy.NewAgentTool(
		BashToolName,
		string(bashDescription(attribution, modelID, policy, limits)),
		func(ctx context.Context, params BashParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			resp, err := run(ctx, params, call)
			return annotateBashExit(resp, err)
		},
	)
}

// formatOutput formats the output of a completed command with error handling
func formatOutput(stdout, stderr string, execErr error, maxOutputLength int) string {
	interrupted := shell.IsInterrupt(execErr)
	exitCode := shell.ExitCode(execErr)

	stdout = TruncateOutputTo(stdout, maxOutputLength)
	stderr = TruncateOutputTo(stderr, maxOutputLength)

	errorMessage := stderr
	if errorMessage == "" && execErr != nil {
		errorMessage = execErr.Error()
	}

	if interrupted {
		if errorMessage != "" {
			errorMessage += "\n"
		}
		errorMessage += "Command was aborted before completion"
	} else if exitCode != 0 {
		if errorMessage != "" {
			errorMessage += "\n"
		}
		errorMessage += fmt.Sprintf("Exit code %d", exitCode)
	}

	hasBothOutputs := stdout != "" && stderr != ""

	if hasBothOutputs {
		stdout += "\n"
	}

	if errorMessage != "" {
		stdout += "\n" + errorMessage
	}

	return stdout
}

func TruncateOutput(content string) string {
	return TruncateOutputTo(content, MaxOutputLength)
}

// TruncateOutputTo keeps the head and tail of content, dropping the middle,
// so that what comes back is at most maxLength wide. A maxLength of zero or
// less means the built-in limit rather than "keep nothing".
func TruncateOutputTo(content string, maxLength int) string {
	if maxLength <= 0 {
		maxLength = MaxOutputLength
	}
	if ansi.StringWidth(content) <= maxLength {
		return content
	}

	halfLength := maxLength / 2
	start := ansi.Truncate(content, halfLength, "")
	end := ansi.TruncateLeft(content, ansi.StringWidth(content)-halfLength, "")

	truncatedLinesCount := max(strings.Count(content, "\n")-strings.Count(start, "\n")-strings.Count(end, "\n"), 0)
	return fmt.Sprintf("%s\n\n... [%d lines truncated] ...\n\n%s", start, truncatedLinesCount, end)
}

func normalizeWorkingDir(path string) string {
	if runtime.GOOS == "windows" {
		path = strings.ReplaceAll(path, fsext.WindowsWorkingDirDrive(), "")
	}
	return filepath.ToSlash(path)
}
