package tools

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/experiments"
)

//go:embed experiments.md
var experimentsDescription string

type ExperimentParams struct {
	Argv          []string `json:"argv,omitempty" description:"Literal executable and arguments; minimization requires one exact {input} argument."`
	Input         string   `json:"input,omitempty" description:"Text input, at most 32KiB, supplied as a single literal argument."`
	ExpectedExit  int      `json:"expected_exit,omitempty" description:"Nonzero expected reproducer exit code."`
	FailureMarker string   `json:"failure_marker,omitempty" description:"Nonempty literal output signature of the same failure."`
	Budget        int      `json:"budget,omitempty" description:"Minimization run budget 2-64; default 16."`
	BaselineArgv  []string `json:"baseline_argv,omitempty"`
	CandidateArgv []string `json:"candidate_argv,omitempty"`
	Samples       int      `json:"samples,omitempty" description:"Benchmark samples per variant, 3-10; default 5."`
}

type experimentRecord struct {
	Kind       string          `json:"kind"`
	SessionID  string          `json:"session_id"`
	Source     string          `json:"source"`
	InputHash  string          `json:"input_hash"`
	ResultHash string          `json:"result_hash"`
	Succeeded  bool            `json:"succeeded"`
	At         int64           `json:"at"`
	Result     json.RawMessage `json:"result,omitempty"`
}

func experimentNamespace(root string) string { return "experiments-" + engineering.Hash(root) }

func experimentRun(ctx context.Context, root string, argv []string, invoke ToolInvoker, id string) (experiments.Observation, *fantasy.ToolResponse, error) {
	if len(argv) == 0 || len(argv) > 64 || len(argv[0]) == 0 {
		return experiments.Observation{}, nil, fmt.Errorf("require 1-64 literal arguments")
	}
	for _, arg := range argv {
		if len(arg) > 32768 || strings.ContainsRune(arg, 0) {
			return experiments.Observation{}, nil, fmt.Errorf("invalid or oversized argument")
		}
	}
	data, err := json.Marshal(BashParams{Argv: argv, WorkingDir: root, Description: "Engineering experiment"})
	if err != nil {
		return experiments.Observation{}, nil, err
	}
	if invoke == nil {
		return experiments.Observation{}, nil, fmt.Errorf("guarded command dispatch unavailable")
	}
	runCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	resp, err := invoke(runCtx, fantasy.ToolCall{ID: id, Name: BashToolName, Input: string(data)})
	if err != nil {
		return experiments.Observation{}, nil, err
	}
	if resp.IsError || resp.StopTurn {
		return experiments.Observation{}, &resp, fmt.Errorf("experiment command was denied or unavailable")
	}
	var meta BashResponseMetadata
	if err := json.Unmarshal([]byte(resp.Metadata), &meta); err != nil || meta.ExitCode == nil || meta.Background || meta.EndTime < meta.StartTime {
		return experiments.Observation{}, nil, fmt.Errorf("command has no observed foreground exit and duration")
	}
	return experiments.Observation{ExitCode: *meta.ExitCode, Output: meta.Output, DurationMS: meta.EndTime - meta.StartTime}, nil, nil
}

