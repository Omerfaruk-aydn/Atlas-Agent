package codereview

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ghpr"
	"github.com/stretchr/testify/require"
)

const sampleDiff = `diff --git a/a.go b/a.go
--- a/a.go
+++ b/a.go
@@ -1,3 +1,4 @@
 package a
+var x = 1
 func f() {}
 func g() {}
diff --git a/old.go b/old.go
deleted file mode 100644
--- a/old.go
+++ /dev/null
@@ -1 +0,0 @@
-package old
`

func TestParseEffortAndPlan(t *testing.T) {
	t.Parallel()
	e, err := ParseEffort("")
	require.NoError(t, err)
	require.Equal(t, EffortMedium, e)
	_, err = ParseEffort("extreme")
	require.Error(t, err)
	require.Equal(t, Plan{Passes: 1}, PlanFor(EffortLow))
	require.Equal(t, Plan{Passes: 4, Verifiers: 2}, PlanFor(EffortMax))
	require.Greater(t, PlanFor(EffortHigh).Passes, PlanFor(EffortMedium).Passes)
}

func TestDiffFilesAndCommentableLines(t *testing.T) {
	t.Parallel()
	require.Equal(t, []string{"a.go", "old.go"}, DiffFiles(sampleDiff))
	lines := CommentableLines(sampleDiff)
	require.True(t, lines["a.go"][2])
	require.True(t, lines["a.go"][1])
	require.True(t, lines["a.go"][4])
	require.False(t, lines["a.go"][5])
	require.Empty(t, lines["old.go"])
}

func TestParseReviewToleratesProseAndFlexibleTypes(t *testing.T) {
	t.Parallel()
	text := "Here you go:\n```json\n{\"summary\":\"s\",\"findings\":[{\"file\":\"a.go\",\"line\":\"2\",\"severity\":\"Major\",\"title\":\"t\",\"scenario\":\"sc\",\"confidence\":85}]}\n```"
	parsed, ok := parseReview(text)
	require.True(t, ok)
	f, reason := toFinding(parsed.Findings[0], "/repo", map[string]bool{"a.go": true})
	require.Empty(t, reason)
	require.Equal(t, 2, f.Line)
	require.Equal(t, SeverityHigh, f.Severity)
	require.InDelta(t, 0.85, f.Confidence, 0.001)

	arr, ok := parseReview(`[{"path":"a.go","line":1,"title":"t","scenario":"x"}]`)
	require.True(t, ok)
	require.Len(t, arr.Findings, 1)

	_, ok = parseReview("no json at all")
	require.False(t, ok)
}

func TestNormalizePath(t *testing.T) {
	t.Parallel()
	known := map[string]bool{"pkg/a.go": true}
	require.Equal(t, "pkg/a.go", normalizePath("b/pkg/a.go", "", known))
	require.Equal(t, "pkg/a.go", normalizePath("./pkg/a.go:12", "", known))
	require.Equal(t, "pkg/a.go", normalizePath("C:/repo/pkg/a.go", "C:/repo", known))
	require.Empty(t, normalizePath("../etc/passwd", "", known))
	require.Empty(t, normalizePath("/etc/passwd", "", known))
}

func TestMergeCombinesDuplicatePassFindings(t *testing.T) {
	t.Parallel()
	merged := merge([]Finding{
		{Path: "a.go", Line: 10, Title: "nil map write panics", Scenario: "map is nil when called", Severity: SeverityMedium, Confidence: 0.6, Passes: 1},
		{Path: "a.go", Line: 11, Title: "write to nil map panics", Scenario: "the map is nil when called first", Severity: SeverityHigh, Confidence: 0.7, Passes: 1},
		{Path: "a.go", Line: 40, Title: "unrelated", Scenario: "other", Severity: SeverityLow, Confidence: 0.5, Passes: 1},
		{Path: "b.go", Line: 10, Title: "nil map write panics", Scenario: "x", Severity: SeverityLow, Confidence: 0.5, Passes: 1},
	})
	require.Len(t, merged, 3)
	require.Equal(t, 2, merged[0].Passes)
	require.Equal(t, SeverityHigh, merged[0].Severity)
	require.InDelta(t, 0.8, merged[0].Confidence, 0.001)
}

