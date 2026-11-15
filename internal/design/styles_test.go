package design

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// styleBlocks returns the design-styles skill's shared block and each
// style's script by its heading word.
func styleBlocks(t *testing.T, skill string) (shared string, styles map[string]string) {
	t.Helper()
	data, err := os.ReadFile("../skills/builtin/" + skill + "/SKILL.md")
	require.NoError(t, err)
	styles = map[string]string{}
	for _, part := range strings.Split(string(data), "\n## ")[1:] {
		head, body, _ := strings.Cut(part, "\n")
		_, code, ok := strings.Cut(body, "```js\n")
		if !ok {
			continue
		}
		code, _, _ = strings.Cut(code, "```")
		name, _, _ := strings.Cut(head, ":")
		if name == "Shared" {
			shared = code
			continue
		}
		styles[name] = code
	}
	return shared, styles
}

func TestDesignStylesEveryStyleBuildsAnApp(t *testing.T) {
	t.Parallel()
	kit, _ := kitBlocks(t, "## Using it on a screen")
	shared, styles := styleBlocks(t, "design-styles")
	require.NotEmpty(t, shared)
	require.Len(t, styles, 10)
	plusShared, plus := styleBlocks(t, "design-styles-plus")
	require.NotEmpty(t, plusShared)
	require.Len(t, plus, 17)
	for name, code := range plus {
		styles[name] = plusShared + "\n" + code
	}
	var problems []string
	for name, code := range styles {
		res, err := RunScript(context.Background(), New(name), nil, kit+"\n"+shared+"\n"+code, ScriptOptions{Name: "s"})
		if err != nil {
			problems = append(problems, name+": "+err.Error())
			continue
		}
		for _, is := range SystemLint(res.Doc, &res.System, "") {
			if is.Severity == "error" || slices.Contains([]string{"app-chrome", "web-hero", "mixed-language", "raw-colour", "font-missing"}, is.Rule) {
				problems = append(problems, name+" "+is.Rule+": "+is.Message)
			}
		}
		screens := res.Doc.PageByRef("Screens")
		require.NotNil(t, screens, name)
		report, err := Lint(res.Doc, screens.ID, "")
		require.NoError(t, err, name)
		for _, is := range report.Issues {
			if is.Severity == "error" {
				problems = append(problems, name+" "+is.Rule+": "+is.Message)
			}
		}
		if dir := os.Getenv("KIT_PREVIEW"); dir != "" {
			svg, err := SVG(res.Doc, screens, nil)
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(filepath.Join(dir, "style-"+name+".svg"), []byte(svg), 0o644))
		}
	}
	require.Empty(t, problems)
}
