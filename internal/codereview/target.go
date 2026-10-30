package codereview

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ghpr"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/gitx"
)

// Kind says what a review target is.
type Kind string

// The target kinds.
const (
	KindWorking Kind = "working"
	KindCommit  Kind = "commit"
	KindRange   Kind = "range"
	KindBranch  Kind = "branch"
	KindPR      Kind = "pr"
)

const (
	// MaxDiffBytes bounds the patch text handed to reviewers. They have
	// read-only tools and can open any file, so a truncated patch costs
	// coverage hints, not correctness.
	MaxDiffBytes = 160 * 1024
	// maxUntrackedBytes bounds one untracked file inlined as a new-file
	// diff.
	maxUntrackedBytes = 24 * 1024
	maxUntrackedFiles = 40
)

// Target is a resolved review scope.
type Target struct {
	Kind  Kind   `json:"kind"`
	Spec  string `json:"spec,omitempty"`
	Label string `json:"label"`
	Root  string `json:"root"`
	// Diff is the patch under review, bounded by [MaxDiffBytes].
	Diff      string   `json:"-"`
	Truncated bool     `json:"truncated,omitempty"`
	Files     []string `json:"files"`
	// DiffCommand is how a reader with shell tools can fetch the patch.
	DiffCommand string `json:"diff_command,omitempty"`
	// PR is set for pull request targets.
	PR *ghpr.PR `json:"-"`
}

// Resolver turns a target specification into a [Target]. The seams exist
// so tests can run without git or gh state they do not control.
type Resolver struct {
	Git func(ctx context.Context, dir string, args ...string) (string, error)
	PR  func(ctx context.Context, dir, ref string) (ghpr.PR, error)
}

// NewResolver returns a Resolver backed by the real git and gh binaries.
func NewResolver() *Resolver {
	return &Resolver{
		Git: gitx.Run,
		PR: func(ctx context.Context, dir, ref string) (ghpr.PR, error) {
			return ghpr.New().View(ctx, dir, ref)
		},
	}
}

var (
	prURLSpec  = regexp.MustCompile(`^https?://github\.com/[^/\s]+/[^/\s]+/pull/\d+`)
	prRefSpec  = regexp.MustCompile(`^[^/\s#]+/[^/\s#]+#\d+$`)
	prNumSpec  = regexp.MustCompile(`^#?\d+$`)
	hunkHeader = regexp.MustCompile(`^@@ -\d+(?:,\d+)? \+(\d+)(?:,\d+)? @@`)
)

// IsPRSpec reports whether spec names a GitHub pull request.
func IsPRSpec(spec string) bool {
	spec = strings.TrimSpace(spec)
	return prURLSpec.MatchString(spec) || prRefSpec.MatchString(spec) || prNumSpec.MatchString(spec)
}

// Resolve builds the target for spec: empty for the uncommitted changes,
// a pull request number or URL, a "a..b" or "a...b" range, or a single
// ref. A ref that is an ancestor of HEAD means "changes since that ref";
// any other ref is a branch and means "what that branch adds to HEAD".
func (r *Resolver) Resolve(ctx context.Context, root, spec string) (Target, error) {
	spec = strings.TrimSpace(spec)
	switch {
	case spec == "":
		return r.working(ctx, root)
	case IsPRSpec(spec):
		return r.pullRequest(ctx, root, spec)
	case strings.Contains(spec, ".."):
		return r.rangeTarget(ctx, root, spec)
	default:
		return r.refTarget(ctx, root, spec)
	}
}

// Commit builds a target for one commit.
func (r *Resolver) Commit(ctx context.Context, root, ref string) (Target, error) {
	sha, err := r.verify(ctx, root, ref)
	if err != nil {
		return Target{}, err
	}
	out, err := r.Git(ctx, root, "-c", "core.quotepath=off", "show", "--format=", "--no-ext-diff", "--no-textconv", sha, "--")
	if err != nil {
		return Target{}, err
	}
	return finishTarget(Target{Kind: KindCommit, Spec: ref, Label: "commit " + shortRef(sha), Root: root, DiffCommand: "git show --format= " + sha}, out), nil
}

