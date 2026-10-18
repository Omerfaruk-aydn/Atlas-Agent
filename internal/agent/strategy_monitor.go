package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
)

// maxStrategySignals bounds consecutive strategy reviews of a stalled
// approach. A successful non-polling observation starts a fresh episode.
const maxStrategySignals = 2

// strategyWindow is how many recent tool interactions are inspected.
const strategyWindow = 6

// strategyMonitor watches the tool interactions of one turn for evidence
// that the current approach has stopped producing information: the same
// class of failure across different calls, or the same call returning the
// same result. It asks for a different method once per distinct signal and
// stops the turn when consecutive signals keep coming without recovery.
type strategyMonitor struct {
	mu       sync.Mutex
	signals  int
	pending  string
	stopped  string
	lastSeen int
}

type toolInteraction struct {
	callID, tool, input, output string
	metadata                    string
	isError                     bool
	stop                        bool
}

func interactions(steps []fantasy.StepResult) []toolInteraction {
	var out []toolInteraction
	for _, step := range steps {
		results := map[string]fantasy.ToolResultContent{}
		for _, r := range step.Content.ToolResults() {
			results[r.ToolCallID] = r
		}
		for _, call := range step.Content.ToolCalls() {
			r, ok := results[call.ToolCallID]
			if !ok {
				continue
			}
			_, isErr := r.Result.(fantasy.ToolResultOutputContentError)
			out = append(out, toolInteraction{callID: call.ToolCallID, tool: call.ToolName, input: call.Input, output: toolResultOutputString(r.Result), metadata: r.ClientMetadata, isError: isErr, stop: r.StopTurn})
		}
	}
	return out
}

var (
	errorNumbers = regexp.MustCompile(`\d+`)
	errorQuoted  = regexp.MustCompile("(\"[^\"]*\"|'[^']*'|`[^`]*`)")
	errorPaths   = regexp.MustCompile(`([A-Za-z]:)?[\\/][^\s:]+`)
	errorSpaces  = regexp.MustCompile(`\s+`)
)

// errorClass normalizes a failure so that the same failure on different
// inputs, paths or line numbers compares equal.
func errorClass(output string, metadata ...string) string {
	var structured struct {
		Code      string `json:"code"`
		ErrorCode string `json:"error_code"`
	}
	for _, data := range append(metadata, output) {
		structured.Code, structured.ErrorCode = "", ""
		if json.Unmarshal([]byte(data), &structured) == nil {
			if code := cmpOr(structured.Code, structured.ErrorCode); code != "" {
				return strings.ToLower(code)
			}
		}
	}
	s := strings.ToLower(output)
	if i := strings.IndexByte(s, '\n'); i > 0 && i < 240 {
		s = s[:i]
	}
	s = errorQuoted.ReplaceAllString(s, "*")
	s = errorPaths.ReplaceAllString(s, "<path>")
	s = errorNumbers.ReplaceAllString(s, "#")
	s = strings.TrimSpace(errorSpaces.ReplaceAllString(s, " "))
	if len(s) > 160 {
		s = s[:160]
	}
	return s
}

func interactionHash(i toolInteraction) string {
	h := sha256.Sum256([]byte(i.tool + "\x00" + i.input + "\x00" + i.output))
	return hex.EncodeToString(h[:8])
}

// strategySignal is an observed reason to change method.
type strategySignal struct {
	Kind     string
	Class    string
	Tools    []string
	Evidence string
}

func detectStrategySignal(steps []fantasy.StepResult) (strategySignal, bool) {
	all := interactions(steps)
	if len(all) == 0 {
		return strategySignal{}, false
	}
	window := all[max(0, len(all)-strategyWindow):]
	last := window[len(window)-1]
	if last.stop {
		return strategySignal{}, false
	}
	// Three consecutive failures of the same class, whatever the inputs.
	if n := len(window); n >= 3 {
		tail := window[n-3:]
		class := errorClass(tail[0].output, tail[0].metadata)
		same := class != ""
		inputs := map[string]bool{}
		var tools []string
		for _, i := range tail {
			if !i.isError || i.tool != last.tool || errorClass(i.output, i.metadata) != class {
				same = false
				break
			}
			inputs[i.tool+"\x00"+i.input] = true
			tools = append(tools, i.tool)
		}
		if same {
			kind := "repeated_failure"
			if len(inputs) > 1 {
				kind = "rephrased_retry"
			}
			return strategySignal{Kind: kind, Class: class, Tools: tools, Evidence: last.callID}, true
		}
	}
	// Three identical calls with identical results: nothing new is learned.
	if n := len(window); n >= 3 && !expectedPolling(last) {
		h := interactionHash(window[n-1])
		if interactionHash(window[n-2]) == h && interactionHash(window[n-3]) == h {
			return strategySignal{Kind: "no_new_information", Class: last.tool + " returned the same result three times", Tools: []string{last.tool}, Evidence: last.callID}, true
		}
	}
	return strategySignal{}, false
}

