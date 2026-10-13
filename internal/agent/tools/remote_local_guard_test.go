package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/stretchr/testify/require"
)

type countingTool struct {
	name string
	runs int
}

func (c *countingTool) Info() fantasy.ToolInfo { return fantasy.ToolInfo{Name: c.name} }

func (c *countingTool) Run(context.Context, fantasy.ToolCall) (fantasy.ToolResponse, error) {
	c.runs++
	return fantasy.NewTextResponse("ok"), nil
}

func (c *countingTool) ProviderOptions() fantasy.ProviderOptions   { return nil }
func (c *countingTool) SetProviderOptions(fantasy.ProviderOptions) {}

// Reported session: during a /support session, "go to Google Stitch"
// opened the local browser.
func TestLocalBrowserDuringSupportSessionIsChecked(t *testing.T) {
	t.Parallel()
	session := "ss-d70f"
	target := func() (string, string) { return "Omerfaruk", session }
	acks := NewRemoteGuardAcks()
	browser := &countingTool{name: BrowserToolName}
	computer := &countingTool{name: ComputerToolName}
	gb := WithRemoteLocalGuard(browser, target, acks)
	gc := WithRemoteLocalGuard(computer, target, acks)
	ctx := context.WithValue(t.Context(), SessionIDContextKey, "agent-1")

	navigate := fantasy.ToolCall{Name: BrowserToolName, Input: `{"action":"navigate","url":"https://stitch.withgoogle.com"}`}
	resp, err := gb.Run(ctx, navigate)
	require.NoError(t, err)
	require.True(t, resp.IsError)
	require.Contains(t, resp.Content, "wrong_computer_check")
	require.Contains(t, resp.Content, "remote_support")
	require.Zero(t, browser.runs, "nothing ran on this computer")

	// Reads are never blocked.
	_, err = gc.Run(ctx, fantasy.ToolCall{Name: ComputerToolName, Input: `{"action":"screenshot"}`})
	require.NoError(t, err)
	require.Equal(t, 1, computer.runs)

	// A retry does not mean the user selected this computer.
	resp, err = gb.Run(ctx, navigate)
	require.NoError(t, err)
	require.True(t, resp.IsError)
	require.Zero(t, browser.runs)
	// The trusted human instruction, not tool arguments, selects local work.
	local := WithUserInstruction(ctx, "Bu bilgisayarda Chrome'u aç")
	resp, err = gb.Run(local, navigate)
	require.NoError(t, err)
	require.False(t, resp.IsError)
	require.Equal(t, 1, browser.runs)

	// No session: no check at all.
	session = ""
	other := &countingTool{name: BrowserToolName}
	resp, err = WithRemoteLocalGuard(other, target, NewRemoteGuardAcks()).Run(ctx, navigate)
	require.NoError(t, err)
	require.False(t, resp.IsError)
	require.Equal(t, 1, other.runs)

	// Other tools are untouched.
	view := &countingTool{name: ViewToolName}
	require.Same(t, fantasy.AgentTool(view), WithRemoteLocalGuard(view, target, acks))
}

// Reported session: 14 bash sleeps (11 min 36 s) waited for Stitch.
func TestSleepDuringSupportSessionPointsToRemoteWait(t *testing.T) {
	t.Parallel()
	session := "ss-d70f"
	target := func() (string, string) { return "Omerfaruk", session }
	bash := &countingTool{name: BashToolName}
	g := WithRemoteLocalGuard(bash, target, NewRemoteGuardAcks())
	for _, cmd := range []string{"sleep 58; echo ok", "sleep 45; echo done", "sleep 90", "Start-Sleep -Seconds 30"} {
		in, _ := json.Marshal(map[string]string{"command": cmd})
		resp, err := g.Run(t.Context(), fantasy.ToolCall{Name: BashToolName, Input: string(in)})
		require.NoError(t, err)
		require.True(t, resp.IsError, cmd)
		require.Contains(t, resp.Content, "remote_support action wait")
	}
	require.Zero(t, bash.runs)
	for _, cmd := range []string{"sleep 2", "go test ./... && sleep 30", "sleep 30 && ./deploy.sh"} {
		in, _ := json.Marshal(map[string]string{"command": cmd})
		resp, err := g.Run(t.Context(), fantasy.ToolCall{Name: BashToolName, Input: string(in)})
		require.NoError(t, err)
		require.False(t, resp.IsError, cmd)
	}
	session = ""
	in, _ := json.Marshal(map[string]string{"command": "sleep 58; echo ok"})
	resp, err := g.Run(t.Context(), fantasy.ToolCall{Name: BashToolName, Input: string(in)})
	require.NoError(t, err)
	require.False(t, resp.IsError, "no session: local sleeps are the user's business")
}
