package evaluation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/subagents"
)

type (
	LiveCheck struct {
		Criterion string   `json:"criterion"`
		Command   []string `json:"command"`
	}
	LiveCase struct {
		Scenario string      `json:"scenario"`
		Prompt   string      `json:"prompt"`
		Checks   []LiveCheck `json:"checks"`
	}
	LiveManifest struct {
		Repeats        int        `json:"repeats,omitempty"`
		Role           string     `json:"role,omitempty"`
		Repository     string     `json:"repository"`
		Models         []string   `json:"models"`
		Cases          []LiveCase `json:"cases"`
		TimeoutSeconds int        `json:"timeout_seconds,omitempty"`
		PromptVersion  string     `json:"prompt_version"`
	}
)

type LiveRecord struct {
	CaseHash    string `json:"case_hash,omitempty"`
	Baseline    string `json:"baseline,omitempty"`
	FixtureHash string `json:"fixture_hash,omitempty"`
	RecordedAt  int64  `json:"recorded_at,omitempty"`
	Run
	Workspace     string `json:"workspace"`
	SessionID     string `json:"session_id,omitempty"`
	AgentError    string `json:"agent_error,omitempty"`
	ClaimObserved bool   `json:"claim_observed"`
	MetricsSource string `json:"metrics_source"`
}

type CommandRunner func(context.Context, string, string, []string) ([]byte, error)

func RunCommand(ctx context.Context, dir, program string, args []string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, program, args...)
	cmd.Dir = dir
	// JSON non-interactive output requires the local execution path.
	for _, v := range os.Environ() {
		if !strings.HasPrefix(v, "ATLAS_AGENT_CLIENT_SERVER=") && !strings.HasPrefix(v, "GIT_DIR=") && !strings.HasPrefix(v, "GIT_WORK_TREE=") && !strings.HasPrefix(v, "GIT_INDEX_FILE=") && !strings.HasPrefix(v, "GIT_COMMON_DIR=") {
			cmd.Env = append(cmd.Env, v)
		}
	}
	cmd.Env = append(cmd.Env, "ATLAS_AGENT_CLIENT_SERVER=false")
	var out limitedLiveOutput
	var stderr limitedLiveOutput
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	err := cmd.Run()
	if out.exceeded || stderr.exceeded {
		return out.data, errors.New("benchmark output exceeded 1 MiB")
	}
	if err != nil && len(stderr.data) > 0 {
		err = fmt.Errorf("%w: %s", err, stderr.data[:min(len(stderr.data), 1024)])
	}
	return out.data, err
}

type limitedLiveOutput struct {
	data     []byte
	exceeded bool
}

func (b *limitedLiveOutput) Write(p []byte) (int, error) {
	n := len(p)
	if len(b.data)+n > 1024*1024 {
		b.exceeded = true
		p = p[:max(0, 1024*1024-len(b.data))]
	}
	b.data = append(b.data, p...)
	return n, nil
}

func ValidateLive(m LiveManifest) error {
	repeats := m.Repeats
	if repeats == 0 {
		repeats = 1
		if m.Role != "" {
			repeats = 3
		}
	}
	if repeats < 1 || repeats > 20 || len(m.Models)*len(m.Cases)*repeats > 256 {
		return errors.New("repeats must be 1-20 with at most 256 total runs")
	}
	if m.Role != "" {
		if _, ok := subagents.Find(subagents.Builtin(), m.Role); !ok {
			return errors.New("role must name a built-in specialist")
		}
	}
	if m.Repository == "" || m.PromptVersion == "" || len(m.Models) == 0 || len(m.Models) > 8 || len(m.Cases) == 0 || len(m.Cases) > 9 {
		return errors.New("repository, prompt_version, 1-8 models and 1-9 cases are required")
	}
	if m.TimeoutSeconds < 0 || m.TimeoutSeconds > 1800 {
		return errors.New("timeout_seconds must be at most 1800")
	}
	scenarios, err := Scenarios()
	if err != nil {
		return err
	}
	known := map[string]Scenario{}
	for _, s := range scenarios {
		known[s.ID] = s
	}
	seen := map[string]bool{}
	for _, model := range m.Models {
		if model == "" || strings.HasPrefix(model, "-") || seen[model] {
			return errors.New("models must be distinct explicit IDs")
		}
		seen[model] = true
	}
	seen = map[string]bool{}
	for _, c := range m.Cases {
		s, ok := known[c.Scenario]
		if !ok || seen[c.Scenario] || c.Prompt == "" || len(c.Prompt) > 32*1024 {
			return errors.New("cases need unique known scenarios and concrete prompts")
		}
		seen[c.Scenario] = true
		criteria := map[string]bool{}
		for _, check := range c.Checks {
			if !slices.Contains(s.Criteria, check.Criterion) || criteria[check.Criterion] || len(check.Command) == 0 || len(check.Command) > 64 {
				return errors.New("checks require unique scenario criteria and executable argument arrays")
			}
			criteria[check.Criterion] = true
			for _, arg := range check.Command {
				if len(arg) > 4096 || strings.ContainsRune(arg, 0) {
					return errors.New("invalid checker argument")
				}
			}
		}
		if len(criteria) != len(s.Criteria) {
			return errors.New("every scenario criterion requires an external checker")
		}
	}
	return nil
}

