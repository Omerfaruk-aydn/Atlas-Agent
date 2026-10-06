package activity

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func formAt(p Prompt) (*islandForm, time.Time) {
	opened := time.Unix(100, 0)
	return newIslandForm(&p, opened), opened.Add(islandInputGuard)
}

func TestIslandSelectionIsSeparateFromSubmission(t *testing.T) {
	f, now := formAt(questionFixture("s"))
	_, sent := f.key(keyDown, now)
	require.False(t, sent)
	_, sent = f.key(keyEnter, now)
	require.False(t, sent, "Enter on an option selects it only")
	require.True(t, f.isSelected("b"))
	c, _ := f.focused()
	require.Equal(t, controlSubmit, c.kind, "Selection moves focus to the send button")
	r, sent := f.key(keyEnter, now)
	require.True(t, sent)
	require.Equal(t, []string{"b"}, r.Answers[0].Selected)
	_, again := f.key(keyEnter, now)
	require.False(t, again, "Repeated Enter never sends twice")
	_, clicked := f.activate(islandControl{kind: controlSubmit})
	require.False(t, clicked, "A double click never sends twice")
}

func TestIslandIgnoresKeysInFlightWhenItOpens(t *testing.T) {
	p := permissionFixture("guard")
	f, now := formAt(p)
	_, sent := f.key(keyEnter, now.Add(-time.Millisecond))
	require.False(t, sent)
	_, sent = f.key(keyEnter, now)
	require.False(t, sent, "Nothing is focused on a permission, so Enter cannot approve")
	f.key(keyTab, now)
	c, _ := f.focused()
	require.Equal(t, DecisionDeny, c.decision, "The first focus stop is the safe decision")
}
