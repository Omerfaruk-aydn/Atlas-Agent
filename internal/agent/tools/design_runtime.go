}

type localDesignPreviewKey struct{}

func withDesignPreview(ctx context.Context, origin string) context.Context {
	return context.WithValue(ctx, localDesignPreviewKey{}, origin)
}

func isDesignPreviewCall(ctx context.Context, call fantasy.ToolCall) bool {
	origin, _ := ctx.Value(localDesignPreviewKey{}).(string)
	if origin == "" || call.Name != BrowserToolName {
		return false
	}
	var p BrowserParams
	if json.Unmarshal([]byte(call.Input), &p) != nil {
		return false
	}
	switch p.Action {
	case "navigate":
		return strings.HasPrefix(p.URL, origin+"/")
	case "tab_new":
		return strings.HasPrefix(p.Advanced.URL, origin+"/")
	case "tabs", "tab_select", "tab_close":
		return true
	case "screenshot", "snapshot", "console":
		return true
	case "cdp":
		return p.CDPMethod == "Emulation.setDeviceMetricsOverride"
	}
	return false
}

// openDesignTab leaves the previous automation tab and its viewport intact.
// Cleanup uses a bounded context even when the requesting turn was canceled.
func openDesignTab(ctx context.Context, deps DesignServices, id, url string) (func(), error) {
	tabs, err := designBrowser(ctx, deps, id+"-tabs", BrowserParams{Action: "tabs"})
	if err != nil {
		return nil, err
	}
	var previous struct {
		Active string `json:"active_tab"`
	}
	if json.Unmarshal([]byte(tabs.Content), &previous) != nil {
		return nil, fmt.Errorf("cannot identify the previous automation tab")
	}
	created, err := designBrowser(ctx, deps, id+"-new-tab", BrowserParams{Action: "tab_new", Advanced: browser.Request{URL: url, Purpose: "design preview"}})
	if err != nil {
		return nil, err
	}
	var current struct {
		Active string `json:"active_tab"`
	}
	if json.Unmarshal([]byte(created.Content), &current) != nil || current.Active == "" || current.Active == previous.Active {
		return nil, fmt.Errorf("design tab was not independently created")
	}
	return func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_, _ = designBrowser(cleanup, deps, id+"-close-tab", BrowserParams{Action: "tab_close", Advanced: browser.Request{TabID: current.Active}})
		if previous.Active != "" {
			_, _ = designBrowser(cleanup, deps, id+"-return-tab", BrowserParams{Action: "tab_select", Advanced: browser.Request{TabID: previous.Active}})
		}
	}, nil
}

func storeDesignRuntime(root, rel string, value any) error {
	path, err := transfer.Resolve(root, filepath.FromSlash(rel))
	if err != nil {
		return err
	}
	return taskstate.WriteJSONAtomic(path, value)
}

func writeDesignArtifact(root, rel string, data []byte) error {
	path, err := transfer.Resolve(root, rel)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return taskstate.WriteFileAtomic(path, data, 0o600)
}
