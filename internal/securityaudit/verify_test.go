package securityaudit

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/gotest"
)

const proofSource = `package app

import "testing"

func TestSecurityProof_V1(t *testing.T) {
	if got := 1; got != 2 {
		t.Errorf("[V1] got %d", got)
	}
}
`

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}

func testResult(name string, status gotest.Status, output string) gotest.Result {
	return gotest.Result{
		Tests:    []gotest.Test{{Package: "example.com/app", Name: name, Status: status, Output: output}},
		Packages: []gotest.PackageResult{{Package: "example.com/app", Status: status}},
	}
}

func baseFinding() Finding {
	return Finding{
		ID: "V1", Title: "Path traversal", CWE: "CWE-22", Severity: SeverityHigh,
		Proof: &Proof{File: "app/securityproof_V1_test.go", Test: "TestSecurityProof_V1"},
	}
}

func newRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module example.com/app\n\ngo 1.22\n")
	writeFile(t, filepath.Join(root, "app", "securityproof_V1_test.go"), proofSource)
	return root
}

func TestVerifyConfirmsRepeatedlyFailingProof(t *testing.T) {
	t.Parallel()
	root := newRoot(t)
	var runs atomic.Int32
	v := Verifier{Root: root, Run: func(_ context.Context, _ string, opts gotest.Options) (gotest.Result, error) {
		runs.Add(1)
		require.Equal(t, "^TestSecurityProof_V1$", opts.Run)
		require.Equal(t, "./app", opts.Packages)
		return testResult("TestSecurityProof_V1", gotest.StatusFail, "    proof_test.go:7: [V1] got 1\n--- FAIL: TestSecurityProof_V1\n"), nil
	}}
	f := baseFinding()
	v.Verify(t.Context(), &f)
	require.Equal(t, StatusConfirmed, f.Status, f.Reason)
	require.EqualValues(t, 2, runs.Load())
	require.NotEmpty(t, f.Proof.SHA256)
	require.Contains(t, f.Evidence, "[V1] got 1")
	require.NotContains(t, f.Evidence, "--- FAIL")
}

func TestVerifyRejectionsStayUnconfirmed(t *testing.T) {
	t.Parallel()
	failWith := func(output string) TestRunner {
		return func(context.Context, string, gotest.Options) (gotest.Result, error) {
			return testResult("TestSecurityProof_V1", gotest.StatusFail, output), nil
		}
	}
	cases := []struct {
		name   string
		runner TestRunner
		mutate func(root string, f *Finding)
		reason string
	}{
		{"passing proof", func(context.Context, string, gotest.Options) (gotest.Result, error) {
			return testResult("TestSecurityProof_V1", gotest.StatusPass, ""), nil
		}, nil, "passes"},
		{"skipped proof", func(context.Context, string, gotest.Options) (gotest.Result, error) {
			return testResult("TestSecurityProof_V1", gotest.StatusSkip, "skipped"), nil
		}, nil, "skipped"},
		{"failure not tied to finding", failWith("panic: nil pointer dereference"), nil, "never names [V1]"},
		{"test did not run", func(context.Context, string, gotest.Options) (gotest.Result, error) {
			return gotest.Result{NoTests: true}, nil
		}, nil, "did not run"},
		{"build error", func(context.Context, string, gotest.Options) (gotest.Result, error) {
			return gotest.Result{Packages: []gotest.PackageResult{{Package: "example.com/app", BuildError: "undefined: x"}}}, nil
		}, nil, "does not compile"},
		{"toolchain error", func(context.Context, string, gotest.Options) (gotest.Result, error) {
			return gotest.Result{}, os.ErrPermission
		}, nil, "does not compile or run"},
		{"timeout", func(context.Context, string, gotest.Options) (gotest.Result, error) {
			return gotest.Result{Timeout: true}, nil
		}, nil, "timeout"},
		{"no proof", failWith("[V1]"), func(_ string, f *Finding) { f.Proof = nil }, "no proof"},
		{"proof outside project", failWith("[V1]"), func(_ string, f *Finding) { f.Proof.File = "../evil_test.go" }, "does not exist"},
		{"proof not a test file", failWith("[V1]"), func(_ string, f *Finding) { f.Proof.File = "go.mod" }, "not a _test.go"},
		{"bad test name", failWith("[V1]"), func(_ string, f *Finding) { f.Proof.Test = "TestX|.*" }, "not a valid"},
		{"missing test func", failWith("[V1]"), func(_ string, f *Finding) { f.Proof.Test = "TestSecurityProof_Other" }, "does not declare"},
		{"bad cwe", failWith("[V1]"), func(_ string, f *Finding) { f.CWE = "79" }, "cwe"},
		{"bad id", failWith("[V1]"), func(_ string, f *Finding) { f.ID = "a b" }, "finding id"},
		{"bad severity", failWith("[V1]"), func(_ string, f *Finding) { f.Severity = "scary" }, "severity"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := newRoot(t)
			f := baseFinding()
			if tc.mutate != nil {
				tc.mutate(root, &f)
			}
			Verifier{Root: root, Run: tc.runner}.Verify(t.Context(), &f)
			require.Equal(t, StatusUnconfirmed, f.Status)
			require.Contains(t, f.Reason, tc.reason)
			require.Empty(t, f.Evidence)
		})
	}
}

