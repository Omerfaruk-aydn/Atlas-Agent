package util

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	powernap "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-powernap/pkg/lsp"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-powernap/pkg/lsp/protocol"
	"github.com/stretchr/testify/require"
)

func TestEditPlanUnicodeAndCRLF(t *testing.T) {
	t.Parallel()
	for _, encoding := range []powernap.OffsetEncoding{powernap.UTF16, powernap.UTF8} {
		root := t.TempDir()
		first, second := filepath.Join(root, "a.go"), filepath.Join(root, "b.go")
		require.NoError(t, os.WriteFile(first, []byte("x😀name\r\nnext\r\n"), 0o600))
		require.NoError(t, os.WriteFile(second, []byte("🌍 name\r\n"), 0o600))
		start := uint32(3)
		if encoding == powernap.UTF8 {
			start = 5
		}
		rangeName := protocol.Range{Start: protocol.Position{Character: start}, End: protocol.Position{Character: start + 4}}
		edit := protocol.WorkspaceEdit{Changes: map[protocol.DocumentURI][]protocol.TextEdit{
			protocol.URIFromPath(first):  {{Range: rangeName, NewText: "renamed"}},
			protocol.URIFromPath(second): {{Range: rangeName, NewText: "renamed"}},
		}}
		plan, err := PrepareWorkspaceEdit(t.Context(), root, edit, encoding)
		require.NoError(t, err)
		require.Len(t, plan.Changes, 2)
		original, err := os.ReadFile(first)
		require.NoError(t, err)
		require.Equal(t, "x😀name\r\nnext\r\n", string(original), "preparation writes nothing")
		result, err := ApplyEditPlan(t.Context(), root, t.TempDir(), plan)
		require.NoError(t, err)
		require.Equal(t, "applied", result.Status)
		data, err := os.ReadFile(first)
		require.NoError(t, err)
		require.Equal(t, "x😀renamed\r\nnext\r\n", string(data))
		data, err = os.ReadFile(second)
		require.NoError(t, err)
		require.Equal(t, "🌍 renamed\r\n", string(data))
		edit.Changes[protocol.URIFromPath(first)] = []protocol.TextEdit{{Range: rangeName, NewText: "a"}, {Range: rangeName, NewText: "b"}}
		_, err = PrepareWorkspaceEdit(t.Context(), root, edit, encoding)
		require.Error(t, err)
	}
}

func TestEditPlanProcessCrashRecovery(t *testing.T) {
	if os.Getenv("ATLAS_EDIT_CRASH_FIXTURE") == "1" {
		root, journalDir := os.Getenv("ATLAS_EDIT_CRASH_ROOT"), os.Getenv("ATLAS_EDIT_CRASH_JOURNAL")
		edit := protocol.WorkspaceEdit{Changes: map[protocol.DocumentURI][]protocol.TextEdit{}}
		for _, name := range []string{"a.go", "b.go"} {
			edit.Changes[protocol.URIFromPath(filepath.Join(root, name))] = []protocol.TextEdit{{Range: protocol.Range{End: protocol.Position{Character: 3}}, NewText: "new"}}
		}
		plan, err := PrepareWorkspaceEdit(t.Context(), root, edit, powernap.UTF16)
		if err != nil {
			panic(err)
		}
		if err := os.WriteFile(filepath.Join(journalDir, "identity"), []byte(plan.ID), 0o600); err != nil {
			panic(err)
		}
		_, _ = applyEditPlan(t.Context(), root, journalDir, plan, func(project *os.Root, change PlannedChange) error {
			if err := writePlanned(project, change); err != nil {
				panic(err)
			}
			// Terminate after a real source write and before the entry's final
			// status is persisted. No defers or automatic rollback run.
			os.Exit(71)
			return nil
		})
		os.Exit(72)
	}
	t.Parallel()
	root, journalDir := t.TempDir(), t.TempDir()
	for _, name := range []string{"a.go", "b.go"} {
		require.NoError(t, os.WriteFile(filepath.Join(root, name), []byte("old"), 0o600))
	}
	executable, err := os.Executable()
	require.NoError(t, err)
	child := exec.CommandContext(t.Context(), executable, "-test.run=^TestEditPlanProcessCrashRecovery$")
	child.Env = append(os.Environ(), "ATLAS_EDIT_CRASH_FIXTURE=1", "ATLAS_EDIT_CRASH_ROOT="+root, "ATLAS_EDIT_CRASH_JOURNAL="+journalDir)
	output, err := child.CombinedOutput()
	var exit *exec.ExitError
	require.ErrorAs(t, err, &exit, string(output))
	require.Equal(t, 71, exit.ExitCode(), string(output))
	identity, err := os.ReadFile(filepath.Join(journalDir, "identity"))
	require.NoError(t, err)
	id := string(identity)
	result, err := InspectEditJournal(t.Context(), root, journalDir, id)
	require.NoError(t, err)
	require.Equal(t, "applying", result.Status)
	canonical, opened, err := editRoot(root)
	require.NoError(t, err)
	require.NoError(t, opened.Close())
	journalRoot, release, err := openEditJournal(t.Context(), journalDir, canonical)
	require.NoError(t, err)
	journal, err := readEditJournal(journalRoot, canonical, id)
	require.NoError(t, err)
	release()
	require.NoError(t, ValidateEditJournal(t.Context(), root, journalDir, journal.Plan))
	forgedPlan := journal.Plan
	forgedPlan.Changes = append([]PlannedChange{}, journal.Plan.Changes...)
	forgedPlan.Changes[0].Content = []byte("forged")
	require.ErrorContains(t, ValidateEditJournal(t.Context(), root, journalDir, forgedPlan), "approved plan")
	_, err = RecoverEditPlan(t.Context(), root, journalDir, id, forgedPlan)
	require.ErrorContains(t, err, "approved plan")
	proposed, err := PreviewEditRecovery(t.Context(), root, journalDir, id)
	require.NoError(t, err)
	require.Len(t, proposed, 1)
	require.Equal(t, "old", string(proposed[0].Content))
	result, err = RecoverEditPlan(t.Context(), root, journalDir, id)
	require.NoError(t, err)
	require.Equal(t, "rolled_back", result.Status)
	for _, name := range []string{"a.go", "b.go"} {
		data, err := os.ReadFile(filepath.Join(root, name))
		require.NoError(t, err)
		require.Equal(t, "old", string(data))
	}
}

