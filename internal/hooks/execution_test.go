package hooks

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/execution"
	"github.com/stretchr/testify/require"
)

func TestExecutionHooksAndBuiltins(t *testing.T) {
	root := t.TempDir()
	ctx := execution.WithBinding(t.Context(), execution.Binding{Root: root, Store: engineering.NewStore(t.TempDir()), Factory: func(context.Context) (execution.Runner, error) { return nil, fmt.Errorf("runtime unavailable") }})
	r := NewRunner([]config.HookConfig{{Command: "printf escaped > escaped.txt"}}, root, root)
	result, err := r.Run(ctx, EventPreToolUse, "session", "bash", "{}")
	require.NoError(t, err)
	require.Equal(t, DecisionDeny, result.Decision, "Required isolation failure must block the tool")
	_, err = os.Stat(filepath.Join(root, "escaped.txt"))
	require.True(t, os.IsNotExist(err), "Shell builtins must not write on the host")
}
