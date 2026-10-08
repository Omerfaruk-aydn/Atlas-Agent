package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/permission"
)

// resolvedFileCheckPath resolves existing ancestors too, so absent checks
// and symlinks cannot make an outside read look like a workspace read.
func resolvedFileCheckPath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	var suffix []string
	for {
		resolved, err := filepath.EvalSymlinks(abs)
		if err == nil {
			for i := len(suffix) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, suffix[i])
			}
			return resolved, nil
		}
		if !os.IsNotExist(err) || filepath.Dir(abs) == abs {
			return "", err
		}
		suffix = append(suffix, filepath.Base(abs))
		abs = filepath.Dir(abs)
	}
}

// checkFile is dispatched through the ordinary computer tool and its hooks.
// Unlike pointer actions, an outside file read is not implicitly Safe just
// because the user enabled computer-use.
func (s *computerToolState) checkFile(ctx context.Context, params ComputerParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
	if err := validateDesktopFileCheck(params.File); err != nil {
		return fantasy.NewTextErrorResponse("invalid_file_check: " + err.Error()), nil
	}
	if err := ctx.Err(); err != nil {
		return fantasy.ToolResponse{}, err
	}
	file := *params.File
	resolve := file.Path
	if file.Condition == "absent" {
		// Absence concerns the entry, not a symlink's target. Resolve only
		// its ancestors, then Lstat the final component without following it.
		resolve = filepath.Dir(file.Path)
	}
	path, err := resolvedFileCheckPath(resolve)
	if err != nil {
		return fantasy.NewTextErrorResponse("file_unreadable: cannot resolve file path: " + err.Error()), nil
	}
	if file.Condition == "absent" {
		path = filepath.Join(path, filepath.Base(file.Path))
	}
	root, err := resolvedFileCheckPath(s.workingDir)
	if err != nil {
		return fantasy.ToolResponse{}, fmt.Errorf("resolve file check working directory: %w", err)
	}
	rel, err := filepath.Rel(root, path)
	inside := err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
	granted, err := s.permissions.Request(ctx, permission.CreatePermissionRequest{
		SessionID: GetSessionFromContext(ctx), ToolCallID: call.ID,
		ToolName: ComputerToolName, Path: path, Action: "file_check",
		Description: "Read saved file for " + file.Condition + " verification: " + path,
		Params:      map[string]any{"action": "file_check", "file_path": path, "condition": file.Condition},
		Safe:        inside,
	})
	if err != nil {
		return withToolOutcome(fantasy.ToolResponse{}, computerOutcome("file_check", params, fantasy.ToolResponse{}, false)), err
	}
	if !granted {
		r := NewPermissionDeniedResponse(s.permissions)
		return withToolOutcome(r, computerOutcome("file_check", params, r, false)), nil
	}
	file.Path = path
	if s.actionTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, s.actionTimeout)
		defer cancel()
	}
	r := runDesktopFileCheck(ctx, file)
	var evidence map[string]any
	if json.Unmarshal([]byte(r.Content), &evidence) == nil {
		evidence["requested_path"] = params.File.Path
		data, _ := json.Marshal(evidence)
		r.Content = string(data)
	}
	return withToolOutcome(r, computerOutcome("file_check", params, r, true)), nil
}
