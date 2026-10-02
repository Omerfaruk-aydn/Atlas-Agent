package shell

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/execution"
	"github.com/stretchr/testify/require"
)

func TestExecutionRequiredBuiltinCannotEscape(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	ctx := execution.WithBinding(t.Context(), execution.Binding{Root: root, Store: engineering.NewStore(t.TempDir()), Factory: func(context.Context) (execution.Runner, error) { return nil, fmt.Errorf("isolation unavailable") }})
	require.Error(t, Run(ctx, RunOptions{Cwd: root, Command: "printf escaped > escaped.txt"}))
	_, err := os.Stat(filepath.Join(root, "escaped.txt"))
	require.True(t, os.IsNotExist(err))
	require.NoError(t, Run(t.Context(), RunOptions{Cwd: root, Command: "printf legacy > legacy.txt"}))
	data, err := os.ReadFile(filepath.Join(root, "legacy.txt"))
	require.NoError(t, err)
	require.Equal(t, "legacy", string(data))
}

func TestExecutionCommandPolicyBeforeContainer(t *testing.T) {
	t.Parallel()
	blocks := []BlockFunc{CommandsBlocker([]string{"curl"})}
	require.Error(t, checkIsolatedScript("printf safe; curl example.com", blocks))
	require.Error(t, checkIsolatedScript("$COMMAND example.com", blocks))
	require.NoError(t, checkIsolatedScript("go test ./...", blocks))
}
