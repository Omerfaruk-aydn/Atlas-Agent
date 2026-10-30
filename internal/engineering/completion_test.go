package engineering

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCompletionStatusRechecksArtifactRecordsWithoutStateWrite(t *testing.T) {
	t.Parallel()
	s := NewStore(t.TempDir())
	root := t.TempDir()
	ctx := t.Context()
	stamp, _, err := s.WorkflowRecords(ctx, root, "s")
	require.NoError(t, err)
	source, err := ProjectFingerprint(ctx, root, s.Dir())
	require.NoError(t, err)
	require.NoError(t, s.Update(ctx, "s", func(st *State) error {
		st.Completion = &CompletionCheckpoint{Verdict: "complete", Root: root, RecordsFingerprint: stamp, SourceFingerprint: source, Revision: st.Revision + 1}
		return nil
	}))
	before, err := s.Read(ctx, "s")
	require.NoError(t, err)
	view, err := s.CompletionStatus(ctx, "s")
	require.NoError(t, err)
	require.Equal(t, "complete", view.Verdict)
	// A new finding manifest has its own revision; task state is untouched.
	_, err = s.PutRecordStrict(ctx, findingNamespace("s"), "new-finding", 0, []byte(`{"status":"open"}`))
	require.NoError(t, err)
	after, err := s.Read(ctx, "s")
	require.NoError(t, err)
	require.Equal(t, before.Revision, after.Revision)
	view, err = s.CompletionStatus(ctx, "s")
	require.NoError(t, err)
	require.Equal(t, "unverified", view.Verdict)
}
