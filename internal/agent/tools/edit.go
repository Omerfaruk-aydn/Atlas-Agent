package tools

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/diff"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/filepathext"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/filetracker"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/fsext"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/history"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/lsp"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/permission"
)

type EditParams struct {
	ExpectedFileHash string `json:"expected_file_hash,omitempty" description:"Optional SHA-256 of the exact current file bytes. Checked before editing and again after approval; drift rejects the write."`
	FilePath         string `json:"file_path" description:"The absolute path to the file to modify"`
	OldString        string `json:"old_string,omitempty" description:"The exact text to replace. Omit this and use anchor_line + anchor_hash instead for a single-line change, when view is showing hash anchors."`
	NewString        string `json:"new_string" description:"The text to replace it with. May be empty to delete, or span multiple lines to expand one line into several."`
	ReplaceAll       bool   `json:"replace_all,omitempty" description:"Replace all occurrences of old_string (default false)"`

	AnchorLine int    `json:"anchor_line,omitempty" description:"1-indexed line number to replace, using the hash view showed next to it. An alternative to old_string for a single line: no need to reproduce its exact whitespace. Requires anchor_hash; cannot be combined with old_string."`
	AnchorHash string `json:"anchor_hash,omitempty" description:"The hash view displayed for anchor_line. Verified against the file's current content before the edit is applied; a mismatch means the file changed since it was last viewed, and the edit is rejected instead of landing on the wrong line."`
}

type EditPermissionsParams struct {
	FilePath   string `json:"file_path"`
	OldContent string `json:"old_content,omitempty"`
	NewContent string `json:"new_content,omitempty"`
}

type EditResponseMetadata struct {
	Additions  int    `json:"additions"`
	Removals   int    `json:"removals"`
	OldContent string `json:"old_content,omitempty"`
	NewContent string `json:"new_content,omitempty"`
}

const EditToolName = "edit"

//go:embed edit.md
var editDescription string

type editContext struct {
	ctx         context.Context
	permissions permission.Service
	files       history.Service
	filetracker filetracker.Service
	workingDir  string
}

type expectedEditHashKey struct{}

func NewEditTool(
	lspManager *lsp.Manager,
	permissions permission.Service,
	files history.Service,
	filetracker filetracker.Service,
	workingDir string,
	pathPolicy PathPolicy,
) fantasy.AgentTool {
	return fantasy.NewAgentTool(
		EditToolName,
		editDescription,
		func(ctx context.Context, params EditParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			if params.FilePath == "" {
				return fantasy.NewTextErrorResponse("file_path is required"), nil
			}

			params.FilePath = filepathext.SmartJoin(workingDir, params.FilePath)

			if err := pathPolicy.Check(params.FilePath); err != nil {
				return fantasy.NewTextErrorResponse(err.Error()), nil
			}

			var response fantasy.ToolResponse
			var err error

			editCtx := editContext{ctx, permissions, files, filetracker, workingDir}
			if params.ExpectedFileHash != "" {
				if params.OldString == "" || len(params.ExpectedFileHash) != 64 {
					return fantasy.NewTextErrorResponse("expected_file_hash requires a 64-character SHA-256 and old_string"), nil
				}
				editCtx.ctx = context.WithValue(ctx, expectedEditHashKey{}, params.ExpectedFileHash)
			}

			switch {
			case params.AnchorLine > 0:
				if params.OldString != "" {
					return fantasy.NewTextErrorResponse("use either old_string or anchor_line, not both"), nil
				}
				if params.AnchorHash == "" {
					return fantasy.NewTextErrorResponse("anchor_hash is required together with anchor_line"), nil
				}
				response, err = replaceByAnchor(editCtx, params.FilePath, params.AnchorLine, params.AnchorHash, params.NewString, call)
			case params.OldString == "":
				response, err = createNewFile(editCtx, params.FilePath, params.NewString, call)
			case params.NewString == "":
				response, err = deleteContent(editCtx, params.FilePath, params.OldString, params.ReplaceAll, call)
			default:
				response, err = replaceContent(editCtx, params.FilePath, params.OldString, params.NewString, params.ReplaceAll, call)
			}

			if err != nil {
				return response, err
			}
			if response.IsError {
				// Return early if there was an error during content replacement
				// This prevents unnecessary LSP diagnostics processing
				return response, nil
			}

			notifyLSPs(ctx, lspManager, params.FilePath)

			text := fmt.Sprintf("<result>\n%s\n</result>\n", response.Content)
			text += getDiagnostics(params.FilePath, lspManager)
			response.Content = text
			return response, nil
		},
	)
}