func NewExperimentTools(root string, store *engineering.Store, invoke ToolInvoker) []fantasy.AgentTool {
	var result []fantasy.AgentTool
	for _, name := range []string{"bug_reproduce", "repro_minimize", "benchmark_compare"} {
		result = append(result, fantasy.NewAgentTool(name, experimentsDescription, func(ctx context.Context, p ExperimentParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			source, err := engineering.SourceFingerprint(ctx, root, store.Dir())
			if err != nil {
				return fantasy.NewTextErrorResponse(err.Error()), nil
			}
			index := 0
			var denied *fantasy.ToolResponse
			run := func(argv []string) (experiments.Observation, error) {
				index++
				observation, rejection, err := experimentRun(ctx, root, argv, invoke, fmt.Sprintf("%s-experiment-%d", call.ID, index))
				if rejection != nil {
					denied = rejection
				}
				return observation, err
			}
			var outcome any
			passed := false
			switch name {
			case "bug_reproduce", "repro_minimize":
				if p.ExpectedExit == 0 || strings.TrimSpace(p.FailureMarker) == "" || len(p.FailureMarker) > 1024 || len(p.Input) > 32768 {
					return fantasy.NewTextErrorResponse("require a nonzero expected_exit, failure_marker <=1024 bytes and input <=32KiB"), nil
				}
				oracle := func(ctx context.Context, input string) (experiments.Observation, error) {
					argv := slices.Clone(p.Argv)
					for i := range argv {
						if argv[i] == "{input}" {
							argv[i] = input
						}
					}
					return run(argv)
				}
				if name == "repro_minimize" {
					if !slices.Contains(p.Argv, "{input}") {
						return fantasy.NewTextErrorResponse("minimization requires an exact {input} argv placeholder"), nil
					}
					if p.Budget == 0 {
						p.Budget = 16
					}
					outcome, err = experiments.Minimize(ctx, p.Input, p.ExpectedExit, p.FailureMarker, p.Budget, oracle)
					passed = err == nil
				} else {
					var observation experiments.Observation
					observation, err = oracle(ctx, p.Input)
					passed = err == nil && observation.ExitCode == p.ExpectedExit && strings.Contains(observation.Output, p.FailureMarker)
					outcome = map[string]any{"reproduced": passed, "observation": observation}
				}
			case "benchmark_compare":
				if p.Samples == 0 {
					p.Samples = 5
				}
				if p.Samples < 3 || p.Samples > 10 {
					return fantasy.NewTextErrorResponse("samples must be 3-10"), nil
				}
				baseline, candidate := make([]float64, 0, p.Samples), make([]float64, 0, p.Samples)
				for i := 0; i < p.Samples && err == nil; i++ {
					variants := [][]string{p.BaselineArgv, p.CandidateArgv}
					if i%2 != 0 {
						variants[0], variants[1] = variants[1], variants[0]
					}
					for j, argv := range variants {
						var observation experiments.Observation
						observation, err = run(argv)
						if err != nil {
							break
						}
						if observation.ExitCode != 0 {
							err = fmt.Errorf("benchmark command exited %d", observation.ExitCode)
							break
						}
						if (j == 0) == (i%2 == 0) {
							baseline = append(baseline, float64(observation.DurationMS))
						} else {
							candidate = append(candidate, float64(observation.DurationMS))
						}
					}
				}
				if err == nil {
					b, bErr := experiments.Summarize(baseline)
					c, cErr := experiments.Summarize(candidate)
					if bErr != nil {
						err = bErr
					} else if cErr != nil {
						err = cErr
					} else {
						outcome = map[string]any{"baseline": b, "candidate": c, "mean_delta_ms": c.Mean - b.Mean, "os": runtime.GOOS, "arch": runtime.GOARCH, "scope": "process wall time; not CPU, allocation or statistical significance"}
						passed = true
					}
				}
			}
			if denied != nil {
				return *denied, nil
			}
			if err != nil {
				return fantasy.NewTextErrorResponse(err.Error()), nil
			}
			after, err := engineering.SourceFingerprint(ctx, root, store.Dir())
			if err != nil {
				return fantasy.NewTextErrorResponse(err.Error()), nil
			}
			if after != source {
				return fantasy.NewTextErrorResponse("workspace source changed during the experiment; result is not comparable"), nil
			}
			data, err := json.Marshal(outcome)
			if err != nil {
				return fantasy.ToolResponse{}, err
			}
			input, _ := json.Marshal(p)
			ns := experimentNamespace(root)
			record, history, readErr := store.ReadRecord(ctx, ns, "history")
			var records []experimentRecord
			if readErr == nil {
				if err := json.Unmarshal(history, &records); err != nil {
					return fantasy.NewTextErrorResponse(err.Error()), nil
				}
			} else if !errors.Is(readErr, os.ErrNotExist) {
				return fantasy.NewTextErrorResponse(readErr.Error()), nil
			}
			var savedResult json.RawMessage
			if len(data) <= 32*1024 {
				savedResult = data
			}
			records = append([]experimentRecord{{Kind: name, SessionID: GetSessionFromContext(ctx), Source: source, InputHash: engineering.Hash(string(input)), ResultHash: engineering.Hash(string(data)), Succeeded: passed, At: time.Now().UnixMilli(), Result: savedResult}}, records...)
			records = records[:min(len(records), 20)]
			manifest, err := json.Marshal(records)
			if err != nil {
				return fantasy.ToolResponse{}, err
			}
			if _, err := store.PutRecord(ctx, ns, "history", record.Revision, manifest); err != nil {
				return fantasy.NewTextErrorResponse(err.Error()), nil
			}
			return fantasy.NewTextResponse(string(data)), nil
		}))
	}
	return append(result, fantasy.NewAgentTool("failure_history", experimentsDescription, func(ctx context.Context, _ struct{}, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
		_, data, err := store.ReadRecord(ctx, experimentNamespace(root), "history")
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return fantasy.NewTextErrorResponse("experiment history unavailable: " + err.Error()), nil
		}
		var records []experimentRecord
		if len(data) > 0 {
			if err := json.Unmarshal(data, &records); err != nil {
				return fantasy.NewTextErrorResponse(err.Error()), nil
			}
		}
		source, err := engineering.SourceFingerprint(ctx, root, store.Dir())
		if err != nil {
			return fantasy.NewTextErrorResponse(err.Error()), nil
		}
		items := make([]map[string]any, 0, len(records))
		for _, record := range records {
			items = append(items, map[string]any{"record": record, "current_source": record.Source == source})
		}
		_, knowledge, err := store.ProjectKnowledge(ctx, root)
		if err != nil {
			return fantasy.NewTextErrorResponse(err.Error()), nil
		}
		lessons := make([]engineering.KnowledgeView, 0)
		for _, view := range knowledge {
			if view.Record.Kind == "lesson" {
				lessons = append(lessons, view)
			}
		}
		data, err = json.Marshal(map[string]any{"experiments": items, "repair_lessons": lessons})
		return fantasy.NewTextResponse(string(data)), err
	}))
}