// fakeRunner answers reviewer requests with review and verifier requests
// by looking at which finding ids the prompt mentions.
type fakeRunner struct {
	review    func(call int) (string, error)
	verify    func(prompt string) (string, error)
	reviews   atomic.Int32
	verifies  atomic.Int32
	lastModel atomic.Value
}

func (f *fakeRunner) Run(_ context.Context, req Request) (string, error) {
	f.lastModel.Store(req.Model)
	if req.Kind == "verify" {
		f.verifies.Add(1)
		return f.verify(req.Prompt)
	}
	n := int(f.reviews.Add(1))
	return f.review(n)
}

func reviewJSON(findings ...map[string]any) string {
	data, _ := json.Marshal(map[string]any{"summary": "Adds a variable.", "findings": findings})
	return string(data)
}

func newRepoTarget(t *testing.T) Target {
	t.Helper()
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "a.go"), []byte("package a\nvar x = 1\nfunc f() {}\nfunc g() {}\n"), 0o644))
	return Target{Kind: KindWorking, Label: "uncommitted changes", Root: root, Diff: sampleDiff, Files: DiffFiles(sampleDiff)}
}

func TestReviewVerificationDropsRefutedFindings(t *testing.T) {
	t.Parallel()
	target := newRepoTarget(t)
	runner := &fakeRunner{
		review: func(int) (string, error) {
			return reviewJSON(
				map[string]any{"path": "a.go", "line": 2, "severity": "high", "title": "real bug", "scenario": "x is shared", "confidence": 0.8},
				map[string]any{"path": "a.go", "line": 3, "severity": "critical", "title": "phantom defect", "scenario": "never happens", "confidence": 0.9},
				map[string]any{"path": "ghost.go", "line": 1, "severity": "high", "title": "hallucinated file", "scenario": "n/a", "confidence": 0.9},
				map[string]any{"path": "a.go", "line": 400, "severity": "high", "title": "past eof", "scenario": "n/a", "confidence": 0.9},
				map[string]any{"path": "a.go", "line": 2, "title": "", "scenario": "n/a"},
			), nil
		},
		verify: func(prompt string) (string, error) {
			verdicts := []map[string]any{}
			for _, f := range []struct{ title, verdict string }{{"real bug", "confirmed"}, {"phantom defect", "rejected"}} {
				id := idOf(t, prompt, f.title)
				verdicts = append(verdicts, map[string]any{"id": id, "verdict": f.verdict, "confidence": 0.9, "reason": "checked a.go:2"})
			}
			data, _ := json.Marshal(map[string]any{"verdicts": verdicts})
			return string(data), nil
		},
	}
	report, err := Review(t.Context(), runner, target, Options{Effort: EffortMedium})
	require.NoError(t, err)
	require.Len(t, report.Findings, 1)
	f := report.Findings[0]
	require.Equal(t, "real bug", f.Title)
	require.Equal(t, "F1", f.ID)
	require.True(t, f.Verified)
	require.Equal(t, 2, f.Passes, "both passes reported it")
	require.Equal(t, 2, int(runner.reviews.Load()))
	require.Positive(t, report.VerifyCalls)

	reasons := map[string]string{}
	for _, d := range report.Dropped {
		reasons[d.Title] = d.Reason
	}
	require.Contains(t, reasons["phantom defect"], "rejected by verification")
	require.Contains(t, reasons["hallucinated file"], "file not found")
	require.Contains(t, reasons["past eof"], "past the end")
	require.Contains(t, reasons[""], "missing title")
}

// idOf finds the id attribute of the <finding> with the given title.
func idOf(t *testing.T, prompt, title string) string {
	t.Helper()
	for _, block := range strings.Split(prompt, "<finding ")[1:] {
		if strings.Contains(block, "Title: "+title+"\n") {
			start := strings.Index(block, `id="`) + 4
			return block[start : start+strings.Index(block[start:], `"`)]
		}
	}
	t.Fatalf("finding %q not in verifier prompt", title)
	return ""
}

