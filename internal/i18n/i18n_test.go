package i18n

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLocalesAndTranslations(t *testing.T) {
	t.Parallel()
	require.Len(t, Languages(), 6)
	for _, code := range []string{"en", "tr", "de", "fr", "it", "ar"} {
		require.True(t, Supported(code))
		require.NotEmpty(t, Text(code, "Language"))
	}
	require.False(t, Supported("xx"))
	require.Equal(t, "Dil", Text("tr", "Language"))
	require.Equal(t, "Sprache", Text("de", "Language"))
	require.Equal(t, "/language", Text("ar", "/language"))
	require.Equal(t, "user content", Text("tr", "user content"))
	require.Equal(t, "Language", Text("unknown", "Language"))
}

func TestTranslatorIsolationAndConcurrentChanges(t *testing.T) {
	t.Parallel()
	require.NoError(t, ValidateCatalogs())
	first, second := New("tr"), New("de")
	var readers sync.WaitGroup
	for range 8 {
		readers.Go(func() {
			for range 1000 {
				text := first.Text("Language")
				if text != "Dil" && text != "Lingua" {
					t.Errorf("Unexpected translation %q", text)
				}
				if text := second.Text("Language"); text != "Sprache" {
					t.Errorf("Independent translator changed to %q", text)
				}
			}
		})
	}
	for range 1000 {
		first.Set("it")
		first.Set("tr")
	}
	readers.Wait()
}

func TestCatalogRejectsBrokenContracts(t *testing.T) {
	t.Parallel()
	for _, data := range []string{
		"Label|Etiket",
		"Label|Etiket|Bezeichnung|Libellé|Etichetta|",
		"Value %s|Değer %d|Wert %s|Valeur %s|Valore %s|قيمة %s",
		"A|B|C|D|E|F\nA|B|C|D|E|F",
	} {
		_, err := readCatalogs(data)
		require.Error(t, err)
	}
}

// Verify every explicitly localized source literal has all six catalog cells.
func TestLocalizedCallSiteCoverage(t *testing.T) {
	t.Parallel()
	err := filepath.WalkDir("../ui", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return err
		}
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok || len(call.Args) != 1 {
				return true
			}
			localized := false
			switch method := call.Fun.(type) {
			case *ast.SelectorExpr:
				localized = method.Sel.Name == "Text"
			case *ast.Ident:
				localized = method.Name == "text" && strings.Contains(filepath.Base(path), "workflow_")
			}
			if !localized {
				return true
			}
			literal, ok := call.Args[0].(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING {
				return true
			}
			source, err := strconv.Unquote(literal.Value)
			require.NoError(t, err)
			for _, language := range Languages() {
				require.True(t, Has(language.Code, source), "%s: missing %s translation for %q", path, language.Code, source)
			}
			return true
		})
		return nil
	})
	require.NoError(t, err)
}

func TestCatalogIntegrity(t *testing.T) {
	t.Parallel()
	require.NoError(t, ValidateCatalogs())
}

func TestCatalogMarkdownAndLiteralPercentages(t *testing.T) {
	t.Parallel()
	require.Equal(t, "### Hata:", Text("tr", "### Error:"))
	require.Empty(t, formatVerbs("80% -- empty resets to built-in"))
	require.Equal(t, []string{"%s", "%d", "%%"}, formatVerbs("%s: %d (%%)"))
}
