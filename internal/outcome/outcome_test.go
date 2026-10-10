package outcome

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestClassifySeparatesReturnFromDone(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   Result
		want Phase
	}{
		{"read ok", Result{}, PhaseApplied},
		{"mutation ok", Result{Mutation: true}, PhaseApplied},
		{"observed check", Result{Observed: true}, PhaseVerified},
		{"refused before send", Result{Rejected: true, Mutation: true}, PhaseFailed},
		{"rejected after send", Result{Rejected: true, Mutation: true, Sent: true}, PhaseUnknown},
		{"timeout mutation", Result{Err: errors.New("deadline"), Mutation: true}, PhaseUnknown},
		{"timeout read", Result{Err: errors.New("deadline")}, PhaseFailed},
		{"canceled mutation", Result{Canceled: true, Mutation: true}, PhaseUnknown},
		{"canceled read", Result{Canceled: true}, PhaseCanceled},
		{"question", Result{Waiting: true, Mutation: true}, PhaseBlocked},
		{"question with unknown effect", Result{Waiting: true, Mutation: true, Sent: true, DispatchKnown: true, Uncertain: true}, PhaseUnknown},
		{"question after known send", Result{Waiting: true, Mutation: true, Sent: true, DispatchKnown: true}, PhaseBlocked},
		{"unknown effect without error", Result{Mutation: true, Sent: true, DispatchKnown: true, Uncertain: true}, PhaseUnknown},
	}
	for _, tt := range tests {
		require.Equal(t, tt.want, Classify(tt.in), tt.name)
	}
}

func TestMutationVoidsOutputEvidenceButNotObservations(t *testing.T) {
	t.Parallel()
	var l Ledger
	require.False(t, l.Add(Evidence{Kind: KindOutput, Subject: "a.txt"}), "claim and source are required")
	require.True(t, l.Add(Evidence{ID: "1", Kind: KindOutput, Source: "op1", Subject: "a.txt", Claim: "content ok"}))
	require.True(t, l.Add(Evidence{ID: "2", Kind: KindObservation, Source: "op1", Subject: "a.txt", Claim: "content ok"}))
	require.True(t, l.Supports(KindOutput, "a.txt", "content ok"))

	l.Record(Record{OpID: "op2", Tool: "edit", Phase: PhaseFailed, Mutation: true, Subjects: []string{"a.txt"}})
	require.True(t, l.Supports(KindOutput, "a.txt", "content ok"), "a failed write changed nothing")

	l.Record(Record{OpID: "op3", Tool: "edit", Phase: PhaseApplied, Mutation: true, Subjects: []string{"b.txt"}})
	require.True(t, l.Supports(KindOutput, "a.txt", "content ok"), "other subject")

	l.Record(Record{OpID: "op4", Tool: "edit", Phase: PhaseUnknown, Mutation: true, Subjects: []string{"a.txt"}})
	require.False(t, l.Supports(KindOutput, "a.txt", "content ok"))
	require.Equal(t, "op4", l.Evidence[0].StaleBy)
	require.Empty(t, l.Evidence[1].StaleBy, "history is kept")
	require.Len(t, l.Unresolved(), 1)

	require.True(t, l.Resolve("op4", PhaseApplied))
	require.False(t, l.Resolve("op4", PhaseApplied))
	require.Empty(t, l.Unresolved())
}

func TestUnresolvedRecordsSurviveAging(t *testing.T) {
	t.Parallel()
	var l Ledger
	l.Record(Record{OpID: "keep", Phase: PhaseUnknown, Mutation: true})
	for range maxRecords * 2 {
		l.Record(Record{OpID: "x", Phase: PhaseApplied})
	}
	require.LessOrEqual(t, len(l.Records), maxRecords+1)
	require.Len(t, l.Unresolved(), 1)
}