func createNewFile(edit editContext, filePath, content string, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
	fileInfo, err := os.Stat(filePath)
	if err == nil {
		if fileInfo.IsDir() {
			return fantasy.NewTextErrorResponse(fmt.Sprintf("path is a directory, not a file: %s", filePath)), nil
		}
		return fantasy.NewTextErrorResponse(fmt.Sprintf("file already exists: %s", filePath)), nil
	} else if !os.IsNotExist(err) {
		return fantasy.ToolResponse{}, fmt.Errorf("failed to access file: %w", err)
	}

	dir := filepath.Dir(filePath)
	if err = os.MkdirAll(dir, 0o755); err != nil {
		return fantasy.ToolResponse{}, fmt.Errorf("failed to create parent directories: %w", err)
	}

	sessionID := GetSessionFromContext(edit.ctx)
	if sessionID == "" {
		return fantasy.ToolResponse{}, fmt.Errorf("session ID is required for creating a new file")
	}

	_, additions, removals := diff.GenerateDiff(
		"",
		content,
		strings.TrimPrefix(filePath, edit.workingDir),
	)
	p, err := edit.permissions.Request(
		edit.ctx,
		permission.CreatePermissionRequest{
			SessionID:   sessionID,
			Path:        fsext.PathOrPrefix(filePath, edit.workingDir),
			ToolCallID:  call.ID,
			ToolName:    EditToolName,
			Action:      "write",
			Description: fmt.Sprintf("Create file %s", filePath),
			Params: EditPermissionsParams{
				FilePath:   filePath,
				OldContent: "",
				NewContent: content,
			},
		},
	)
	if err != nil {
		return fantasy.ToolResponse{}, err
	}
	if !p {
		resp := NewPermissionDeniedResponse(edit.permissions)
		resp = fantasy.WithResponseMetadata(resp, EditResponseMetadata{
			OldContent: "",
			NewContent: content,
			Additions:  additions,
			Removals:   removals,
		})
		return resp, nil
	}

	err = os.WriteFile(filePath, []byte(content), 0o644)
	if err != nil {
		return fantasy.ToolResponse{}, fmt.Errorf("failed to write file: %w", err)
	}

	// File can't be in the history so we create a new file history. This is
	// a brand-new file (createNewFile), not a pre-chat baseline, so tag it
	// with the current message rather than leaving it message-less.
	_, err = edit.files.Create(edit.ctx, sessionID, filePath, "", GetMessageFromContext(edit.ctx))
	if err != nil {
		// Log error but don't fail the operation
		return fantasy.ToolResponse{}, fmt.Errorf("error creating file history: %w", err)
	}

	// Add the new content to the file history
	_, err = edit.files.CreateVersion(edit.ctx, sessionID, filePath, content, GetMessageFromContext(edit.ctx))
	if err != nil {
		// Log error but don't fail the operation
		slog.Error("Error creating file history version", "error", err)
	}

	edit.filetracker.RecordRead(edit.ctx, sessionID, filePath)

	return fantasy.WithResponseMetadata(
		fantasy.NewTextResponse("File created: "+filePath),
		EditResponseMetadata{
			OldContent: "",
			NewContent: content,
			Additions:  additions,
			Removals:   removals,
		},
	), nil
}

// findAndReplace performs a find-and-replace on content. When replaceAll is
// false it requires exactly one match. If an exact match fails, it falls back
// to whitespace-normalized matching and, failing that, returns a diagnostic
// hint describing why the replacement could not be made. The returned boolean
// reports whether the replacement relied on the whitespace-normalized
// fallback rather than an exact match.
func findAndReplace(content, old, new string, replaceAll bool) (string, bool, error) {
	if replaceAll {
		if strings.Contains(content, old) {
			return strings.ReplaceAll(content, old, new), false, nil
		}
	} else {
		index := strings.Index(content, old)
		switch {
		case index == -1:
			// Fall through to the fuzzy fallback below.
		case index != strings.LastIndex(content, old):
			return "", false, fmt.Errorf("old_string appears multiple times in the file. Please provide more context to ensure a unique match, or set replace_all to true")
		default:
			return content[:index] + new + content[index+len(old):], false, nil
		}
	}

	if result, ok := normalizedReplace(content, old, new, replaceAll); ok {
		return result, true, nil
	}
	return "", false, notFoundError(content, old)
}

