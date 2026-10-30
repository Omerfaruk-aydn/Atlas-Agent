package harnessmetrics

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/vault"
	"github.com/stretchr/testify/require"
)

func TestTraceTurnsRedactionAndGateTotals(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	r := New(func() string { return root })
	require.NoError(t, vault.Register("trace-test", "private-trace-value"))
	first := r.BeginTrace("trace-test")
	first.Record(TraceEvent{Kind: "gate", Name: "review", Outcome: "fail_open", Reason: "private-trace-value", InputTokens: 9, OutputTokens: 2, DurationMS: 4})
	second := New(func() string { return root }).BeginTrace("trace-test")
	require.Equal(t, first.Turn+1, second.Turn)
	second.Record(TraceEvent{Kind: "gate", Name: "review", Outcome: "pass"})
	events, err := LoadTrace(root, "trace-test", first.Turn)
	require.NoError(t, err)
	require.Len(t, events, 2)
	require.Equal(t, "[REDACTED]", events[1].Reason)
	totals := SummarizeGates(events)
	require.Len(t, totals, 1)
	require.Equal(t, 1, totals[0].Triggered)
	require.Equal(t, 1, totals[0].FailOpen)
	require.EqualValues(t, 11, totals[0].Tokens)
	require.EqualValues(t, 4, totals[0].DurationMS)
}

func TestTraceRotationKeepsMonotonicTurnNumbers(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	r := New(func() string { return root })
	first := r.BeginTrace("rotate")
	line, err := json.Marshal(TraceEvent{Version: 1, Session: "rotate", TraceID: first.ID, Turn: 1, Kind: "step", Reason: strings.Repeat("a", 512)})
	require.NoError(t, err)
	line = append(line, '\n')
	path := tracePath(root, "rotate")
	require.NoError(t, os.WriteFile(path, bytes.Repeat(line, maxFileBytes/len(line)+1), 0o600))
	first.Record(TraceEvent{Kind: "turn_end"})
	require.FileExists(t, path+".1")
	second := r.BeginTrace("rotate")
	require.Equal(t, 2, second.Turn)
	events, err := LoadTrace(root, "rotate", 2)
	require.NoError(t, err)
	require.Len(t, events, 1)
}

func BenchmarkTraceMetadataWrite(b *testing.B) {
	root := b.TempDir()
	r := New(func() string { return root })
	trace := r.BeginTrace("benchmark")
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		trace.Record(TraceEvent{Kind: "model", Name: "mock/model", Outcome: "pass", InputTokens: 100, OutputTokens: 10})
	}
}

func TestTraceWriteFailureIsObservableAndBestEffort(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, Dir), []byte("not a directory"), 0o600))
	r := New(func() string { return root })
	trace := r.BeginTrace("s")
	trace.Record(TraceEvent{Kind: "step"})
	require.Positive(t, r.TraceFailures())
}

func TestTraceConcurrentTurnsAndPathConfinement(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	r := New(func() string { return root })
	var wg sync.WaitGroup
	for range 12 {
		wg.Go(func() { r.BeginTrace("../../escape").Record(TraceEvent{Kind: "step"}) })
	}
	wg.Wait()
	events, err := LoadTrace(root, "../../escape", 0)
	require.NoError(t, err)
	turns := map[int]bool{}
	for _, event := range events {
		if event.Kind == "turn_start" {
			require.False(t, turns[event.Turn])
			turns[event.Turn] = true
		}
	}
	require.Len(t, turns, 12)
	require.Zero(t, r.TraceFailures())
}