// Polling for an asynchronous job or native condition may legitimately
// return identical observations; the tool's own timeout bounds that wait.
func expectedPolling(i toolInteraction) bool {
	if i.tool == "job_output" || i.tool == "log_tail" {
		return true
	}
	if i.tool != "browser" && i.tool != "computer" {
		return false
	}
	var p struct {
		Action string `json:"action"`
	}
	if json.Unmarshal([]byte(i.input), &p) != nil {
		return false
	}
	return p.Action == "wait" || p.Action == "wait_for"
}

// observe runs after every step. It returns true when the turn must stop.
func (m *strategyMonitor) observe(ctx context.Context, store *engineering.Store, sessionID string, steps []fantasy.StepResult) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	// Only a new tool interaction can carry a new signal; a text step
	// after a review must not re-raise the same tail of failures.
	all := interactions(steps)
	seen := len(all)
	if seen == m.lastSeen {
		return m.stopped != ""
	}
	m.lastSeen = seen
	signal, ok := detectStrategySignal(steps)
	if !ok {
		if seen > 0 && !all[seen-1].isError && !expectedPolling(all[seen-1]) {
			m.signals = 0
		}
		return false
	}
	// A signal that is still present after a review counts again: the
	// review did not change the approach.
	m.signals++
	recordStrategySignal(ctx, store, sessionID, signal, m.signals > maxStrategySignals)
	if m.signals > maxStrategySignals {
		m.stopped = fmt.Sprintf("stopped: %s (%s) persisted after %d strategy reviews", signal.Kind, signal.Class, maxStrategySignals)
		return true
	}
	m.pending = fmt.Sprintf(`<strategy_review>
Observed %s: %s (tools: %s; latest call %s).
Repeating this will not produce new information. Before the next call: name the failure class, decide what new evidence would distinguish its causes, and switch method (inspect the cause directly, build a minimal reproduction, try an alternative API, or ask the user if it is blocked on their decision). Record the change with workflow {"action":"work","work_action":"method","work":{"method":...,"reason":...,"evidence":["%s"]}}. If an earlier call may have changed state, inspect the target before sending it again. After %d consecutive reviews without a successful observation, a further signal ends the turn.
</strategy_review>`, signal.Kind, signal.Class, strings.Join(signal.Tools, ", "), signal.Evidence, signal.Evidence, maxStrategySignals)
	return false
}

// takePending returns the review to inject into the next request, once.
func (m *strategyMonitor) takePending() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	p := m.pending
	m.pending = ""
	return p
}

func recordStrategySignal(ctx context.Context, store *engineering.Store, sessionID string, signal strategySignal, stopping bool) {
	if store == nil || sessionID == "" {
		return
	}
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	_, err := store.UpdateWork(cleanup, sessionID, func(w *engineering.WorkState, _ engineering.State) error {
		now := time.Now()
		w.Strategy.Signals++
		w.Strategy.LastClass, w.Strategy.LastAt = signal.Kind+": "+signal.Class, now.UnixMilli()
		what := strings.Join(signal.Tools, ", ") + " calls"
		_ = w.RecordAttempt(what, truncateRunes(signal.Kind+": "+signal.Class, 120), signal.Evidence, "", "strategy_monitor", now)
		if stopping {
			w.Phase, w.Waiting = "waiting", "turn stopped: "+truncateRunes(signal.Class, 200)+" kept recurring after strategy reviews; a different approach or user input is needed"
		}
		return nil
	})
	if err != nil {
		// Recording is best effort; the review itself still reaches the model.
		slog.Warn("Failed to record strategy signal", "session_id", sessionID, "error", err)
	}
}

// stoppedReason reports why the monitor ended the turn, if it did.
func (m *strategyMonitor) stoppedReason() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.stopped
}
