package tools

import (
	"context"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/browser"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/computer"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/interaction"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/vault"
)

func recordableBrowserAction(action string) bool {
	switch action {
	case "navigate", "back", "forward", "click", "semantic_click", "scroll", "tab_select":
		return true
	}
	return false
}

func recordableComputerAction(action string) bool {
	switch action {
	case "move", "click", "double_click", "right_click", "drag", "scroll", "focus", "invoke":
		return true
	}
	return false
}

func captureBrowserTrace(ctx context.Context, sess browser.Session, root, id string) string {
	if vault.Sensitive("") {
		return ""
	}
	if ctx.Err() != nil {
		return ""
	}
	driver, ok := sess.(interface {
		Capture(context.Context) ([]byte, error)
	})
	if !ok {
		return ""
	}
	data, err := driver.Capture(ctx)
	if err != nil {
		return ""
	}
	path, _ := interaction.Default.Capture(root, id, data)
	return path
}

func captureComputerTrace(backend computer.Backend, root, id string) string {
	if vault.Sensitive("") {
		return ""
	}
	data, err := backend.Screenshot()
	if err != nil {
		return ""
	}
	path, _ := interaction.Default.Capture(root, id, data)
	return path
}
