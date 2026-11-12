package design

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEvalTable(t *testing.T) {
	t.Parallel()
	require.Len(t, DesignBriefs(), 10)
	data, err := os.ReadFile(filepath.Join("kit", "examples", "cop31-ops.json"))
	require.NoError(t, err)
	var ops []Op
	require.NoError(t, json.Unmarshal(data, &ops))
	doc, _, err := Apply(New("COP31 Companion"), "", ops)
	require.NoError(t, err)
	row, err := EvalDoc("cop31", doc)
	require.NoError(t, err)
	require.GreaterOrEqual(t, row.Score, 90)
	require.Equal(t, 0, row.Errors)
	rows := []EvalRow{row}
	for _, b := range DesignBriefs() {
		rows = append(rows, EvalRow{Name: b.ID, Note: "no design yet"})
	}
	table := FormatEval(rows)
	require.Contains(t, table, "cop31")
	require.Contains(t, table, "landing")
	t.Log("\n" + table)
}
