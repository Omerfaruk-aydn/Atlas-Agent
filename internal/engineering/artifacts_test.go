package engineering

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
)

// Artifact calls use reflection so missing API fails as an assertion
// before implementation.
func artifactCall(t *testing.T, store *Store, method string, args ...any) (reflect.Value, error) {
	t.Helper()
	fn := reflect.ValueOf(store).MethodByName(method)
	require.True(t, fn.IsValid(), "Missing artifact API: %s", method)
	values := make([]reflect.Value, len(args))
	for i, arg := range args {
		if value, ok := arg.(reflect.Value); ok {
			values[i] = value
		} else {
			values[i] = reflect.ValueOf(arg)
		}
	}
	result := fn.Call(values)
	if !result[len(result)-1].IsNil() {
		return result[0], result[len(result)-1].Interface().(error)
	}
	return result[0], nil
}

func TestArtifactsIntegrityAndCAS(t *testing.T) {
	t.Parallel()
	s := NewStore(t.TempDir())
	ctx := t.Context()
	first, err := artifactCall(t, s, "PutArtifact", ctx, "text", []byte("verified"))
	require.NoError(t, err)
	second, err := artifactCall(t, s, "PutArtifact", ctx, "text", []byte("verified"))
	require.NoError(t, err)
	require.Equal(t, first.Interface(), second.Interface())
	read, err := artifactCall(t, s, "ReadArtifact", ctx, first)
	require.NoError(t, err)
	require.Equal(t, []byte("verified"), read.Interface())
	hash := first.FieldByName("Hash").String()
	require.NoError(t, os.WriteFile(filepath.Join(s.Dir(), "artifacts", hash+".blob"), []byte("corrupt"), 0o600))
	_, err = artifactCall(t, s, "ReadArtifact", ctx, first)
	require.Error(t, err)
	r, err := artifactCall(t, s, "PutRecord", ctx, "test", "task", uint64(0), []byte(`{"v":1}`))
	require.NoError(t, err)
	require.Equal(t, uint64(1), r.FieldByName("Revision").Uint())
	r, err = artifactCall(t, s, "PutRecord", ctx, "test", "task", uint64(1), []byte(`{"v":2}`))
	require.NoError(t, err)
	require.Equal(t, uint64(2), r.FieldByName("Revision").Uint())
	_, err = artifactCall(t, s, "PutRecord", ctx, "test", "task", uint64(1), []byte(`{"v":3}`))
	require.Error(t, err)
	records, err := artifactCall(t, NewStore(filepath.Dir(s.Dir())), "ListRecords", ctx, "test")
	require.NoError(t, err)
	require.Equal(t, uint64(2), records.MapIndex(reflect.ValueOf("task")).FieldByName("Revision").Uint())
}

func TestArtifactsBoundsAndCleanup(t *testing.T) {
	t.Parallel()
	s := NewStore(t.TempDir())
	ctx := t.Context()
	_, err := artifactCall(t, s, "PutArtifact", ctx, "text", make([]byte, 32*1024*1024+1))
	require.Error(t, err)
	linked, err := artifactCall(t, s, "PutArtifact", ctx, "text", []byte("linked output"))
	require.NoError(t, err)
	_, err = artifactCall(t, s, "PutRecord", ctx, "scope", "keep", uint64(0), []byte("record"), linked)
	require.NoError(t, err)
	orphan, err := artifactCall(t, s, "PutArtifact", ctx, "text", []byte("unreferenced"))
	require.NoError(t, err)
	_, err = artifactCall(t, s, "CleanArtifacts", ctx)
	require.NoError(t, err)
	_, err = artifactCall(t, s, "ReadArtifact", ctx, linked)
	require.NoError(t, err)
	_, err = artifactCall(t, s, "ReadArtifact", ctx, orphan)
	require.Error(t, err)
	for i := range 127 {
		_, err = artifactCall(t, s, "PutRecord", ctx, "scope", fmt.Sprint(i), uint64(0), []byte("bounded"))
		require.NoError(t, err)
	}
	_, err = artifactCall(t, s, "PutRecord", ctx, "scope", "overflow", uint64(0), []byte("overflow"))
	require.Error(t, err)
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	_, err = artifactCall(t, s, "PutArtifact", cancelled, "text", []byte("cancelled"))
	require.ErrorIs(t, err, context.Canceled)
	_, err = artifactCall(t, s, "PutRecord", ctx, "scope2", "invalid-link", uint64(0), []byte("record"), orphan)
	require.Error(t, err)
}

func TestArtifactsIdempotentRecordRejectsMissingLinkedEvidence(t *testing.T) {
	t.Parallel()
	s := NewStore(t.TempDir())
	ctx := t.Context()
	linked, err := s.PutArtifact(ctx, "text", []byte("linked"))
	require.NoError(t, err)
	_, err = s.PutRecord(ctx, "scope", "run", 0, []byte("run"), linked)
	require.NoError(t, err)
	require.NoError(t, os.Remove(filepath.Join(s.Dir(), "artifacts", linked.Hash+".blob")))
	_, err = s.PutRecord(ctx, "scope", "run", 0, []byte("run"), linked)
	require.Error(t, err)
}
