package app

import (
	"context"
	"errors"
	"path/filepath"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agent"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/connections"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/voice"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/voice/service"
)

// RunVoiceService serves the phone voice assistant until ctx ends. The
// coder agent must be initialized first. Every call, listener and agent
// turn it started is stopped before it returns; app shutdown cancels it
// through the global context.
func (app *App) RunVoiceService(ctx context.Context) error {
	cfg := voice.Config(app.config.Config().Tools.VoiceCalls)
	if !cfg.Enabled {
		return voice.ErrServiceDisabled
	}
	if app.AgentCoordinator == nil {
		return errors.New("configure an agent model before starting the voice service")
	}
	svc, err := service.New(service.Options{
		Config:   cfg,
		DataDir:  agent.VoiceDataDir(),
		Env:      connections.Env(filepath.Dir(config.GlobalConfigData())),
		Runner:   &agent.VoiceTurnRunner{Coordinator: app.AgentCoordinator, Sessions: app.Sessions},
		ReadOnly: agent.VoiceReadOnlyTool,
	})
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(app.globalCtx, cancel)
	defer stop()
	return svc.Run(ctx)
}
