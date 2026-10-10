package remotecontrol

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/i18n"
	"github.com/stretchr/testify/require"
)

// Every Msg literal in this package is shown in /rc, so each one needs all
// six catalog translations.
func TestEveryMessageIsTranslated(t *testing.T) {
	t.Parallel()
	entries, err := os.ReadDir(".")
	require.NoError(t, err)
	count := 0
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), e.Name(), nil, 0)
		require.NoError(t, err)
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) == 0 {
				return true
			}
			if ident, ok := call.Fun.(*ast.Ident); !ok || ident.Name != "Msg" {
				return true
			}
			lit, ok := call.Args[0].(*ast.BasicLit)
			require.True(t, ok, "%s: Msg must take a string literal", e.Name())
			key, err := strconv.Unquote(lit.Value)
			require.NoError(t, err)
			for _, lang := range i18n.Languages() {
				require.True(t, i18n.Has(lang.Code, key), "%s: missing %s translation for %q", e.Name(), lang.Code, key)
			}
			count++
			return true
		})
	}
	require.Greater(t, count, 100)
	require.Equal(t, "Bağlantıyı test et", Msg("Test connection").Localize(func(s string) string { return i18n.Text("tr", s) }))
	require.Equal(t, "Port 22 is already used by another service; Atlas will not stop it.", Msg("Port %s is already used by another service; Atlas will not stop it.", "22").String())
}
