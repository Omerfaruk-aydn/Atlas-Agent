package dots

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func str(s string) *string { return &s }

func TestTriggersAndDue(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	a, err := OpenAutomation(dir)
	require.NoError(t, err)
	now := time.Date(2026, 11, 3, 8, 59, 30, 0, time.UTC)
	a.now = func() time.Time { return now }

	_, err = a.CreateTrigger("dot_1", TriggerInput{Name: str("x"), Cron: str("not cron"), Prompt: str("go")})
	require.Error(t, err)
	_, err = a.CreateTrigger("dot_1", TriggerInput{Name: str("x"), Cron: str("0 9 * * *")})
	require.Error(t, err, "a prompt is required")

	tr, err := a.CreateTrigger("dot_1", TriggerInput{Name: str("Sabah"), Cron: str("0 9 * * *"), Timezone: str("UTC"), Prompt: str("özet çıkar")})
	require.NoError(t, err)
	require.Equal(t, time.Date(2026, 11, 3, 9, 0, 0, 0, time.UTC), tr.NextFireAt)

	require.Empty(t, a.Due(now))
	due := a.Due(now.Add(time.Minute))
	require.Len(t, due, 1)
	require.Empty(t, a.Due(now.Add(time.Minute)), "fires once")
	got, err := a.Trigger("dot_1", tr.ID)
	require.NoError(t, err)
	require.Equal(t, time.Date(2026, 11, 4, 9, 0, 0, 0, time.UTC), got.NextFireAt)

	off := false
	got, err = a.UpdateTrigger("dot_1", tr.ID, TriggerInput{Enabled: &off})
	require.NoError(t, err)
	require.True(t, got.NextFireAt.IsZero())

	hook, err := a.CreateTrigger("dot_1", TriggerInput{Name: str("Hook"), Kind: str(KindWebhook), Prompt: str("handle it")})
	require.NoError(t, err)
	require.Len(t, hook.Token, 48)
	found, ok := a.WebhookTrigger(hook.Token)
	require.True(t, ok)
	require.Equal(t, hook.ID, found.ID)
	_, ok = a.WebhookTrigger("nope")
	require.False(t, ok)

	// State survives a reopen.
	b, err := OpenAutomation(dir)
	require.NoError(t, err)
	require.Len(t, b.TriggersFor("dot_1"), 2)
	require.NoError(t, a.DeleteTrigger("dot_1", tr.ID))
	require.ErrorIs(t, a.DeleteTrigger("dot_1", tr.ID), ErrTriggerNotFound)
}

func TestRunsAndRecovery(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	a, err := OpenAutomation(dir)
	require.NoError(t, err)
	r, err := a.StartRun(Run{DotID: "dot_1", Source: SourceManual, Goal: "do it"})
	require.NoError(t, err)
	_, live := a.LiveRun("dot_1")
	require.True(t, live)
	r, err = a.UpdateRun(r.ID, func(r *Run) { r.Status = StatusComplete; r.Summary = "done" })
	require.NoError(t, err)
	require.False(t, r.EndedAt.IsZero())
	_, live = a.LiveRun("dot_1")
	require.False(t, live)

	_, err = a.StartRun(Run{DotID: "dot_1", Source: SourceManual, Goal: "again"})
	require.NoError(t, err)
	b, err := OpenAutomation(dir)
	require.NoError(t, err)
	runs := b.RunsFor("dot_1", 0)
	require.Len(t, runs, 2)
	require.Equal(t, StatusFailed, runs[0].Status, "a run live at shutdown is closed as failed")

	for range maxRuns + 20 {
		_, err = b.StartRun(Run{DotID: "dot_2", Status: StatusSkipped})
		require.NoError(t, err)
	}
	require.LessOrEqual(t, len(b.RunsFor("dot_2", 0))+len(b.RunsFor("dot_1", 0)), maxRuns)
}

