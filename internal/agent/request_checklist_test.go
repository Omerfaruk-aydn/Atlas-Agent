package agent

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRequestItems(t *testing.T) {
	t.Parallel()
	require.Equal(t, []string{"add a login page", "make it work on phones", "keep the tests green"},
		requestItems("Please do the following:\n- add a login page\n2) make it work on phones\n* keep the tests green\nThanks"))
	require.Equal(t, []string{"fix the parser", "add tests for quotes"},
		requestItems("1. fix the parser 2. add tests for quotes"))
	require.Nil(t, requestItems("Fix the bug in the parser and add a test."), "prose is not split")
	require.Nil(t, requestItems("- only one item"))
	require.Nil(t, requestItems("version 2. then 5. later"), "numbers out of sequence are not a list")
}

func TestUnmetRequirementsBecomeFindings(t *testing.T) {
	t.Parallel()
	pass, findings, ok := parseEvaluatorVerdict("REQUIREMENTS:\n- [met] login page\n- [missing] dark mode\n- [partial] phone layout\nVERDICT: FAIL")
	require.True(t, ok)
	require.False(t, pass)
	require.Equal(t, "- Requirement missing: dark mode\n- Requirement partial: phone layout", findings)

	pass, _, ok = parseEvaluatorVerdict("REQUIREMENTS:\n- [met] login page\nVERDICT: PASS")
	require.True(t, ok)
	require.True(t, pass)
}

func TestConfirmChecklistAsksOnce(t *testing.T) {
	t.Parallel()
	a := &sessionAgent{}
	ledger := &evaluatorLedger{request: "- add search\n- add paging"}
	decision, stop := a.confirmChecklist("s1", ledger)
	require.True(t, stop)
	require.Contains(t, decision.Continue, "1. add search\n2. add paging")
	_, stop = a.confirmChecklist("s1", ledger)
	require.False(t, stop)

	_, stop = a.confirmChecklist("s1", &evaluatorLedger{request: "fix the bug"})
	require.False(t, stop)
}

func TestRequestChecklistBlockShowsOriginalAfterCompaction(t *testing.T) {
	t.Parallel()
	a := newLedgerAgent()
	a.evaluatorLedgers.Set("s1", &evaluatorLedger{request: "- one\n- two"})
	block := a.requestChecklistBlock("s1")
	require.Contains(t, block, "<request_checklist>")
	require.NotContains(t, block, "<original_request>")

	a.markCompacted("s1")
	require.Contains(t, a.requestChecklistBlock("s1"), "<original_request>\nThe conversation was summarized during this request. The user's request, word for word:\n- one\n- two\n</original_request>")
	require.Empty(t, a.requestChecklistBlock("unknown"))
}
