package execution

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestOCIPrivateMountsAndEvidenceRetention(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(root, ".atlas"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".atlas", "atlas.json"), []byte("private credential"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".env"), []byte("SECRET=private"), 0o600))
	r := &ociRunner{store: engineering.NewStore(t.TempDir())}
	args, ref, err := r.privateMounts(t.Context(), root)
	require.NoError(t, err)
	joined := strings.Join(args, " ")
	require.Contains(t, joined, "/workspace/.atlas:ro")
	require.Contains(t, joined, "target=/workspace/.env,readonly")
	require.NotContains(t, joined, "SECRET=private")
	data, err := r.store.ReadArtifact(t.Context(), ref)
	require.NoError(t, err)
	require.Empty(t, data)
	require.NoError(t, r.persist(t.Context(), runRecord{Result: Result{RunID: uuid.NewString()}, MaskRef: ref}, 0))
	_, err = r.store.CleanArtifacts(t.Context())
	require.NoError(t, err)
	_, err = r.store.ReadArtifact(t.Context(), ref)
	require.NoError(t, err)
}
