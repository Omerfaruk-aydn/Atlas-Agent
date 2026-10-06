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

func TestIslandMultiQuestionProgressAndAnswers(t *testing.T) {
	p := Prompt{Kind: KindQuestion, ID: "batch", Questions: []PromptQuestion{
		{ID: "pick", Type: QuestionMultiChoice, Choices: []PromptChoice{{ID: "x"}, {ID: "y"}, {ID: "z"}}},
		{ID: "ok", Type: QuestionYesNo},
		{ID: "why", Type: QuestionFreeText},
	}}
	f, now := formAt(p)
	require.False(t, f.enabled(islandControl{kind: controlNext}), "Next waits for an answer")
	f.key(keySpace, now)
	f.key(keyDown, now)
	f.key(keyDown, now)
	f.key(keySpace, now)
	require.True(t, f.isSelected("x"))
	require.True(t, f.isSelected("z"))
	f.focusPrimary()
	f.key(keyEnter, now)
	require.Equal(t, 1, f.page)
	f.key(keyDown, now)
	f.key(keyEnter, now)
	require.False(t, *f.yes[1])
	f.key(keyEnter, now)
	require.Equal(t, 2, f.page)
	for _, c := range f.controls() {
		require.NotEqual(t, controlNext, c.kind, "The last page submits")
	}
	f.focus = 0
	f.insert("Merhaba, çğışöü — 日本語\r\nikinci satır\x07")
	f.key(keyLeft, now)
	f.key(keyBackspace, now)
	require.Equal(t, "Merhaba, çğışöü — 日本語\nikinci satr", string(f.text[2]))
	f.key(keyEnter, now)
	r, sent := f.key(keyEnter, now)
	require.True(t, sent)
	require.Equal(t, []string{"x", "z"}, r.Answers[0].Selected)
	require.False(t, *r.Answers[1].Yes)
	require.Equal(t, "Merhaba, çğışöü — 日本語\nikinci satr", r.Answers[2].Text)
	require.True(t, p.accepts(r), "The island only produces responses its request accepts")
}
