package agent

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/harnessmetrics"
)

const (
	// minOutputFloor is the least output room worth sending a request for.
	minOutputFloor = 1024
	// elideKeepBytes of a reduced tool result stay visible.
	elideKeepBytes = 400
	// elideMinBytes is the smallest tool result worth reducing.
	elideMinBytes = 2048
	// protectedTail keeps the latest assistant/tool rounds intact. Pruning is
	// allowed only on older output, even when the active window is small.
	protectedTail = 6
	// adaptivePruneAt starts reclaiming stale outputs before compaction.
	adaptivePruneAt = 0.8
	// adaptivePruneTarget leaves useful room for the next tool rounds.
	adaptivePruneTarget = 0.65
)

// contextOverflowError means a request cannot fit the model that is about to
// answer it even after reduction. It is raised before anything is sent, so the
// caller can compact the conversation instead of letting the provider reject
// it or silently drop the oldest content.
type contextOverflowError struct {
	Model          string
	Window, Needed int64
}

func (e *contextOverflowError) Error() string {
	return fmt.Sprintf("request needs about %d tokens but %s accepts %d", e.Needed, e.Model, e.Window)
}

// fitReport says what fitting a request to a model changed.
type fitReport struct {
	Window, Before, After int64
	OutputCapped          bool
	Elided                int
}

func (r fitReport) changed() bool { return r.OutputCapped || r.Elided > 0 }

// estimateCallTokens approximates the whole request: messages, the tool
// schemas sent with it and nothing else.
func estimateCallTokens(call fantasy.Call) int64 {
	tokens := estimateMessageTokens(call.Prompt)
	for _, tool := range call.Tools {
		if data, err := json.Marshal(tool); err == nil {
			tokens += approxTokenCount(string(data))
		}
	}
	return tokens
}

// requestedOutput is the output room the request reserves on this model.
func requestedOutput(model Model, call fantasy.Call) int64 {
	if call.MaxOutputTokens != nil && *call.MaxOutputTokens > 0 {
		return *call.MaxOutputTokens
	}
	if model.ModelCfg.MaxTokens > 0 {
		return model.ModelCfg.MaxTokens
	}
	return int64(model.CatwalkCfg.DefaultMaxTokens)
}

// fitCall makes a request fit the model that will answer it, using that
// model's own window: system prompt, tool schemas, messages and the output
// reserve all count. It first lowers the output cap, then replaces the bulk of
// the oldest large tool results with a visible marker naming what was left
// out. The most recent messages and every non-tool-result content are never
// touched. A request that still does not fit returns a contextOverflowError.
func fitCall(model Model, call fantasy.Call) (fantasy.Call, fitReport, error) {
	window := activeContextWindow(model)
	report := fitReport{Window: window}
	if window <= 0 {
		return call, report, nil
	}
	prompt := estimateCallTokens(call)
	report.Before, report.After = prompt, prompt
	reserve := requestedOutput(model, call)
	floor := min(reserve, max(int64(minOutputFloor), window/8))
	name := model.ModelCfg.Provider + "/" + model.ModelCfg.Model

	if float64(prompt) >= float64(window)*adaptivePruneAt {
		need := prompt - int64(float64(window)*adaptivePruneTarget)
		call.Prompt, report.Elided = elideOldToolResults(call.Prompt, need)
		prompt = estimateCallTokens(call)
		report.After = prompt
	}
	if prompt+reserve > window {
		// Reduce the prompt first, but only as far as the output floor needs.
		if prompt+floor > window {
			var elided int
			call.Prompt, elided = elideOldToolResults(call.Prompt, prompt+floor-window)
			report.Elided += elided
			prompt = estimateCallTokens(call)
			report.After = prompt
		}
		if prompt+floor > window {
			return call, report, &contextOverflowError{Model: name, Window: window, Needed: prompt + floor}
		}
		if prompt+reserve > window {
			room := window - prompt
			call.MaxOutputTokens = &room
			report.OutputCapped = true
		}
	}
	return call, report, nil
}

