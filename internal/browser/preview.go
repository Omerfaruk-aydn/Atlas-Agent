package browser

import (
	"context"
	"errors"

	"github.com/chromedp/chromedp"
)

// CaptureExisting observes an already open tab and never launches a browser.
func CaptureExisting(ctx context.Context, id string) ([]byte, error) {
	manager := currentManager()
	if manager == nil {
		return nil, errors.New("no active browser")
	}
	manager.mu.Lock()
	sess := manager.sessions[id]
	manager.mu.Unlock()
	if sess == nil {
		return nil, errors.New("no active browser for the selected session")
	}
	if driver, ok := sess.(interface {
		Capture(context.Context) ([]byte, error)
	}); ok {
		return driver.Capture(ctx)
	}
	return nil, errors.New("browser driver cannot provide a live preview")
}

// ExistingOwnershipResource returns a lease identity without reconfiguring Chrome.
func ExistingOwnershipResource(id string) string {
	manager := currentManager()
	if manager == nil {
		return "browser/" + id
	}
	return manager.OwnershipResource(id)
}

// Capture uses the caller's cancellation without changing tab lifetime.
func (s *chromedpSession) Capture(parent context.Context) ([]byte, error) {
	ctx, cancel := context.WithCancel(s.currentContext())
	defer cancel()
	stop := context.AfterFunc(parent, cancel)
	defer stop()
	var data []byte
	err := chromedp.Run(ctx, chromedp.CaptureScreenshot(&data))
	return data, err
}