func TestEditPlanResourceRollbackRestoresCreatesAndDeletes(t *testing.T) {
	t.Parallel()
	root, journalDir := t.TempDir(), t.TempDir()
	created, deleted, trigger := filepath.Join(root, "a.go"), filepath.Join(root, "b.go"), filepath.Join(root, "c.go")
	require.NoError(t, os.WriteFile(deleted, []byte("keep"), 0o600))
	require.NoError(t, os.WriteFile(trigger, []byte("old"), 0o600))
	edit := protocol.WorkspaceEdit{DocumentChanges: []protocol.DocumentChange{
		{CreateFile: &protocol.CreateFile{URI: protocol.URIFromPath(created)}},
		{DeleteFile: &protocol.DeleteFile{URI: protocol.URIFromPath(deleted)}},
		{TextDocumentEdit: &protocol.TextDocumentEdit{TextDocument: protocol.OptionalVersionedTextDocumentIdentifier{TextDocumentIdentifier: protocol.TextDocumentIdentifier{URI: protocol.URIFromPath(trigger)}}, Edits: []protocol.Or_TextDocumentEdit_edits_Elem{{Value: protocol.TextEdit{Range: protocol.Range{End: protocol.Position{Character: 3}}, NewText: "new"}}}}},
	}}
	plan, err := PrepareWorkspaceEdit(t.Context(), root, edit, powernap.UTF16)
	require.NoError(t, err)
	count := 0
	result, err := applyEditPlan(t.Context(), root, journalDir, plan, func(project *os.Root, change PlannedChange) error {
		count++
		if count == 3 {
			return errors.New("injected write failure")
		}
		return writePlanned(project, change)
	})
	require.Error(t, err)
	require.Equal(t, "rolled_back", result.Status)
	_, err = os.Stat(created)
	require.True(t, os.IsNotExist(err))
	data, err := os.ReadFile(deleted)
	require.NoError(t, err)
	require.Equal(t, "keep", string(data))
	data, err = os.ReadFile(trigger)
	require.NoError(t, err)
	require.Equal(t, "old", string(data))
}

