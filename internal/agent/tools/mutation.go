package tools

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/experiments"
)

//go:embed mutation_test.md
var mutationTestDescription string

type MutationTestParams struct {
	BaselineArgv []string `json:"baseline_argv"`
	MutationArgv []string `json:"mutation_argv"`
	ReportPath   string   `json:"report_path" description:"Workspace-relative fresh JSON report under the configured engineering data directory."`
}

func NewMutationTestTool(root string, store *engineering.Store, invoke ToolInvoker) fantasy.AgentTool {
	return fantasy.NewAgentTool("mutation_test", mutationTestDescription, func(ctx context.Context, p MutationTestParams, call fantasy.ToolCall) (response fantasy.ToolResponse, runErr error) {
		path := filepath.Join(root, p.ReportPath)
		rel, err := filepath.Rel(store.Dir(), path)
		if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) || !strings.HasSuffix(strings.ToLower(path), ".json") {
			return fantasy.NewTextErrorResponse("mutation report must be a JSON file under the engineering data directory"), nil
		}
		if _, err := engineering.ReadProjectEvidence(ctx, root, p.ReportPath); err == nil {
			return fantasy.NewTextErrorResponse("use a fresh report path; existing mutation reports cannot be replayed"), nil
		} else if !os.IsNotExist(err) {
			return fantasy.NewTextErrorResponse(err.Error()), nil
		}
		source, err := engineering.SourceFingerprint(ctx, root, store.Dir())
		if err != nil {
			return fantasy.NewTextErrorResponse(err.Error()), nil
		}
		defer func() {
			checkCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
			defer cancel()
			after, err := engineering.SourceFingerprint(checkCtx, root, store.Dir())
			if err != nil || source != after {
				stopped := response.StopTurn
				response = fantasy.NewTextErrorResponse("Workspace source integrity could not be confirmed after the mutation run; inspect and recover before continuing")
				response.StopTurn = stopped
			}
		}()
		baseline, denied, err := experimentRun(ctx, root, p.BaselineArgv, invoke, call.ID+"-baseline")
		if denied != nil {
			return *denied, nil
		}
		if err != nil {
			return fantasy.NewTextErrorResponse(err.Error()), nil
		}
		if baseline.ExitCode != 0 {
			return fantasy.NewTextErrorResponse("mutation baseline failed; fix ordinary tests first"), nil
		}
		start := time.Now()
		mutation, denied, err := experimentRun(ctx, root, p.MutationArgv, invoke, call.ID+"-mutations")
		if denied != nil {
			return *denied, nil
		}
		if err != nil {
			return fantasy.NewTextErrorResponse(err.Error()), nil
		}
		if mutation.ExitCode != 0 {
			return fantasy.NewTextErrorResponse(fmt.Sprintf("mutation engine exited %d; inspect its output", mutation.ExitCode)), nil
		}
		after, err := engineering.SourceFingerprint(ctx, root, store.Dir())
		if err != nil {
			return fantasy.NewTextErrorResponse(err.Error()), nil
		}
		if source != after {
			return fantasy.NewTextErrorResponse("mutation engine changed workspace source; inspect and recover before continuing"), nil
		}
		data, err := engineering.ReadProjectEvidence(ctx, root, p.ReportPath)
		if err != nil {
			return fantasy.NewTextErrorResponse(err.Error()), nil
		}
		info, err := os.Stat(path)
		if err != nil {
			return fantasy.NewTextErrorResponse(err.Error()), nil
		}
		if info.ModTime().Before(start.Add(-2 * time.Second)) {
			return fantasy.NewTextErrorResponse("mutation report is stale"), nil
		}
		result, err := experiments.MutationReport(data)
		if err != nil {
			return fantasy.NewTextErrorResponse(err.Error()), nil
		}
		passed := result.Killed > 0 && result.Survived == 0 && result.NoCoverage == 0 && result.Pending == 0 && result.Invalid == 0
		output, err := json.Marshal(map[string]any{"summary": result, "source_fingerprint": source, "report_hash": engineering.Hash(string(data)), "engine_exit": mutation.ExitCode, "all_mutants_detected": passed})
		return fantasy.WithResponseMetadata(fantasy.NewTextResponse(string(output)), MeasuredCheckMetadata{Observed: true, Passed: passed}), err
	})
}
