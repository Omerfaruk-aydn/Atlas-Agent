func (b *windowsBackend) listNativeMonitors(ctx context.Context) (json.RawMessage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	monitorMu.Lock()
	defer monitorMu.Unlock()
	out := []MonitorObservation{}
	monitorTarget = &out
	defer func() { monitorTarget = nil }()
	ok, _, callErr := modUser32.NewProc("EnumDisplayMonitors").Call(0, 0, monitorCallback, 0)
	if ok == 0 && len(out) == 0 {
		return nil, fmt.Errorf("monitor_observation_failed: EnumDisplayMonitors: %v", callErr)
	}
	for i := range out {
		out[i].CoordinateSpace = CoordinateSpacePhysical
	}
	return json.Marshal(out)
}
