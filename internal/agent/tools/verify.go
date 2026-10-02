package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
)

type ToolInvoker func(context.Context, fantasy.ToolCall) (fantasy.ToolResponse, error)

type VerificationStep struct {
	Name  string          `json:"name" description:"Observable check name."`
	Tool  string          `json:"tool" description:"test_run, lint_run or bash. Commands retain ordinary tool permissions."`
	Input json.RawMessage `json:"input" description:"Arguments for the chosen tool; use foreground commands."`
}

type VerifyParams struct {
	FindingID     string             `json:"finding_id,omitempty" description:"Runtime finding identity associated with executed checks."`
	FindingReview string             `json:"finding_review,omitempty" description:"Runtime source and independent review binding for finding checks."`
	ContractHash  string             `json:"contract_hash,omitempty" description:"Optional runtime contract revision hash bound to actual executed checks; does not grant permissions."`
	Action        string             `json:"action,omitempty" description:"plan (default) discovers checks from the existing stack; run executes supplied checks or the discovered plan sequentially and stops at the first failure."`
	Checks        []VerificationStep `json:"checks,omitempty" description:"At most 12 checks; successful completion requires actual tool results, not submitted assertions."`
}

// ToolOutcomeObserved separates an executed failure from unavailable tooling.
func ToolOutcomeObserved(tool string, resp fantasy.ToolResponse, err error) bool {
	if err != nil || resp.StopTurn {
		return false
	}
	var meta struct {
		OK         *bool `json:"ok"`
		Issues     *int  `json:"issues"`
		ExitCode   *int  `json:"exit_code"`
		Background bool  `json:"background"`
	}
	if json.Unmarshal([]byte(resp.Metadata), &meta) != nil {
		return false
	}
	switch tool {
	case "bash":
		return !meta.Background && meta.ExitCode != nil
	case "test_run":
		return !resp.IsError && meta.OK != nil
	case "lint_run":
		return !resp.IsError && meta.Issues != nil
	}
	return false
}

// ToolEvidenceHash omits volatile call and process identities from diagnostics.
func ToolEvidenceHash(tool string, resp fantasy.ToolResponse, err error) string {
	if err != nil {
		return engineering.Hash(err.Error())
	}
	if tool == BashToolName {
		var metadata BashResponseMetadata
		if json.Unmarshal([]byte(resp.Metadata), &metadata) == nil && metadata.ExitCode != nil && !metadata.Background {
			data, _ := json.Marshal(struct {
				Output   string
				ExitCode int
			}{metadata.Output, *metadata.ExitCode})
			return engineering.Hash(string(data))
		}
	}
	return engineering.Hash(resp.Content)
}

// ToolSucceeded interprets actual machine-readable results, including shell exit codes.
func ToolSucceeded(tool string, resp fantasy.ToolResponse, err error) bool {
	if err != nil || resp.IsError || resp.StopTurn {
		return false
	}
	var meta struct {
		OK         *bool `json:"ok"`
		Issues     *int  `json:"issues"`
		ExitCode   *int  `json:"exit_code"`
		Background bool  `json:"background"`
	}
	_ = json.Unmarshal([]byte(resp.Metadata), &meta)
	switch tool {
	case "test_run":
		return meta.OK != nil && *meta.OK
	case "lint_run":
		return meta.Issues != nil && *meta.Issues == 0
	case "bash":
		return !meta.Background && meta.ExitCode != nil && *meta.ExitCode == 0
	default:
		return true
	}
}

