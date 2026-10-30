package agent

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/session"
	"github.com/stretchr/testify/require"
)

func candidates(kind string, n int) []contextCandidate {
	out := make([]contextCandidate, n)
	for i := range out {
		out[i] = contextCandidate{path: fmt.Sprintf("%s/%d.go", kind, i), kind: kind}
	}
	return out
}

// A broad ownership used to take every slot before a single test or caller
// was considered.
func TestBroadOwnershipCannotCrowdOutTestsAndCallers(t *testing.T) {
	t.Parallel()

	packet := &ContextPacket{}
	chosen := selectContext(3, candidates("src", 40), candidates("test", 6), candidates("caller", 5), packet)

	count := map[string]int{}
	for _, c := range chosen {
		count[c.kind]++
	}
	require.LessOrEqual(t, len(chosen), contextMaxSources-3-contextKnowledgeSlots)
	require.Equal(t, contextTestSlots, count["test"], "tests keep their reserved share")
	require.Equal(t, contextRelatedSlots, count["caller"], "callers keep their reserved share")
	require.NotZero(t, count["src"])

	// What did not fit is named, so the model knows to go and read it.
	require.True(t, packet.Truncated)
	omitted := strings.Join(packet.Gaps, "\n")
	require.Contains(t, omitted, "Omitted (source limit): 36 src file(s)")
	require.Contains(t, omitted, "2 test file(s)", "tests beyond the share are listed too, not crowded out by the source list")
}

func TestUnusedSharesGoBackToSource(t *testing.T) {
	t.Parallel()

	packet := &ContextPacket{}
	chosen := selectContext(0, candidates("src", 30), nil, nil, packet)
	require.Len(t, chosen, contextMaxSources-contextKnowledgeSlots, "with no tests or callers, source may use their slots")
}

func TestExcerptFollowsTheSymbolNotTheFileStart(t *testing.T) {
	t.Parallel()

	var b strings.Builder
	for i := range 400 {
		fmt.Fprintf(&b, "// filler line %d that says nothing relevant\n", i)
	}
	b.WriteString("func ReconcileInvoiceTotals(ctx context.Context) error {\n\treturn nil\n}\n")
	for i := range 100 {
		fmt.Fprintf(&b, "// trailing %d\n", i)
	}
	content := b.String()

	terms := termsFrom("Fix ReconcileInvoiceTotals so refunds are subtracted")
	require.Contains(t, terms, "reconcileinvoicetotals")

	text, start, end, cut := excerptAround(content, terms, contextSourceBytes)
	require.True(t, cut)
	require.Contains(t, text, "func ReconcileInvoiceTotals", "the symbol being changed is in the excerpt; the head of the file is not what matters")
	require.Greater(t, start, 1)
	require.GreaterOrEqual(t, end, start)
	require.LessOrEqual(t, len(text), contextSourceBytes)

	// No match falls back to the start; small files are kept whole.
	head, headStart, _, _ := excerptAround(content, nil, contextSourceBytes)
	require.Equal(t, 1, headStart)
	require.True(t, strings.HasPrefix(head, "// filler line 0"))
	whole, _, _, cut := excerptAround("package a\n", terms, contextSourceBytes)
	require.False(t, cut)
	require.Equal(t, "package a\n", whole)
}

func TestTaskTermsPreferDistinctiveIdentifiers(t *testing.T) {
	t.Parallel()

	terms := taskTerms(session.Todo{Content: "Update the handler so that parse_config rejects empty values", AcceptanceCriteria: []string{"ReadManifest returns an error"}})
	require.Contains(t, terms, "parse_config")
	require.Contains(t, terms, "readmanifest")
	require.NotContains(t, terms, "update", "generic words do not pick the excerpt")
}