func TestReviewLowEffortSkipsVerification(t *testing.T) {
	t.Parallel()
	target := newRepoTarget(t)
	runner := &fakeRunner{
		review: func(int) (string, error) {
			return reviewJSON(
				map[string]any{"path": "a.go", "line": 2, "severity": "low", "title": "minor", "scenario": "s", "confidence": 0.7},
				map[string]any{"path": "a.go", "line": 3, "severity": "critical", "title": "major", "scenario": "s", "confidence": 0.9},
			), nil
		},
	}
	report, err := Review(t.Context(), runner, target, Options{Effort: EffortLow})
	require.NoError(t, err)
	require.Zero(t, runner.verifies.Load())
	require.Equal(t, 1, report.Passes)
	require.Len(t, report.Findings, 2)
	require.Equal(t, "major", report.Findings[0].Title, "ranked by severity")
	require.False(t, report.Findings[0].Verified)
}

func TestReviewMaxRequiresNoRejection(t *testing.T) {
	t.Parallel()
	target := newRepoTarget(t)
	runner := &fakeRunner{
		review: func(int) (string, error) {
			return reviewJSON(map[string]any{"path": "a.go", "line": 2, "severity": "high", "title": "contested", "scenario": "s", "confidence": 0.9}), nil
		},
		verify: func(prompt string) (string, error) {
			verdict := "confirmed"
			if strings.Contains(prompt, "reproduce") {
				verdict = "rejected"
			}
			id := idOf(t, prompt, "contested")
			return `{"verdicts":[{"id":"` + id + `","verdict":"` + verdict + `","confidence":0.9,"reason":"r"}]}`, nil
		},
	}
	report, err := Review(t.Context(), runner, target, Options{Effort: EffortMax})
	require.NoError(t, err)
	require.Empty(t, report.Findings, "one rejecting verifier is enough to drop a finding")
	require.Equal(t, 2, report.VerifyCalls)
}

func TestReviewKeepsFindingsWhenVerifierUnavailable(t *testing.T) {
	t.Parallel()
	target := newRepoTarget(t)
	runner := &fakeRunner{
		review: func(int) (string, error) {
			return reviewJSON(map[string]any{"path": "a.go", "line": 2, "severity": "high", "title": "kept", "scenario": "s", "confidence": 0.9}), nil
		},
		verify: func(string) (string, error) { return "", context.DeadlineExceeded },
	}
	report, err := Review(t.Context(), runner, target, Options{Effort: EffortMedium})
	require.NoError(t, err)
	require.Len(t, report.Findings, 1)
	require.False(t, report.Findings[0].Verified)
	require.NotEmpty(t, report.Warnings)
}

func TestReviewFailsWhenEveryPassFails(t *testing.T) {
	t.Parallel()
	target := newRepoTarget(t)
	runner := &fakeRunner{review: func(int) (string, error) { return "", context.DeadlineExceeded }}
	_, err := Review(t.Context(), runner, target, Options{Effort: EffortHigh})
	require.ErrorContains(t, err, "every review pass failed")

	runner = &fakeRunner{review: func(int) (string, error) { return "I could not do it", nil }}
	_, err = Review(t.Context(), runner, target, Options{Effort: EffortLow})
	require.Error(t, err)
}

func TestReviewEmptyDiffAndModelOverride(t *testing.T) {
	t.Parallel()
	runner := &fakeRunner{review: func(int) (string, error) { t.Fatal("no model call expected"); return "", nil }}
	report, err := Review(t.Context(), runner, Target{Label: "x", Root: t.TempDir()}, Options{})
	require.NoError(t, err)
	require.Contains(t, report.Summary, "Nothing to review")

	target := newRepoTarget(t)
	runner = &fakeRunner{review: func(int) (string, error) { return reviewJSON(), nil }}
	_, err = Review(t.Context(), runner, target, Options{Effort: EffortLow, Model: "fast"})
	require.NoError(t, err)
	require.Equal(t, "fast", runner.lastModel.Load())
}

