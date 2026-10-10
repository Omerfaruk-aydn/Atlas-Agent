package integrations

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/i18n"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/localapps"
	"github.com/stretchr/testify/require"
)

// TestMessagesAreTranslated keeps every catalog literal this package shows
// to users translated in every interface language.
func TestMessagesAreTranslated(t *testing.T) {
	t.Parallel()
	files, err := filepath.Glob("*.go")
	require.NoError(t, err)
	var keys []string
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, err := os.ReadFile(f)
		require.NoError(t, err)
		file, err := parser.ParseFile(token.NewFileSet(), f, src, 0)
		require.NoError(t, err)
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) == 0 {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "M" {
				return true
			}
			if pkg, ok := sel.X.(*ast.Ident); !ok || pkg.Name != "i18n" {
				return true
			}
			if lit, ok := call.Args[0].(*ast.BasicLit); ok && lit.Kind == token.STRING {
				s, err := strconv.Unquote(lit.Value)
				require.NoError(t, err)
				keys = append(keys, s)
			}
			return true
		})
	}
	require.NotEmpty(t, keys)
	// Runtime remedies are passed through i18n.M as variables.
	for _, rt := range localapps.SupportedRuntimes() {
		keys = append(keys, rt.Remedy)
	}
	for _, k := range keys {
		for _, lang := range i18n.Languages() {
			require.True(t, i18n.Has(lang.Code, k), "missing %s translation for %q", lang.Code, k)
		}
	}
}
