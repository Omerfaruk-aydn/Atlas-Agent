package codegraph

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func graphFixture(t *testing.T, text string) CodeGraph {
	t.Helper()
	graph, err := BuildGo(t.Context(), t.TempDir(), []Source{{Path: "fixture.go", Hash: digest(text), Content: []byte(text)}})
	require.NoError(t, err)
	return graph
}

func TestGraphResolvedReferencesAndImplementations(t *testing.T) {
	t.Parallel()
	graph := graphFixture(t, "package fixture\ntype Reader interface { Read() }\ntype Impl struct{}\nfunc (*Impl) Read() {}\nfunc Consume(r Reader) { r.Read() }\nfunc Main() { x := &Impl{}; x.Read() }\n")
	kinds := map[string]int{}
	for _, edge := range graph.Edges {
		kinds[edge.Kind]++
	}
	require.Positive(t, kinds["references"])
	require.Positive(t, kinds["implements"], "Pointer receiver implementations must be resolved")
	require.Equal(t, 1, kinds["calls"], "Dynamic interface calls cannot be exact calls")
	require.True(t, graph.Partial)
}

func TestGraphPaginationRejectsStaleCursor(t *testing.T) {
	t.Parallel()
	text := "package fixture\n"
	for i := range 60 {
		text += fmt.Sprintf("func F%d() {}\n", i)
	}
	graph := graphFixture(t, text)
	page, err := Symbols(t.Context(), graph, Query{})
	require.NoError(t, err)
	require.Len(t, page.Nodes, 50)
	require.NotEmpty(t, page.Cursor)
	last, err := Symbols(t.Context(), graph, Query{Cursor: page.Cursor})
	require.NoError(t, err)
	require.Len(t, last.Nodes, 10)
	graph.SourceFingerprint = "changed"
	_, err = Symbols(t.Context(), graph, Query{Cursor: page.Cursor})
	require.Error(t, err)
}

func TestGraphPartialRefreshRetainsUnknownFiles(t *testing.T) {
	t.Parallel()
	graph := graphFixture(t, "package fixture\nimport \"missing.invalid/package\"\nfunc F() {}\n")
	require.True(t, graph.Partial)
	require.NotEmpty(t, graph.Gaps)
	require.NotEmpty(t, graph.Nodes)
	_, err := Impact(t.Context(), graph, graph.Nodes[0].ID, Query{Depth: 6})
	require.Error(t, err)
	_, err = Impact(t.Context(), graph, "not-a-node", Query{Depth: 1})
	require.Error(t, err)
}
