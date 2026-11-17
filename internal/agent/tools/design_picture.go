package tools

import (
	"encoding/base64"
	"fmt"
	"html"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/design"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/screens"
)

func designRender(workingDir string, store *design.Store, doc *design.Doc, rev, file string, page *design.Page, nodeID string) (string, error) {
	rel, abs, missing, err := designWriteSVG(workingDir, doc, file, page, nodeID)
	if err != nil {
		return "", err
	}
	note := ""
	if len(missing) > 0 {
		note = fmt.Sprintf(" These pictures were not found and render blank: %s. Make them with action image or photo with file %q, and place the src the result gives.", strings.Join(missing, ", "), file)
	}
	png := abs[:len(abs)-len(".svg")] + ".png"
	pw := design.PlaywrightDir(workingDir)
	if pw == "" {
		return "", fmt.Errorf("wrote %s but could not rasterise it: playwright-core is not under app/node_modules. Open the SVG and screenshot it", rel)
	}
	if err := design.Raster(abs, png, design.FontsDir(pw), pw); err != nil {
		return "", err
	}
	pngRel := rel[:len(rel)-len(".svg")] + ".png"
	if nodeID != "" {
		return fmt.Sprintf("Rendered %s (%s). A node render does not stamp the page; render the page with no node_id before lint.%s", pngRel, png, note), nil
	}
	doc.NoteRender(page.ID)
	if _, err := store.SaveWith(file, doc, rev, false, design.SaveOptions{Agent: true, Label: "render"}); err != nil {
		return "", fmt.Errorf("rendered %s but could not stamp the file: %w", pngRel, err)
	}
	return fmt.Sprintf("Rendered %s (%s). The page is stamped. Call critique, answer the rubric from the PNG, then lint.%s", pngRel, png, note), nil
}

func designCritique(workingDir string, doc *design.Doc, file string, page *design.Page, nodeID string) (string, error) {
	base := file
	if nodeID != "" {
		base += "-" + nodeID
	} else if len(doc.Pages) > 1 {
		base += "-" + design.Slug(page.Name)
	}
	rel := filepath.ToSlash(filepath.Join(".atlas", "design", "exports", base+".png"))
	abs := filepath.Join(workingDir, filepath.FromSlash(rel))
	if _, err := os.Stat(abs); err != nil {
		return "", fmt.Errorf("no render at %s; call action render first", rel)
	}
	return design.Critique(doc, page.ID, rel)
}

func designWriteSVG(workingDir string, doc *design.Doc, file string, page *design.Page, nodeID string) (rel, abs string, missing []string, err error) {
	var ids []string
	if nodeID != "" {
		ids = []string{nodeID}
	}
	svg, err := design.SVG(doc, page, ids)
	if err != nil {
		return "", "", nil, err
	}
	base := file
	if nodeID != "" {
		base += "-" + nodeID
	} else if len(doc.Pages) > 1 {
		base += "-" + design.Slug(page.Name)
	}
	rel = filepath.ToSlash(filepath.Join(".atlas", "design", "exports", base+".svg"))
	abs = filepath.Join(workingDir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return "", "", nil, err
	}
	svg, missing = inlineImages(svg, workingDir, file)
	if err := os.WriteFile(abs, []byte(svg), 0o644); err != nil {
		return "", "", nil, err
	}
	return rel, abs, missing, nil
}

var svgHref = regexp.MustCompile(`href="([^"]+)"`)

// inlineImages embeds the project's image files in the SVG as data URLs, so
// the exported file and its PNG show the pictures wherever they are opened.
// A src is read from the project root, then from the design's own screen
// folder (where actions photo and image save). missing lists the local
// srcs that were not found.
func inlineImages(svg, workingDir, file string) (string, []string) {
	cache := map[string]string{}
	var missing []string
	out := svgHref.ReplaceAllStringFunc(svg, func(m string) string {
		src := html.UnescapeString(svgHref.FindStringSubmatch(m)[1])
		if strings.HasPrefix(src, "#") || strings.Contains(src, ":") {
			return m
		}
		if data, ok := cache[src]; ok {
			return `href="` + data + `"`
		}
		rel := filepath.FromSlash(strings.TrimPrefix(src, "/"))
		var raw []byte
		for _, dir := range []string{workingDir, screens.Dir(workingDir, file)} {
			p := filepath.Join(dir, rel)
			if !strings.HasPrefix(filepath.Clean(p), filepath.Clean(dir)+string(filepath.Separator)) {
				continue
			}
			if b, err := os.ReadFile(p); err == nil && len(b) <= 12<<20 {
				raw = b
				break
			}
		}
		if raw == nil {
			if !slices.Contains(missing, src) && !strings.HasPrefix(src, "..") {
				missing = append(missing, src)
			}
			return m
		}
		mime := http.DetectContentType(raw)
		if strings.HasSuffix(strings.ToLower(src), ".svg") {
			mime = "image/svg+xml"
		}
		data := "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(raw)
		cache[src] = data
		return `href="` + data + `"`
	})
	return out, missing
}
