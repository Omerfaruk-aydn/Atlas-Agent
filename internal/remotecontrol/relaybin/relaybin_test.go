package relaybin

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

// The generated artifacts are not committed; this checks them whenever a
// build has them, which includes every build /rc can install from.
func TestBundledRelaysAreLinuxExecutablesWithRecordedChecksums(t *testing.T) {
	t.Parallel()
	archs := Available()
	if len(archs) == 0 {
		_, _, err := Lookup("amd64")
		require.ErrorIs(t, err, ErrNotBundled)
		t.Skip("relay artifacts are not generated in this checkout")
	}
	require.Equal(t, []string{"amd64", "arm64"}, archs)
	machines := map[string]uint16{"amd64": 62, "arm64": 183}
	for _, arch := range archs {
		a, data, err := Lookup(arch)
		require.NoError(t, err)
		sum := sha256.Sum256(data)
		require.Equal(t, a.SHA256, hex.EncodeToString(sum[:]))
		require.True(t, bytes.HasPrefix(data, []byte("\x7fELF")), "%s is not an ELF binary", arch)
		require.Equal(t, machines[arch], uint16(data[18])|uint16(data[19])<<8, "%s has the wrong machine type", arch)
	}
	_, _, err := Lookup("riscv64")
	require.Error(t, err)
	require.False(t, errors.Is(err, ErrNotBundled))
}
