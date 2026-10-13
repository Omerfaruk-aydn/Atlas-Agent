	}
	defer closeServer()
	ctx = withDesignPreview(ctx, origin)
	closeTab, err := openDesignTab(ctx, deps, call.ID, origin+"/0")
	if err != nil {
		return fail(err)
	}
	defer closeTab()
	metrics, _ := json.Marshal(map[string]any{"width": width, "height": height, "deviceScaleFactor": 1, "mobile": false})
	if _, err := designBrowser(ctx, deps, call.ID+"-viewport", BrowserParams{Action: "cdp", CDPMethod: "Emulation.setDeviceMetricsOverride", CDPParams: BrowserCDPParams(metrics)}); err != nil {
		return fail(err)
	}
	rel := filepath.ToSlash(filepath.Join(".atlas", "design", "directions", engineering.Hash(GetSessionFromContext(ctx) + call.ID)[:20]))
	var alternatives []renderedDirection
	for i, sys := range p.Directions {
		if err := ctx.Err(); err != nil {
			return fail(err)
		}
		url := fmt.Sprintf("%s/%d", origin, i)
		if _, err := designBrowser(ctx, deps, fmt.Sprintf("%s-preview-%d", call.ID, i), BrowserParams{Action: "navigate", URL: url}); err != nil {
			return fail(err)
		}
		annotate := false
		shot, err := designBrowser(ctx, deps, fmt.Sprintf("%s-capture-%d", call.ID, i), BrowserParams{Action: "screenshot", Annotate: &annotate})
		if err != nil {
			return fail(err)
		}
		d, err := PrepareImageForDelivery(shot.Data, shot.MediaType)
		if err != nil {
			return fail(err)
		}
		if d.OriginalWidth != width || d.OriginalHeight != height {
			return fail(fmt.Errorf("preview viewport mismatch: got %dx%d, requested %dx%d", d.OriginalWidth, d.OriginalHeight, width, height))
		}
		stem := fmt.Sprintf("direction-%d", i+1)
		source := rel + "/" + stem + ".html"
		preview := rel + "/" + stem + ".png"
		if err := writeDesignArtifact(root, source, []byte(pages[fmt.Sprintf("/%d", i)])); err != nil {
			return fail(err)
		}
		if shot.MediaType != "image/png" {
			return fail(fmt.Errorf("design renderer must return a PNG screenshot"))
		}
		if err := writeDesignArtifact(root, preview, shot.Data); err != nil {
			return fail(err)
		}
		alternatives = append(alternatives, renderedDirection{Name: sys.Direction.Name, Rationale: sys.Direction.Rationale, System: sys, Source: source, Preview: preview, SHA256: imageDeliveryHash(shot.Data), Width: width, Height: height})
	}
	if err := storeDesignRuntime(root, rel+"/manifest.json", map[string]any{"alternatives": alternatives, "brief": p.Brief, "created_at": time.Now().UTC().Format(time.RFC3339), "approval": "not_selected"}); err != nil {
		return fail(err)
	}
	var gallery strings.Builder
	gallery.WriteString("<!doctype html><meta charset=utf-8><title>Atlas design directions</title><style>body{font:16px system-ui;margin:24px;background:#f5f6fa}main{display:flex;gap:24px;flex-wrap:wrap}section{max-width:390px}img{max-width:100%;height:auto}h2{font-size:20px}</style><h1>Atlas · Tasarım yönleri</h1><main>")
	for i, a := range alternatives {
		fmt.Fprintf(&gallery, "<section><h2>%s</h2><p>%s</p><img src=direction-%d.png alt='%s'></section>", template.HTMLEscapeString(a.Name), template.HTMLEscapeString(a.Rationale), i+1, template.HTMLEscapeString(a.Name))
	}
	gallery.WriteString("</main>")
	if err := writeDesignArtifact(root, rel+"/index.html", []byte(gallery.String())); err != nil {
		return fail(err)
	}
	data, _ := json.Marshal(map[string]any{"alternatives": alternatives, "comparison": rel + "/index.html", "selection_required": true, "note": "Actual rendered previews, not final product screens. Show them with view. Store the selected system with set; approve only from explicit user intent and expected_version."})
	return fantasy.NewTextResponse(string(data)), nil
}
