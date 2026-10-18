package agent

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWeakeningFindingsCatchesLoosenedTests(t *testing.T) {
	t.Parallel()
	before := "func TestA(t *testing.T) {\n\trequire.Equal(t, 1, f())\n\tassert.True(t, g())\n}\n\nfunc TestB(t *testing.T) {\n\trequire.NoError(t, h())\n}\n"
	after := "func TestA(t *testing.T) {\n\tt.Skip(\"later\")\n\trequire.Equal(t, 1, f())\n}\n"
	findings := weakeningFindings(fileChange{path: "pkg/a_test.go", before: before, after: after, existed: true, exists: true})
	joined := strings.Join(findings, "\n")
	require.Contains(t, joined, "test cases went from 2 to 1")
	require.Contains(t, joined, "assertions went from 3 to 1")
	require.Contains(t, joined, "new skip")
}

func TestWeakeningFindingsCatchesSuppressionsCoverageAndDeletion(t *testing.T) {
	t.Parallel()
	findings := weakeningFindings(fileChange{path: "src/app.ts", before: "const a = 1\n", after: "// @ts-ignore\nconst a = 1\n", existed: true, exists: true})
	require.Len(t, findings, 1)
	require.Contains(t, findings[0], "suppression")

	findings = weakeningFindings(fileChange{path: "pyproject.toml", before: "fail_under = 90\n", after: "fail_under = 60\n", existed: true, exists: true})
	require.Len(t, findings, 1)
	require.Contains(t, findings[0], "lowered from 90 to 60")

	findings = weakeningFindings(fileChange{path: "tests/test_api.py", before: "def test_ok():\n    assert f() == 1\n", existed: true})
	require.Len(t, findings, 1)
	require.Contains(t, findings[0], "test file deleted")

	require.Empty(t, weakeningFindings(fileChange{path: "new_test.go", after: "func TestX(t *testing.T) {}", exists: true}))
}

func TestWeakeningFindingsIgnoresStrongerTests(t *testing.T) {
	t.Parallel()
	before := "it('works', () => {\n  expect(f()).toBe(1)\n})\n"
	after := before + "it('handles empty', () => {\n  expect(f('')).toBe(0)\n})\n"
	require.Empty(t, weakeningFindings(fileChange{path: "src/f.test.js", before: before, after: after, existed: true, exists: true}))
}

func TestHygieneFindings(t *testing.T) {
	t.Parallel()
	secret := "sk-" + strings.Repeat("a", 32)
	after := strings.Join([]string{
		"package main",
		"<<<<<<< HEAD",
		"var key = \"" + secret + "\"",
		"// TODO: handle errors",
		"// x := compute()",
		"// if x > 0 {",
		"//     return x",
		"// }",
		"func main() {}",
	}, "\n")
	findings := hygieneFindings(fileChange{path: "main.go", before: "package main\n", after: after, existed: true, exists: true})
	joined := strings.Join(findings, "\n")
	require.Contains(t, joined, "main.go:2: merge conflict marker")
	require.Contains(t, joined, "main.go:3: looks like a API key")
	require.NotContains(t, joined, secret, "a finding never repeats the secret")
	require.Contains(t, joined, "1 new TODO/FIXME marker(s) at line 4")
	require.Contains(t, joined, "commented-out code")
}

func TestHygieneFindingsDebugStatementsRespectTheFileStyle(t *testing.T) {
	t.Parallel()
	findings := hygieneFindings(fileChange{path: "src/a.js", before: "let a = 1\n", after: "let a = 1\nconsole.log(a)\ndebugger;\n", existed: true, exists: true})
	require.Len(t, findings, 2)

	// A file that already logs to the console keeps doing so unflagged, and
	// a new script may print.
	require.Empty(t, hygieneFindings(fileChange{path: "src/b.js", before: "console.log(1)\n", after: "console.log(1)\nconsole.log(2)\n", existed: true, exists: true}))
	require.Empty(t, hygieneFindings(fileChange{path: "scripts/c.js", after: "console.log('done')\n", exists: true}))

	// Placeholders are not secrets.
	require.Empty(t, hygieneFindings(fileChange{path: "config.py", after: "API_KEY = \"your-api-key-here-please\"\n", exists: true}))
}

func TestHygieneFindingsFlagsLargeNewFiles(t *testing.T) {
	t.Parallel()
	findings := hygieneFindings(fileChange{path: "dist/bundle.js", exists: true, size: 2 << 20})
	require.Len(t, findings, 1)
	require.Contains(t, findings[0], "new file of 2048 KiB")
}

func TestPlanWeakening(t *testing.T) {
	t.Parallel()
	before := `{"verify":[{"command":"make test"}],"features":[{"id":"F1","title":"a","verify":"./check.sh","done":true}]}`
	after := `{"verify":[{"command":"make build"}],"features":[{"id":"F1","title":"a","done":true}]}`
	findings := planWeakening(fileChange{path: ".atlas/work.json", before: before, after: after, existed: true, exists: true})
	require.Len(t, findings, 2)
	require.Contains(t, findings[0], "`make test` was removed or changed")
	require.Contains(t, findings[1], "F1 lost its check")

	stronger := `{"verify":[{"command":"make test"},{"command":"make lint"}],"features":[{"id":"F1","title":"a","verify":"./check.sh","done":true}]}`
	require.Empty(t, planWeakening(fileChange{path: ".atlas/work.json", before: before, after: stronger, existed: true, exists: true}))
}

func TestDiffGuardPromptListsBothKinds(t *testing.T) {
	t.Parallel()
	prompt := diffGuardPrompt([]string{"a_test.go: assertions went from 3 to 1"}, []string{"main.go:2: merge conflict marker"})
	require.Contains(t, prompt, "Checks that got weaker:\n- a_test.go")
	require.Contains(t, prompt, "Left in the change:\n- main.go:2")
	require.Contains(t, prompt, "Never loosen a check")
}

func TestAddedLinesCountsDuplicates(t *testing.T) {
	t.Parallel()
	added := addedLines("a\nb\n", "a\nb\nb\nc\n")
	require.Equal(t, []numberedLine{{n: 3, text: "b"}, {n: 4, text: "c"}}, added)
}
