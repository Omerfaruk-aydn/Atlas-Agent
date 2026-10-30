package codereview

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
)

// GH runs the gh CLI in dir with stdin and returns stdout. It is the seam
// for posting reviews; tests replace it.
type GH func(ctx context.Context, dir, stdin string, args ...string) (string, error)

// RunGH is the production [GH].
func RunGH(ctx context.Context, dir, stdin string, args ...string) (string, error) {
	if _, err := exec.LookPath("gh"); err != nil {
		return "", errors.New("gh is not installed or not on PATH")
	}
	cmd := exec.CommandContext(ctx, "gh", args...)
	cmd.Dir = dir
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("gh %s: %s", strings.Join(args, " "), clip(msg, 300))
	}
	return stdout.String(), nil
}

// InlineComment is one review comment attached to a line of the new file.
type InlineComment struct {
	Path string `json:"path"`
	Line int    `json:"line"`
	Side string `json:"side"`
	Body string `json:"body"`
}

// ReviewPayload is the body of a GitHub pull request review.
type ReviewPayload struct {
	CommitID string          `json:"commit_id,omitempty"`
	Body     string          `json:"body"`
	Event    string          `json:"event"`
	Comments []InlineComment `json:"comments,omitempty"`
}

// BuildReview turns a report into a pull request review. Findings whose
// line is part of the diff become inline comments; the rest are listed in
// the review body, because GitHub rejects the whole review when one inline
// comment points outside the diff.
func BuildReview(r *Report, t Target) ReviewPayload {
	commentable := CommentableLines(t.Diff)
	payload := ReviewPayload{Event: "COMMENT"}
	var general []Finding
	for _, f := range r.Findings {
		if commentable[f.Path][f.Line] {
			payload.Comments = append(payload.Comments, InlineComment{Path: f.Path, Line: f.Line, Side: "RIGHT", Body: commentBody(f)})
			continue
		}
		general = append(general, f)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Automated code review (effort %s): %d finding(s).\n\n%s\n", r.Effort, len(r.Findings), r.Summary)
	if len(general) > 0 {
		b.WriteString("\nFindings outside the changed lines:\n")
		for _, f := range general {
			fmt.Fprintf(&b, "\n- **[%s] %s** (`%s:%d`)\n  %s\n", f.Severity, f.Title, f.Path, f.Line, f.Scenario)
			if f.Suggestion != "" {
				fmt.Fprintf(&b, "  Suggested fix: %s\n", f.Suggestion)
			}
		}
	}
	payload.Body = b.String()
	return payload
}

func commentBody(f Finding) string {
	var b strings.Builder
	fmt.Fprintf(&b, "**[%s] %s**\n\n%s", f.Severity, f.Title, f.Scenario)
	if f.Suggestion != "" {
		fmt.Fprintf(&b, "\n\nSuggested fix: %s", f.Suggestion)
	}
	fmt.Fprintf(&b, "\n\n_Confidence %.2f%s._", f.Confidence, map[bool]string{true: ", verified", false: ""}[f.Verified])
	return b.String()
}

var prURLParts = regexp.MustCompile(`github\.com/([^/\s]+)/([^/\s]+)/pull/(\d+)`)

// PostReview posts the report to the pull request as one COMMENT review
// and returns the review URL. It posts nothing and returns an empty URL
// when the report has no findings. Callers must have the user's explicit
// consent before calling it.
func PostReview(ctx context.Context, gh GH, t Target, r *Report) (string, error) {
	if t.Kind != KindPR || t.PR == nil {
		return "", errors.New("--comment needs a pull request target")
	}
	if len(r.Findings) == 0 {
		return "", nil
	}
	m := prURLParts.FindStringSubmatch(t.PR.URL)
	if m == nil {
		return "", fmt.Errorf("cannot determine the repository from pull request URL %q", t.PR.URL)
	}
	owner, repo, number := m[1], m[2], m[3]
	head, err := gh(ctx, t.Root, "", "pr", "view", number, "-R", owner+"/"+repo, "--json", "headRefOid", "-q", ".headRefOid")
	if err != nil {
		return "", err
	}
	payload := BuildReview(r, t)
	payload.CommitID = strings.TrimSpace(head)
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	out, err := gh(ctx, t.Root, string(body), "api", "--method", "POST", fmt.Sprintf("repos/%s/%s/pulls/%s/reviews", owner, repo, number), "--input", "-")
	if err != nil {
		return "", err
	}
	var resp struct {
		HTMLURL string `json:"html_url"`
	}
	_ = json.Unmarshal([]byte(out), &resp)
	return resp.HTMLURL, nil
}
