package routine

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCronTZPinsTheZone(t *testing.T) {
	t.Parallel()
	zone, err := time.LoadLocation("Europe/Istanbul")
	if err != nil {
		t.Skip("tzdata unavailable")
	}
	s, err := ParseSchedule("CRON_TZ=Europe/Istanbul 0 9 * * *")
	require.NoError(t, err)
	// 05:00 UTC is 08:00 in Istanbul (UTC+3), so the next 09:00 is an hour away.
	from := time.Date(2026, 11, 17, 5, 0, 0, 0, time.UTC)
	next := s.Next(from)
	require.Equal(t, time.Date(2026, 11, 17, 9, 0, 0, 0, zone), next)
	require.Equal(t, 6, next.UTC().Hour())

	_, err = ParseSchedule("CRON_TZ=Nowhere/Land 0 9 * * *")
	require.Error(t, err)
	_, err = ParseSchedule("CRON_TZ=Europe/Istanbul")
	require.Error(t, err)
	_, err = ParseSchedule("TZ=UTC @daily")
	require.NoError(t, err)
}

func TestParseAutomation(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	r, err := ParseAutomation("morning", "---\nschedule: 0 9 * * 1-5\ntimeout: 120\ngoal: true\n---\nSummarize the repo.\n", dir)
	require.NoError(t, err)
	require.Equal(t, "auto-morning", r.Name)
	require.Equal(t, PermissionsReadOnly, r.Permissions)
	require.Equal(t, 120, r.TimeoutSeconds)
	require.True(t, r.Goal)
	require.Equal(t, "Summarize the repo.", r.Prompt)

	_, err = ParseAutomation("x", "no front matter", dir)
	require.Error(t, err)
	_, err = ParseAutomation("x", "---\nschedule: 0 9 * * *\npermissions: auto\n---\ndo it", dir)
	require.ErrorContains(t, err, "cannot request permissions: auto")
	_, err = ParseAutomation("x", "---\nschedule: nonsense\n---\ndo it", dir)
	require.Error(t, err)
}

func TestSyncCreatesDisabledUpdatesAndRemoves(t *testing.T) {
	t.Parallel()
	project := t.TempDir()
	dir := filepath.Join(project, ".atlas", "automations")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	write := func(name, schedule string) {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name+".md"), []byte("---\nschedule: "+schedule+"\n---\nDo "+name+".\n"), 0o644))
	}
	write("daily", "0 9 * * *")
	write("weekly", "0 9 * * 1")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "broken.md"), []byte("nothing"), 0o644))
	store := NewStore(t.TempDir())

	report, err := Sync(t.Context(), store, dir, project)
	require.NoError(t, err)
	require.Equal(t, []string{"auto-daily", "auto-weekly"}, report.Added)
	require.Len(t, report.Errors, 1)
	all, _ := store.List(t.Context())
	for _, r := range all {
		require.False(t, r.Enabled, "file-defined routines start disabled")
	}

	// Enabling survives an update; an unchanged file is left alone.
	_, err = store.SetEnabled(t.Context(), "auto-daily", true)
	require.NoError(t, err)
	write("daily", "30 9 * * *")
	report, err = Sync(t.Context(), store, dir, project)
	require.NoError(t, err)
	require.Equal(t, []string{"auto-daily"}, report.Updated)
	require.Equal(t, []string{"auto-weekly"}, report.Unchanged)
	got, err := store.Get(t.Context(), "auto-daily")
	require.NoError(t, err)
	require.True(t, got.Enabled)
	require.Equal(t, "30 9 * * *", got.Schedule)

	// Deleting the file removes the routine; routines made by hand stay.
	_, err = store.Add(t.Context(), Routine{Name: "manual", Schedule: "@daily", Prompt: "x", Dir: project})
	require.NoError(t, err)
	require.NoError(t, os.Remove(filepath.Join(dir, "weekly.md")))
	report, err = Sync(t.Context(), store, dir, project)
	require.NoError(t, err)
	require.Equal(t, []string{"auto-weekly"}, report.Removed)
	all, _ = store.List(t.Context())
	var names []string
	for _, r := range all {
		names = append(names, r.Name)
	}
	require.ElementsMatch(t, []string{"auto-daily", "manual"}, names)
}
