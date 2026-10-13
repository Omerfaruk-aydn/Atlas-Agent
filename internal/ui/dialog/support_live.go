package dialog

import (
	"context"
	"fmt"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ansi"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/support"
	"github.com/pkg/browser"
)

// supportTimelineRows is how many recent operations the panel lists.
const supportTimelineRows = 8

// liveScreenLines describes the supported computer's latest screen. The
// panel only shows frames the agent already captured: capturing here
// would replace the observation the agent's next click is bound to.
func (d *Support) liveScreenLines(now time.Time) []string {
	title := d.strong(d.com.Text("Remote screen"))
	if !d.hasFrame {
		return []string{title, d.muted(d.com.Text("No screen captured yet in this session."))}
	}
	f := d.frame
	age := now.Sub(time.UnixMilli(f.CapturedAt)).Round(time.Second)
	line := fmt.Sprintf(d.com.Text("Captured %s (%s ago) · %d×%d · connection %d"), time.UnixMilli(f.CapturedAt).Format("15:04:05"), age, f.Width, f.Height, f.Generation)
	lines := []string{title, line}
	if f.Stale(now) {
		lines = append(lines, d.strong(d.com.Text("Stale: the screen may have changed since. It updates when the agent looks again.")))
	}
	lines = append(lines, d.muted(d.com.Text("o opens it in the image viewer.")))
	return lines
}

// timelineLines lists the recent operations of this session, newest last.
func (d *Support) timelineLines(text int) []string {
	lines := []string{d.strong(d.com.Text("Task timeline"))}
	if len(d.timeline) == 0 {
		return append(lines, d.muted(d.com.Text("No operations yet.")))
	}
	for _, e := range d.timeline {
		mark := "✓"
		switch e.State {
		case "ok", "completed", "":
		default:
			mark = "✗"
		}
		if e.Effect == string(support.EffectUnknown) {
			mark = "?"
		}
		row := fmt.Sprintf("  %s %s %s", time.UnixMilli(e.At).Format("15:04:05"), mark, e.Detail)
		if e.State != "" && e.State != "ok" {
			row += " · " + d.com.Text(e.State)
		}
		lines = append(lines, ansi.Truncate(row, max(text, 10), "…"))
	}
	return lines
}

// openFrame opens a session-scoped memory-only viewer, off the UI loop.
func (d *Support) openFrame(_ support.Frame) Action {
	return d.run("open-frame", func(context.Context) SupportDone {
		url, err := d.svc.ActiveController().OpenScreenViewer()
		if err != nil {
			return SupportDone{Err: err}
		}
		if err := browser.OpenURL(url); err != nil {
			return SupportDone{Err: err}
		}
		return SupportDone{Notice: d.com.Text("Opened the latest remote screen.")}
	})
}
