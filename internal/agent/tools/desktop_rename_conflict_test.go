package tools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/stretchr/testify/require"
)

// conflictFolder holds the real files behind the fixture's Explorer view:
// the source hesap.txt and an existing sonuc.txt.
func conflictFolder(t *testing.T, source, existing string, extra ...string) string {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "hesap.txt"), []byte(source), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "sonuc.txt"), []byte(existing), 0o644))
	for _, name := range extra {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte("other"), 0o644))
	}
	return dir
}

// conflictReplies makes "sonuc" visible in the view, resolves every display
// name to its hidden-extension file in dir, and makes the inline editor
// show renamed when keep_both picks another name.
func conflictReplies(dir, renamed string) func(ComputerParams, fantasy.ToolResponse) fantasy.ToolResponse {
	return func(p ComputerParams, r fantasy.ToolResponse) fantasy.ToolResponse {
		switch {
		case p.Action == "find" && p.Automation.Name == "sonuc":
			return fantasy.NewTextResponse(`{"result":{"matches":[{"element_id":"existing","name":"sonuc","role":"ControlType.ListItem","process_id":7,"enabled":true}],"truncated":false}}`)
		case p.Action == "assert" && p.Automation.Condition == "file_name":
			full := p.Automation.Name + ".txt"
			data, _ := json.Marshal(map[string]any{"window_id": "11", "passed": p.Automation.Expected == "" || full == p.Automation.Expected, "actual": full, "folder_path": dir, "condition": "file_name", "source": "shell_view_item"})
			return fantasy.NewTextResponse(string(data))
		case p.Action == "inspect" && renamed != "":
			r.Content = strings.ReplaceAll(r.Content, `"name":"sonuc"`, `"name":"`+renamed+`"`)
		}
		return r
	}
}

func renameMutations(calls []ComputerParams) []ComputerParams {
	var out []ComputerParams
	for _, c := range calls {
		if desktopMutation(c.Action) {
			out = append(out, c)
		}
	}
	return out
}

func requireFile(t *testing.T, path, content string) {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, content, string(data))
}

// Session 6c6446a0 ended with the rename undone because sonuc.txt already
// existed and no outcome was decided. The default policy now refuses before
// any input with both files' identity and digest and names the choices.
func TestRenameConflictDefaultFailsWithEvidence(t *testing.T) {
	t.Parallel()
	for _, identical := range []bool{false, true} {
		dir := conflictFolder(t, "9000", map[bool]string{false: "8638", true: "9000"}[identical])
		invoke, calls := renameFixture(t, conflictReplies(dir, ""))
		p := renameParams()
		p.Rename.ExpectedFullName = "sonuc.txt"
		r, err := runDesktopWorkflow(t.Context(), p, fantasy.ToolCall{ID: "rename"}, invoke)
		require.NoError(t, err)
		contract := contractOf(t, r)
		require.Equal(t, "name_conflict", contract.Code)
		require.Equal(t, "desktop.rename.new_name", contract.Field)
		require.Contains(t, contract.Message, `"sha256"`)
		require.Contains(t, contract.Message, `"full_name":"sonuc.txt"`)
		require.Contains(t, contract.NextStep, "keep_both")
		require.Contains(t, contract.NextStep, "do not delete or overwrite")
		if identical {
			require.Contains(t, contract.Message, `"identical":true`)
			require.Contains(t, contract.Message, "use_existing_if_identical")
		} else {
			require.Contains(t, contract.Message, `"identical":false`)
		}
		require.Empty(t, renameMutations(*calls), "no input before a decided outcome")
		requireFile(t, filepath.Join(dir, "hesap.txt"), "9000")
	}
}

// use_existing_if_identical succeeds without input only for byte-identical
// files and keeps both untouched; differing files stay a conflict.
func TestRenameConflictUseExistingOnlyWhenIdentical(t *testing.T) {
	t.Parallel()
	for _, identical := range []bool{true, false} {
		existing := "9000"
		if !identical {
			existing = "9000\r\n"
		}
		dir := conflictFolder(t, "9000", existing)
		invoke, calls := renameFixture(t, conflictReplies(dir, ""))
		p := renameParams()
		p.Rename.OnConflict = "use_existing_if_identical"
		r, err := runDesktopWorkflow(t.Context(), p, fantasy.ToolCall{ID: "rename"}, invoke)
		require.NoError(t, err)
		require.Empty(t, renameMutations(*calls))
		if !identical {
			require.Equal(t, "name_conflict", contractOf(t, r).Code, "a line-ending difference is a difference")
			continue
		}
		require.False(t, r.IsError, r.Content)
		require.Contains(t, r.Content, `"outcome":"existing_identical"`)
		require.Contains(t, r.Content, `"source_kept":true`)
		require.Contains(t, r.Content, `"input_sent":false`)
		requireFile(t, filepath.Join(dir, "hesap.txt"), "9000")
		requireFile(t, filepath.Join(dir, "sonuc.txt"), "9000")
	}
}

// keep_both renames to the first name free in the view and on disk, checks
// the hidden extension of that name and never touches the existing file.
func TestRenameConflictKeepBothPicksFreeName(t *testing.T) {
	t.Parallel()
	// "sonuc (2).txt" exists on disk but is filtered out of the view.
	dir := conflictFolder(t, "9000", "8638", "sonuc (2).txt")
	invoke, calls := renameFixture(t, conflictReplies(dir, "sonuc (3)"))
	p := renameParams()
	p.Rename.ExpectedFullName = "sonuc.txt"
	p.Rename.OnConflict = "keep_both"
	r, err := runDesktopWorkflow(t.Context(), p, fantasy.ToolCall{ID: "rename"}, invoke)
	require.NoError(t, err)
	require.False(t, r.IsError, r.Content)
	var typed []string
	for _, c := range renameMutations(*calls) {
		if c.Action == "type" {
			typed = append(typed, c.Text)
		}
	}
	require.Equal(t, []string{"sonuc (3)"}, typed)
	require.Contains(t, r.Content, `"actual_name":"sonuc (3)"`)
	require.Contains(t, r.Content, `"requested_name":"sonuc"`)
	require.Contains(t, r.Content, `"existing_item_untouched":true`)
	requirements := renameRequirements(t, r.Content)
	require.Equal(t, true, requirements["full_name"]["verified"])
	require.Equal(t, "sonuc (3).txt", requirements["full_name"]["actual"])
	requireFile(t, filepath.Join(dir, "sonuc.txt"), "8638")
}

// Replacing is not a rename policy: it is refused before any child call.
func TestRenameConflictRefusesReplacePolicies(t *testing.T) {
	t.Parallel()
	for _, policy := range []string{"replace", "overwrite", "replace_if_identical", "skip"} {
		invoke, calls := renameFixture(t, nil)
		p := renameParams()
		p.Rename.OnConflict = policy
		r, err := runDesktopWorkflow(t.Context(), p, fantasy.ToolCall{ID: "rename"}, invoke)
		require.NoError(t, err)
		contract := contractOf(t, r)
		require.Equal(t, "desktop.rename.on_conflict", contract.Field, policy)
		require.Empty(t, *calls, policy)
	}
}

func TestRenameKeepBothName(t *testing.T) {
	t.Parallel()
	require.Equal(t, "sonuc (2)", renameKeepBothName("sonuc", 2, false))
	require.Equal(t, "sonuc (2).txt", renameKeepBothName("sonuc.txt", 2, true))
	require.Equal(t, "rapor.v1 (3).txt", renameKeepBothName("rapor.v1.txt", 3, true))
	require.Equal(t, ".env (2)", renameKeepBothName(".env", 2, true))
}
