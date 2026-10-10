package integrations

import (
	"context"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/i18n"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/localapps"
	"github.com/stretchr/testify/require"
)

func TestLiveAppHealthFailureAndRecovery(t *testing.T) {
	t.Parallel()
	s := New(Options{})
	t.Cleanup(s.Shutdown)
	result := CheckResult{Status: StatusConnectionError, Err: i18n.Raw("HTTP 500")}
	a := &fakeAdapter{
		items: []Item{{ID: "app:probe", Kind: KindApp, Status: StatusReady, Live: true, Actions: []Action{ActionTest}}},
		parts: map[string]any{},
		check: func(context.Context, Item) CheckResult { return result },
	}
	s.Add(a)
	it, err := s.Check(t.Context(), "app:probe")
	require.NoError(t, err)
	require.Equal(t, StatusConnectionError, it.Status)
	require.Contains(t, it.LastError.String(), "HTTP 500")
	it, ok := s.Get(t.Context(), "app:probe")
	require.True(t, ok)
	require.Equal(t, StatusConnectionError, it.Status)
	result = CheckResult{Status: StatusReady, Detail: i18n.Raw("health passed")}
	it, err = s.Check(t.Context(), "app:probe")
	require.NoError(t, err)
	require.Equal(t, StatusReady, it.Status)
	require.True(t, it.LastError.IsZero())
	// A new source/process identity must not inherit the old health result.
	a.setPart("app:probe", "new source")
	a.items[0].Status = StatusUnchecked
	it, ok = s.Get(t.Context(), "app:probe")
	require.True(t, ok)
	require.Equal(t, StatusUnchecked, it.Status)
}

func TestChangedSourceAndUncheckedProcessAreNotReady(t *testing.T) {
	t.Parallel()
	for _, app := range []localapps.App{
		{Record: localapps.Record{State: localapps.StateReady}, SourceChanged: true},
		{Record: localapps.Record{State: localapps.StateVerified}, SourceChanged: true, Running: true},
		{Record: localapps.Record{State: localapps.StateReady}, Running: true},
	} {
		it := Item{}
		appStatus(&it, app)
		require.Equal(t, StatusUnchecked, it.Status)
	}
}
