package tools

import (
	"context"
	_ "embed"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/planspec"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/walkthrough"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/workplan"
)

//go:embed walkthrough.md
var walkthroughDescription string

const WalkthroughToolName = "walkthrough"

// approvedPlanFile is where exit_plan_mode records the plan the user
// approved, so the walkthrough can compare it with what changed.
const approvedPlanFile = "approved-plan.json"

type approvedPlan struct {
	Digest   string   `json:"digest"`
	Paths    []string `json:"paths"`
	ReplanIf []string `json:"replan_if,omitempty"`
}

// saveApprovedPlan records an approved structured plan under .atlas. A plan
// with no tagged entries is not recorded; failures are ignored because the
// record only improves the final report.
func saveApprovedPlan(workingDir string, spec planspec.Plan) {
	if len(spec.Entries) == 0 {
		return
	}
	dir := filepath.Join(workingDir, workplan.Dir)
	if os.MkdirAll(dir, 0o755) != nil {
		return
	}
	data, err := json.Marshal(approvedPlan{Digest: spec.Digest, Paths: spec.Paths(), ReplanIf: spec.ReplanIf})
	if err != nil {
		return
	}
	_ = os.WriteFile(filepath.Join(dir, approvedPlanFile), data, 0o644)
}

type WalkthroughParams struct {
	Goal string `json:"goal,omitempty" description:"One line saying what the work was for. Optional."`
}

// NewWalkthroughTool builds the end-of-task report from recorded facts.
func NewWalkthroughTool(workingDir string) fantasy.AgentTool {
	return fantasy.NewAgentTool(
		WalkthroughToolName,
		walkthroughDescription,
		func(ctx context.Context, params WalkthroughParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			in := walkthrough.Input{Goal: params.Goal, ChangedPaths: gitChangedPaths(ctx, workingDir)}
			if data, err := os.ReadFile(filepath.Join(workingDir, workplan.Dir, approvedPlanFile)); err == nil {
				var plan approvedPlan
				if json.Unmarshal(data, &plan) == nil {
					in.PlanDigest, in.PlannedPaths = plan.Digest, plan.Paths
				}
			}
			if greens, err := workplan.Greens(workingDir); err == nil && len(greens) > 0 {
				for _, cmd := range greens[len(greens)-1].Passed {
					in.Checks = append(in.Checks, walkthrough.Check{Command: cmd, Passed: true})
				}
			}
			if session := GetSessionFromContext(ctx); session != "" {
				for _, r := range workplan.LoadSessionState(workingDir, session).RedEvidence {
					if r.GreenSequence > 0 {
						in.RedGreen = append(in.RedGreen, walkthrough.RedGreen{Command: r.Command})
					}
				}
			}
			return fantasy.NewTextResponse(walkthrough.Render(in)), nil
		},
	)
}

// gitChangedPaths lists files that differ from HEAD, including untracked
// ones. It returns nil outside a git repository.
func gitChangedPaths(ctx context.Context, dir string) []string {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "git", "-C", dir, "status", "--porcelain", "-uall").Output()
	if err != nil {
		return nil
	}
	var paths []string
	for _, line := range strings.Split(strings.ReplaceAll(string(out), "\r\n", "\n"), "\n") {
		if len(line) < 4 {
			continue
		}
		p := strings.TrimSpace(line[3:])
		if _, after, ok := strings.Cut(p, " -> "); ok {
			p = after
		}
		if strings.HasPrefix(p, workplan.Dir+"/") || strings.HasPrefix(p, "tmp/") {
			continue
		}
		paths = append(paths, strings.Trim(p, `"`))
	}
	return paths
}
