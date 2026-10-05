package activity

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestClearSessionIncludesBrowserManagers(t *testing.T) {
	native := New(&testRenderer{})
	defer native.Close()
	browser := New(&testRenderer{})
	defer browser.Close()
	native.Begin(context.Background(), Event{Session: "one", Resource: "desktop"})
	browser.Begin(context.Background(), Event{Session: "one", Resource: "browser"})
	ClearSession("one")
	require.False(t, native.Snapshot().Visible)
	require.False(t, browser.Snapshot().Visible)
	require.True(t, browser.Snapshot().FinishedUntil.IsZero())
}

func TestClearedOperationCannotReviveOnFinish(t *testing.T) {
	m := New(&testRenderer{})
	defer m.Close()
	finish := m.Begin(context.Background(), Event{Session: "one", Action: "click"})
	m.Clear("one")
	finish()
	require.False(t, m.Snapshot().Visible)
	require.True(t, m.Snapshot().FinishedUntil.IsZero())
}
