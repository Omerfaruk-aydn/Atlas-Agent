package agent

import (
	"context"
	"errors"
	"path/filepath"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agent/tools"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/permission"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/session"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/voice"
)

// VoiceDataDir is where the voice service keeps its state: the global data
// directory, so routines and sessions in any project reach the same
// service.
func VoiceDataDir() string { return filepath.Dir(config.GlobalConfigData()) }

func dialVoiceService() (tools.VoiceCalls, error) {
	return voice.DialControl(VoiceDataDir())
}

// VoiceTurnRunner runs voice turns on the coordinator. Each call gets its
// own session. Turns run without permission prompts, because nobody is at
// the terminal: a tool that would need a prompt is refused unless the
// voice policy got an explicit spoken confirmation for that one call.
type VoiceTurnRunner struct {
	Coordinator Coordinator
	Sessions    session.Service
}

// NewSession implements voice.TurnRunner.
func (r *VoiceTurnRunner) NewSession(ctx context.Context, title string) (string, error) {
	s, err := r.Sessions.Create(ctx, title)
	if err != nil {
		return "", err
	}
	return s.ID, nil
}

// RunTurn implements voice.TurnRunner.
func (r *VoiceTurnRunner) RunTurn(ctx context.Context, req voice.TurnRequest) (voice.TurnResult, error) {
	ctx = permission.WithoutPrompts(ctx)
	res, err := r.Coordinator.Run(ctx, req.SessionID, req.Prompt)
	if err != nil {
		if errors.Is(err, permission.ErrPromptUnavailable) {
			return voice.TurnResult{}, voice.Errorf(voice.CodePermissionRequired, "voice turn", err)
		}
		return voice.TurnResult{}, voice.Errorf(voice.CodeAgentFailed, "voice turn", err)
	}
	if res == nil {
		return voice.TurnResult{}, voice.Errorf(voice.CodeAgentFailed, "voice turn", errors.New("no response"))
	}
	return voice.TurnResult{Text: res.Response.Content.Text()}, nil
}

// VoiceReadOnlyTool reports the tools a voice turn may run without spoken
// confirmation by default.
func VoiceReadOnlyTool(name string) bool {
	return permission.CategoryForTool(name) == permission.CategoryReadOnly
}