func (r *Resolver) working(ctx context.Context, root string) (Target, error) {
	out, err := r.Git(ctx, root, "-c", "core.quotepath=off", "diff", "--no-ext-diff", "--no-textconv", "HEAD", "--")
	if err != nil {
		// A repository with no commits yet has no HEAD; fall back to the
		// index-relative diff.
		var err2 error
		out, err2 = r.Git(ctx, root, "-c", "core.quotepath=off", "diff", "--no-ext-diff", "--no-textconv", "--")
		if err2 != nil {
			return Target{}, err
		}
	}
	diff := out
	if list, err := r.Git(ctx, root, "ls-files", "--others", "--exclude-standard", "-z"); err == nil {
		count := 0
		for _, name := range strings.Split(list, "\x00") {
			if name == "" {
				continue
			}
			if count++; count > maxUntrackedFiles {
				break
			}
			if block, ok := newFileDiff(root, name); ok {
				diff += block
			}
		}
	}
	return finishTarget(Target{
		Kind: KindWorking, Label: "uncommitted changes", Root: root,
		DiffCommand: "git diff HEAD, plus the untracked files listed by git ls-files --others --exclude-standard",
	}, diff), nil
}

func (r *Resolver) rangeTarget(ctx context.Context, root, spec string) (Target, error) {
	sep := ".."
	if strings.Contains(spec, "...") {
		sep = "..."
	}
	parts := strings.SplitN(spec, sep, 2)
	left, right := parts[0], parts[1]
	if left == "" {
		left = "HEAD"
	}
	if right == "" {
		right = "HEAD"
	}
	for _, ref := range []string{left, right} {
		if _, err := r.verify(ctx, root, ref); err != nil {
			return Target{}, err
		}
	}
	out, err := r.diffRange(ctx, root, left+sep+right)
	if err != nil {
		return Target{}, err
	}
	return finishTarget(Target{Kind: KindRange, Spec: spec, Label: "range " + left + sep + right, Root: root, DiffCommand: "git diff " + left + sep + right}, out), nil
}

func (r *Resolver) refTarget(ctx context.Context, root, ref string) (Target, error) {
	sha, err := r.verify(ctx, root, ref)
	if err != nil {
		return Target{}, err
	}
	if _, err := r.Git(ctx, root, "merge-base", "--is-ancestor", sha, "HEAD"); err == nil {
		out, err := r.diffRange(ctx, root, sha+"...HEAD")
		if err != nil {
			return Target{}, err
		}
		return finishTarget(Target{Kind: KindRange, Spec: ref, Label: "changes since " + ref, Root: root, DiffCommand: "git diff " + sha + "...HEAD"}, out), nil
	}
	out, err := r.diffRange(ctx, root, "HEAD..."+sha)
	if err != nil {
		return Target{}, err
	}
	return finishTarget(Target{Kind: KindBranch, Spec: ref, Label: "branch " + ref, Root: root, DiffCommand: "git diff HEAD..." + sha}, out), nil
}

func (r *Resolver) pullRequest(ctx context.Context, root, spec string) (Target, error) {
	if r.PR == nil {
		return Target{}, errors.New("pull request lookup is not available")
	}
	pr, err := r.PR(ctx, root, spec)
	if err != nil {
		return Target{}, err
	}
	if strings.TrimSpace(pr.Diff) == "" {
		return Target{}, fmt.Errorf("pull request #%d has no readable diff (too large, or gh could not fetch it)", pr.Number)
	}
	t := finishTarget(Target{Kind: KindPR, Spec: spec, Label: fmt.Sprintf("PR #%d %s", pr.Number, pr.Title), Root: root, DiffCommand: fmt.Sprintf("gh pr diff %d", pr.Number)}, pr.Diff)
	t.PR = &pr
	return t, nil
}

func (r *Resolver) diffRange(ctx context.Context, root, rng string) (string, error) {
	return r.Git(ctx, root, "-c", "core.quotepath=off", "diff", "--no-ext-diff", "--no-textconv", rng, "--")
}

