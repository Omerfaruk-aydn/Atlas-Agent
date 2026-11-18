package agent

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/message"
	"github.com/stretchr/testify/require"
)

func assistantWith(calls ...message.ToolCall) message.Message {
	m := message.Message{Role: message.Assistant}
	for _, c := range calls {
		m.Parts = append(m.Parts, c)
	}
	return m
}

func TestSummarizeRunCountsCallsAndNamesChangedFiles(t *testing.T) {
	t.Parallel()
	run := summarizeRun([]message.Message{
		{Role: message.User},
		assistantWith(
			message.ToolCall{Name: "view", Input: `{"file_path":"a.go"}`},
			message.ToolCall{Name: "edit", Input: `{"file_path":"b.go"}`},
		),
		assistantWith(
			message.ToolCall{Name: "edit", Input: `{"file_path":"b.go"}`},
			message.ToolCall{Name: "write", Input: `{"file_path":"c.go"}`},
			message.ToolCall{Name: "bash", Input: `{"command":"ls"}`},
		),
	})
	require.Equal(t, 2, run.Steps)
	require.Equal(t, map[string]int{"view": 1, "edit": 2, "write": 1, "bash": 1}, run.ToolCalls)
	require.Equal(t, []string{"b.go", "c.go"}, run.FilesChanged, "a viewed file is not a changed one; a file edited twice is listed once")
}

func TestSubAgentNoteIsEmptyWhenNothingChanged(t *testing.T) {
	t.Parallel()
	require.Empty(t, SubAgentRun{ToolCalls: map[string]int{"view": 3}}.note())
	require.Contains(t, SubAgentRun{FilesChanged: []string{"x.go"}, DurationMs: 2000}.note(), "changed 1 file(s) in 2s: x.go")
}

func TestSubAgentNoteShortensALongList(t *testing.T) {
	t.Parallel()
	files := make([]string, 25)
	for i := range files {
		files[i] = "f" + strings.Repeat("x", i%3) + ".go"
	}
	note := SubAgentRun{FilesChanged: files}.note()
	require.Contains(t, note, "and 5 more")
}

func TestOutsideOwnedFlagsFilesThatLeaveTheOwnedPaths(t *testing.T) {
	t.Parallel()
	root := filepath.Join(t.TempDir(), "proj")
	files := []string{"src/a.go", filepath.Join(root, "src", "b.go"), "docs/readme.md", filepath.Join(root, "main.go"), "SRC/c.go"}
	require.Equal(t, []string{"docs/readme.md", filepath.Join(root, "main.go")}, outsideOwned(files, root, []string{"src"}))
	require.Nil(t, outsideOwned(files, root, nil), "no owned paths means no restriction")
	require.Nil(t, outsideOwned(files, root, []string{"."}))

	run := SubAgentRun{FilesChanged: []string{"a.go"}, OutsideOwned: []string{"a.go"}}
	require.Contains(t, run.note(), "Outside its owned paths: a.go")
}

func TestChangedBetweenSeesShellWritesAndIgnoresAtlasState(t *testing.T) {
	t.Parallel()
	before := map[string]string{"a.go": "1:1", "b.go": "2:2", ".atlas/x": "1:1"}
	after := map[string]string{"a.go": "1:1", "b.go": "3:9", "new.txt": "5:5", ".atlas/x": "9:9", ".atlas/y": "1:1"}
	require.Equal(t, []string{"b.go", "new.txt"}, changedBetween(before, after))
	require.Equal(t, []string{"gone.go"}, changedBetween(map[string]string{"gone.go": "1:1"}, map[string]string{}))
	require.Nil(t, changedBetween(nil, after), "an unknown snapshot reports nothing")
}

func TestTreeStampsNoticeAFileWrittenOutsideTheEditTools(t *testing.T) {
	root := t.TempDir()
	git := func(args ...string) {
		out, err := exec.Command("git", append([]string{"-C", root}, args...)...).CombinedOutput()
		require.NoError(t, err, string(out))
	}
	git("init", "-q")
	require.NoError(t, os.MkdirAll(filepath.Join(root, "src"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "src", "a.go"), []byte("package a\n"), 0o644))
	before := treeStamps(t.Context(), root)
	require.NotNil(t, before)

	// What a shell command would do: write inside and outside the owned folder.
	require.NoError(t, os.WriteFile(filepath.Join(root, "src", "a.go"), []byte("package a // changed\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "notes.txt"), []byte("hello"), 0o644))
	changed := changedBetween(before, treeStamps(t.Context(), root))
	require.Equal(t, []string{"notes.txt", "src/a.go"}, changed)
	require.Equal(t, []string{"notes.txt"}, outsideOwned(changed, root, []string{"src"}))

	require.Nil(t, treeStamps(t.Context(), t.TempDir()), "outside a repository the snapshot is unknown")
}

func TestNoteSaysWhenOnlyAShellCommandLeftTheOwnedPaths(t *testing.T) {
	t.Parallel()
	require.Empty(t, SubAgentRun{}.note())
	note := SubAgentRun{OutsideOwned: []string{"notes.txt"}}.note()
	require.Contains(t, note, "outside its owned paths: notes.txt")
}
