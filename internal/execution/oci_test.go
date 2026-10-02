package execution

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/google/uuid"

	"github.com/stretchr/testify/require"
)

func TestOCIRequiredNeverFallsBack(t *testing.T) {
	t.Parallel()
	for _, policy := range []ExecutionPolicy{
		{Mode: "container-required", RuntimePath: "missing", Image: "latest"},
		{Mode: "unexpected"},
		{Mode: "container-required", Network: "host"},
	} {
		require.Error(t, ValidatePolicy(t.Context(), policy))
	}
}

func TestOCIArgumentsAndEnvironment(t *testing.T) {
	t.Parallel()
	path, err := os.Executable()
	require.NoError(t, err)
	p := defaults(ExecutionPolicy{Mode: "container-required", RuntimePath: path, Image: "fixture/image@sha256:" + strings.Repeat("a", 64), ReadOnly: true, EnvironmentKeys: []string{"LANG"}})
	require.NoError(t, ValidatePolicy(t.Context(), p))
	r := &ociRunner{policy: p, owner: "owner"}
	args, err := r.createArguments(Request{RunID: uuid.NewString(), Root: t.TempDir(), Argv: []string{"/bin/sh", "-c", "printf '%s' 'literal;data'"}, Env: []string{"SECRET=value", "LANG=C.UTF-8"}})
	require.NoError(t, err)
	require.Contains(t, args, "--pull=never")
	require.Contains(t, args, "--read-only")
	require.Contains(t, args, "--pids-limit")
	require.Contains(t, args, "none")
	require.Contains(t, args, "LANG=C.UTF-8")
	require.Contains(t, args, "GOMODCACHE=/workspace/.atlas-env/go/mod")
	require.Contains(t, args, "CARGO_HOME=/tmp/atlas-cargo")
	require.Contains(t, args, "GOCACHE=/tmp/atlas-go-build")
	require.Contains(t, args, "/tmp:rw,nosuid,nodev,size=1073741824")
	require.Contains(t, strings.Join(args, " "), "cp -R /workspace/.atlas-env/cargo/. /tmp/atlas-cargo/")
	require.NotContains(t, args, "SECRET=value")
	require.Equal(t, "printf '%s' 'literal;data'", args[len(args)-1])
	require.NotContains(t, strings.Join(args, " "), "docker.sock")
	p.Network = "host"
	require.Error(t, ValidatePolicy(t.Context(), p))
}

func TestOCICancelIdentityRace(t *testing.T) {
	t.Parallel()
	store := engineering.NewStore(t.TempDir())
	id := uuid.NewString()
	r := &ociRunner{store: store, owner: "owner", cancel: map[string]context.CancelFunc{}}
	record := runRecord{Result: Result{RunID: id, Status: "preparing"}}
	require.NoError(t, r.persist(t.Context(), record, 0))
	owner := "foreign"
	spawns := 0
	r.commandOverride = func(ctx context.Context, input io.Reader, args ...string) ([]byte, []byte, error) {
		if args[1] == "inspect" {
			data, err := json.Marshal([]map[string]any{{"Id": strings.Repeat("b", 64), "Config": map[string]any{"Labels": map[string]string{"atlas.owner": owner, "atlas.run": id}}, "State": map[string]any{"Running": true, "Status": "running"}}})
			return data, nil, err
		}
		spawns++
		require.Equal(t, strings.Repeat("b", 64), args[len(args)-1])
		require.Contains(t, []string{"kill", "rm"}, args[1])
		return nil, nil, nil
	}
	require.Error(t, r.Cancel(t.Context(), id))
	require.Zero(t, spawns)
	owner = "owner"
	require.NoError(t, r.Cancel(t.Context(), id))
	require.Equal(t, 2, spawns)
}
