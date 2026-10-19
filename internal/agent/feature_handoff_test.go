package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-models/pkg/catwalk"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/message"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/shell"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/workplan"
	"github.com/stretchr/testify/require"
)

const handoffPlan = `{"goal":"solar system","verify":[{"name":"unit","command":"npm test"}],"features":[
{"id":"F1","title":"Scene","acceptance":"planets render","done":%s},
{"id":"F2","title":"Orbits","acceptance":"planets move along their orbits","done":false}]}`

func writeHandoffProject(t *testing.T, root string, f1Done bool) {
	t.Helper()
	done := "false"
	if f1Done {
		done = "true"
	}
	writeWorkPlan(t, root, strings.Replace(handoffPlan, "%s", done, 1))
}

func writeNotes(t *testing.T, root string, at time.Time) {
	t.Helper()
	path := filepath.Join(root, workplan.Dir, workplan.ProgressFile)
	require.NoError(t, os.WriteFile(path, []byte("F1 done: scene renders.\nNext: F2 orbits."), 0o644))
	require.NoError(t, os.Chtimes(path, at, at))
}