// withWhitespaceNote appends the whitespace auto-correction note to a tool
// response message when the edit did not match the file byte-for-byte.
func withWhitespaceNote(message string, whitespaceCorrected bool) string {
	if !whitespaceCorrected {
		return message
	}
	return message + "\n" + whitespaceCorrectedNote
}

// notFoundError builds the "old_string not found" error, appending a
// diagnostic hint when one is available to help the caller self-correct.
func notFoundError(content, old string) error {
	msg := "old_string not found in file. Make sure it matches exactly, including whitespace and line breaks"
	if hint := diagnoseMismatch(content, old); hint != "" {
		msg += "\n\n" + hint
	}
	return errors.New(msg)
}

// commitFileChange writes newContent to filePath, updates the file history,
// and records the read in the file tracker. Callers must convert line endings
// before calling this function.
func commitFileChange(edit editContext, sessionID, filePath, oldContent, newContent string) error {
	if expected, _ := edit.ctx.Value(expectedEditHashKey{}).(string); expected != "" {
		current, err := os.ReadFile(filePath)
		if err != nil || engineering.Hash(string(current)) != expected {
			return fmt.Errorf("file changed before commit; no write sent")
		}
	}
	if err := os.WriteFile(filePath, []byte(newContent), 0o644); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	file, err := edit.files.GetByPathAndSession(edit.ctx, filePath, sessionID)
	if err != nil {
		// Genuine pre-chat baseline: oldContent is what was actually on
		// disk before this edit, so leave it message-less.
		_, err = edit.files.Create(edit.ctx, sessionID, filePath, oldContent, "")
		if err != nil {
			return fmt.Errorf("error creating file history: %w", err)
		}
	}
	if file.Content != oldContent {
		// User manually changed the content outside of chat; store an
		// intermediate version with no message association, since it
		// reflects drift that predates this tool call rather than content
		// this message produced.
		if _, err := edit.files.CreateVersion(edit.ctx, sessionID, filePath, oldContent, ""); err != nil {
			slog.Error("Error creating file history version", "error", err)
		}
	}
	if _, err := edit.files.CreateVersion(edit.ctx, sessionID, filePath, newContent, GetMessageFromContext(edit.ctx)); err != nil {
		slog.Error("Error creating file history version", "error", err)
	}

	edit.filetracker.RecordRead(edit.ctx, sessionID, filePath)
	return nil
}

func loadExistingFile(edit editContext, filePath, sessionError string) (sessionID, oldContent string, isCrlf bool, resp fantasy.ToolResponse, err error) {
	fileInfo, err := os.Stat(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", "", false, fantasy.NewTextErrorResponse(fmt.Sprintf("file not found: %s", filePath)), nil
		}
		return "", "", false, fantasy.ToolResponse{}, fmt.Errorf("failed to access file: %w", err)
	}

	if fileInfo.IsDir() {
		return "", "", false, fantasy.NewTextErrorResponse(fmt.Sprintf("path is a directory, not a file: %s", filePath)), nil
	}

	sessionID = GetSessionFromContext(edit.ctx)
	if sessionID == "" {
		return "", "", false, fantasy.ToolResponse{}, fmt.Errorf("%s", sessionError)
	}

	lastRead := edit.filetracker.LastReadTime(edit.ctx, sessionID, filePath)
	if lastRead.IsZero() {
		return "", "", false, fantasy.NewTextErrorResponse("you must read the file before editing it. Use the View tool first"), nil
	}

	modTime := fileInfo.ModTime().Truncate(time.Second)
	if modTime.After(lastRead) {
		return "", "", false, fantasy.NewTextErrorResponse(
			fmt.Sprintf(
				"file %s has been modified since it was last read (mod time: %s, last read: %s)",
				filePath, modTime.Format(time.RFC3339), lastRead.Format(time.RFC3339),
			),
		), nil
	}

	content, err := os.ReadFile(filePath)
	if err != nil {
		return "", "", false, fantasy.ToolResponse{}, fmt.Errorf("failed to read file: %w", err)
	}
	if expected, _ := edit.ctx.Value(expectedEditHashKey{}).(string); expected != "" && engineering.Hash(string(content)) != expected {
		return "", "", false, fantasy.NewTextErrorResponse("file hash changed; no write sent"), nil
	}

	oldContent, isCrlf = fsext.ToUnixLineEndings(string(content))
	return sessionID, oldContent, isCrlf, fantasy.ToolResponse{}, nil
}

