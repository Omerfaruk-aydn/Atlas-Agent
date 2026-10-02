package subagents

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRenderRoundTrips(t *testing.T) {
	s := &Subagent{Name: "research", Description: "Deep research.", Model: "@research", Instructions: "Dig deep.", ReadOnly: true, AllowCommands: true}

	content, err := Render(s)
	require.NoError(t, err)

	got, err := ParseContent(content)
	require.NoError(t, err)
	require.Equal(t, s.Name, got.Name)
	require.Equal(t, s.Description, got.Description)
	require.Equal(t, s.Model, got.Model)
	require.Equal(t, s.ReadOnly, got.ReadOnly)
	require.Equal(t, s.AllowCommands, got.AllowCommands)
	require.Equal(t, s.Instructions, got.Instructions)
}

func TestRenderOmitsAnEmptyModel(t *testing.T) {
	content, err := Render(&Subagent{Name: "generic", Description: "d", Instructions: "i"})
	require.NoError(t, err)
	require.NotContains(t, string(content), "model:")
}

func TestSaveWritesAParsableFile(t *testing.T) {
	dir := t.TempDir()
	path, err := Save(dir, &Subagent{Name: "frontend", Description: "UI work.", Model: "@frontend", Instructions: "Body."})
	require.NoError(t, err)
	require.Equal(t, filepath.Join(dir, "frontend.md"), path)

	got, err := Parse(path)
	require.NoError(t, err)
	require.Equal(t, "frontend", got.Name)
	require.Equal(t, "@frontend", got.Model)
}

func TestSaveRejectsAnInvalidSubagent(t *testing.T) {
	_, err := Save(t.TempDir(), &Subagent{Name: "", Description: "d"})
	require.Error(t, err)
}

func TestSaveCreatesTheDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "agents")
	_, err := Save(dir, &Subagent{Name: "x", Description: "d"})
	require.NoError(t, err)
	require.DirExists(t, dir)
}

func TestDeleteRemovesTheFile(t *testing.T) {
	dir := t.TempDir()
	path, err := Save(dir, &Subagent{Name: "old", Description: "d"})
	require.NoError(t, err)

	require.NoError(t, Delete(dir, "old"))
	_, err = os.Stat(path)
	require.True(t, os.IsNotExist(err))
}

func TestDeleteFailsWhenNothingIsThere(t *testing.T) {
	require.Error(t, Delete(t.TempDir(), "nope"))
}

func TestProjectDirIsDotAtlasAgentsUnderWorkingDir(t *testing.T) {
	require.Equal(t, filepath.Join("wd", ".atlas", "agents"), ProjectDir("wd"))
}

func TestSaveNamedCreatesInProjectDirByDefault(t *testing.T) {
	workingDir := t.TempDir()
	path, err := SaveNamed(nil, workingDir, Subagent{Name: "research", Description: "d"}, false)
	require.NoError(t, err)
	require.Equal(t, ProjectDir(workingDir), filepath.Dir(path))
}

func TestSaveNamedCreatesInUserDirWhenRequested(t *testing.T) {
	// home.Config() reads XDG_CONFIG_HOME fresh on every call, so
	// setting just this env var is enough to isolate UserDir() for
	// the test -- home.Dir() itself is resolved once at process start
	// and is not part of this path when XDG_CONFIG_HOME is set.
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	workingDir := t.TempDir()
	path, err := SaveNamed(nil, workingDir, Subagent{Name: "research", Description: "d"}, true)
	require.NoError(t, err)
	require.Equal(t, UserDir(), filepath.Dir(path))
}

func TestSaveNamedEditsInPlaceRegardlessOfUserScope(t *testing.T) {
	projectDir := t.TempDir()
	original, err := Save(projectDir, &Subagent{Name: "research", Description: "old description"})
	require.NoError(t, err)

	// userScope: true must not move an existing subagent out of the
	// directory it was already discovered in.
	path, err := SaveNamed([]string{projectDir}, t.TempDir(),
		Subagent{Name: "research", Description: "new description"}, true)
	require.NoError(t, err)
	require.Equal(t, original, path, "editing an existing subagent must keep it in its current directory")

	got, err := Parse(path)
	require.NoError(t, err)
	require.Equal(t, "new description", got.Description)
}

func TestDeleteNamedRemovesTheDiscoveredFile(t *testing.T) {
	dir := t.TempDir()
	path, err := Save(dir, &Subagent{Name: "old", Description: "d"})
	require.NoError(t, err)

	require.NoError(t, DeleteNamed([]string{dir}, "old"))
	_, err = os.Stat(path)
	require.True(t, os.IsNotExist(err))
}

func TestDeleteNamedFailsForAnUnknownName(t *testing.T) {
	err := DeleteNamed([]string{t.TempDir()}, "nope")
	require.Error(t, err)
	require.Contains(t, err.Error(), `no subagent named "nope"`)
}
