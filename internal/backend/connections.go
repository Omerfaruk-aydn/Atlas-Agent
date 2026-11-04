package backend

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/connections"
)

func (b *Backend) connectionService(workspaceID string) (*Workspace, *connections.Service, error) {
	ws, err := b.GetWorkspace(workspaceID)
	if err != nil {
		return nil, nil, err
	}
	return ws, connections.New(ws.Cfg, filepath.Dir(config.GlobalConfigData())), nil
}

// ListConnections returns the redacted connection catalog.
func (b *Backend) ListConnections(workspaceID string) ([]connections.View, error) {
	ws, svc, err := b.connectionService(workspaceID)
	if err != nil {
		return nil, err
	}
	views := svc.List()
	applyElevenLabsAccountState(views, ws.Cfg)
	return views, nil
}

// SaveConnection saves one allowlisted connection and publishes its new state.
func (b *Backend) SaveConnection(workspaceID, id string, req connections.SaveRequest) error {
	ws, svc, err := b.connectionService(workspaceID)
	if err != nil {
		return err
	}
	if err := svc.Save(id, req); err != nil {
		return err
	}
	publishConfigChanged(ws)
	return nil
}

// TestConnection makes a read-only request to the configured provider.
func (b *Backend) TestConnection(ctx context.Context, workspaceID, id string) (connections.TestResult, error) {
	ws, svc, err := b.connectionService(workspaceID)
	if err != nil {
		return connections.TestResult{}, err
	}
	if id == "elevenlabs" {
		if result, handled, err := testElevenLabsAccount(ws.Cfg); handled {
			return result, err
		}
	}
	return svc.Test(ctx, id)
}

// StartConnectionOAuth starts a loopback-only OAuth callback for this workspace.
func (b *Backend) StartConnectionOAuth(ctx context.Context, workspaceID, id, returnURL string, reauth bool) (connections.OAuthStart, error) {
	if id == "elevenlabs" {
		return b.startElevenLabsOAuth(workspaceID, reauth)
	}
	_, svc, err := b.connectionService(workspaceID)
	if err != nil {
		return connections.OAuthStart{}, err
	}
	switch id {
	case "google":
		return svc.StartGoogleOAuth(ctx, returnURL)
	case "outlook":
		return svc.StartOutlookOAuth(ctx)
	case "slack", "github", "notion", "discord":
		return svc.StartAccountOAuth(ctx, id)
	default:
		return connections.OAuthStart{}, fmt.Errorf("this connection does not use OAuth")
	}
}