func deleteContent(edit editContext, filePath, oldString string, replaceAll bool, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
	sessionID, oldContent, isCrlf, resp, err := loadExistingFile(edit, filePath, "session ID is required for deleting content")
	if err != nil {
		return fantasy.ToolResponse{}, err
	}
	if resp.Content != "" || resp.IsError {
		return resp, nil
	}

	newContent, whitespaceCorrected, err := findAndReplace(oldContent, oldString, "", replaceAll)
	if err != nil {
		return fantasy.NewTextErrorResponse(err.Error()), nil
	}

	_, additions, removals := diff.GenerateDiff(
		oldContent,
		newContent,
		strings.TrimPrefix(filePath, edit.workingDir),
	)

	p, err := edit.permissions.Request(
		edit.ctx,
		permission.CreatePermissionRequest{
			SessionID:   sessionID,
			Path:        fsext.PathOrPrefix(filePath, edit.workingDir),
			ToolCallID:  call.ID,
			ToolName:    EditToolName,
			Action:      "write",
			Description: fmt.Sprintf("Delete content from file %s", filePath),
			Params: EditPermissionsParams{
				FilePath:   filePath,
				OldContent: oldContent,
				NewContent: newContent,
			},
		},
	)
	if err != nil {
		return fantasy.ToolResponse{}, err
	}
	if !p {
		resp := NewPermissionDeniedResponse(edit.permissions)
		resp = fantasy.WithResponseMetadata(resp, EditResponseMetadata{
			OldContent: oldContent,
			NewContent: newContent,
			Additions:  additions,
			Removals:   removals,
		})
		return resp, nil
	}

	writeContent := newContent
	if isCrlf {
		writeContent, _ = fsext.ToWindowsLineEndings(writeContent)
	}

	if err := commitFileChange(edit, sessionID, filePath, oldContent, writeContent); err != nil {
		return fantasy.ToolResponse{}, err
	}

	return fantasy.WithResponseMetadata(
		fantasy.NewTextResponse(withWhitespaceNote("Content deleted from file: "+filePath, whitespaceCorrected)),
		EditResponseMetadata{
			OldContent: oldContent,
			NewContent: writeContent,
			Additions:  additions,
			Removals:   removals,
		},
	), nil
}

func replaceContent(edit editContext, filePath, oldString, newString string, replaceAll bool, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
	sessionID, oldContent, isCrlf, resp, err := loadExistingFile(edit, filePath, "session ID is required for editing a file")
	if err != nil {
		return fantasy.ToolResponse{}, err
	}
	if resp.Content != "" || resp.IsError {
		return resp, nil
	}

	result, whitespaceCorrected, err := findAndReplace(oldContent, oldString, newString, replaceAll)
	if err != nil {
		return fantasy.NewTextErrorResponse(err.Error()), nil
	}
	if result == oldContent {
		return fantasy.NewTextErrorResponse("new content is the same as old content. No changes made."), nil
	}

	_, additions, removals := diff.GenerateDiff(
		oldContent,
		result,
		strings.TrimPrefix(filePath, edit.workingDir),
	)

	p, err := edit.permissions.Request(
		edit.ctx,
		permission.CreatePermissionRequest{
			SessionID:   sessionID,
			Path:        fsext.PathOrPrefix(filePath, edit.workingDir),
			ToolCallID:  call.ID,
			ToolName:    EditToolName,
			Action:      "write",
			Description: fmt.Sprintf("Replace content in file %s", filePath),
			Params: EditPermissionsParams{
				FilePath:   filePath,
				OldContent: oldContent,
				NewContent: result,
			},
		},
	)
	if err != nil {
		return fantasy.ToolResponse{}, err
	}
	if !p {
		resp := NewPermissionDeniedResponse(edit.permissions)
		resp = fantasy.WithResponseMetadata(resp, EditResponseMetadata{
			OldContent: oldContent,
			NewContent: result,
			Additions:  additions,
			Removals:   removals,
		})
		return resp, nil
	}

	writeContent := result
	if isCrlf {
		writeContent, _ = fsext.ToWindowsLineEndings(writeContent)
	}

	if err := commitFileChange(edit, sessionID, filePath, oldContent, writeContent); err != nil {
		return fantasy.ToolResponse{}, err
	}

	return fantasy.WithResponseMetadata(
		fantasy.NewTextResponse(withWhitespaceNote("Content replaced in file: "+filePath, whitespaceCorrected)),
		EditResponseMetadata{
			OldContent: oldContent,
			NewContent: writeContent,
			Additions:  additions,
			Removals:   removals,
		},
	), nil
}