// verify resolves ref to a commit hash. Refs starting with "-" are
// refused so a hostile ref cannot be read as a git option.
func (r *Resolver) verify(ctx context.Context, root, ref string) (string, error) {
	if strings.HasPrefix(ref, "-") {
		return "", fmt.Errorf("invalid review revision %q", ref)
	}
	out, err := r.Git(ctx, root, "rev-parse", "--verify", "--end-of-options", ref+"^{commit}")
	if err != nil {
		return "", fmt.Errorf("invalid review revision %q: %w", ref, err)
	}
	return strings.TrimSpace(out), nil
}

func shortRef(sha string) string {
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
}

// finishTarget bounds the diff and extracts the changed file list.
func finishTarget(t Target, diff string) Target {
	t.Files = DiffFiles(diff)
	t.Diff, t.Truncated = truncateDiff(diff, MaxDiffBytes)
	return t
}

// truncateDiff cuts diff to at most limit bytes, preferring a file
// boundary so a half-cut file header does not confuse reviewers.
func truncateDiff(diff string, limit int) (string, bool) {
	if len(diff) <= limit {
		return diff, false
	}
	cut := strings.LastIndex(diff[:limit], "\ndiff --git ")
	if cut <= 0 {
		cut = limit
		// Never split a UTF-8 sequence.
		for cut > 0 && diff[cut]&0xC0 == 0x80 {
			cut--
		}
	}
	return diff[:cut], true
}

// newFileDiff renders an untracked text file as a new-file diff.
func newFileDiff(root, name string) (string, bool) {
	path := filepath.Join(root, filepath.FromSlash(name))
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxUntrackedBytes {
		return "", false
	}
	data, err := os.ReadFile(path)
	if err != nil || strings.ContainsRune(string(data), 0) {
		return "", false
	}
	text := string(data)
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	if text == "" {
		lines = nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "diff --git a/%s b/%s\nnew file mode 100644\n--- /dev/null\n+++ b/%s\n", name, name, name)
	if len(lines) > 0 {
		fmt.Fprintf(&b, "@@ -0,0 +1,%d @@\n", len(lines))
		for _, l := range lines {
			b.WriteString("+" + l + "\n")
		}
	}
	return b.String(), true
}

// DiffFiles lists the paths a unified diff touches, in order.
func DiffFiles(diff string) []string {
	var files []string
	seen := map[string]bool{}
	add := func(p string) {
		if p != "" && !seen[p] {
			seen[p] = true
			files = append(files, p)
		}
	}
	var minus string
	for _, line := range strings.Split(diff, "\n") {
		switch {
		case strings.HasPrefix(line, "--- "):
			minus = diffPath(line[4:], "a/")
		case strings.HasPrefix(line, "+++ "):
			if p := diffPath(line[4:], "b/"); p != "" {
				add(p)
			} else {
				add(minus)
			}
		}
	}
	return files
}

func diffPath(s, prefix string) string {
	s = strings.TrimRight(s, "\t\r ")
	s = strings.Trim(s, `"`)
	if s == "/dev/null" {
		return ""
	}
	return strings.TrimPrefix(s, prefix)
}

// CommentableLines maps each file to the new-side line numbers a pull
// request review comment can attach to: added lines and context lines
// inside a hunk.
func CommentableLines(diff string) map[string]map[int]bool {
	out := map[string]map[int]bool{}
	var current string
	next := 0
	inHunk := false
	for _, line := range strings.Split(diff, "\n") {
		switch {
		case strings.HasPrefix(line, "diff --git "):
			inHunk = false
			current = ""
		case !inHunk && strings.HasPrefix(line, "+++ "):
			current = diffPath(line[4:], "b/")
		case strings.HasPrefix(line, "@@"):
			m := hunkHeader.FindStringSubmatch(line)
			if m == nil || current == "" {
				inHunk = false
				continue
			}
			next, _ = strconv.Atoi(m[1])
			inHunk = true
		case inHunk && (strings.HasPrefix(line, "+") || strings.HasPrefix(line, " ")):
			mark(out, current, next)
			next++
		}
	}
	return out
}

func mark(m map[string]map[int]bool, file string, line int) {
	if m[file] == nil {
		m[file] = map[int]bool{}
	}
	m[file][line] = true
}
