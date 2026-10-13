type remoteWaiter interface {
	Observe(ctx context.Context, image bool) (nodes.DesktopObservation, error)
	Automation(ctx context.Context, req nodes.AutomationRequest) (json.RawMessage, error)
}

type remoteWaitResult struct {
	Condition string `json:"condition"`
	Met       bool   `json:"met"`
	WaitedMS  int64  `json:"waited_ms"`
	Samples   int    `json:"samples"`
	State     string `json:"state"`
	// UnknownSamples records incomplete observations, never absence evidence.
	UnknownSamples int `json:"unknown_samples,omitempty"`
	// Difference is the last measured screen difference (0-1).
	Difference float64 `json:"difference,omitempty"`
	Name       string  `json:"name,omitempty"`
}

// remoteWait polls until cond holds or timeout passes. It never sends
// input. The final observation is returned for the caller to show.
func remoteWait(ctx context.Context, c remoteWaiter, cond, name, windowID string, timeout, interval time.Duration) (remoteWaitResult, nodes.DesktopObservation, error) {
	if timeout <= 0 {
		timeout = remoteWaitDefault
	}
	timeout = min(timeout, remoteWaitMax)
	res := remoteWaitResult{Condition: cond, Name: name, State: "waiting"}
	switch cond {
	case "change", "stable":
	case "text", "text_gone":
		if name == "" {
			return res, nodes.DesktopObservation{}, fmt.Errorf("wait_for %s needs name: the visible text or control name to wait for", cond)
		}
	default:
		return res, nodes.DesktopObservation{}, fmt.Errorf("wait_for must be change, stable, text or text_gone")
	}
	start := time.Now()
	parent := ctx
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if interval <= 0 {
		interval = remoteWaitInterval
	}
	finish := func(err error) (remoteWaitResult, nodes.DesktopObservation, error) {
		res.WaitedMS = time.Since(start).Milliseconds()
		if parent.Err() != nil {
			res.State = "canceled"
			return res, nodes.DesktopObservation{}, parent.Err()
		}
		if ctx.Err() == context.DeadlineExceeded {
			res.Met, res.State = false, "timeout"
			return res, nodes.DesktopObservation{}, nil
		}
		return res, nodes.DesktopObservation{}, err
	}
	var baseline, previous []float64
	same := 0
	var last nodes.DesktopObservation
	for {
		if err := ctx.Err(); err != nil {
			r, _, e := finish(err)
			return r, last, e
		}
		res.Samples++
		switch cond {
		case "change", "stable":
			obs, err := c.Observe(ctx, true)
			if err != nil {
				r, _, e := finish(err)
				return r, last, e
			}
			last = obs
			fp, err := screenFingerprint(obs.PNG)
			if err != nil {
				return res, last, err
			}
			if cond == "change" {
				if baseline == nil {
					baseline = fp
				} else if res.Difference = fingerprintDiff(baseline, fp); res.Difference > changeThreshold {
					res.Met = true
				}
			} else {
				if previous != nil {
					res.Difference = fingerprintDiff(previous, fp)
					if res.Difference < stableThreshold {
						same++
					} else {
						same = 0
					}
				}
				previous = fp
				res.Met = same >= stableSamples-1
			}
		case "text", "text_gone":
			raw, err := c.Automation(ctx, nodes.AutomationRequest{Action: "find", Name: name, WindowID: windowID, MaxElements: 2000})
			if err != nil {
				r, _, e := finish(err)
				return r, last, e
			}
			found, known := automationPresence(raw)
			if !known {
				res.UnknownSamples++
			}
			res.Met = known && found == (cond == "text")
		}
		if err := ctx.Err(); err != nil {
			r, _, e := finish(err)
			return r, last, e
		}
		res.WaitedMS = time.Since(start).Milliseconds()
		if res.Met {
			res.State = "met"
			return res, last, nil
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			r, _, e := finish(ctx.Err())
			return r, last, e
		case <-timer.C:
		}
	}
}

// automationPresence distinguishes absence from an incomplete or invalid scan.
func automationPresence(raw json.RawMessage) (found, known bool) {
	var list []json.RawMessage
	if bytes.HasPrefix(bytes.TrimSpace(raw), []byte("[")) && json.Unmarshal(raw, &list) == nil {
		return len(list) > 0, true
	}
	var obj struct {
		Elements  []json.RawMessage `json:"elements"`
		Matches   []json.RawMessage `json:"matches"`
		Count     *int              `json:"count"`
		Truncated bool              `json:"truncated"`
		Error     json.RawMessage   `json:"error"`
	}
	if json.Unmarshal(raw, &obj) == nil {
		if len(obj.Error) > 0 && string(obj.Error) != "null" {
			return false, false
		}
		found = len(obj.Elements) > 0 || len(obj.Matches) > 0 || obj.Count != nil && *obj.Count > 0
		if found {
			return true, true
		}
		return false, !obj.Truncated && (obj.Count != nil || obj.Elements != nil || obj.Matches != nil)
	}
	return false, false
}

// screenFingerprint averages brightness over a coarse grid, so a blinking
// caret or a moving mouse barely registers while a new screen does.
func screenFingerprint(data []byte) ([]float64, error) {
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decode remote screen: %w", err)
	}
	const cols, rows = 48, 27
	b := img.Bounds()
	if b.Dx() < cols || b.Dy() < rows {
		return nil, fmt.Errorf("remote screen too small to compare")
	}
	out := make([]float64, cols*rows)
	// Sample a fixed number of pixels per cell to bound the cost on large
	// screens.
	const samples = 6
	for cy := range rows {
		for cx := range cols {
			var sum float64
			for sy := range samples {
				for sx := range samples {
					x := b.Min.X + cx*b.Dx()/cols + sx*(b.Dx()/cols)/samples
					y := b.Min.Y + cy*b.Dy()/rows + sy*(b.Dy()/rows)/samples
					r, g, bl, _ := img.At(x, y).RGBA()
					sum += (0.299*float64(r) + 0.587*float64(g) + 0.114*float64(bl)) / 65535
				}
			}
			out[cy*cols+cx] = sum / (samples * samples)
		}
	}
	return out, nil
}

func fingerprintDiff(a, b []float64) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 1
	}
	var d float64
	for i := range a {
		v := a[i] - b[i]
		if v < 0 {
			v = -v
		}
		d += v
	}
	return d / float64(len(a))
}