// replaceByAnchor replaces a single line, addressed by its 1-indexed line
// number and verified against the content hash view showed for it, instead
// of by reproducing its exact text. It is the anchor-mode counterpart to
// replaceContent: same diff/permission/write/history path, different way
// of locating what to change. newString may be empty (delete the line) or
// span multiple lines (expand it into several); either way the anchored
// line itself is always fully replaced, never partially matched within it.
func replaceByAnchor(edit editContext, filePath string, anchorLine int, anchorHash, newString string, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
	sessionID, oldContent, isCrlf, resp, err := loadExistingFile(edit, filePath, "session ID is required for editing a file")
	if err != nil {
		return fantasy.ToolResponse{}, err
	}
	if resp.Content != "" || resp.IsError {
		return resp, nil
	}

	lines := strings.Split(oldContent, "\n")
	if anchorLine < 1 || anchorLine > len(lines) {
		return fantasy.NewTextErrorResponse(fmt.Sprintf(
			"anchor_line %d is out of range: the file has %d lines. Use View to re-read it and get a current anchor.",
			anchorLine, len(lines),
		)), nil
	}

	target := lines[anchorLine-1]
	gotHash := lineAnchorHash(target)
	if gotHash != anchorHash {
		return fantasy.NewTextErrorResponse(fmt.Sprintf(
			"anchor_hash does not match line %d (expected %s, got %s). The file has changed since it was last viewed -- use View to re-read it and get the current anchor.",
			anchorLine, anchorHash, gotHash,
		)), nil
	}

	newLines := make([]string, 0, len(lines)+1)
	newLines = append(newLines, lines[:anchorLine-1]...)
	if newString != "" {
		newLines = append(newLines, strings.Split(newString, "\n")...)
	}
	newLines = append(newLines, lines[anchorLine:]...)
	result := strings.Join(newLines, "\n")

	if result == oldContent {
		return fantasy.NewTextErrorResponse("new content is the same as old content. No changes made."), nil
	}

	_, additions, removals := diff.GenerateDiff(
		oldContent,
		result,
		strings.TrimPrefix(filePath, edit.workingDir),
	)

	p, err := edit.permissions.Request(
		edit.ctx,
		permission.CreatePermissionRequest{
			SessionID:   sessionID,
			Path:        fsext.PathOrPrefix(filePath, edit.workingDir),
			ToolCallID:  call.ID,
			ToolName:    EditToolName,
			Action:      "write",
			Description: fmt.Sprintf("Replace line %d in file %s", anchorLine, filePath),
			Params: EditPermissionsParams{
				FilePath:   filePath,
				OldContent: oldContent,
				NewContent: result,
			},
		},
	)
	if err != nil {
		return fantasy.ToolResponse{}, err
	}
	if !p {
		resp := NewPermissionDeniedResponse(edit.permissions)
		resp = fantasy.WithResponseMetadata(resp, EditResponseMetadata{
			OldContent: oldContent,
			NewContent: result,
			Additions:  additions,
			Removals:   removals,
		})
		return resp, nil
	}

	writeContent := result
	if isCrlf {
		writeContent, _ = fsext.ToWindowsLineEndings(writeContent)
	}

	if err := commitFileChange(edit, sessionID, filePath, oldContent, writeContent); err != nil {
		return fantasy.ToolResponse{}, err
	}

	return fantasy.WithResponseMetadata(
		fantasy.NewTextResponse(fmt.Sprintf("Content replaced in file: %s", filePath)),
		EditResponseMetadata{
			OldContent: oldContent,
			NewContent: writeContent,
			Additions:  additions,
			Removals:   removals,
		},
	), nil
}