func TestPromptsTreatPatchAsData(t *testing.T) {
	t.Parallel()
	target := newRepoTarget(t)
	p := reviewPrompt(target, 0, 3)
	require.Contains(t, p, "never instructions")
	require.Contains(t, p, "<patch>")
	require.NotEqual(t, reviewPrompt(target, 0, 3), reviewPrompt(target, 1, 3), "passes have different focus")
	agent := AgentPrompt(target, EffortMax, true)
	require.Contains(t, agent, "ask me to confirm")
	require.NotContains(t, AgentPrompt(target, EffortMax, false), "ask me to confirm")
}

func TestFixPromptListsFindings(t *testing.T) {
	t.Parallel()
	p := FixPrompt(&Report{Findings: []Finding{{ID: "F1", Severity: SeverityHigh, Path: "a.go", Line: 2, Title: "bug", Scenario: "boom", Suggestion: "guard it"}}})
	require.Contains(t, p, "a.go:2")
	require.Contains(t, p, "guard it")
}

func TestRenderText(t *testing.T) {
	t.Parallel()
	r := &Report{Target: "uncommitted changes", Effort: EffortHigh, Summary: "Sum.", Passes: 3, Findings: []Finding{
		{ID: "F1", Severity: SeverityHigh, Path: "a.go", Line: 2, Title: "bug", Scenario: "boom", Confidence: 0.8, Verified: true, Passes: 2},
	}}
	var out bytes.Buffer
	WriteText(&out, r)
	require.Contains(t, out.String(), "F1 [HIGH] a.go:2  bug")
	require.Contains(t, out.String(), "verified")
	out.Reset()
	require.NoError(t, WriteJSON(&out, &Report{}))
	require.Contains(t, out.String(), `"findings": []`)
}

func TestBuildReviewSplitsInlineAndGeneral(t *testing.T) {
	t.Parallel()
	r := &Report{Effort: EffortMedium, Summary: "S", Findings: []Finding{
		{Path: "a.go", Line: 2, Severity: SeverityHigh, Title: "inline", Scenario: "s", Confidence: 0.9, Verified: true},
		{Path: "a.go", Line: 99, Severity: SeverityLow, Title: "outside", Scenario: "s2"},
	}}
	payload := BuildReview(r, Target{Diff: sampleDiff})
	require.Len(t, payload.Comments, 1)
	require.Equal(t, "RIGHT", payload.Comments[0].Side)
	require.Contains(t, payload.Body, "outside")
	require.NotContains(t, payload.Body, "inline")
}

func TestPostReviewUsesGH(t *testing.T) {
	t.Parallel()
	target := Target{Kind: KindPR, Root: t.TempDir(), Diff: sampleDiff, PR: &ghpr.PR{Number: 7, URL: "https://github.com/o/r/pull/7"}}
	r := &Report{Effort: EffortLow, Findings: []Finding{{Path: "a.go", Line: 2, Severity: SeverityHigh, Title: "t", Scenario: "s"}}}
	var calls [][]string
	var stdin string
	gh := func(_ context.Context, _ string, in string, args ...string) (string, error) {
		calls = append(calls, args)
		if args[0] == "pr" {
			return "abc123\n", nil
		}
		stdin = in
		return `{"html_url":"https://github.com/o/r/pull/7#review-1"}`, nil
	}
	url, err := PostReview(t.Context(), gh, target, r)
	require.NoError(t, err)
	require.Equal(t, "https://github.com/o/r/pull/7#review-1", url)
	require.Len(t, calls, 2)
	require.Contains(t, calls[1], "repos/o/r/pulls/7/reviews")
	var payload ReviewPayload
	require.NoError(t, json.Unmarshal([]byte(stdin), &payload))
	require.Equal(t, "abc123", payload.CommitID)
	require.Equal(t, "COMMENT", payload.Event)
	require.Len(t, payload.Comments, 1)

	_, err = PostReview(t.Context(), gh, Target{Kind: KindWorking}, r)
	require.Error(t, err)
	url, err = PostReview(t.Context(), gh, target, &Report{})
	require.NoError(t, err)
	require.Empty(t, url)
	require.Len(t, calls, 2, "nothing is posted without findings")
}

