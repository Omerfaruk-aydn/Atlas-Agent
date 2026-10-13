func (c *Controller) watchScreen(ctx context.Context, interval time.Duration, session string, onError func(string)) {
	interval = max(interval, 500*time.Millisecond)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		status := c.Status()
		if status.Remote == nil || status.Remote.SessionID != session || status.State.Terminal() {
			onError("session_ended")
			return
		}
		remote, client, err := c.Target()
		if remote.SessionID != session {
			onError("session_ended")
			return
		}
		if err == nil && client != nil {
			captureCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			obs, captureErr := client.Preview(captureCtx)
			cancel()
			if captureErr == nil && ctx.Err() == nil {
				c.SetFrame(Frame{PNG: obs.PNG, MediaType: "image/png", SessionID: obs.SessionID, Generation: obs.Generation, Width: obs.Width, Height: obs.Height, CapturedAt: obs.CapturedAt})
				onError("")
			} else if captureErr != nil {
				code := Classify(captureErr, remote.RemoteDeviceID, "").Code
				onError(string(code))
				c.live.mu.Lock()
				if c.live.frame != nil && c.live.frame.SessionID == remote.SessionID && c.live.frame.Generation == remote.Generation {
					c.live.frame.Cached = true
					if code == CodeCapabilityDenied || code == CodeSessionRevoked || code == CodeSessionExpired {
						c.live.frame = nil
					}
				}
				c.live.mu.Unlock()
				if code == CodeCapabilityDenied || code == CodeSessionRevoked || code == CodeSessionExpired {
					return
				}
			}
		} else {
			onError("remote_offline")
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (w *screenWatch) start(parent context.Context, c *Controller, session string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.cancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(parent)
	w.cancel, w.done = cancel, make(chan struct{})
	done := w.done
	go func() {
		defer func() {
			close(done)
			w.mu.Lock()
			if w.done == done {
				w.cancel = nil
			}
			w.mu.Unlock()
		}()
		c.watchScreen(ctx, time.Second, session, func(err string) { w.mu.Lock(); w.err = err; w.mu.Unlock() })
	}()
}

func (w *screenWatch) stop() {
	w.mu.Lock()
	cancel, done := w.cancel, w.done
	w.mu.Unlock()
	if cancel == nil {
		return
	}
	cancel()
	<-done
	w.mu.Lock()
	if w.done == done {
		w.cancel, w.done = nil, nil
	}
	w.mu.Unlock()
}

func (w *screenWatch) status() (bool, string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.cancel != nil, w.err
}
