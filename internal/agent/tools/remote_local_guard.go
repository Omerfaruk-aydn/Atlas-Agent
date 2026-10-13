package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
)

// RemoteTargetFunc reports the /support computer this one controls: its
// name and session id, or empty strings when none is connected.
type RemoteTargetFunc func() (name, sessionID string)

// remoteLocalGuard stops the reflex of acting on this computer while the
// user is working on a supported one. In the reported session, "go to
// Google Stitch" during a /support session opened the local browser. The
// changing local browser or desktop call is refused until the trusted user
// instruction selects this computer. A tool retry cannot grant that choice.
// Reads (screenshots, window lists, viewing an image) are never blocked.
type remoteLocalGuard struct {
	fantasy.AgentTool
	target RemoteTargetFunc
}

type remoteGuardAcks struct{}

// WithRemoteLocalGuard wraps the local browser, computer and pipeline
// tools; other tools are returned unchanged. acks may be shared by the
// wrapped tools so one explanation covers all of them.
func WithRemoteLocalGuard(tool fantasy.AgentTool, target RemoteTargetFunc, _ *remoteGuardAcks) fantasy.AgentTool {
	if target == nil {
		return tool
	}
	switch tool.Info().Name {
	case BrowserToolName, ComputerToolName, ToolPipelineName, BashToolName:
		return &remoteLocalGuard{AgentTool: tool, target: target}
	}
	return tool
}

// NewRemoteGuardAcks returns acknowledgement state shared by a tool set.
func NewRemoteGuardAcks() *remoteGuardAcks { return &remoteGuardAcks{} }

func (g *remoteLocalGuard) Run(ctx context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
	name, sessionID := g.target()
	if sessionID != "" && call.Name == BashToolName && !localComputerRequested(ctx) {
		if secs, ok := pureSleep(call.Input); ok && secs >= 10 {
			return fantasy.NewTextErrorResponse(fmt.Sprintf("use_remote_wait: sleeping %d s on this computer does not watch %q. Nothing was run. Use remote_support action wait with wait_for stable (or change, text, text_gone) and timeout_ms; it returns as soon as the remote screen shows the result.", secs, name)), nil
		}
	}
	if sessionID == "" || call.Name == BashToolName || !CallMutates(call.Name, call.Input) {
		return g.AgentTool.Run(ctx, call)
	}
	if localComputerRequested(ctx) || isDesignPreviewCall(ctx, call) {
		return g.AgentTool.Run(ctx, call)
	}
	msg := fmt.Sprintf("wrong_computer_check: a /support session to %q is active, and %s acts on THIS computer, not on %q. Nothing was done. "+
		"If the user meant the supported computer, use remote_support (tool=%s with its device_id and session_id). "+
		"Only an explicit user instruction selecting this computer can change the target; repeating this call does not authorize it.", name, call.Name, name, call.Name)
	return fantasy.NewTextErrorResponse(msg), nil
}

var pureSleepPattern = regexp.MustCompile(`^\s*(?:sleep|Start-Sleep(?:\s+-Seconds)?|timeout(?:\s+/t)?)\s+(\d+)\s*(?:(?:;|&&)\s*echo\b[^;&|]*)?$`)

// pureSleep reports a bash call that only sleeps (optionally followed by
// an echo) and for how many seconds.
func pureSleep(input string) (int, bool) {
	var p struct {
		Command string `json:"command"`
	}
	if json.Unmarshal([]byte(input), &p) != nil {
		return 0, false
	}
	m := pureSleepPattern.FindStringSubmatch(strings.TrimSpace(p.Command))
	if m == nil {
		return 0, false
	}
	n, err := strconv.Atoi(m[1])
	return n, err == nil
}
