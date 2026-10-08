package tools

import (
	"encoding/json"
	"strings"
	"testing"

	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/stretchr/testify/require"
)

// fileNameReply makes the rename fixture's shell view report a file-system
// name for the renamed item, the way Explorer resolves a hidden extension.
func fileNameReply(actual, folder string) func(ComputerParams, fantasy.ToolResponse) fantasy.ToolResponse {
	return func(p ComputerParams, r fantasy.ToolResponse) fantasy.ToolResponse {
		if p.Action != "assert" || p.Automation.Condition != "file_name" {
			return r
		}
		if actual == "" {
			return fantasy.NewTextErrorResponse("File name unavailable: the item is not one uniquely resolved file-system item of this Explorer folder view; no name is claimed")
		}
		data, _ := json.Marshal(map[string]any{"window_id": "11", "passed": actual == p.Automation.Expected, "actual": actual, "folder_path": folder, "condition": "file_name", "source": "shell_view_item"})
		return fantasy.NewTextResponse(string(data))
	}
}

func renameRequirements(t *testing.T, content string) map[string]map[string]any {
	t.Helper()
	start := strings.Index(content, `{"workflow"`)
	require.GreaterOrEqual(t, start, 0, content)
	var result struct {
		Workflow struct {
			Requirements map[string]map[string]any `json:"requirements"`
		} `json:"workflow"`
	}
	require.NoError(t, json.Unmarshal([]byte(content[start:]), &result), content)
	return result.Workflow.Requirements
}

// Session 69361c0a: Explorer hid ".txt"; the model renamed "hesap" to
// "sonuc" and reported that it had not verified "sonuc.txt" directly. The
// recipe now reads the real file-system name from the window's shell view
// and tracks folder, full name and content as separate requirements.
func TestRenameVerifiesHiddenExtensionFromShellView(t *testing.T) {
	t.Parallel()
	folder := `C:\Users\Test\OneDrive\Masaüstü\Atlas-Test`
	invoke, calls := renameFixture(t, fileNameReply("sonuc.txt", folder))
	p := renameParams()
	p.Rename.ExpectedFullName = "sonuc.txt"
	r, err := runDesktopWorkflow(t.Context(), p, fantasy.ToolCall{ID: "rename"}, invoke)
	require.NoError(t, err)
	require.False(t, r.IsError, r.Content)
	requirements := renameRequirements(t, r.Content)
	require.Equal(t, true, requirements["full_name"]["verified"])
	require.Equal(t, "sonuc.txt", requirements["full_name"]["actual"])
	require.Equal(t, "sonuc", requirements["full_name"]["display_name"])
	require.Equal(t, true, requirements["folder"]["verified"])
	require.Equal(t, folder, requirements["folder"]["path"])
	require.Equal(t, false, requirements["content"]["verified"], "a rename never proves content")
	last := (*calls)[len(*calls)-1]
	require.Equal(t, "file_name", last.Automation.Condition)
	require.Equal(t, "sonuc", last.Automation.Name, "the display label selects the item; it is not the claimed name")
}

// A display label, a "Metin Belgesi" type column or a *.txt save filter is
// not proof of the extension. When the shell view cannot resolve the item,
// the requested full name is reported unverified and no input is repeated.
func TestRenameDoesNotClaimFullNameFromDisplayLabel(t *testing.T) {
	t.Parallel()
	invoke, calls := renameFixture(t, fileNameReply("", ""))
	p := renameParams()
	p.Rename.ExpectedFullName = "sonuc.txt"
	r, err := runDesktopWorkflow(t.Context(), p, fantasy.ToolCall{ID: "rename"}, invoke)
	require.NoError(t, err)
	require.True(t, r.IsError)
	require.True(t, strings.HasPrefix(r.Content, "full_name_unverified:"), r.Content)
	require.Contains(t, r.Content, "Do not rename again")
	requirements := renameRequirements(t, r.Content)
	require.Equal(t, false, requirements["full_name"]["verified"])
	require.NotContains(t, requirements["full_name"], "actual")
	var keys []string
	for _, c := range *calls {
		if c.Action == "key" {
			keys = append(keys, c.Key)
		}
	}
	require.Equal(t, []string{"f2", "enter"}, keys, "no rename input was repeated")

	// Without an expected name the rename still succeeds, but the report
	// does not claim the unreadable full name.
	invoke, _ = renameFixture(t, fileNameReply("", ""))
	r, err = runDesktopWorkflow(t.Context(), renameParams(), fantasy.ToolCall{ID: "rename"}, invoke)
	require.NoError(t, err)
	require.False(t, r.IsError, r.Content)
	require.Equal(t, false, renameRequirements(t, r.Content)["full_name"]["verified"])
}

// A different real extension fails the requested full name.
func TestRenameRejectsMismatchedFullName(t *testing.T) {
	t.Parallel()
	invoke, _ := renameFixture(t, fileNameReply("sonuc.log", `C:\x`))
	p := renameParams()
	p.Rename.ExpectedFullName = "sonuc.txt"
	r, err := runDesktopWorkflow(t.Context(), p, fantasy.ToolCall{ID: "rename"}, invoke)
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(r.Content, "full_name_unverified:"), r.Content)
	require.Equal(t, "sonuc.log", renameRequirements(t, r.Content)["full_name"]["actual"])
}

// An expected full name that is not new_name plus one extension is refused
// before any child call.
func TestRenameValidatesExpectedFullNameBeforeInput(t *testing.T) {
	t.Parallel()
	for _, bad := range []string{"other.txt", "sonuc.txt.bak", "sonuc.", "sonuc/x.txt"} {
		invoke, calls := renameFixture(t, nil)
		p := renameParams()
		p.Rename.ExpectedFullName = bad
		r, err := runDesktopWorkflow(t.Context(), p, fantasy.ToolCall{ID: "rename"}, invoke)
		require.NoError(t, err)
		require.True(t, strings.HasPrefix(r.Content, "invalid_name:"), "%s: %s", bad, r.Content)
		require.Empty(t, *calls, bad)
	}
}
