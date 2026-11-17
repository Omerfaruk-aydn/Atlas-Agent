package tools

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/computer"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/design"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/screens"
)

// assetUse tells the author how to place a saved picture in a design.
func assetUse(src string) string {
	return fmt.Sprintf("In a script: image(%q, {w: 160, h: 120, radius: \"md\"}) (it covers the box; fit: \"contain\" shows all of it), or a frame with fills: [{type: \"image\", src: %q}]. Text over a photo needs a scrim: a rect with a dark gradient or a solid band under the words.", src, src)
}

// designImage makes a picture for a design file (an illustration, a tile's
// artwork, a background) with the image model, saves it in the file's assets
// and returns it so the model sees what it got.
func designImage(ctx context.Context, workingDir string, gen ImageGenerator, p DesignCanvasParams) fantasy.ToolResponse {
	if gen == nil {
		return fantasy.NewTextErrorResponse("image: no image model is available here. Sign in to ChatGPT or Antigravity (atlas-agent login), or draw the artwork with path(), pattern() and map() in a script.")
	}
	if strings.TrimSpace(p.Prompt) == "" || strings.TrimSpace(p.Image) == "" {
		return fantasy.NewTextErrorResponse("image: give file (the design), prompt (what to draw) and image (the file name, e.g. tile-schedule).")
	}
	if !design.ValidName(p.File) {
		return fantasy.NewTextErrorResponse("image: file is required, the design the picture is for.")
	}
	aspect := "1:1"
	if p.Aspect != "" {
		a, ok := screens.Aspects[strings.ToLower(strings.TrimSpace(p.Aspect))]
		if !ok {
			return fantasy.NewTextErrorResponse("image: aspect is square, portrait, tall, landscape or wide.")
		}
		aspect = a
	}
	data, mime, err := gen(ctx, p.Prompt, aspect)
	if err != nil {
		return fantasy.NewTextErrorResponse("image: " + err.Error())
	}
	src, w, h, err := screens.SaveAsset(workingDir, p.File, p.Image, data, mime)
	if err != nil {
		return fantasy.NewTextErrorResponse(err.Error())
	}
	text := fmt.Sprintf("Saved %dx%d image as %s. %s\nOne look across the file: start every prompt for this design with the same style sentence (medium, light, palette words, \"no text, no letters\") and change only the subject.", w, h, src, assetUse(src))
	view := data
	if scaled, err := computer.DownscaleScreenshot(data, 1024); err == nil && len(scaled.PNG) > 0 {
		view = scaled.PNG
		mime = "image/png"
	}
	resp := fantasy.NewImageResponse(view, mime)
	resp.Content = text
	return resp
}

