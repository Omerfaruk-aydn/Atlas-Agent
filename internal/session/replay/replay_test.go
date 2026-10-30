package replay

import (
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/message"
	"github.com/stretchr/testify/require"
)

func text(id string, role message.MessageRole, s string) message.Message {
	return message.Message{ID: id, Role: role, Parts: []message.ContentPart{message.TextContent{Text: s}}}
}

func call(id, tool, input string) message.Message {
	return message.Message{
		ID: id, Role: message.Assistant, Model: "m1",
		Parts: []message.ContentPart{message.ToolCall{ID: id + "-c", Name: tool, Input: input}},
	}
}

func result(id, callID string, isErr bool) message.Message {
	return message.Message{
		ID: id, Role: message.Tool,
		Parts: []message.ContentPart{message.ToolResult{ToolCallID: callID, Name: "x", Content: "ok", IsError: isErr}},
	}
}

func sample() []message.Message {
	return []message.Message{
		text("u1", message.User, "fix the bug"),
		call("a1", "view", `{"p":1}`),
		result("t1", "a1-c", false),
		call("a2", "edit", `{}`),
		result("t2", "a2-c", true),
		text("a3", message.Assistant, "done"),
	}
}

func TestTimelineMarksUnansweredToolCallsUnbranchable(t *testing.T) {
	t.Parallel()
	steps := BuildTimeline(sample())
	require.Len(t, steps, 6)
	require.True(t, steps[0].Branchable)
	require.False(t, steps[1].Branchable, "the call has no result yet")
	require.True(t, steps[2].Branchable)
	require.False(t, steps[3].Branchable)
	require.True(t, steps[4].Branchable)
	require.Equal(t, 1, steps[4].Errors)
	require.Equal(t, []string{"view"}, steps[1].Tools)
	require.Equal(t, "calls view", steps[1].Summary)
	require.Equal(t, 1, steps[0].Number)
}

func TestResolveStepByNumberIDAndPrefix(t *testing.T) {
	t.Parallel()
	msgs := sample()
	i, err := ResolveStep(msgs, "3")
	require.NoError(t, err)
	require.Equal(t, 2, i)
	i, err = ResolveStep(msgs, "t2")
	require.NoError(t, err)
	require.Equal(t, 4, i)
	i, err = ResolveStep(msgs, "a3")
	require.NoError(t, err)
	require.Equal(t, 5, i)

	_, err = ResolveStep(msgs, "0")
	require.Error(t, err)
	_, err = ResolveStep(msgs, "7")
	require.Error(t, err)
	_, err = ResolveStep(msgs, "nope")
	require.Error(t, err)
	_, err = ResolveStep(msgs, "")
	require.Error(t, err)
	_, err = ResolveStep(msgs, "a") // a1, a2, a3
	require.ErrorContains(t, err, "ambiguous")
}

func TestCheckBranchableSuggestsNextStep(t *testing.T) {
	t.Parallel()
	msgs := sample()
	require.NoError(t, CheckBranchable(msgs, 2))
	err := CheckBranchable(msgs, 1)
	require.ErrorContains(t, err, "branch at step 3")
	require.Error(t, CheckBranchable(msgs, 99))
}

func TestCompareFindsDivergenceToolsAndFiles(t *testing.T) {
	t.Parallel()
	a := Side{
		ID: "a", Messages: sample(), PromptTokens: 100, CompletionTokens: 10, Cost: 0.5,
		Files: map[string]string{"x.go": "1", "y.go": "same", "only_a.go": "z"},
	}
	bMsgs := append([]message.Message{}, sample()[:3]...)
	bMsgs = append(bMsgs, call("b2", "grep", `{}`), result("bt2", "b2-c", false), text("b3", message.Assistant, "other answer"))
	b := Side{
		ID: "b", Messages: bMsgs, PromptTokens: 150, CompletionTokens: 20, Cost: 0.25,
		Files: map[string]string{"x.go": "2", "y.go": "same", "only_b.go": "q"},
	}

	c := Compare(a, b)
	require.Equal(t, 3, c.Divergence.CommonSteps)
	require.Contains(t, c.Divergence.A, "calls edit")
	require.Contains(t, c.Divergence.B, "calls grep")

	require.Equal(t, []ToolDelta{{Name: "edit", A: 1}, {Name: "grep", B: 1}}, c.Tools)
	require.Equal(t, 1, c.A.ToolErrors)
	require.Equal(t, 0, c.B.ToolErrors)

	require.Equal(t, []string{"only_a.go"}, c.Files.OnlyA)
	require.Equal(t, []string{"only_b.go"}, c.Files.OnlyB)
	require.Equal(t, []string{"x.go"}, c.Files.Differ)
	require.Equal(t, []string{"y.go"}, c.Files.Same)

	require.Equal(t, "done", c.A.Outcome)
	require.Equal(t, "other answer", c.B.Outcome)
	require.Equal(t, []string{"m1"}, c.A.Models)
}

func TestCompareIdenticalAndEmpty(t *testing.T) {
	t.Parallel()
	c := Compare(Side{Messages: sample()}, Side{Messages: sample()})
	require.Equal(t, 6, c.Divergence.CommonSteps)
	require.Empty(t, c.Divergence.A)
	require.Empty(t, c.Divergence.B)
	require.Empty(t, c.Tools)

	c = Compare(Side{}, Side{})
	require.Zero(t, c.Divergence.CommonSteps)
	require.Empty(t, c.A.Outcome)
}

func TestTruncateIsRuneSafe(t *testing.T) {
	t.Parallel()
	require.Equal(t, "abc", truncate("abc", 5))
	got := truncate("çççççççç", 4)
	require.Equal(t, "ççç…", got)
}