func gitIn(t *testing.T, root string, args ...string) string {
	t.Helper()
	out, err := exec.CommandContext(t.Context(), "git", append([]string{"-C", root}, args...)...).CombinedOutput()
	require.NoError(t, err, string(out))
	return string(out)
}

func TestResolveGitTargets(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	gitIn(t, root, "init", "-q", "-b", "main")
	gitIn(t, root, "config", "user.name", "Fixture")
	gitIn(t, root, "config", "user.email", "fixture@example.invalid")
	write := func(name, content string) {
		require.NoError(t, os.WriteFile(filepath.Join(root, name), []byte(content), 0o644))
	}
	write("base.txt", "base\n")
	gitIn(t, root, "add", ".")
	gitIn(t, root, "commit", "-q", "-m", "base")
	gitIn(t, root, "checkout", "-q", "-b", "feature")
	write("feature.txt", "feature\n")
	gitIn(t, root, "add", ".")
	gitIn(t, root, "commit", "-q", "-m", "feature")
	gitIn(t, root, "checkout", "-q", "main")
	write("base.txt", "base\nedited\n")
	write("fresh.txt", "brand new\n")

	r := NewResolver()
	ctx := t.Context()

	working, err := r.Resolve(ctx, root, "")
	require.NoError(t, err)
	require.Equal(t, KindWorking, working.Kind)
	require.ElementsMatch(t, []string{"base.txt", "fresh.txt"}, working.Files)
	require.Contains(t, working.Diff, "+brand new")

	branch, err := r.Resolve(ctx, root, "feature")
	require.NoError(t, err)
	require.Equal(t, KindBranch, branch.Kind)
	require.Equal(t, []string{"feature.txt"}, branch.Files)

	gitIn(t, root, "stash", "push", "-u", "-q", "-m", "review-test")
	gitIn(t, root, "checkout", "-q", "feature")
	since, err := r.Resolve(ctx, root, "main")
	require.NoError(t, err)
	require.Equal(t, KindRange, since.Kind)
	require.Equal(t, []string{"feature.txt"}, since.Files)

	rng, err := r.Resolve(ctx, root, "main..feature")
	require.NoError(t, err)
	require.Equal(t, []string{"feature.txt"}, rng.Files)

	commit, err := r.Commit(ctx, root, "HEAD")
	require.NoError(t, err)
	require.Equal(t, KindCommit, commit.Kind)
	require.Equal(t, []string{"feature.txt"}, commit.Files)

	_, err = r.Resolve(ctx, root, "--help")
	require.Error(t, err)
	_, err = r.Resolve(ctx, root, "no-such-ref")
	require.Error(t, err)
}

func TestResolvePullRequestUsesGHPR(t *testing.T) {
	t.Parallel()
	r := &Resolver{PR: func(_ context.Context, _, ref string) (ghpr.PR, error) {
		require.Equal(t, "#12", ref)
		return ghpr.PR{Number: 12, Title: "Add x", Diff: sampleDiff}, nil
	}}
	target, err := r.Resolve(t.Context(), t.TempDir(), "#12")
	require.NoError(t, err)
	require.Equal(t, KindPR, target.Kind)
	require.Equal(t, 12, target.PR.Number)
	require.Contains(t, target.Label, "Add x")

	r.PR = func(context.Context, string, string) (ghpr.PR, error) { return ghpr.PR{Number: 1}, nil }
	_, err = r.Resolve(t.Context(), t.TempDir(), "1")
	require.ErrorContains(t, err, "no readable diff")
}

func TestIsPRSpecAndTruncate(t *testing.T) {
	t.Parallel()
	for _, s := range []string{"12", "#12", "o/r#12", "https://github.com/o/r/pull/12"} {
		require.True(t, IsPRSpec(s), s)
	}
	for _, s := range []string{"", "main", "a..b", "feature/x"} {
		require.False(t, IsPRSpec(s), s)
	}
	big := "diff --git a/a b/a\n" + strings.Repeat("x", 50) + "\ndiff --git a/b b/b\n" + strings.Repeat("y", 50)
	cut, truncated := truncateDiff(big, 80)
	require.True(t, truncated)
	require.NotContains(t, cut, "diff --git a/b")
}
