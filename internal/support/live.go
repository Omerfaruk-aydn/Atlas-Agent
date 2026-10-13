	defer c.mu.Unlock()
	remote := c.remote
	if remote == nil || remote.SessionID != f.SessionID || remote.Generation != f.Generation {
		return
	}
	if f.CapturedAt == 0 {
		f.CapturedAt = time.Now().UnixMilli()
	}
	f.PNG = slices.Clone(f.PNG)
	c.live.mu.Lock()
	if old := c.live.frame; old != nil && old.SessionID == f.SessionID && old.Generation == f.Generation && old.CapturedAt > f.CapturedAt {
		c.live.mu.Unlock()
		return
	}
	c.live.frame = &f
	c.live.mu.Unlock()
}

// LatestFrame returns the latest frame of the current session.
func (c *Controller) LatestFrame() (Frame, bool) {
	if c == nil {
		return Frame{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	remote := c.remote
	c.live.mu.Lock()
	defer c.live.mu.Unlock()
	f := c.live.frame
	if f == nil || remote == nil || remote.SessionID != f.SessionID || remote.Generation != f.Generation {
		return Frame{}, false
	}
	out := *f
	out.PNG = slices.Clone(f.PNG)
	if c.machine != nil {
		state, _, _ := c.machine.State()
		out.Cached = out.Cached || state != StateConnected && state != StatePaused
	}
	return out, true
}

// viewerSnapshot binds the device, image and timeline in one lock interval.
// Separate Status/LatestFrame calls could straddle a session replacement.
func (c *Controller) viewerSnapshot(session string) (string, State, Frame, bool, []Event) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.remote == nil || c.remote.SessionID != session {
		return "", StateDisconnected, Frame{}, false, nil
	}
	state, _, _ := c.machine.State()
	c.live.mu.Lock()
	defer c.live.mu.Unlock()
	var events []Event
	for _, event := range c.live.timeline {
		if event.SessionID == session {
			events = append(events, event)
		}
	}
	frame := c.live.frame
	if frame == nil || frame.SessionID != session || frame.Generation != c.remote.Generation {
		return c.remote.RemoteName, state, Frame{}, false, events
	}
	copy := *frame
	copy.PNG = slices.Clone(frame.PNG)
	copy.Cached = copy.Cached || !state.Active()
	return c.remote.RemoteName, state, copy, true, events
}

// Timeline returns up to n recent events of the current session, oldest
// first.
func (c *Controller) Timeline(n int) []Event {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	remote := c.remote
	c.mu.Unlock()
	if remote == nil {
		return nil
	}
	c.live.mu.Lock()
	defer c.live.mu.Unlock()
	var out []Event
	for _, e := range c.live.timeline {
		if e.SessionID == remote.SessionID {
			out = append(out, e)
		}
	}
	if n > 0 && len(out) > n {
		out = out[len(out)-n:]
	}
	return slices.Clone(out)
}

// LoadTimeline restores persisted evidence without performing IO on a UI tick.
func (c *Controller) LoadTimeline(sessionID string) error {
	if c == nil || c.history == nil {
		return nil
	}
	events, err := c.history.RecentEvents(sessionID, timelineCap)
	if err != nil {
		return err
	}
	c.live.mu.Lock()
	defer c.live.mu.Unlock()
	// Do not overwrite events that arrived while history was being read.
	for _, e := range events {
		found := false
		for _, old := range c.live.timeline {
			if old.OperationID == e.OperationID && old.Kind == e.Kind && old.At == e.At {
				found = true
				break
			}
		}
		if !found {
			c.live.timeline = append(c.live.timeline, e)
		}
	}
	slices.SortStableFunc(c.live.timeline, func(a, b Event) int {
		if a.At < b.At {
			return -1
		}
		if a.At > b.At {
			return 1
		}
		return 0
	})
	if len(c.live.timeline) > timelineCap {
		c.live.timeline = slices.Clone(c.live.timeline[len(c.live.timeline)-timelineCap:])
	}
	return nil
}
