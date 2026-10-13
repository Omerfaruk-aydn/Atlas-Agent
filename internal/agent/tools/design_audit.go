	if err != nil {
		return fail(err)
	}
	d, err := PrepareImageForDelivery(shot.Data, shot.MediaType)
	if err != nil {
		return fail(err)
	}
	if d.OriginalWidth != p.Width || d.OriginalHeight != p.Height {
		return fail(fmt.Errorf("audit screenshot viewport changed"))
	}
	// Re-check document identity after capture; a navigation cannot supply
	// geometry from one page and the image from another.
	id, err := designBrowser(ctx, deps, call.ID+"-identity", BrowserParams{Action: "eval", Script: "({url:location.href,document:String(performance.timeOrigin)})"})
	if err != nil {
		return fail(err)
	}
	var identity struct {
		URL      string `json:"url"`
		Document string `json:"document"`
	}
	if json.Unmarshal([]byte(id.Content), &identity) != nil || identity.URL != audit.URL || identity.Document != audit.Document {
		return fail(fmt.Errorf("stale_design_document: navigation occurred during audit"))
	}
	after, err := prepareDesignSources(root, p.SourceFiles)
	if err != nil {
		return fail(err)
	}
	for _, s := range after {
		if sourceHashes[s.info.Source] != s.info.SHA256 {
			return fail(fmt.Errorf("design source changed during audit"))
		}
	}
	audit.ID = engineering.Hash(GetSessionFromContext(ctx) + call.ID)[:24]
	audit.CapturedAt = time.Now().UnixMilli()
	audit.Screenshot = ".atlas/design/audits/" + audit.ID + ".png"
	audit.ScreenshotSHA256 = imageDeliveryHash(shot.Data)
	if shot.MediaType != "image/png" {
		return fail(fmt.Errorf("design renderer must return a PNG screenshot"))
	}
	if err := writeDesignArtifact(root, audit.Screenshot, shot.Data); err != nil {
		return fail(err)
	}
	if err := storeDesignRuntime(root, ".atlas/design/audits/"+audit.ID+".json", audit); err != nil {
		return fail(err)
	}
	registerDesignAudit(root, audit)
	data, _ := json.Marshal(map[string]any{"audit": audit, "scope": "measured DOM at this viewport only", "visual_inspection_required": true, "not_measured": []string{"aesthetic quality", "native mobile keyboard", "all component states"}})
	shot.Content = string(data)
	shot.IsError = !audit.Passed
	return shot, nil
}

func validDesignAuditID(id string) bool {
	return len(id) == 24 && strings.IndexFunc(id, func(r rune) bool { return r < '0' || r > '9' && r < 'a' || r > 'f' }) == -1
}