// RunLive preserves each disposable worktree and incrementally writes results.
// It runs actual models only when the user explicitly invokes the eval run command.
func RunLive(ctx context.Context, m LiveManifest, program, output string, allowTools bool, runner CommandRunner) ([]LiveRecord, error) {
	if err := ValidateLive(m); err != nil {
		return nil, err
	}
	root, err := filepath.Abs(m.Repository)
	if err != nil {
		return nil, err
	}
	output, err = filepath.Abs(output)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(output), 0o700); err != nil {
		return nil, err
	}
	store := engineering.NewStore(filepath.Join(filepath.Dir(output), ".atlas-evaluations"))
	timeout := time.Duration(m.TimeoutSeconds) * time.Second
	if timeout == 0 {
		timeout = 5 * time.Minute
	}
	if runner == nil {
		runner = RunCommand
	}
	var records []LiveRecord
	repeats := m.Repeats
	if repeats == 0 {
		repeats = 1
		if m.Role != "" {
			repeats = 3
		}
	}
	for _, model := range m.Models {
		for range repeats {
			for _, c := range m.Cases {
				if err := ctx.Err(); err != nil {
					return records, err
				}
				w, err := store.CreateWorkspace(ctx, root, "benchmark", c.Scenario)
				if err != nil {
					return records, err
				}
				started := time.Now()
				runCtx, cancel := context.WithTimeout(ctx, timeout)
				role := m.Role
				if role == "" {
					role = "none"
				}
				args := []string{"--cwd", w.Path, "--data-dir", filepath.Join(w.Path, ".atlas-eval-state"), "run", "--model", model, "--role", role, "--json", "--quiet", c.Prompt}
				if allowTools {
					args = append([]string{"--yolo"}, args...)
				}
				data, runErr := runner(runCtx, w.Path, program, args)
				cancel()
				var result struct {
					SessionID        string  `json:"session_id"`
					PromptTokens     int64   `json:"prompt_tokens"`
					CompletionTokens int64   `json:"completion_tokens"`
					Cost             float64 `json:"cost"`
					ClaimedComplete  *bool   `json:"claimed_complete"`
				}
				decodeErr := json.Unmarshal(data, &result)
				record := LiveRecord{Run: Run{Scenario: c.Scenario, Model: model, PromptVersion: m.PromptVersion, Tokens: result.PromptTokens + result.CompletionTokens, Cost: result.Cost}, Workspace: w.Path, SessionID: result.SessionID}
				record.MetricsSource = "session_context_fallback"
				if result.ClaimedComplete != nil {
					record.ClaimObserved = true
					record.ClaimedComplete = *result.ClaimedComplete
				}
				if runErr != nil {
					record.AgentError = runErr.Error()
				} else if decodeErr != nil {
					record.AgentError = "agent returned invalid JSON: " + decodeErr.Error()
				}
				record.ExecutionError = record.AgentError
				record.Role = m.Role
				record.Baseline = w.Base
				fixture, _ := json.Marshal(c)
				record.CaseHash = engineering.Hash(string(fixture))
				if m.Role != "" {
					role, _ := subagents.Find(subagents.Builtin(), m.Role)
					fixture = append(fixture, []byte(role.Instructions+role.RolePrompt())...)
				}
				record.FixtureHash = engineering.Hash(string(fixture))
				record.RecordedAt = time.Now().Unix()
				for _, check := range c.Checks {
					checkCtx, done := context.WithTimeout(ctx, timeout)
					observed, checkErr := runner(checkCtx, w.Path, check.Command[0], check.Command[1:])
					done()
					passed := checkErr == nil
					evidence := fmt.Sprintf("Executed %q in %s; passed=%t; output SHA256=%s", check.Command, w.Path, passed, engineering.Hash(string(observed)))
					if checkErr != nil {
						evidence += "; " + checkErr.Error()
					}
					record.Checks = append(record.Checks, Check{Criterion: check.Criterion, Passed: passed, Evidence: evidence})
				}
				record.DurationMS = time.Since(started).Milliseconds()
				if result.SessionID != "" {
					state, err := engineering.NewStore(filepath.Join(w.Path, ".atlas-eval-state")).Read(ctx, result.SessionID)
					if err == nil && state.Revision > 0 {
						record.MetricsSource = "cumulative_runtime_ledger"
						record.Tokens = state.Usage.Tokens
						record.Cost = state.Usage.Cost
						record.ToolCalls = state.Usage.ToolCalls
						record.RepeatedToolCalls = state.Usage.RepeatedFailedCalls
					}
				}
				records = append(records, record)
				encoded, err := json.MarshalIndent(records, "", "  ")
				if err != nil {
					return records, err
				}
				if err := engineering.AtomicWrite(output, encoded); err != nil {
					return records, err
				}
			}
		}
	}
	return records, nil
}
