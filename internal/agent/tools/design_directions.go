package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
)

var designFontSafe = regexp.MustCompile(`^[A-Za-z0-9 '\",._-]{1,120}$`)

const directionHTML = `<!doctype html><html lang="tr"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><style>{{.CSS}}
*{box-sizing:border-box}body{margin:0;background:var(--bg);color:var(--text);font-family:var(--font);line-height:1.5}main{max-width:580px;margin:auto;padding:var(--space)}header{display:flex;justify-content:space-between;align-items:center;border-bottom:1px solid var(--line);padding-bottom:16px}header strong{color:var(--accent);font-size:20px}h1{font-size:var(--title);line-height:1.2;margin:32px 0 12px}.brief{white-space:pre-wrap;margin-bottom:28px}.request{background:var(--panel);border:1px solid var(--line);padding:20px;border-radius:var(--radius)}.request h2{font-size:18px;margin:0 0 8px}.request p{margin:0 0 20px}button{font:inherit;min-height:48px;border-radius:var(--radius);padding:12px 16px;width:100%;border:1px solid var(--line);background:var(--bg);color:var(--text);margin-top:10px}.primary{background:var(--accent);border-color:var(--accent);color:var(--onaccent)}footer{margin-top:32px;font-size:14px}textarea{display:block;width:100%;min-height:96px;border:1px solid var(--line);border-radius:var(--radius);padding:14px;background:var(--bg);color:var(--text);font:inherit}</style></head><body><main><header><strong>Atlas</strong><span>Remote</span></header><h1>{{.Product}}</h1><p class="brief">{{.Brief}}</p><section class="request"><h2>Nasıl devam edelim?</h2><p>Göreve devam etmek için bir seçim yap.</p><button class="primary">Göreve devam et</button><button>Değişiklikleri incele</button></section><footer><label for="message">Atlas'a bir şey söyle</label><textarea id="message" placeholder="Mesajını yaz…"></textarea></footer></main></body></html>`

type renderedDirection struct {
	Name      string       `json:"name"`
	Rationale string       `json:"rationale"`
	System    DesignSystem `json:"system"`
	Source    string       `json:"source"`
	Preview   string       `json:"preview"`
	SHA256    string       `json:"sha256"`
	Width     int          `json:"width"`
	Height    int          `json:"height"`
}

func directionPage(s DesignSystem, brief string) (string, error) {
	color := func(keys []string, fallback string) string {
		for _, key := range keys {
			if v, ok := s.Colors[key]; ok {
				return v
			}
		}
		return fallback
	}
	font := s.FontFamily
	if !designFontSafe.MatchString(font) {
		font = "system-ui, sans-serif"
	}
	space, radius, title := 24, 12, 28
	if len(s.Spacing) > 0 {
		space = max(16, min(40, s.Spacing[len(s.Spacing)-1]))
	}
	if len(s.Radius) > 0 {
		radius = max(0, min(28, s.Radius[len(s.Radius)-1]))
	}
	if len(s.TypeScale) > 0 {
		title = max(22, min(40, s.TypeScale[len(s.TypeScale)-1]))
	}
	css := fmt.Sprintf(":root{--bg:%s;--text:%s;--accent:%s;--onaccent:%s;--panel:%s;--line:%s;--font:%s;--space:%dpx;--radius:%dpx;--title:%dpx}", color([]string{"bg", "background"}, "#FFFFFF"), color([]string{"text", "foreground"}, "#202127"), color([]string{"accent", "primary"}, "#6046D8"), color([]string{"on_accent", "primary_text"}, "#FFFFFF"), color([]string{"surface", "panel", "bg"}, "#F5F6FA"), color([]string{"border", "line"}, "#E3E6ED"), font, space, radius, title)
	tpl, err := template.New("direction").Parse(directionHTML)
	if err != nil {
		return "", err
	}
	var out bytes.Buffer
	err = tpl.Execute(&out, map[string]any{"CSS": template.CSS(css), "Product": s.Product, "Brief": brief})
	return out.String(), err
}

func designDirections(ctx context.Context, root string, p DesignSystemParams, call fantasy.ToolCall, deps DesignServices) (fantasy.ToolResponse, error) {
	fail := designFailure
	if len(p.Directions) < 2 || len(p.Directions) > 3 || len(strings.TrimSpace(p.Brief)) < 10 || len(p.Brief) > 10000 {
		return fail(fmt.Errorf("directions needs 2-3 systems and the same concrete representative screen brief"))
	}
	width, height := p.Width, p.Height
	if width == 0 {
		width = 390
	}
	if height == 0 {
		height = 844
	}
	if width < 240 || width > 3840 || height < 240 || height > 2160 {
		return fail(fmt.Errorf("invalid design viewport"))
	}
	pages := map[string]string{}
	names := map[string]bool{}
	for i, sys := range p.Directions {
		if err := validateDesignSystem(sys); err != nil {
			return fail(err)
		}
		if sys.Platform != p.Directions[0].Platform || sys.Product != p.Directions[0].Product {
			return fail(fmt.Errorf("all directions must share product and platform"))
		}
		if names[sys.Direction.Name] {
			return fail(fmt.Errorf("directions must have distinct names"))
		}
		names[sys.Direction.Name] = true
		page, err := directionPage(sys, p.Brief)
		if err != nil {
			return fail(err)
		}
		pages[fmt.Sprintf("/%d", i)] = page
	}
	if err := designPermission(ctx, p, call, deps, root); err != nil {
		return fail(err)
	}
	origin, closeServer, err := serveDesign(ctx, pages)
	if err != nil {
		return fail(err)
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