func TestDependenciesBlockWithoutToolCalls(t *testing.T) {
	t.Parallel()
	reqs := []Requirement{
		{ID: "build", Phase: PhaseFailed, Reason: "compile error"},
		{ID: "accept", DependsOn: []string{"build"}, Phase: PhasePlanned},
		{ID: "docs", Phase: PhasePlanned},
		{ID: "file", Phase: PhaseApplied},
		{ID: "readback", DependsOn: []string{"file"}, Phase: PhasePlanned},
	}
	got := map[string]State{}
	for _, r := range Resolve(reqs) {
		got[r.ID] = r.State
	}
	require.Equal(t, StateFailed, got["build"])
	require.Equal(t, StateBlockedDep, got["accept"])
	require.Equal(t, StateReady, got["docs"])
	require.Equal(t, StateWaiting, got["readback"], "applied output is not verified output")
	require.Equal(t, []string{"docs", "file"}, Executable(reqs), "applied work still needs its check")

	reqs[3].Phase = PhaseVerified
	require.Contains(t, Executable(reqs), "readback", "unblocked once the upstream is verified")
}

func TestResolveCycleAndUnknownDependency(t *testing.T) {
	t.Parallel()
	got := Resolve([]Requirement{
		{ID: "a", DependsOn: []string{"b"}, Phase: PhasePlanned},
		{ID: "b", DependsOn: []string{"a"}, Phase: PhasePlanned},
		{ID: "c", DependsOn: []string{"missing"}, Phase: PhasePlanned},
	})
	for _, r := range got {
		require.NotEqual(t, StateReady, r.State, r.ID)
	}
}

func TestReportNeverCalledCompleteWhileWorkIsOpen(t *testing.T) {
	t.Parallel()
	rep := Summarize([]Requirement{
		{ID: "r1", Description: "write file", Phase: PhaseVerified},
		{ID: "r2", Description: "rename file", Phase: PhaseApplied},
		{ID: "r3", Description: "upload", Phase: PhaseBlocked, Reason: "no network"},
		{ID: "r4", Description: "notify", Phase: PhaseUnknown},
		{ID: "r5", Description: "report", DependsOn: []string{"r3"}, Phase: PhasePlanned},
	})
	require.False(t, rep.Complete())
	require.Len(t, rep.Verified, 1)
	require.Len(t, rep.Unverified, 1)
	require.Len(t, rep.Unknown, 1)
	require.Len(t, rep.Blocked, 2)
	block := rep.Block()
	require.Contains(t, block, "no network")
	require.Contains(t, block, "never resend")
	require.Contains(t, block, "Do not describe this work as complete")

	all := Summarize([]Requirement{{ID: "r1", Phase: PhaseVerified}})
	require.True(t, all.Complete())
	require.Empty(t, all.Block(), "nothing to qualify when everything is verified")
	require.Empty(t, Summarize(nil).Block())
}

func TestUnknownMutationBlocksIdenticalResendUntilObserved(t *testing.T) {
	t.Parallel()
	var l Ledger
	l.Record(Record{OpID: "op1", Tool: "mcp_x_send", Phase: PhaseUnknown, Mutation: true, Fingerprint: "fp"})
	_, ok := l.UnresolvedCall("fp")
	require.True(t, ok)
	_, ok = l.UnresolvedCall("other")
	require.False(t, ok)
	_, ok = l.UnresolvedCall("")
	require.False(t, ok)
	require.Len(t, l.Requirements(), 1)
	require.Contains(t, Summarize(l.Requirements()).Block(), "Outcome unknown")

	l.MarkObserved("op1")
	_, ok = l.UnresolvedCall("fp")
	require.True(t, ok, "legacy observation cannot establish the effect")
	require.Len(t, l.Requirements(), 1)
	require.True(t, l.Resolve("op1", PhaseFailed))
	require.Empty(t, l.Requirements())
}

func TestWildcardSubjectVoidsAllOutputEvidence(t *testing.T) {
	t.Parallel()
	var l Ledger
	l.Add(Evidence{Kind: KindOutput, Source: "s", Subject: "a", Claim: "c"})
	l.Add(Evidence{Kind: KindAuthority, Source: "s", Subject: "w", Claim: "c"})
	l.Record(Record{OpID: "op", Phase: PhaseApplied, Mutation: true, Subjects: []string{"*"}})
	require.False(t, l.Supports(KindOutput, "a", "c"))
	require.False(t, l.Supports(KindAuthority, "w", "c"))
}
