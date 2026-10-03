package agent

import (
	"context"
	"fmt"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/browser"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/computer"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/interaction"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/permission"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/vault"
)

type interactionPreviewSuppressed struct{}

func (c *coordinator) captureInteractionPreview(ctx context.Context, id string) error {
	if vault.Sensitive("") {
		return fmt.Errorf("preview disabled after credential use to avoid visual secret exposure")
	}
	state := interaction.Default.Snapshot(id)
	owner := state.LastOwner
	if owner == "" {
		owner = id
	}
	resource := state.LastResource
	if resource == "" {
		resource = "browser"
	}
	if resource == "desktop" && !c.cfg.Config().Tools.Computer.IsEnabled() {
		return fmt.Errorf("computer-use is disabled")
	}
	if resource == "browser" && !c.cfg.Config().Tools.Browser.IsEnabled() {
		return fmt.Errorf("browser-use is disabled")
	}
	if c.permissions == nil {
		return fmt.Errorf("interaction preview permissions unavailable")
	}
	allowed, err := c.permissions.Request(ctx, permission.CreatePermissionRequest{SessionID: id, ToolCallID: "interaction-preview", ToolName: map[string]string{"desktop": "computer", "browser": "browser"}[resource], Action: "screenshot", Description: "Capture the selected interaction for TUI preview", Safe: true})
	if err != nil {
		return err
	}
	if !allowed {
		return fmt.Errorf("interaction preview permission denied")
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		key := "desktop"
		if resource == "browser" {
			key = browser.ExistingOwnershipResource(owner)
		}
		release, err := interaction.Default.Acquire(ctx, id, key, true)
		if err != nil {
			done <- err
			return
		}
		defer release()
		var data []byte
		if resource == "browser" {
			data, err = browser.CaptureExisting(ctx, owner)
		} else {
			var backend computer.Backend
			backend, err = computer.Open()
			if err == nil {
				data, err = backend.Screenshot()
			}
		}
		if err == nil {
			_, err = interaction.Default.Capture(c.cfg.WorkingDir(), id, data)
		}
		done <- err
	}()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}
