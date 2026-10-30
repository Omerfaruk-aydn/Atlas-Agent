package securityaudit

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/gotest"
)

// TestRunner runs Go tests. It is gotest.Run in production and a fake in
// tests, so the audit logic is checked without a toolchain.
type TestRunner func(ctx context.Context, dir string, opts gotest.Options) (gotest.Result, error)

// Verifier decides whether a finding's proof test really demonstrates it.
type Verifier struct {
	// Root is the project root; proof files must live under it.
	Root string
	// Run executes tests. Nil means gotest.Run.
	Run TestRunner
	// Repeat is how many times the proof must fail before the finding is
	// confirmed. A proof that fails once and passes once is a flaky test,
	// not evidence. Zero means two.
	Repeat int
	// Timeout bounds each proof run. Zero means one minute.
	Timeout time.Duration
}

func (v Verifier) runner() TestRunner {
	if v.Run != nil {
		return v.Run
	}
	return gotest.Run
}

func (v Verifier) repeat() int {
	if v.Repeat > 0 {
		return v.Repeat
	}
	return 2
}

func (v Verifier) timeout() time.Duration {
	if v.Timeout > 0 {
		return v.Timeout
	}
	return time.Minute
}

// failingMethods are testing.T methods that, called as a top-level
// statement of a test, make the test fail every time it runs.
var failingMethods = map[string]bool{
	"Fatal": true, "Fatalf": true, "Error": true, "Errorf": true, "Fail": true, "FailNow": true,
}

// checkProofFile validates the proof statically and returns its absolute
// path. It rejects proofs that could not be evidence: files outside the
// project, files that are not tests, tests that do not exist, and tests
// that fail unconditionally.
func (v Verifier) checkProofFile(f Finding) (string, error) {
	if f.Proof == nil || strings.TrimSpace(f.Proof.File) == "" || strings.TrimSpace(f.Proof.Test) == "" {
		return "", fmt.Errorf("no proof test supplied")
	}
	if !testPattern.MatchString(f.Proof.Test) {
		return "", fmt.Errorf("proof test name %q is not a valid top-level Go test name", f.Proof.Test)
	}
	if !strings.HasSuffix(f.Proof.File, "_test.go") {
		return "", fmt.Errorf("proof file %q is not a _test.go file", f.Proof.File)
	}

	abs := filepath.FromSlash(f.Proof.File)
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(v.Root, abs)
	}
	realRoot, err := filepath.EvalSymlinks(v.Root)
	if err != nil {
		return "", fmt.Errorf("cannot resolve project root: %w", err)
	}
	realFile, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("proof file %q does not exist", f.Proof.File)
	}
	rel, err := filepath.Rel(realRoot, realFile)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("proof file %q is outside the project", f.Proof.File)
	}
	info, err := os.Stat(realFile)
	if err != nil || !info.Mode().IsRegular() {
		return "", fmt.Errorf("proof file %q is not a regular file", f.Proof.File)
	}

	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, realFile, nil, parser.SkipObjectResolution)
	if err != nil {
		return "", fmt.Errorf("proof file does not parse: %v", err)
	}
	decl := findTest(parsed, f.Proof.Test)
	if decl == nil {
		return "", fmt.Errorf("proof file does not declare func %s(t *testing.T)", f.Proof.Test)
	}
	if unconditionalFailure(decl) {
		return "", fmt.Errorf("proof test %s fails unconditionally and demonstrates nothing; it must exercise the vulnerable code and fail only when the weakness is present", f.Proof.Test)
	}
	return realFile, nil
}

func findTest(file *ast.File, name string) *ast.FuncDecl {
	for _, d := range file.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Recv != nil || fn.Name.Name != name || fn.Body == nil {
			continue
		}
		if fn.Type.Params == nil || len(fn.Type.Params.List) != 1 {
			continue
		}
		return fn
	}
	return nil
}

func unconditionalFailure(fn *ast.FuncDecl) bool {
	for _, stmt := range fn.Body.List {
		expr, ok := stmt.(*ast.ExprStmt)
		if !ok {
			continue
		}
		call, ok := expr.X.(*ast.CallExpr)
		if !ok {
			continue
		}
		switch fun := call.Fun.(type) {
		case *ast.SelectorExpr:
			if failingMethods[fun.Sel.Name] {
				return true
			}
		case *ast.Ident:
			if fun.Name == "panic" {
				return true
			}
		}
	}
	return false
}

// proofPattern is the go package pattern for the proof file's directory.
func (v Verifier) proofPattern(abs string) string {
	realRoot, err := filepath.EvalSymlinks(v.Root)
	if err != nil {
		realRoot = v.Root
	}
	rel, err := filepath.Rel(realRoot, filepath.Dir(abs))
	if err != nil || rel == "." {
		return "."
	}
	return "./" + filepath.ToSlash(rel)
}

type proofOutcome int

