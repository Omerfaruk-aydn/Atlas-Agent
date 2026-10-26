package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/reviewbot"
	"github.com/spf13/cobra"
)

type githubPREnvironment struct {
	Repository string
	HeadSHA    string
}

func ghOutput(ctx context.Context, root string, args ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, "gh", args...)
	command.Dir = root
	command.Env = os.Environ()
	var stderr bytes.Buffer
	command.Stderr = &stderr
	output, err := command.Output()
	if err != nil {
		return nil, fmt.Errorf("gh %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return output, nil
}

func loadGitHubPR(ctx context.Context, root string, number int) (githubPREnvironment, string, error) {
	data, err := ghOutput(ctx, root, "pr", "view", fmt.Sprint(number), "--json", "headRefOid,url")
	if err != nil {
		return githubPREnvironment{}, "", err
	}
	var view struct {
		HeadSHA string `json:"headRefOid"`
		URL     string `json:"url"`
	}
	if err := json.Unmarshal(data, &view); err != nil {
		return githubPREnvironment{}, "", err
	}
	parts := strings.Split(strings.TrimPrefix(view.URL, "https://github.com/"), "/")
	if len(parts) < 2 || view.HeadSHA == "" {
		return githubPREnvironment{}, "", fmt.Errorf("could not determine PR repository and head commit")
	}
	diff, err := ghOutput(ctx, root, "pr", "diff", fmt.Sprint(number))
	return githubPREnvironment{Repository: parts[0] + "/" + parts[1], HeadSHA: view.HeadSHA}, string(diff), err
}

func reviewModelJSON(ctx context.Context, root, prompt string) ([]byte, error) {
	executable, err := os.Executable()
	if err != nil {
		return nil, err
	}
	command := exec.CommandContext(ctx, executable, "run", "--cwd", root, "--role", "review", "--json", "--", prompt)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	output, err := command.Output()
	if err != nil {
		return nil, fmt.Errorf("review model: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	var result struct {
		Response string `json:"response"`
	}
	if err := json.Unmarshal(output, &result); err != nil {
		return nil, fmt.Errorf("review model returned invalid JSON envelope: %w", err)
	}
	return []byte(strings.TrimSpace(result.Response)), nil
}

func pullRequestFindings(ctx context.Context, root string, number int, minimum string) ([]reviewbot.Finding, error) {
	pr, diff, err := loadGitHubPR(ctx, root, number)
	if err != nil {
		return nil, err
	}
	first, err := reviewModelJSON(ctx, root, "Review this pull request read-only. Return only JSON with a findings array. Each finding needs path, line, severity (low|medium|high|critical), title, body, evidence and confirmed=false. Do not invent missing evidence.\n<patch>"+diff+"</patch>")
	if err != nil {
		return nil, err
	}
	candidates, err := reviewbot.DecodeFindings(string(first))
	if err != nil {
		return nil, err
	}
	encoded, _ := json.Marshal(candidates)
	second, err := reviewModelJSON(ctx, root, "Second-pass validation. Re-open the cited code and verify each candidate against its actual trigger and consequence. Return only the same findings JSON schema. Preserve confirmed=false unless the evidence is directly present; set confirmed=true only for verified defects.\n"+string(encoded))
	if err != nil {
		return nil, err
	}
	verified, err := reviewbot.DecodeFindings(string(second))
	if err != nil {
		return nil, err
	}
	publishable, err := reviewbot.Publishable(verified, minimum)
	if err != nil {
		return nil, err
	}
	comments, err := ghOutput(ctx, root, "api", "--paginate", "--slurp", fmt.Sprintf("repos/%s/pulls/%d/comments", pr.Repository, number))
	if err != nil {
		return nil, err
	}
	var pages [][]struct {
		Body string `json:"body"`
	}
	if err := json.Unmarshal(comments, &pages); err != nil {
		return nil, fmt.Errorf("decode existing PR comments: %w", err)
	}
	var bodies []string
	for _, page := range pages {
		for _, comment := range page {
			bodies = append(bodies, comment.Body)
		}
	}
	return reviewbot.NewFindings(publishable, bodies), nil
}

func publishPRFindings(ctx context.Context, root string, number int, findings []reviewbot.Finding) error {
	pr, _, err := loadGitHubPR(ctx, root, number)
	if err != nil {
		return err
	}
	for _, finding := range findings {
		_, err := ghOutput(ctx, root, "api", fmt.Sprintf("repos/%s/pulls/%d/comments", pr.Repository, number),
			"-f", "body="+reviewbot.Comment(finding), "-f", "commit_id="+pr.HeadSHA,
			"-f", "path="+finding.Path, "-F", fmt.Sprintf("line=%d", finding.Line), "-f", "side=RIGHT")
		if err != nil {
			return err
		}
	}
	return nil
}

func addPRReviewFlags(command *cobra.Command) {
	command.Flags().Int("pr", 0, "Review a GitHub pull request through gh")
	command.Flags().Bool("publish", false, "Publish only second-pass verified, not previously posted findings")
	command.Flags().String("min-severity", "medium", "Minimum PR comment severity: low, medium, high or critical")
}
