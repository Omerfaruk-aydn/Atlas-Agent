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