func TestBudgetLedger(t *testing.T) {
	t.Parallel()
	a, err := OpenAutomation(t.TempDir())
	require.NoError(t, err)
	day := time.Date(2026, 11, 3, 10, 0, 0, 0, time.Local)
	a.now = func() time.Time { return day }
	require.NoError(t, a.SetLimit("d", 1))
	require.Error(t, a.SetLimit("d", -1))

	// The first reading of a day is the baseline.
	b := a.Spend("d", "s1", 2.00)
	require.InDelta(t, 0, b.SpentUSD, 1e-9)
	require.False(t, b.Exceeded())
	b = a.Spend("d", "s1", 2.40)
	require.InDelta(t, 0.40, b.SpentUSD, 1e-9)
	b = a.Spend("d", "s1", 3.05)
	require.True(t, b.Exceeded())

	// A new session the same day counts in full, on top of the earlier one.
	b = a.Spend("d", "s2", 0.30)
	require.InDelta(t, 1.35, b.SpentUSD, 1e-9)
	b = a.Spend("d", "s2", 0.50)
	require.InDelta(t, 1.55, b.SpentUSD, 1e-9)

	// A new day starts over from the current cost.
	day = day.Add(24 * time.Hour)
	b = a.Spend("d", "s2", 0.90)
	require.InDelta(t, 0, b.SpentUSD, 1e-9)
	b = a.Spend("d", "s2", 1.10)
	require.InDelta(t, 0.20, b.SpentUSD, 1e-9)

	// The history keeps one total per day, oldest first.
	h := a.History("d", 3)
	require.Len(t, h, 3)
	require.Equal(t, "2026-11-02", h[0].Day)
	require.InDelta(t, 0, h[0].USD, 1e-9)
	require.InDelta(t, 1.55, h[1].USD, 1e-9)
	require.InDelta(t, 0.20, h[2].USD, 1e-9)

	// It survives a reload.
	re, err := OpenAutomation(filepath.Dir(a.path))
	require.NoError(t, err)
	re.now = a.now
	require.InDelta(t, 1.55, re.History("d", 2)[0].USD, 1e-9)

	require.NoError(t, a.SetLimit("d", 0))
	require.False(t, a.Spend("d", "s2", 99).Exceeded())
}

func TestWatchTriggers(t *testing.T) {
	t.Parallel()
	a, err := OpenAutomation(t.TempDir())
	require.NoError(t, err)
	now := time.Date(2026, 11, 3, 9, 0, 0, 0, time.UTC)
	a.now = func() time.Time { return now }
	kind := func(k string) *string { return &k }
	every := func(n int) *int { return &n }

	_, err = a.CreateTrigger("d", TriggerInput{Name: str("x"), Kind: kind(KindFile), Path: str("../outside"), Prompt: str("p")})
	require.Error(t, err, "paths must stay in the workspace")
	_, err = a.CreateTrigger("d", TriggerInput{Name: str("x"), Kind: kind(KindPage), URL: str("file:///etc/passwd"), Prompt: str("p")})
	require.Error(t, err, "pages must be http")
	_, err = a.CreateTrigger("d", TriggerInput{Name: str("x"), Kind: kind(KindPage), URL: str("https://example.com"), Every: every(1), Prompt: str("p")})
	require.Error(t, err, "pages are checked at most every five minutes")

	f, err := a.CreateTrigger("d", TriggerInput{Name: str("Gelen"), Kind: kind(KindFile), Path: str("inbox/*.txt"), Prompt: str("oku")})
	require.NoError(t, err)
	require.Equal(t, 1, f.Every)
	require.Empty(t, f.Token)

	require.Len(t, a.Watched(now), 1)
	require.Empty(t, a.Watched(now.Add(30*time.Second)), "not due again before its interval")
	require.Len(t, a.Watched(now.Add(time.Minute)), 1)

	require.False(t, a.Observe("d", f.ID, "a"), "the first look sets the baseline")
	require.False(t, a.Observe("d", f.ID, "a"))
	require.True(t, a.Observe("d", f.ID, "b"))

	// A new pattern forgets the old baseline.
	got, err := a.UpdateTrigger("d", f.ID, TriggerInput{Path: str("notes")})
	require.NoError(t, err)
	require.Empty(t, got.Fingerprint)
	require.False(t, a.Observe("d", f.ID, "c"))
}
