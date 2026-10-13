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
