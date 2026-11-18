package agent

import (
	"context"
	"strings"
	"testing"

	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/stretchr/testify/require"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/message"
)

func TestReviewToolsAreReadOnly(t *testing.T) {
	t.Parallel()
	writers := []string{"bash", "edit", "multiedit", "write", "download", "fetch", "agentic_fetch", "agent", "delegate", "orchestrate", "debate", "job_kill", "job_output"}
	for _, name := range reviewTools {
		require.NotContains(t, writers, name, "a reviewer must not be handed %q", name)
	}
	require.True(t, reviewSubagent().ReadOnly)
	require.Equal(t, reviewTools, reviewSubagent().Tools)
	for _, name := range reviewTools {
		require.Contains(t, sideQuestionTools, name, "reviewTools should stay within what side questions may already use: %s", name)
	}
}

func TestNormalizeReviewInput(t *testing.T) {
	t.Parallel()
	_, err := normalizeReviewInput(ReviewInput{})
	require.Error(t, err)
	_, err = normalizeReviewInput(ReviewInput{Paths: []string{"  ", ""}})
	require.Error(t, err)

	in, err := normalizeReviewInput(ReviewInput{Task: "  fix it  ", Paths: []string{"a.go", " a.go ", "b.go"}})
	require.NoError(t, err)
	require.Equal(t, "fix it", in.Task)
	require.Equal(t, []string{"a.go", "b.go"}, in.Paths)

	many := make([]string, ReviewMaxPaths+1)
	for i := range many {
		many[i] = "f" + strings.Repeat("x", i) + ".go"
	}
	_, err = normalizeReviewInput(ReviewInput{Paths: many})
	require.Error(t, err)

	long, err := normalizeReviewInput(ReviewInput{Task: strings.Repeat("é", ReviewTaskMaxLength), Paths: []string{"a.go"}})
	require.NoError(t, err)
	require.LessOrEqual(t, len(long.Task), ReviewTaskMaxLength+len(" [truncated]"))
	require.True(t, strings.HasSuffix(long.Task, "[truncated]"))
}

func TestBuildReviewPromptNamesTheTaskAndTheFiles(t *testing.T) {
	t.Parallel()
	got := buildReviewPrompt(ReviewInput{Task: "add retries", Paths: []string{"a.go", "dir/b.go"}})
	require.Contains(t, got, "<task>\nadd retries\n</task>")
	require.Contains(t, got, "- a.go\n- dir/b.go\n")
	empty := buildReviewPrompt(ReviewInput{Paths: []string{"a.go"}})
	require.Contains(t, empty, "No task text was given")
}

func TestParseReviewReadsTheLastFencedObject(t *testing.T) {
	t.Parallel()
	text := "I looked at it.\n\n```json\n{\"summary\":\"draft\",\"findings\":[]}\n```\n\nOn reflection:\n\n```json\n" +
		`{"summary":"One real problem.","findings":[
		  {"severity":"Minor","title":"Typo in error","file":"a.go","line":"12","explanation":"x","suggestion":"y"},
		  {"severity":"blocker","title":"Index out of range","file":"b.go","line":42,"explanation":"Empty slice panics.","suggestion":"Check the length."},
		  {"severity":"weird","title":"Odd severity","file":"c.go","line":0,"explanation":"z"}
		]}` + "\n```\n"
	out := ParseReview(text)
	require.True(t, out.Structured)
	require.Equal(t, "One real problem.", out.Summary)
	require.Len(t, out.Findings, 3)
	// Worst first, ties in the order written.
	require.Equal(t, "critical", out.Findings[0].Severity)
	require.Equal(t, "Index out of range", out.Findings[0].Title)
	require.Equal(t, 42, out.Findings[0].Line)
	require.Equal(t, "medium", out.Findings[1].Severity)
	require.Equal(t, "low", out.Findings[2].Severity)
	require.Equal(t, 12, out.Findings[2].Line)
}

func TestParseReviewDropsMalformedAndDuplicateFindings(t *testing.T) {
	t.Parallel()
	text := "```json\n" + `{"summary":"s","findings":[
	  {"severity":"high","title":"","explanation":""},
	  {"severity":"high","title":"Same","file":"a.go","line":3,"explanation":"e"},
	  {"severity":"high","title":"same","file":"a.go","line":3,"explanation":"e again"},
	  {"severity":"low","title":"","file":"a.go","explanation":"only an explanation, which becomes the title"}
	]}` + "\n```"
	out := ParseReview(text)
	require.Len(t, out.Findings, 2)
	require.Equal(t, "Same", out.Findings[0].Title)
	require.Equal(t, "only an explanation, which becomes the title", out.Findings[1].Title)
}

func TestParseReviewAcceptsBareJSONAndEmptyFindings(t *testing.T) {
	t.Parallel()
	out := ParseReview(`{"summary":"Nothing to report; I read the diff and the callers.","findings":[]}`)
	require.True(t, out.Structured)
	require.Empty(t, out.Findings)
	require.Contains(t, out.Summary, "Nothing to report")
}