// designPhoto finds a real, licensed photograph for a design file, saves it
// in the file's assets and returns it so the model sees what it got.
func designPhoto(ctx context.Context, workingDir string, p DesignCanvasParams) fantasy.ToolResponse {
	if strings.TrimSpace(p.Query) == "" || strings.TrimSpace(p.Image) == "" {
		return fantasy.NewTextErrorResponse("photo: give file (the design), query (what the photograph shows, in plain words) and image (the file name, e.g. news-summit).")
	}
	orient := ""
	switch strings.ToLower(strings.TrimSpace(p.Aspect)) {
	case "portrait", "tall":
		orient = "portrait"
	case "landscape", "wide":
		orient = "landscape"
	case "square":
		orient = "square"
	case "":
	default:
		return fantasy.NewTextErrorResponse("photo: aspect is square, portrait, tall, landscape or wide.")
	}
	if !design.ValidName(p.File) {
		return fantasy.NewTextErrorResponse("photo: file is required, the design the photograph is for.")
	}
	found, err := screens.FindPhotos(ctx, p.Query, orient)
	if err != nil {
		return fantasy.NewTextErrorResponse(err.Error())
	}
	pick := max(p.Pick, 1)
	if pick > len(found) {
		return fantasy.NewTextErrorResponse(fmt.Sprintf("photo: only %d results for this search; pick 1 to %d.", len(found), len(found)))
	}
	chosen := found[pick-1]
	src, w, h, data, mime, err := screens.SavePhoto(ctx, workingDir, p.File, p.Image, chosen)
	if err != nil {
		return fantasy.NewTextErrorResponse("photo: " + err.Error())
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Saved %dx%d photograph from %s as %s (%s). %s\n", w, h, chosen.Provider, src, chosen.License, assetUse(src))
	if c := chosen.Credit(); c != "" {
		fmt.Fprintf(&b, "Licence asks for credit: show \"%s\" somewhere the design can carry it (a caption, an about screen). Source: %s\n", c, chosen.Page)
	}
	if len(found) > 1 {
		b.WriteString("Other results (pick N to use one): ")
		for i, f := range found {
			if i == pick-1 || i >= 6 {
				continue
			}
			t := f.Title
			if len(t) > 50 {
				t = t[:50]
			}
			fmt.Fprintf(&b, "%d) %s; ", i+1, strings.TrimSpace(t+" by "+f.Author))
		}
		b.WriteString("\n")
	}
	view := data
	if scaled, err := computer.DownscaleScreenshot(data, 1024); err == nil && len(scaled.PNG) > 0 {
		view = scaled.PNG
		mime = "image/png"
	}
	resp := fantasy.NewImageResponse(view, mime)
	resp.Content = b.String()
	return resp
}

// designAppshots shows the store screenshots of real apps as one contact
// sheet, one app per row, kept with the design file.
func designAppshots(ctx context.Context, workingDir, file string, p DesignCanvasParams) fantasy.ToolResponse {
	store := "appstore"
	if strings.EqualFold(p.Platform, "android") {
		store = "play"
	}
	apps, err := screens.FindApps(ctx, p.Query, store, 4)
	if err != nil {
		return fantasy.NewTextErrorResponse(err.Error())
	}
	path, data, err := screens.AppSheet(ctx, workingDir, file, p.Query, apps, 4)
	if err != nil {
		return fantasy.NewTextErrorResponse(err.Error())
	}
	rel, _ := filepath.Rel(workingDir, path)
	var b strings.Builder
	fmt.Fprintf(&b, "Store screenshots of shipped apps, one app per row (saved as %s). These are marketing frames: study the screen inside each frame, not the caption above it.\n", filepath.ToSlash(rel))
	for i, a := range screens.AppsWithRows(workingDir, file, p.Query) {
		rating := ""
		if a.Ratings > 0 {
			rating = fmt.Sprintf(", %.1f from %d ratings", a.Rating, a.Ratings)
		}
		fmt.Fprintf(&b, "Row %d: %s by %s (%s%s) %s\n", i+1, a.Name, a.Developer, a.Store, rating, a.Page)
	}
	b.WriteString("\nBefore the system, write two short lists. TAKE: the discipline you will borrow (how much of the surface the colour owns, how the subject's own object is drawn, the scale of the one dominant element, how dense the rows are, how the copy sounds). REFUSE: what you will not copy (their layout, brand colours, mascots, illustrations, names). Then name the one thing none of these apps does that yours will. Never reproduce a screen.\n")
	view := data
	mime := "image/jpeg"
	if scaled, err := computer.DownscaleScreenshot(data, 1568); err == nil && len(scaled.PNG) > 0 {
		view, mime = scaled.PNG, "image/png"
	}
	resp := fantasy.NewImageResponse(view, mime)
	resp.Content = b.String()
	return resp
}

// designKnowledge answers from the written platform and UX rules.
func designKnowledge(p DesignCanvasParams) (string, error) {
	switch topic := strings.ToLower(strings.TrimSpace(p.Topic)); topic {
	case "platform":
		plat := p.Platform
		if plat == "" {
			plat = "ios"
		}
		return screens.PlatformRules(plat)
	case "scale":
		return screens.TypeScale(p.Scale)
	case "ux", "mobile", "charts":
		return screens.Knowledge(topic, p.Query, p.Platform, 6)
	default:
		return "", fmt.Errorf("knowledge: topic is platform, scale, ux, mobile or charts, not %q", p.Topic)
	}
}
