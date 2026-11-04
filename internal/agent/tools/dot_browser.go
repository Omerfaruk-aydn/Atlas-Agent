package tools

import (
	"context"
	"sync"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/browser"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/dots"
)

// dotBrowser attaches the browser tool to the dot's private Chrome.
// It never copies the owner's profile and never falls back to the
// process-wide browser manager.
type dotBrowser struct {
	dir string
	cfg config.ToolBrowser

	mu  sync.Mutex
	url string
	mgr *browser.Manager
}

func newDotBrowser(dir string, cfg config.ToolBrowser) *dotBrowser {
	return &dotBrowser{dir: dir, cfg: cfg}
}

func (d *dotBrowser) Session(id string) (browser.Session, error) {
	ep, err := dots.Ensure(context.Background(), d.dir)
	if err != nil {
		return nil, err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.mgr == nil || d.url != ep.HTTP {
		if d.mgr != nil {
			d.mgr.CloseAll()
		}
		d.url = ep.HTTP
		d.mgr = browser.NewManager(browser.Options{
			RemoteURL:       ep.HTTP,
			Headless:        true,
			UserDataDir:     dots.ProfileDir(d.dir),
			UseRealProfile:  false,
			OverlayDisabled: true,
			ExecutablePath:  d.cfg.ExecutablePath,
			ActionTimeout:   d.cfg.GetActionTimeout(),
			IdleTimeout:     d.cfg.GetIdleTimeout(),
		})
	}
	return d.mgr.Session(id)
}

func (d *dotBrowser) Close(id string) {
	d.mu.Lock()
	mgr := d.mgr
	d.mu.Unlock()
	if mgr != nil {
		mgr.Close(id)
	}
}