func TestParseReviewKeepsUnstructuredText(t *testing.T) {
	t.Parallel()
	for _, text := range []string{"The code looks fine to me.", "```json\n{not json}\n```", "```go\nfunc main() {}\n```"} {
		out := ParseReview(text)
		require.False(t, out.Structured, text)
		require.Equal(t, text, out.Summary)
		require.Empty(t, out.Findings)
	}
}

// An object that parses is not yet a review: without a findings array the
// answer is kept as text instead of passing for "no defects".
func TestParseReviewNeedsTheFindingsArray(t *testing.T) {
	t.Parallel()
	for _, text := range []string{`{}`, `{"error":"could not inspect the files"}`, `{"summary":"x"}`, `{"summary":"x","findings":null}`, `{"summary":"x","findings":"none"}`} {
		out := ParseReview(text)
		require.False(t, out.Structured, text)
		require.Equal(t, text, out.Summary)
		require.Empty(t, out.Findings)
	}
}

func TestParseReviewBoundsEveryField(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("é", 5000)
	text := "```json\n{\"summary\":\"" + long + "\",\"findings\":[{\"severity\":\"low\",\"title\":\"" + long + "\",\"file\":\"" + long + "\",\"line\":1,\"explanation\":\"" + long + "\",\"suggestion\":\"" + long + "\"}]}\n```"
	out := ParseReview(text)
	require.Len(t, out.Findings, 1)
	f := out.Findings[0]
	require.LessOrEqual(t, len(f.Title), maxFindingTitle+len("…"))
	require.LessOrEqual(t, len(f.File), maxFindingFile+len("…"))
	require.LessOrEqual(t, len(f.Explanation), maxFindingExplanation+len("…"))
	require.LessOrEqual(t, len(f.Suggestion), maxFindingSuggestion+len("…"))
	require.LessOrEqual(t, len(out.Summary), maxReviewSummary+len("…"))
}

func TestParseReviewCapsTheNumberOfFindings(t *testing.T) {
	t.Parallel()
	var items []string
	for i := 0; i < reviewMaxFindings+10; i++ {
		items = append(items, `{"severity":"low","title":"t`+strings.Repeat("x", i)+`","file":"a.go","line":1,"explanation":"e"}`)
	}
	out := ParseReview("```json\n{\"summary\":\"s\",\"findings\":[" + strings.Join(items, ",") + "]}\n```")
	require.Len(t, out.Findings, reviewMaxFindings)
}

func TestLineOf(t *testing.T) {
	t.Parallel()
	require.Equal(t, 7, lineOf(float64(7)))
	require.Equal(t, 0, lineOf(float64(-3)))
	require.Equal(t, 42, lineOf("42-50"))
	require.Equal(t, 9, lineOf("L9"))
	require.Equal(t, 0, lineOf("none"))
	require.Equal(t, 0, lineOf(nil))
}

func TestReviewRunsInAnEmptyScratchSessionAndLeavesNoTrace(t *testing.T) {
	const providerID = "test-provider"
	env := testEnv(t)
	coord := newTestCoordinator(t, env, providerID, config.ProviderConfig{ID: providerID})

	parent, err := env.sessions.Create(t.Context(), "Main")
	require.NoError(t, err)
	_, err = env.messages.Create(t.Context(), parent.ID, message.CreateMessageParams{
		Role:  message.User,
		Parts: []message.ContentPart{message.TextContent{Text: "a long private story about why the code is right"}},
	})
	require.NoError(t, err)

	var scratchID, seenPrompt string
	mock := newMockAgent(providerID, 4096, func(_ context.Context, call SessionAgentCall) (*fantasy.AgentResult, error) {
		scratchID, seenPrompt = call.SessionID, call.Prompt
		require.NotEqual(t, parent.ID, call.SessionID)
		require.True(t, call.NonInteractive)
		// The scratch session starts empty: nothing of the main chat is in it.
		msgs, err := env.messages.List(t.Context(), call.SessionID)
		require.NoError(t, err)
		require.Empty(t, msgs)
		return agentResultWithText("Looked.\n```json\n{\"summary\":\"ok\",\"findings\":[{\"severity\":\"high\",\"title\":\"Bug\",\"file\":\"a.go\",\"line\":3,\"explanation\":\"e\"}]}\n```"), nil
	})

	text, _, err := coord.runScratch(t.Context(), mock, scratchRun{ParentSessionID: parent.ID, Label: "review", Title: "Review", Prompt: buildReviewPrompt(ReviewInput{Task: "fix it", Paths: []string{"a.go"}}), Timeout: reviewTimeout})
	require.NoError(t, err)
	out := ParseReview(text)
	require.True(t, out.Structured)
	require.Len(t, out.Findings, 1)

	require.NotContains(t, seenPrompt, "private story", "the reviewer must not see the author's conversation")
	require.Contains(t, seenPrompt, "fix it")

	msgs, err := env.messages.List(t.Context(), parent.ID)
	require.NoError(t, err)
	require.Len(t, msgs, 1)
	_, err = env.sessions.Get(t.Context(), scratchID)
	require.Error(t, err)
	children, err := env.sessions.ListByParent(t.Context(), parent.ID)
	require.NoError(t, err)
	require.Empty(t, children)
}
