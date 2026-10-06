package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/activity"
	cdpbrowser "github.com/chromedp/cdproto/browser"
	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

type browserViewport struct {
	Width, Height, DPR float64
}

// browserScreenTransform maps CSS viewport input to physical desktop pixels.
// Chrome zoom is included in DPR; desktop DPI alone is not sufficient.
type browserScreenTransform struct {
	Left, Top, Scale float64
}

func (v browserViewport) valid() bool {
	return finitePositive(v.Width) && finitePositive(v.Height) &&
		finitePositive(v.DPR) && v.DPR <= 16 && v.Width*v.DPR < 1e6 && v.Height*v.DPR < 1e6
}

func finitePositive(v float64) bool { return v > 0 && !math.IsNaN(v) && !math.IsInf(v, 0) }

func (t browserScreenTransform) point(x, y float64) (int, int) {
	return int(math.Round(t.Left + x*t.Scale)), int(math.Round(t.Top + y*t.Scale))
}

// desktopBrowserEvent retains the last verified desktop point between browser
// actions instead of following an unrelated physical mouse during model waits.
func desktopBrowserEvent(e activity.Event, t browserScreenTransform, valid bool, lastX, lastY int, hasLast bool) activity.Event {
	if e.Point && valid {
		x, y := float64(e.X), float64(e.Y)
		if e.PointerRevision != 0 {
			x, y = e.PointerX, e.PointerY
		}
		e.X, e.Y = t.point(x, y)
		e.PointerX, e.PointerY = float64(e.X), float64(e.Y)
	} else if hasLast {
		e.X, e.Y, e.Point = lastX, lastY, true
		e.PointerX, e.PointerY = float64(lastX), float64(lastY)
		// An unresolved CSS target must not produce feedback at a stale point.
		if !valid && e.PointerKind != "" {
			e.PointerKind = ""
		}
	} else {
		e.Point, e.PointerKind = false, ""
	}
	return e
}

// readNativeBrowserViewport uses an isolated world so page scripts cannot
// replace the geometry getters used to position a desktop indicator.
func (s *chromedpSession) readNativeBrowserViewport() (browserViewport, *cdpbrowser.Bounds, error) {
	ctx, cancel := context.WithTimeout(s.currentContext(), 300*time.Millisecond)
	defer cancel()
	var view browserViewport
	var bounds *cdpbrowser.Bounds
	err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		c := chromedp.FromContext(ctx)
		if c == nil || c.Browser == nil || c.Target == nil {
			return fmt.Errorf("browser geometry is unavailable")
		}
		var err error
		_, bounds, err = cdpbrowser.GetWindowForTarget().WithTargetID(c.Target.TargetID).Do(cdp.WithExecutor(ctx, c.Browser))
		if err != nil {
			return err
		}
		frames, err := page.GetFrameTree().Do(ctx)
		if err != nil {
			return err
		}
		world, err := page.CreateIsolatedWorld(frames.Frame.ID).WithWorldName("AtlasDesktopGeometry").Do(ctx)
		if err != nil {
			return err
		}
		result, exception, err := runtime.Evaluate(`({Width:innerWidth,Height:innerHeight,DPR:devicePixelRatio})`).WithContextID(world).WithReturnByValue(true).Do(ctx)
		if err != nil {
			return err
		}
		if exception != nil {
			return fmt.Errorf("read browser viewport: %s", exception.Error())
		}
		return json.Unmarshal(result.Value, &view)
	}))
	if err == nil && !view.valid() {
		err = fmt.Errorf("invalid browser viewport geometry")
	}
	return view, bounds, err
}
