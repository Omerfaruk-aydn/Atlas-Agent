package engineering

import (
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/outcome"
	"github.com/stretchr/testify/require"
)

func TestExternalResolutionRequiresExactReadOnlyAdapterEvidence(t *testing.T) {
	t.Parallel()
	for _, phase := range []outcome.Phase{outcome.PhaseApplied, outcome.PhaseUnknown, outcome.PhaseDispatched} {
		for _, variant := range []string{"bound", "unrelated-resource", "different-result", "mutation", "stale", "unobserved", "overlapping", "other-session"} {
			t.Run(string(phase)+"/"+variant, func(t *testing.T) {
				t.Parallel()
				s := NewStore(t.TempDir())
				ctx := t.Context()
				require.NoError(t, s.Update(ctx, "s", func(st *State) error {
					effect := outcome.Record{OpID: "effect", Tool: "mcp_crm_update_ticket", External: true, Mutation: true, Phase: phase, Fingerprint: Hash(`{"ticket_id":42,"status":"closed"}`), At: 10}
					st.Outcomes.Record(effect)
					st.Operations = []Operation{{ID: "effect", Status: "completed"}, {ID: "read", Status: "completed", StartedAt: 11, FinishedAt: 12, OutcomeObserved: true, EvidenceHash: Hash("readback")}}
					read := outcome.Record{OpID: "read", Tool: "api_probe", Phase: outcome.PhaseVerified, At: 12}
					proof := outcome.Evidence{Source: "read", Kind: outcome.KindOutput, Subject: "effect", Claim: ExternalResultClaim(effect)}
					switch variant {
					case "unrelated-resource":
						proof.Subject = "another-operation"
					case "different-result":
						proof.Claim = "external-result:" + Hash("different requested change")
					case "mutation":
						read.Mutation = true
					case "stale":
						proof.StaleBy = "newer-change"
					case "unobserved":
						st.Operations[1].OutcomeObserved = false
					case "overlapping":
						st.Operations[1].StartedAt = 9
					}
					if variant != "other-session" {
						st.Outcomes.Record(read)
						st.Outcomes.Add(proof)
					}
					return nil
				}))
				err := s.ResolveObserved(ctx, "s", "effect", "read", "ticket 42 is closed")
				st, readErr := s.Read(ctx, "s")
				require.NoError(t, readErr)
				if variant == "bound" {
					require.NoError(t, err)
					require.Equal(t, outcome.PhaseVerified, st.Outcomes.Records[0].Phase)
					require.True(t, st.Outcomes.Records[0].Resolved)
					require.False(t, st.Outcomes.Records[1].Resolved, "checking one effect never consumes or promotes the observer")
				} else {
					require.Error(t, err)
					require.Equal(t, phase, st.Outcomes.Records[0].Phase)
					require.False(t, st.Outcomes.Records[0].Resolved)
				}
			})
		}
	}
}
