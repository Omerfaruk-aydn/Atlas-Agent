	// The started application outlives this request, so cancellation must
	// not kill it.
	cmd := exec.CommandContext(context.WithoutCancel(ctx), program, plan.Argv[1:]...)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x00000200} // CREATE_NEW_PROCESS_GROUP.
	if err := cmd.Start(); err != nil {
		return 0, err
	}
	pid := cmd.Process.Pid
	_ = cmd.Process.Release()
	if plan.Entry.Launch.Method == LaunchAppsFolder {
		pid = 0
	}
	return pid, nil
}

// VisibleWindows lists visible top-level windows for launch verification.
func (b *windowsBackend) VisibleWindows(ctx context.Context) ([]AppWindowObservation, error) {
	raw, err := b.listNativeWindows(ctx)
	if err != nil {
		return nil, err
	}
	var out []AppWindowObservation
	return out, json.Unmarshal(raw, &out)
}
