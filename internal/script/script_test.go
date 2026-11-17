package script

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// fakeHost answers agent() calls from a function.
type fakeHost struct {
	fn    func(ctx context.Context, req AgentRequest) (AgentResult, error)
	calls atomic.Int64
}

func (h *fakeHost) RunAgent(ctx context.Context, req AgentRequest) (AgentResult, error) {
	h.calls.Add(1)
	return h.fn(ctx, req)
}

func echo() *fakeHost {
	return &fakeHost{fn: func(_ context.Context, req AgentRequest) (AgentResult, error) {
		return AgentResult{Text: "echo: " + req.Prompt}, nil
	}}
}

func TestMainsReturnValueIsTheResultAsJSON(t *testing.T) {
	res, err := Run(t.Context(), `
def main():
    return {"total": 2 + 3, "names": ["a", "b"], "ok": True, "none": None}
`, echo(), Limits{})
	require.NoError(t, err)
	require.JSONEq(t, `{"total":5,"names":["a","b"],"ok":true,"none":null}`, res.Value)
	require.Zero(t, res.Agents)
}

func TestAStringResultIsReturnedAsIs(t *testing.T) {
	res, err := Run(t.Context(), "def main():\n    return \"plain text\"\n", echo(), Limits{})
	require.NoError(t, err)
	require.Equal(t, "plain text", res.Value)
}

func TestAgentReturnsItsReplyAndIsCounted(t *testing.T) {
	host := echo()
	res, err := Run(t.Context(), `
def main():
    a = agent("find the bug", agent_name="reviewer")
    return a
`, host, Limits{})
	require.NoError(t, err)
	require.Equal(t, "echo: find the bug", res.Value)
	require.Equal(t, 1, res.Agents)
}

func TestAgentWithASchemaReturnsParsedData(t *testing.T) {
	var got AgentRequest
	host := &fakeHost{fn: func(_ context.Context, req AgentRequest) (AgentResult, error) {
		got = req
		return AgentResult{Text: `{"verdict":"pass","count":3}`, Value: map[string]any{"verdict": "pass", "count": float64(3)}}, nil
	}}
	res, err := Run(t.Context(), `
def main():
    r = agent("review", schema={"type": "object", "required": ["verdict"]})
    return {"passed": r["verdict"] == "pass", "next": r["count"] + 1}
`, host, Limits{})
	require.NoError(t, err)
	require.JSONEq(t, `{"passed":true,"next":4}`, res.Value, "a whole number from JSON is an int in the script")
	require.Equal(t, "object", got.Schema["type"])
}

