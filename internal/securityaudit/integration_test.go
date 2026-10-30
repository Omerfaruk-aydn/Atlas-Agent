package securityaudit

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const vulnerableLib = `package safe

import "path/filepath"

// Join resolves rel under base.
func Join(base, rel string) string { return filepath.Join(base, rel) }
`

const fixedLib = `package safe

import (
	"path/filepath"
	"strings"
)

// Join resolves rel under base, falling back to base for escapes.
func Join(base, rel string) string {
	p := filepath.Join(base, rel)
	if p != base && !strings.HasPrefix(p, base+string(filepath.Separator)) {
		return base
	}
	return p
}
`

const realProof = `package safe

import (
	"strings"
	"testing"
)

func TestSecurityProof_V1(t *testing.T) {
	base := t.TempDir()
	got := Join(base, "../../etc/passwd")
	if got != base && !strings.HasPrefix(got, base) {
		t.Errorf("[V1] Join escaped the base directory: %s", got)
	}
}
`

// TestAuditEndToEndWithRealToolchain drives the real go tool against a
// throwaway module: the proof must fail first (confirmed), then pass after
// the fix, with the module's other test staying green.
func TestAuditEndToEndWithRealToolchain(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("runs the real go toolchain several times")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not on PATH")
	}
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module example.com/safe\n\ngo 1.22\n")
	writeFile(t, filepath.Join(root, "safe", "safe.go"), vulnerableLib)
	writeFile(t, filepath.Join(root, "safe", "safe_test.go"), `package safe

import "testing"

func TestJoinInside(t *testing.T) {
	if Join("/b", "a") == "" {
		t.Fatal("empty")
	}
}
`)

	manifest := `{"version":1,"findings":[{"id":"V1","title":"Path traversal in Join","cwe":"CWE-22","severity":"high","file":"safe/safe.go","line":6,"proof":{"file":"safe/securityproof_V1_test.go","test":"TestSecurityProof_V1"}}]}`
	agent := agentFunc(func(_ context.Context, prompt string) error {
		switch {
		case strings.Contains(prompt, "Run a defensive security audit"):
			writeFile(t, filepath.Join(root, "safe", "securityproof_V1_test.go"), realProof)
			writeFile(t, filepath.Join(root, ".atlas", "security", ManifestFile), manifest)
		case strings.Contains(prompt, "Fix the confirmed security findings"):
			writeFile(t, filepath.Join(root, "safe", "safe.go"), fixedLib)
		}
		return nil
	})

	report, err := Audit(t.Context(), Options{Root: root, Agent: agent})
	require.NoError(t, err)
	f := findingByID(t, report, "V1")
	require.Equal(t, StatusConfirmed, f.Status, f.Reason)
	require.Contains(t, f.Evidence, "[V1] Join escaped the base directory")

	report, err = Audit(t.Context(), Options{Root: root, Agent: agent, Fix: true})
	require.NoError(t, err)
	f = findingByID(t, report, "V1")
	require.Equal(t, StatusFixed, f.Status, f.Reason)
	require.Empty(t, report.Regressions)
}
