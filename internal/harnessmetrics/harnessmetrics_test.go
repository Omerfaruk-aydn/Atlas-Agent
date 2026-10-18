package harnessmetrics

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRecordLoadAndSummarize(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	r := New(func() string { return root })
	r.Record(ContractSentBack, "s1", map[string]string{"round": "1"})
	r.Record(ContractPassed, "s1", nil)
	r.Record(ToolSearchEmpty, "s2", map[string]string{"query": "Ekran Kaydı"})
	r.Record(ToolSearchEmpty, "s2", map[string]string{"query": "ekran kaydı"})
	r.Record(ToolSearchHit, "s2", nil)
	r.Record(NudgeGiven, "s2", map[string]string{"tool": "browser"})
	r.Record(NudgeFollowed, "s2", map[string]string{"tool": "browser"})

	events, err := Load(root, time.Time{})
	require.NoError(t, err)
	require.Len(t, events, 7)

	s := Summarize(events)
	require.Equal(t, 2, s.Sessions)
	require.Equal(t, 2, s.Counts[ToolSearchEmpty])
	require.Equal(t, []string{"ekran kaydı"}, s.TopEmptySearches, "queries are grouped case-insensitively")
	require.Equal(t, 1, s.NudgedTools["browser"])

	report := Format(s, time.Unix(0, 0))
	require.Contains(t, report, "2 sessions")
	require.Contains(t, report, "pass rate")
	require.Contains(t, report, "100%")
	require.Contains(t, report, "- ekran kaydı")
}

func TestLoadSkipsBrokenLinesAndFiltersByTime(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, Dir), 0o755))
	content := `{"t":1000,"kind":"compaction"}` + "\n" + `{"t":` + "\n" + `{"t":5000,"kind":"loop_stopped"}` + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(root, Dir, File), []byte(content), 0o644))

	events, err := Load(root, time.UnixMilli(2000))
	require.NoError(t, err)
	require.Len(t, events, 1)
	require.Equal(t, LoopStopped, events[0].Kind)
}

func TestRecordRotatesLargeFiles(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	dir := filepath.Join(root, Dir)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, File), []byte(strings.Repeat("x", maxFileBytes+1)), 0o644))

	New(func() string { return root }).Record(Compaction, "s", nil)
	_, err := os.Stat(filepath.Join(dir, "events.1.jsonl"))
	require.NoError(t, err, "the old generation is kept")
	data, err := os.ReadFile(filepath.Join(dir, File))
	require.NoError(t, err)
	require.Contains(t, string(data), `"kind":"compaction"`)
}

func TestNilAndEmptyRecordersDoNothing(t *testing.T) {
	t.Parallel()

	var r *Recorder
	r.Record(Compaction, "s", nil)
	New(func() string { return "" }).Record(Compaction, "s", nil)
	require.Contains(t, Format(Summarize(nil), time.Now()), "No harness events")
}

func TestRecordClipsLongAttributes(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	New(func() string { return root }).Record(ToolSearchEmpty, "s", map[string]string{"query": strings.Repeat("a", 500)})
	events, err := Load(root, time.Time{})
	require.NoError(t, err)
	require.Len(t, events[0].Attrs["query"], 200)
}
