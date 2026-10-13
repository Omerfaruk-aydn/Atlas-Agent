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