const (
	outcomeFail proofOutcome = iota
	outcomePass
	outcomeSkip
	outcomeMissing
	outcomeBuild
	outcomeTimeout
)

// runProof runs the proof once and classifies the result.
func (v Verifier) runProof(ctx context.Context, f Finding, abs string) (proofOutcome, string) {
	res, err := v.runner()(ctx, v.Root, gotest.Options{
		Packages: v.proofPattern(abs),
		Run:      "^" + f.Proof.Test + "$",
		Count:    1,
		Timeout:  v.timeout(),
	})
	if err != nil {
		return outcomeBuild, err.Error()
	}
	for _, p := range res.Packages {
		if p.BuildError != "" {
			return outcomeBuild, clip(strings.TrimSpace(p.BuildError), 400)
		}
	}
	if res.Timeout {
		return outcomeTimeout, "the proof run hit its timeout"
	}
	for _, t := range res.Tests {
		if t.Name != f.Proof.Test {
			continue
		}
		switch t.Status {
		case gotest.StatusFail:
			return outcomeFail, t.Output
		case gotest.StatusSkip:
			return outcomeSkip, t.Output
		default:
			return outcomePass, ""
		}
	}
	return outcomeMissing, ""
}

// Verify runs the finding's proof and sets Status, Reason, Evidence and
// the proof digest. A finding is confirmed only when its proof exists, is
// not an unconditional failure, compiles, fails on every one of Repeat
// runs, and its failure output names the finding id in square brackets.
// The last condition ties the failure to this finding: a test that fails
// because of an unrelated panic or a missing fixture is not evidence.
func (v Verifier) Verify(ctx context.Context, f *Finding) {
	f.Status = StatusUnconfirmed
	f.Evidence = ""

	if err := validateMeta(*f); err != nil {
		f.Reason = err.Error()
		return
	}
	abs, err := v.checkProofFile(*f)
	if err != nil {
		f.Reason = err.Error()
		return
	}
	digest, err := fileDigest(abs)
	if err != nil {
		f.Reason = "cannot read proof file: " + err.Error()
		return
	}

	marker := "[" + f.ID + "]"
	var evidence string
	for i := range v.repeat() {
		if ctx.Err() != nil {
			f.Reason = "audit cancelled before the proof was confirmed"
			return
		}
		outcome, output := v.runProof(ctx, *f, abs)
		switch outcome {
		case outcomeBuild:
			f.Reason = "proof test does not compile or run: " + output
			return
		case outcomeTimeout:
			f.Reason = output
			return
		case outcomeMissing:
			f.Reason = "proof test did not run; check the test name and that its file has no build constraints"
			return
		case outcomeSkip:
			f.Reason = "proof test was skipped"
			return
		case outcomePass:
			if i == 0 {
				f.Reason = "proof test passes: the weakness is not demonstrated (or is already fixed)"
			} else {
				f.Reason = "proof test is flaky: it failed once and then passed"
			}
			return
		}
		if !strings.Contains(output, marker) {
			f.Reason = fmt.Sprintf("proof test fails but its output never names %s, so the failure is not tied to this finding", marker)
			return
		}
		if i == 0 {
			evidence = output
		}
	}

	f.Status = StatusConfirmed
	f.Reason = fmt.Sprintf("proof test %s failed on %d of %d runs", f.Proof.Test, v.repeat(), v.repeat())
	f.Evidence = clip(trimTestOutput(evidence), 800)
	f.Proof.SHA256 = digest
}

// trimTestOutput drops the go test framing so the evidence is the
// assertion message itself.
func trimTestOutput(out string) string {
	var kept []string
	for line := range strings.SplitSeq(out, "\n") {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "=== RUN") || strings.HasPrefix(t, "--- FAIL") ||
			t == "FAIL" || strings.HasPrefix(t, "FAIL\t") {
			continue
		}
		kept = append(kept, t)
	}
	return strings.Join(kept, "\n")
}

// validateMeta checks the fields the report depends on.
func validateMeta(f Finding) error {
	if !idPattern.MatchString(f.ID) {
		return fmt.Errorf("finding id %q must be 1-40 letters, digits, '-' or '_'", f.ID)
	}
	if !cwePattern.MatchString(f.CWE) {
		return fmt.Errorf("cwe %q must look like CWE-79", f.CWE)
	}
	if f.Severity.Rank() == 0 {
		return fmt.Errorf("severity %q must be critical, high, medium, low or info", f.Severity)
	}
	if strings.TrimSpace(f.Title) == "" {
		return fmt.Errorf("finding has no title")
	}
	return nil
}

func fileDigest(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

// proofTampered reports whether a confirmed finding's proof file changed
// since confirmation.
func (v Verifier) proofTampered(f Finding) bool {
	if f.Proof == nil || f.Proof.SHA256 == "" {
		return false
	}
	abs := filepath.FromSlash(f.Proof.File)
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(v.Root, abs)
	}
	digest, err := fileDigest(abs)
	return err != nil || digest != f.Proof.SHA256
}
