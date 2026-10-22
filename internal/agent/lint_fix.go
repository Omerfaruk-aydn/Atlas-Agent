package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agent/tools"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/harnessmetrics"
	"github.com/google/uuid"
)

type qualityTool struct {
	fantasy.AgentTool
	agent *sessionAgent
}

func (a *sessionAgent) qualityTools(palette []fantasy.AgentTool) []fantasy.AgentTool {
	if !a.completionGateActive() {
		return palette
	}
	out := slices.Clone(palette)
	for i, t := range out {
		out[i] = &qualityTool{AgentTool: t, agent: a}
	}
	return out
}

func (a *sessionAgent) diagnostics(ctx context.Context, id, path string) ([]string, error) {
	var lines []string
	observed := false
	for _, tool := range observedTools(trackSourceRoot(guardTools(a.tools.Copy(), a.engineering, a.sessions), a.workingDir)) {
		if tool.Info().Name != tools.DiagnosticsToolName {
			continue
		}
		input, _ := json.Marshal(tools.DiagnosticsParams{FilePath: path})
		resp, err := tool.Run(context.WithValue(ctx, tools.SessionIDContextKey, id), fantasy.ToolCall{ID: uuid.NewString(), Name: tools.DiagnosticsToolName, Input: string(input)})
		if err != nil || resp.IsError {
			return nil, fmt.Errorf("LSP diagnostics unavailable")
		}
		var meta tools.DiagnosticsMetadata
		if json.Unmarshal([]byte(resp.Metadata), &meta) == nil && meta.Observed {
			observed = true
		}
		for line := range strings.SplitSeq(resp.Content, "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "Error") {
				lines = append(lines, line)
			}
		}
	}
	opts := a.harnessOptions()
	if opts != nil && opts.LintCommand != "" {
		resp, err := a.invokeCheck(ctx, id, a.projectRoot(id), opts.LintCommand)
		if err != nil {
			return nil, err
		}
		exit, finished := bashExitCode(resp.Metadata)
		if !finished || resp.StopTurn {
			return nil, fmt.Errorf("fast lint did not complete")
		}
		observed = true
		if exit != 0 || resp.IsError {
			for line := range strings.SplitSeq(evaluatorTruncate(resp.Content), "\n") {
				if strings.TrimSpace(line) != "" {
					lines = append(lines, "Lint: "+line)
				}
			}
			if len(lines) == 0 {
				return nil, fmt.Errorf("fast lint failed without diagnostic evidence")
			}
		}
	}
	if !observed {
		return nil, fmt.Errorf("no observed LSP diagnostics or configured fast linter")
	}
	return lines, nil
}

func diagnosticDelta(before, after []string) []string {
	known := map[string]int{}
	for _, line := range before {
		known[errorClass(line)]++
	}
	var out []string
	for _, line := range after {
		if known[errorClass(line)] == 0 {
			out = append(out, line)
		} else {
			known[errorClass(line)]--
		}
	}
	return out
}

func (t *qualityTool) Run(ctx context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
	id := tools.GetSessionFromContext(ctx)
	if id == "" {
		return t.AgentTool.Run(ctx, call)
	}
	a := t.agent
	opts := a.harnessOptions()
	potential := slices.Contains(evaluatorFileTools, call.Name) || call.Name == "bash" || tools.CallMutates(call.Name, call.Input)
	if !potential {
		return t.AgentTool.Run(ctx, call)
	}
	var before []string
	var diagnosticErr error
	seconds := 10
	if opts != nil && opts.LintTimeoutSeconds > 0 {
		seconds = opts.LintTimeoutSeconds
	}
	if opts.LintFixEnabled() {
		check, cancel := context.WithTimeout(ctx, time.Duration(seconds)*time.Second)
		before, diagnosticErr = a.diagnostics(check, id, "")
		cancel()
	}
	resp, err := t.AgentTool.Run(ctx, call)
	a.syncProjectEvidence(ctx, id)
	if err != nil || resp.IsError {
		return resp, err
	}
	l := a.evaluatorLedgerFor(id)
	l.mu.Lock()
	defer l.mu.Unlock()
	if opts.LintFixEnabled() {
		check, cancel := context.WithTimeout(ctx, time.Duration(seconds)*time.Second)
		defer cancel()
		_, gate := startGate(ctx, "lint_fix")
		after, afterErr := a.diagnostics(check, id, "")
		if diagnosticErr != nil || afterErr != nil {
			l.qualityErrors = []string{"Lint observations are unavailable; rerun diagnostics before completion"}
			gate.finish("unverified", "diagnostics_unavailable")
		} else {
			if l.lintBaseline == nil {
				l.lintBaseline = before
				if l.lintBaseline == nil {
					l.lintBaseline = []string{}
				}
			}
			l.qualityErrors = diagnosticDelta(l.lintBaseline, after)
			if len(l.qualityErrors) > 0 {
				gate.finish("send_back", "new_errors")
			} else {
				gate.finish("pass", "no_new_errors")
			}
		}
	}
	if len(l.qualityErrors) > 0 {
		resp.Content += "\n\n<required_lint_fix>Fix these new errors before further implementation or completion:\n" + strings.Join(l.qualityErrors, "\n") + "\n</required_lint_fix>"
	}
	if !l.fuzzSuggested && opts.FuzzHintsEnabled() {
		for _, c := range a.baselineChanges(l) {
			if hint := fuzzHint(c.path, c.after); hint != "" {
				resp.Content += "\n" + hint
				l.fuzzSuggested = true
				a.sessionTrace(id).record(harnessmetrics.TraceEvent{Kind: "fuzz_suggestion", Reason: "changed_parser_codec_numeric"})
				break
			}
		}
	}
	return resp, nil
}

func fuzzHint(path, source string) string {
	lower := strings.ToLower(source)
	for _, word := range []string{"parse", "decode", "encode", "marshal", "round", "decimal"} {
		if strings.Contains(lower, word) {
			return "Advisory: changed parser/codec/numeric code in " + path + "; consider a round-trip/boundary property or Go testing.F fuzz test to cover inputs examples may miss."
		}
	}
	return ""
}