func TestParallelRunsBranchesConcurrentlyAndKeepsOrder(t *testing.T) {
	var running, peak atomic.Int64
	var barrier sync.WaitGroup
	barrier.Add(3)
	host := &fakeHost{fn: func(_ context.Context, req AgentRequest) (AgentResult, error) {
		n := running.Add(1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		barrier.Done()
		barrier.Wait() // Only returns when all three are inside at once.
		running.Add(-1)
		return AgentResult{Text: req.Prompt}, nil
	}}
	res, err := Run(t.Context(), `
def main():
    return parallel([
        lambda: agent("one"),
        lambda: agent("two"),
        lambda: agent("three"),
    ])
`, host, Limits{})
	require.NoError(t, err)
	require.JSONEq(t, `["one","two","three"]`, res.Value)
	require.Equal(t, int64(3), peak.Load())
	require.Equal(t, 3, res.Agents)
}

func TestAFailedBranchDoesNotStopItsSiblings(t *testing.T) {
	host := &fakeHost{fn: func(_ context.Context, req AgentRequest) (AgentResult, error) {
		if req.Prompt == "bad" {
			return AgentResult{}, errors.New("the model refused")
		}
		return AgentResult{Text: "fine"}, nil
	}}
	res, err := Run(t.Context(), `
def main():
    results = parallel([lambda: agent("ok"), lambda: agent("bad"), lambda: agent("ok")])
    return [hasattr(r, "error") for r in results]
`, host, Limits{})
	require.NoError(t, err)
	require.JSONEq(t, `[false,true,false]`, res.Value)

	res, err = Run(t.Context(), `
def main():
    r = parallel([lambda: agent("bad")])[0]
    return r.error
`, host, Limits{})
	require.NoError(t, err)
	require.Contains(t, res.Value, "the model refused")
}

func TestASequentialFailureStopsTheScript(t *testing.T) {
	host := &fakeHost{fn: func(context.Context, AgentRequest) (AgentResult, error) {
		return AgentResult{}, errors.New("provider down")
	}}
	_, err := Run(t.Context(), "def main():\n    agent(\"x\")\n    return 1\n", host, Limits{})
	require.ErrorContains(t, err, "provider down")
}

// Branches cannot share mutable state; they return values instead.
func TestParallelBranchesCannotMutateSharedState(t *testing.T) {
	res, err := Run(t.Context(), `
def main():
    seen = []
    out = parallel([lambda: seen.append(agent("x"))])
    return hasattr(out[0], "error")
`, echo(), Limits{})
	require.NoError(t, err)
	require.Equal(t, "true", res.Value, "appending to a captured list fails inside a branch")
}

func TestAgentBudgetIsEnforced(t *testing.T) {
	host := echo()
	_, err := Run(t.Context(), `
def main():
    for i in range(10):
        agent("again " + str(i))
    return "done"
`, host, Limits{MaxAgents: 3})
	require.ErrorIs(t, err, ErrBudget)
	require.ErrorContains(t, err, "at most 3 agents")
	require.Equal(t, int64(3), host.calls.Load(), "the fourth agent never started")
}

func TestAnEndlessLoopIsStopped(t *testing.T) {
	start := time.Now()
	_, err := Run(t.Context(), "def main():\n    while True:\n        pass\n", echo(), Limits{MaxSteps: 100_000})
	require.ErrorIs(t, err, ErrBudget)
	require.ErrorContains(t, err, "too many steps")
	require.Less(t, time.Since(start), 5*time.Second)
}

func TestTheWallClockLimitCancelsARunningAgent(t *testing.T) {
	host := &fakeHost{fn: func(ctx context.Context, _ AgentRequest) (AgentResult, error) {
		<-ctx.Done()
		return AgentResult{}, ctx.Err()
	}}
	_, err := Run(t.Context(), "def main():\n    return agent(\"slow\")\n", host, Limits{Timeout: 50 * time.Millisecond})
	require.ErrorIs(t, err, ErrBudget)
	require.ErrorContains(t, err, "ran out of time")
}

func TestLogAndPhaseAreCollectedInOrder(t *testing.T) {
	res, err := Run(t.Context(), `
def main():
    phase("scan")
    log("found", 3, "files")
    phase("fix")
    print("printed too")
    return 0
`, echo(), Limits{})
	require.NoError(t, err)
	require.Equal(t, "== scan ==\nfound 3 files\n== fix ==\nprinted too", res.Log)
}

func TestLogOutputIsCapped(t *testing.T) {
	res, err := Run(t.Context(), `
def main():
    for i in range(1000):
        log("a fairly long line of output to fill the cap " + str(i))
    return 0
`, echo(), Limits{MaxLog: 200})
	require.NoError(t, err)
	require.LessOrEqual(t, len(res.Log), 200+len("…"))
}

func TestScriptsGetNoAmbientAuthority(t *testing.T) {
	for name, code := range map[string]string{
		"open":   "def main():\n    return open(\"/etc/passwd\")\n",
		"load":   "load(\"os\", \"x\")\ndef main():\n    return 1\n",
		"import": "def main():\n    import os\n",
	} {
		_, err := Run(t.Context(), code, echo(), Limits{})
		require.Error(t, err, name)
	}
}

func TestScriptsMustDefineMain(t *testing.T) {
	_, err := Run(t.Context(), "x = 1\n", echo(), Limits{})
	require.ErrorContains(t, err, "main()")
	_, err = Run(t.Context(), "main = 3\n", echo(), Limits{})
	require.ErrorContains(t, err, "main()")
}

func TestErrorsPointAtTheScriptLine(t *testing.T) {
	_, err := Run(t.Context(), "def main():\n    return undefined_name\n", echo(), Limits{})
	require.ErrorContains(t, err, "undefined")
	require.ErrorContains(t, err, "script.star:2")

	_, err = Run(t.Context(), "def main(:\n", echo(), Limits{})
	require.Error(t, err)
}

func TestAgentArgumentsAreChecked(t *testing.T) {
	_, err := Run(t.Context(), "def main():\n    return agent(\"  \")\n", echo(), Limits{})
	require.ErrorContains(t, err, "prompt must not be empty")
	_, err = Run(t.Context(), "def main():\n    return agent(\"x\", schema=\"nope\")\n", echo(), Limits{})
	require.ErrorContains(t, err, "schema must be a dict")
	_, err = Run(t.Context(), "def main():\n    return parallel([agent(\"x\")])\n", echo(), Limits{})
	require.ErrorContains(t, err, "wrap the call")
}

func TestJSONHelpersWork(t *testing.T) {
	res, err := Run(t.Context(), `
def main():
    data = json.decode(agent("x"))
    return json.encode({"n": data["n"] * 2})
`, &fakeHost{fn: func(context.Context, AgentRequest) (AgentResult, error) {
		return AgentResult{Text: `{"n": 21}`}, nil
	}}, Limits{})
	require.NoError(t, err)
	require.Equal(t, `{"n":42}`, res.Value)
}

func TestAgentsUsedReportsTheCount(t *testing.T) {
	res, err := Run(t.Context(), `
def main():
    agent("a")
    agent("b")
    return agents_used()
`, echo(), Limits{})
	require.NoError(t, err)
	require.Equal(t, "2", res.Value)
	require.NotContains(t, res.Log, strings.Repeat("x", 3))
}

func TestSumAddsNumbersAndConcatenatesLists(t *testing.T) {
	res, err := Run(t.Context(), `
def main():
    return [sum([1, 2, 3]), sum([1.5, 2.5]), sum([[1], [2, 3]], []), sum([], 10)]
`, echo(), Limits{})
	require.NoError(t, err)
	require.JSONEq(t, `[6, 4, [1,2,3], 10]`, res.Value)

	_, err = Run(t.Context(), "def main():\n    return sum([1, \"a\"])\n", echo(), Limits{})
	require.ErrorContains(t, err, "sum:")
}