// elideOldToolResults replaces text of the oldest tool results above the size
// threshold until about `need` tokens are saved. The prompt it returns is a
// copy; the caller's messages are not modified.
func elideOldToolResults(prompt fantasy.Prompt, need int64) (fantasy.Prompt, int) {
	out := make(fantasy.Prompt, len(prompt))
	copy(out, prompt)
	elided := 0
	var saved int64
	for i := 0; i < len(out)-protectedTail && saved < need; i++ {
		if out[i].Role != fantasy.MessageRoleTool {
			continue
		}
		parts := append([]fantasy.MessagePart(nil), out[i].Content...)
		changed := false
		for j, part := range parts {
			if saved >= need {
				break
			}
			result, ok := toolResultPart(part)
			if !ok {
				continue
			}
			text, ok := toolResultText(result.Output)
			if !ok || len(text) < elideMinBytes {
				continue
			}
			marker := compactToolText(text)
			result.Output = fantasy.ToolResultOutputContentText{Text: marker}
			parts[j] = result
			saved += approxTokenCount(text) - approxTokenCount(marker)
			elided++
			changed = true
		}
		if changed {
			out[i].Content = parts
		}
	}
	return out, elided
}

// compactToolText retains the beginning and diagnostic lines from failures.
// A reduced result remains in its original tool message, so every call keeps
// its matching result and can be repeated to recover the complete output.
func compactToolText(text string) string {
	keep := validPrefix(text, elideKeepBytes)
	var diagnostics strings.Builder
	for _, line := range strings.Split(text, "\n") {
		lower := strings.ToLower(line)
		if strings.Contains(lower, "error") || strings.Contains(lower, "failed") ||
			strings.Contains(lower, "panic") || strings.Contains(lower, "--- fail:") {
			fmt.Fprintf(&diagnostics, "\n[diagnostic] %s", line)
		}
	}
	return fmt.Sprintf("%s%s\n[%d bytes of stale tool output were pruned from this request; run the tool again to see the rest.]", keep, diagnostics.String(), len(text)-len(keep)-diagnostics.Len())
}

func toolResultPart(part fantasy.MessagePart) (fantasy.ToolResultPart, bool) {
	switch p := part.(type) {
	case fantasy.ToolResultPart:
		return p, true
	case *fantasy.ToolResultPart:
		if p != nil {
			return *p, true
		}
	}
	return fantasy.ToolResultPart{}, false
}

func toolResultText(output fantasy.ToolResultOutputContent) (string, bool) {
	switch o := output.(type) {
	case fantasy.ToolResultOutputContentText:
		return o.Text, true
	case *fantasy.ToolResultOutputContentText:
		if o != nil {
			return o.Text, true
		}
	}
	return "", false
}

// fitAttempt applies fitCall to one transport attempt, so the first request,
// a retry and a request moved to a fallback model each meet the budget of the
// model that will actually receive them.
func (a *sessionAgent) fitAttempt(call fantasy.Call, model Model, sessionID string) (fantasy.Call, error) {
	fitted, report, err := fitCall(model, call)
	if err != nil {
		slog.Warn("Request does not fit the model context window", "model", model.ModelCfg.Model, "error", err)
		return call, err
	}
	if report.Elided > 0 {
		a.recordHarness(harnessmetrics.ContextPruned, sessionID, map[string]string{
			"tool_results_reduced": fmt.Sprint(report.Elided),
			"tokens_before":        fmt.Sprint(report.Before),
			"tokens_after":         fmt.Sprint(report.After),
			"context_window":       fmt.Sprint(report.Window),
		})
	}
	if report.changed() {
		slog.Warn("Request reduced to fit the model context window",
			"model", model.ModelCfg.Model, "window", report.Window,
			"tokens_before", report.Before, "tokens_after", report.After,
			"tool_results_reduced", report.Elided, "output_capped", report.OutputCapped)
	}
	return fitted, nil
}