func DiscoverVerification(root string) []VerificationStep {
	var steps []VerificationStep
	add := func(name, tool string, input any) {
		data, _ := json.Marshal(input)
		steps = append(steps, VerificationStep{Name: name, Tool: tool, Input: data})
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err == nil {
		add("build", "bash", BashParams{Description: "Verify Go build", Command: "go build ./...", AutoBackgroundAfter: 1800})
		add("tests", "test_run", TestRunParams{Packages: "./...", Count: 1})
		add("lint", "lint_run", LintRunParams{Packages: "./..."})
		return steps
	}
	if data, err := os.ReadFile(filepath.Join(root, "package.json")); err == nil && len(data) < 256*1024 {
		var pkg struct {
			Scripts map[string]string `json:"scripts"`
		}
		_ = json.Unmarshal(data, &pkg)
		manager := "npm"
		for _, candidate := range []struct{ lock, name string }{{"pnpm-lock.yaml", "pnpm"}, {"yarn.lock", "yarn"}, {"bun.lock", "bun"}} {
			if _, err := os.Stat(filepath.Join(root, candidate.lock)); err == nil {
				manager = candidate.name
				break
			}
		}
		for _, name := range []string{"build", "test", "lint", "typecheck"} {
			if pkg.Scripts[name] != "" {
				add(name, "bash", BashParams{Description: "Verify project " + name, Command: manager + " run " + name, AutoBackgroundAfter: 1800})
			}
		}
	}
	return steps
}

func NewVerifyTool(root string, store *engineering.Store, invoke ToolInvoker, available ...[]string) fantasy.AgentTool {
	return fantasy.NewAgentTool("verify", "Discover and execute a project verification plan using existing tools and their permissions. Failures are recorded and stop the plan; investigate, repair, then run again. No check passes from a model assertion. Unsupported stacks need explicit foreground commands. No command is run by action plan.", func(ctx context.Context, p VerifyParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
		checks := p.Checks
		if len(checks) == 0 {
			checks = DiscoverVerification(root)
			if len(available) > 0 {
				for i, c := range checks {
					if slices.Contains(available[0], c.Tool) {
						continue
					}
					var command string
					switch c.Tool {
					case "test_run":
						command = "go test -count=1 ./..."
					case "lint_run":
						command = "go vet ./..."
						checks[i].Name = "vet"
					}
					if command != "" && slices.Contains(available[0], "bash") {
						input, _ := json.Marshal(BashParams{Description: "Verify project " + checks[i].Name, Command: command, AutoBackgroundAfter: 1800})
						checks[i].Tool = "bash"
						checks[i].Input = input
					}
				}
			}
		}
		if len(checks) > 12 {
			return fantasy.NewTextErrorResponse("at most 12 checks"), nil
		}
		if p.Action == "" || p.Action == "plan" {
			data, _ := json.Marshal(checks)
			return fantasy.NewTextResponse(string(data)), nil
		}
		if p.Action != "run" || len(checks) == 0 {
			return fantasy.NewTextErrorResponse("use plan or run; explicit checks are required for an unknown stack"), nil
		}
		for _, c := range checks {
			if c.Name == "" || len(c.Name) > 128 || len(c.Input) > 16*1024 || !json.Valid(c.Input) || c.Tool != "bash" && c.Tool != "test_run" && c.Tool != "lint_run" {
				return fantasy.NewTextErrorResponse("invalid verification check"), nil
			}
		}
		var results []map[string]any
		for i, c := range checks {
			id := fmt.Sprintf("%s-check-%d", call.ID, i)
			resp, err := invoke(ctx, fantasy.ToolCall{ID: id, Name: c.Tool, Input: string(c.Input)})
			passed := ToolSucceeded(c.Tool, resp, err)
			evidence := fmt.Sprintf("Observed %s call %s; passed=%t; response SHA256=%s", c.Tool, id, passed, engineering.Hash(resp.Content))
			scope := engineering.GetScope(ctx, GetSessionFromContext(ctx))
			if err := store.AppendVerificationCheck(ctx, scope.SessionID, engineering.Check{FindingID: p.FindingID, FindingReview: p.FindingReview, ContractHash: p.ContractHash, InputHash: engineering.Hash(string(c.Input)), Name: c.Name, Tool: c.Tool, Passed: passed, Evidence: evidence, CheckedAt: time.Now().UnixMilli(), TaskID: scope.TaskID, RunID: call.ID}); err != nil {
				return fantasy.ToolResponse{}, err
			}
			output := resp.Content
			if err != nil {
				output = err.Error()
			}
			output = truncateVerification(output, 4096)
			results = append(results, map[string]any{"name": c.Name, "passed": passed, "observed": ToolOutcomeObserved(c.Tool, resp, err), "outcome_hash": ToolEvidenceHash(c.Tool, resp, err), "evidence": evidence, "output": output})
			if !passed {
				data, _ := json.Marshal(map[string]any{"passed": false, "checks": results, "remaining": len(checks) - i - 1})
				return fantasy.NewTextErrorResponse(string(data)), nil
			}
		}
		data, _ := json.Marshal(map[string]any{"passed": true, "checks": results})
		return fantasy.NewTextResponse(string(data)), nil
	})
}

func truncateVerification(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return strings.ToValidUTF8(s[:n], "") + "\n[truncated]"
}
