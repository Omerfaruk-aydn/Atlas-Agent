package activity

import (
	"strings"
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

func TestIslandTextRespectsAnswerLimit(t *testing.T) {
	f, _ := formAt(Prompt{Kind: KindQuestion, Questions: []PromptQuestion{{ID: "t", Type: QuestionFreeText}}})
	f.insert(strings.Repeat("ş", MaxPromptText+50))
	require.Len(t, f.text[0], MaxPromptText)
}

func TestIslandMotionIsTimeBasedAndInterruptible(t *testing.T) {
	compact := islandRect{CX: 960, Top: 20, W: 300, H: 38, R: 6}
	expanded := islandRect{CX: 960, Top: 20, W: 560, H: 420, R: 18}
	var m islandMotion
	m.snap(compact)
	start := time.Unix(0, 0)
	m.retarget(expanded, start, true, false)
	mid, done := m.at(start.Add(120 * time.Millisecond))
	require.False(t, done)
	require.Greater(t, mid.H, compact.H)
	require.Less(t, mid.H, expanded.H*1.01, "No visible overshoot")
	// The same elapsed time gives the same frame at any refresh rate.
	again, _ := m.at(start.Add(120 * time.Millisecond))
	require.Equal(t, mid, again)
	// Cancel mid-flight: the collapse starts from the visible geometry.
	m.retarget(compact, start.Add(120*time.Millisecond), false, false)
	first, _ := m.at(start.Add(120 * time.Millisecond))
	require.InDelta(t, mid.H, first.H, .001)
	end, done := m.at(start.Add(120*time.Millisecond + islandClose))
	require.True(t, done)
	require.Equal(t, compact, end)
	for _, tick := range []time.Duration{0, 50, 100, 200, 300, 340} {
		frame, _ := (&islandMotion{from: compact, to: expanded, start: start, duration: islandOpen, omega: islandOpenOmega, ready: true}).at(start.Add(tick * time.Millisecond))
		require.LessOrEqual(t, frame.H, expanded.H*1.005)
		require.GreaterOrEqual(t, frame.H, compact.H)
	}
}

func TestIslandContentOrder(t *testing.T) {
	compact, expanded := islandOpacities(0)
	require.Equal(t, 1.0, compact)
	require.Zero(t, expanded)
	compact, expanded = islandOpacities(.4)
	require.Zero(t, compact, "Compact content is gone before expanded content arrives")
	require.Zero(t, expanded)
	_, expanded = islandOpacities(1)
	require.Equal(t, 1.0, expanded)
}
