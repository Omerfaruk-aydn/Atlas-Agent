package outcome

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestInspectionWinsOverLateResponse(t *testing.T) {
	t.Parallel()
	var ledger Ledger
	ledger.Record(Record{OpID: "send", Phase: PhaseUnknown, Mutation: true})
	require.False(t, ledger.Resolve("send", PhaseDispatched))
	require.True(t, ledger.Resolve("send", PhaseFailed))
	ledger.Record(Record{OpID: "send", Phase: PhaseUnknown, Mutation: true})
	require.Len(t, ledger.Records, 1)
	require.Equal(t, PhaseFailed, ledger.Records[0].Phase)
	require.True(t, ledger.Records[0].Resolved)
	require.Empty(t, ledger.Unresolved())
}

func TestResolveAllLegacyDuplicates(t *testing.T) {
	t.Parallel()
	ledger := Ledger{Records: []Record{{OpID: "send", Phase: PhaseUnknown, Observed: true}, {OpID: "send", Phase: PhaseUnknown}}}
	require.Len(t, ledger.Unresolved(), 2)
	require.True(t, ledger.Resolve("send", PhaseApplied))
	require.Empty(t, ledger.Unresolved())
}

func TestDownstreamVerificationDoesNotOverrideUnverifiedDependency(t *testing.T) {
	t.Parallel()
	states := Resolve([]Requirement{{ID: "source", Phase: PhaseApplied}, {ID: "report", Phase: PhaseVerified, DependsOn: []string{"source"}}})
	require.Equal(t, StateWaiting, states[1].State)
}

func TestRunningMutationVoidsProofAndSurvivesRecordAging(t *testing.T) {
	t.Parallel()
	var ledger Ledger
	require.True(t, ledger.Add(Evidence{Source: "check", Kind: KindOutput, Subject: "workspace", Claim: "passed"}))
	ledger.Record(Record{OpID: "job", Phase: PhaseDispatched, Mutation: true, Subjects: []string{"workspace"}, At: 100})
	require.False(t, ledger.Supports(KindOutput, "workspace", "passed"))
	for i := range maxRecords + 1 {
		ledger.Record(Record{OpID: fmt.Sprintf("read-%d", i), Phase: PhaseApplied, At: 200})
	}
	require.Contains(t, ledger.Records, Record{OpID: "job", Phase: PhaseDispatched, Mutation: true, Subjects: []string{"workspace"}, At: 100})
	require.EqualValues(t, 100, ledger.LastMutationAt)
}

func TestReconciliationSurvivesAgingAndLateResponse(t *testing.T) {
	t.Parallel()
	var ledger Ledger
	ledger.Record(Record{OpID: "send", Phase: PhaseUnknown, Mutation: true})
	require.True(t, ledger.Resolve("send", PhaseFailed))
	for i := range maxRecords + 1 {
		ledger.Record(Record{OpID: fmt.Sprintf("read-%d", i), Phase: PhaseApplied})
	}
	ledger.Record(Record{OpID: "send", Phase: PhaseUnknown, Mutation: true})
	require.Empty(t, ledger.Unresolved(), "a late response must not revive an aged reconciliation")
	var restored Ledger
	raw, err := json.Marshal(ledger)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(raw, &restored))
	restored.Record(Record{OpID: "send", Phase: PhaseUnknown, Mutation: true})
	require.Empty(t, restored.Unresolved(), "restarting must preserve the same reconciliation")
}
