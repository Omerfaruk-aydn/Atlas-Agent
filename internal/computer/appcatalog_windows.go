	}
	var raw json.RawMessage
	if out, used, rerr := b.automation.run(ctx, req, data); used {
		if rerr != nil {
			return nil, rerr
		}
		raw, err = automationResult(out)
	} else {
		raw, err = b.automationOnce(ctx, data)
	}
	if err != nil {
		return nil, err
	}
	var apps []startApp
	return apps, json.Unmarshal(raw, &apps)
}

// registryAppPaths reads App Paths registrations, the mechanism Run and
// ShellExecute use to find programs such as chrome.exe that are not on
// PATH.
func registryAppPaths() []appPathValue {
	const key = `SOFTWARE\Microsoft\Windows\CurrentVersion\App Paths`
	var out []appPathValue
	for _, root := range []struct {
		k     registry.Key
		path  string
		scope string
	}{
		{registry.CURRENT_USER, key, "user"},
		{registry.LOCAL_MACHINE, key, "system"},
		{registry.LOCAL_MACHINE, `SOFTWARE\WOW6432Node\Microsoft\Windows\CurrentVersion\App Paths`, "system"},
	} {
		k, err := registry.OpenKey(root.k, root.path, registry.ENUMERATE_SUB_KEYS)
		if err != nil {
			continue
		}
		names, _ := k.ReadSubKeyNames(2000)
		_ = k.Close()
		for _, name := range names {
			sub, err := registry.OpenKey(root.k, root.path+`\`+name, registry.QUERY_VALUE)
			if err != nil {
				continue
			}
			value, _, err := sub.GetStringValue("")
			_ = sub.Close()
			if err != nil || value == "" {
				continue
			}
			if expanded, err := registry.ExpandString(value); err == nil {
				value = expanded
			}
			out = append(out, appPathValue{Exe: name, Path: strings.Trim(value, `"`), Scope: root.scope})
		}
	}
	return out
}

// LaunchApp starts a planned launch without a shell and returns the
// started process id (0 for Start menu entries, which the shell starts).
func (b *windowsBackend) LaunchApp(ctx context.Context, plan LaunchPlan) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if len(plan.Argv) == 0 {
		return 0, fmt.Errorf("%w: empty argv", ErrLaunchArgs)
	}
	program := plan.Argv[0]
	if plan.Entry.Launch.Method == LaunchAppsFolder {
		program = filepath.Join(os.Getenv("WINDIR"), "explorer.exe")
	}
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