func TestVerifyRejectsUnconditionalFailure(t *testing.T) {
	t.Parallel()
	root := newRoot(t)
	writeFile(t, filepath.Join(root, "app", "securityproof_V1_test.go"), `package app

import "testing"

func TestSecurityProof_V1(t *testing.T) {
	t.Fatal("[V1] always")
}
`)
	f := baseFinding()
	called := false
	Verifier{Root: root, Run: func(context.Context, string, gotest.Options) (gotest.Result, error) {
		called = true
		return gotest.Result{}, nil
	}}.Verify(t.Context(), &f)
	require.Equal(t, StatusUnconfirmed, f.Status)
	require.Contains(t, f.Reason, "unconditionally")
	require.False(t, called, "an unconditional failure must not even be run")
}

func TestVerifyFlakyProofIsNotConfirmed(t *testing.T) {
	t.Parallel()
	root := newRoot(t)
	var n atomic.Int32
	f := baseFinding()
	Verifier{Root: root, Run: func(context.Context, string, gotest.Options) (gotest.Result, error) {
		if n.Add(1) == 1 {
			return testResult("TestSecurityProof_V1", gotest.StatusFail, "[V1]"), nil
		}
		return testResult("TestSecurityProof_V1", gotest.StatusPass, ""), nil
	}}.Verify(t.Context(), &f)
	require.Equal(t, StatusUnconfirmed, f.Status)
	require.Contains(t, f.Reason, "flaky")
}

func TestVerifyRejectsSymlinkEscape(t *testing.T) {
	t.Parallel()
	root := newRoot(t)
	outside := t.TempDir()
	writeFile(t, filepath.Join(outside, "x_test.go"), proofSource)
	link := filepath.Join(root, "app", "link_test.go")
	if err := os.Symlink(filepath.Join(outside, "x_test.go"), link); err != nil {
		t.Skip("symlinks unavailable: " + err.Error())
	}
	f := baseFinding()
	f.Proof.File = "app/link_test.go"
	Verifier{Root: root}.Verify(t.Context(), &f)
	require.Equal(t, StatusUnconfirmed, f.Status)
	require.Contains(t, f.Reason, "outside the project")
}

func TestLoadManifestDiscardsSelfDeclaredStatus(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "findings.json")
	writeFile(t, path, `{"version":1,"findings":[{"id":"V1","title":"t","cwe":"CWE-22","severity":"high","status":"confirmed","reason":"trust me","evidence":"x","source":"static","proof":{"file":"a_test.go","test":"TestA","sha256":"abc"}}]}`)
	got, err := LoadManifest(path)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, StatusUnconfirmed, got[0].Status)
	require.Empty(t, got[0].Reason)
	require.Empty(t, got[0].Evidence)
	require.Equal(t, SourceAgent, got[0].Source)
	require.Empty(t, got[0].Proof.SHA256)
}

func TestLoadManifestMissingAndInvalid(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	got, err := LoadManifest(filepath.Join(dir, "none.json"))
	require.NoError(t, err)
	require.Empty(t, got)

	bad := filepath.Join(dir, "bad.json")
	writeFile(t, bad, "{not json")
	_, err = LoadManifest(bad)
	require.Error(t, err)

	big := filepath.Join(dir, "big.json")
	writeFile(t, big, strings.Repeat(" ", maxManifestBytes+10))
	_, err = LoadManifest(big)
	require.ErrorContains(t, err, "exceeds")
}

func TestSeverityParseAndRank(t *testing.T) {
	t.Parallel()
	s, err := ParseSeverity(" HIGH ")
	require.NoError(t, err)
	require.Equal(t, SeverityHigh, s)
	_, err = ParseSeverity("urgent")
	require.Error(t, err)
	require.Greater(t, SeverityCritical.Rank(), SeverityHigh.Rank())
	require.Greater(t, SeverityInfo.Rank(), Severity("").Rank())
}