func TestEditPlanTargetBoundaries(t *testing.T) {
	t.Parallel()
	root, outside := t.TempDir(), t.TempDir()
	file := filepath.Join(outside, "outside.go")
	require.NoError(t, os.WriteFile(file, []byte("original"), 0o600))
	for _, edit := range []protocol.WorkspaceEdit{
		{Changes: map[protocol.DocumentURI][]protocol.TextEdit{protocol.URIFromPath(file): {{NewText: "changed"}}}},
		{DocumentChanges: []protocol.DocumentChange{{CreateFile: &protocol.CreateFile{URI: protocol.URIFromPath(filepath.Join(outside, "new.go"))}}}},
		{DocumentChanges: []protocol.DocumentChange{{DeleteFile: &protocol.DeleteFile{URI: protocol.URIFromPath(file)}}}},
		{DocumentChanges: []protocol.DocumentChange{{DeleteFile: &protocol.DeleteFile{URI: protocol.URIFromPath(filepath.Join(root, "missing.go"))}}}},
	} {
		_, err := PrepareWorkspaceEdit(t.Context(), root, edit, powernap.UTF16)
		require.Error(t, err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err == nil {
		_, err = PrepareWorkspaceEdit(t.Context(), root, protocol.WorkspaceEdit{DocumentChanges: []protocol.DocumentChange{{CreateFile: &protocol.CreateFile{URI: protocol.URIFromPath(filepath.Join(root, "escape", "new.go"))}}}}, powernap.UTF16)
		require.Error(t, err)
	} else {
		t.Logf("Symlink fixture unavailable: %v", err)
	}
}

func TestEditPlanStaleSourceWritesNothing(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	first, second := filepath.Join(root, "a.go"), filepath.Join(root, "b.go")
	for _, file := range []string{first, second} {
		require.NoError(t, os.WriteFile(file, []byte("old"), 0o600))
	}
	edit := protocol.WorkspaceEdit{Changes: map[protocol.DocumentURI][]protocol.TextEdit{}}
	for _, file := range []string{first, second} {
		edit.Changes[protocol.URIFromPath(file)] = []protocol.TextEdit{{Range: protocol.Range{End: protocol.Position{Character: 3}}, NewText: "new"}}
	}
	plan, err := PrepareWorkspaceEdit(t.Context(), root, edit, powernap.UTF16)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(second, []byte("user edit"), 0o600))
	_, err = ApplyEditPlan(t.Context(), root, t.TempDir(), plan)
	require.Error(t, err)
	data, err := os.ReadFile(first)
	require.NoError(t, err)
	require.Equal(t, "old", string(data))
}

func TestEditPlanStaleAndRecoveryPreservesUserChanges(t *testing.T) {
	t.Parallel()
	root, journalDir := t.TempDir(), t.TempDir()
	first, second := filepath.Join(root, "a.go"), filepath.Join(root, "b.go")
	edit := protocol.WorkspaceEdit{Changes: map[protocol.DocumentURI][]protocol.TextEdit{}}
	for _, file := range []string{first, second} {
		require.NoError(t, os.WriteFile(file, []byte("old"), 0o600))
		edit.Changes[protocol.URIFromPath(file)] = []protocol.TextEdit{{Range: protocol.Range{End: protocol.Position{Character: 3}}, NewText: "new"}}
	}
	plan, err := PrepareWorkspaceEdit(t.Context(), root, edit, powernap.UTF16)
	require.NoError(t, err)
	mutations := 0
	result, err := applyEditPlan(t.Context(), root, journalDir, plan, func(opened *os.Root, change PlannedChange) error {
		mutations++
		if mutations == 2 {
			require.NoError(t, os.WriteFile(first, []byte("new user content"), 0o600))
			return errors.New("injected second write failure")
		}
		return writePlanned(opened, change)
	})
	require.ErrorContains(t, err, "injected")
	require.Equal(t, "conflicted", result.Status)
	require.Len(t, result.Conflicted, 1)
	data, err := os.ReadFile(first)
	require.NoError(t, err)
	require.Equal(t, "new user content", string(data))
	data, err = os.ReadFile(second)
	require.NoError(t, err)
	require.Equal(t, "old", string(data))
	observed, err := InspectEditJournal(t.Context(), root, journalDir, plan.ID)
	require.NoError(t, err)
	require.Equal(t, "conflicted", observed.Status)
	_, err = ApplyEditPlan(t.Context(), root, journalDir, plan)
	require.ErrorContains(t, err, "recovery", "retry must not replay edits")
	result, err = RecoverEditPlan(t.Context(), root, journalDir, plan.ID)
	require.NoError(t, err)
	require.Equal(t, "conflicted", result.Status)
	data, err = os.ReadFile(first)
	require.NoError(t, err)
	require.Equal(t, "new user content", string(data))
	// A user may explicitly restore their original content. Recovery observes
	// that state without overwriting it or claiming a fresh application.
	require.NoError(t, os.WriteFile(first, []byte("old"), 0o600))
	result, err = RecoverEditPlan(t.Context(), root, journalDir, plan.ID)
	require.NoError(t, err)
	require.Equal(t, "rolled_back", result.Status)
}

func TestEditPlanRenameAndStrictEncodingBoundaries(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	source, destination := filepath.Join(root, "old.go"), filepath.Join(root, "nested", "new.go")
	require.NoError(t, os.WriteFile(source, []byte("x😀name\r\n"), 0o600))
	for _, encoding := range []powernap.OffsetEncoding{powernap.UTF8, powernap.UTF16} {
		_, err := PrepareWorkspaceEdit(t.Context(), root, protocol.WorkspaceEdit{Changes: map[protocol.DocumentURI][]protocol.TextEdit{protocol.URIFromPath(source): {{Range: protocol.Range{Start: protocol.Position{Character: 2}, End: protocol.Position{Character: 3}}, NewText: "x"}}}}, encoding)
		require.Error(t, err, "a character boundary must not split a codepoint or surrogate pair")
	}
	plan, err := PrepareWorkspaceEdit(t.Context(), root, protocol.WorkspaceEdit{DocumentChanges: []protocol.DocumentChange{{RenameFile: &protocol.RenameFile{OldURI: protocol.URIFromPath(source), NewURI: protocol.URIFromPath(destination)}}}}, powernap.UTF16)
	require.NoError(t, err)
	require.Len(t, plan.Changes, 2)
	result, err := ApplyEditPlan(t.Context(), root, t.TempDir(), plan)
	require.NoError(t, err)
	require.Equal(t, "applied", result.Status)
	_, err = os.Stat(source)
	require.True(t, os.IsNotExist(err))
	data, err := os.ReadFile(destination)
	require.NoError(t, err)
	require.Equal(t, "x😀name\r\n", string(data))
}
