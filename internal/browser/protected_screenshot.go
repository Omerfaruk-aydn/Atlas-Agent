package browser

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/draw"
	_ "image/jpeg"
	"image/png"
	"math"

	"github.com/chromedp/chromedp"
)

// ProtectedScreenshotSession captures page layout with editable fields,
// known plaintext secrets and opaque embedded surfaces masked in the PNG.
// Neither field values nor the page DOM are modified.
type ProtectedScreenshotSession interface {
	ProtectedScreenshot(context.Context, bool, bool, func(string) string) ([]byte, int, error)
}

// ProtectedViewportSession retains coordinate grants for a protected,
// annotated screenshot when its viewport remains stable.
type ProtectedViewportSession interface {
	ProtectedViewportScreenshot(context.Context, func(string) string) ([]byte, Viewport, int, error)
}

func (s *chromedpSession) ProtectedViewportScreenshot(ctx context.Context, redact func(string) string) ([]byte, Viewport, int, error) {
	masks := 0
	data, vp, err := s.captureViewportScreenshot(ctx, func() ([]byte, error) {
		data, count, err := s.ProtectedScreenshot(ctx, false, true, redact)
		masks = count
		return data, err
	})
	return data, vp, masks, err
}

type privacyRegion struct {
	Text string     `json:"text"`
	Mask bool       `json:"mask"`
	Rect [4]float64 `json:"rect"`
}

type privacyLayout struct {
	Width   float64         `json:"width"`
	Height  float64         `json:"height"`
	Ratio   float64         `json:"ratio"`
	Regions []privacyRegion `json:"regions"`
	Version float64         `json:"version"`
}

const privacyLayoutScript = `(full => {
  const key = '__atlasPrivacyObserver';
  if (!globalThis[key]) {
    const state = {version: 0};
    state.observer = new MutationObserver(() => state.version++);
    state.observer.observe(document, {subtree:true, childList:true, characterData:true, attributes:true});
    globalThis[key] = state;
  }
  const rows = [], sx = full ? scrollX : 0, sy = full ? scrollY : 0;
  let textSize = 0;
  const add = (r, text, mask) => {
    if (rows.length >= 15000) throw new Error('privacy_layout_too_large');
    if (r.width > 0 && r.height > 0) {
      textSize += text.length;
      if (textSize > 2097152) throw new Error('privacy_layout_too_large');
      rows.push({text, mask, rect:[r.left+sx,r.top+sy,r.width,r.height]});
    }
  };
  const walk = root => {
    for (const el of root.querySelectorAll('*')) {
      const tag = el.tagName.toLowerCase();
      if (['input','textarea','select','iframe','frame','canvas','img','video','object','embed','svg'].includes(tag) || el.isContentEditable || (tag.includes('-') && !el.shadowRoot)) {
        add(el.getBoundingClientRect(), '', true);
      }
      for (const pseudo of ['::before','::after']) {
        const content = getComputedStyle(el, pseudo).content;
        if (content && content !== 'none' && content !== 'normal') add(el.getBoundingClientRect(), content, false);
      }
      // Catch a known credential split across adjacent inline elements.
      if (tag !== 'body' && tag !== 'html' && el.childElementCount > 0 && el.childElementCount < 16 && [...el.children].every(child => getComputedStyle(child).display.startsWith('inline'))) {
        const text = el.innerText || '';
        if (text.length > 0 && text.length <= 4096) add(el.getBoundingClientRect(), text, false);
      }
      if (el.shadowRoot) {
        globalThis[key].observer.observe(el.shadowRoot, {subtree:true, childList:true, characterData:true, attributes:true});
        walk(el.shadowRoot);
      }
    }
    const walker = document.createTreeWalker(root, NodeFilter.SHOW_TEXT);
    while (walker.nextNode()) {
      const node = walker.currentNode;
      if (!node.textContent.trim() || ['SCRIPT','STYLE','NOSCRIPT'].includes(node.parentElement?.tagName)) continue;
      const range = document.createRange(); range.selectNodeContents(node);
      for (const rect of range.getClientRects()) add(rect, node.textContent, false);
    }
  };
  walk(document);
  return {width: full ? Math.max(document.documentElement.scrollWidth, innerWidth) : innerWidth, height: full ? Math.max(document.documentElement.scrollHeight, innerHeight) : innerHeight, ratio:Math.max(1, devicePixelRatio || 1), regions:rows, version:globalThis[key].version};
})(%s)`

func (s *chromedpSession) privacyLayout(ctx context.Context, full bool) (privacyLayout, error) {
	var result privacyLayout
	flag := "false"
	if full {
		flag = "true"
	}
	script := bytes.ReplaceAll([]byte(privacyLayoutScript), []byte("%s"), []byte(flag))
	err := s.runContext(ctx, chromedp.Evaluate(string(script), &result))
	return result, err
}

// ProtectedScreenshot refuses unstable pages rather than delivering pixels
// whose redaction coordinates may belong to a different layout/document.
func (s *chromedpSession) ProtectedScreenshot(ctx context.Context, full, annotated bool, redact func(string) string) ([]byte, int, error) {
	if redact == nil {
		return nil, 0, errors.New("credential_redaction_unavailable")
	}
	restore, err := s.suspendActivity()
	if err != nil {
		return nil, 0, err
	}
	defer restore()
	identity := s.ExecutionIdentity(ctx)
	if identity.Document == "" {
		return nil, 0, errors.New("credential_redaction_unverified_document")
	}
	var elements []SnapshotElement
	if annotated {
		elements, err = s.Snapshot(full)
		if err != nil {
			return nil, 0, err
		}
	}
	before, err := s.privacyLayout(ctx, full)
	if err != nil || before.Width <= 0 {
		return nil, 0, errors.New("credential_redaction_layout_unavailable")
	}
	if before.Width*before.Height*before.Ratio*before.Ratio > 32_000_000 {
		return nil, 0, errors.New("credential_redaction_image_too_large: use a viewport screenshot instead of a full-page capture")
	}
	data, err := s.captureScreenshot(full)
	if err != nil {
		return nil, 0, err
	}
	after, err := s.privacyLayout(ctx, full)
	first, _ := json.Marshal(before)
	last, _ := json.Marshal(after)
	if err != nil || identity != s.ExecutionIdentity(ctx) || sha256.Sum256(first) != sha256.Sum256(last) {
		return nil, 0, errors.New("credential_redaction_stale_layout: observe the stable page before capturing again")
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || int64(config.Width)*int64(config.Height) > 32_000_000 {
		return nil, 0, errors.New("credential_redaction_image_too_large_or_invalid")
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, 0, err
	}
	canvas := image.NewRGBA(img.Bounds())
	draw.Draw(canvas, canvas.Bounds(), img, img.Bounds().Min, draw.Src)
	scale := float64(img.Bounds().Dx()) / before.Width
	masked := 0
	for _, region := range before.Regions {
		if !region.Mask && redact(region.Text) == region.Text {
			continue
		}
		r := region.Rect
		rect := image.Rect(int(math.Floor(r[0]*scale))-2, int(math.Floor(r[1]*scale))-2, int(math.Ceil((r[0]+r[2])*scale))+2, int(math.Ceil((r[1]+r[3])*scale))+2).Intersect(img.Bounds())
		if !rect.Empty() {
			draw.Draw(canvas, rect, image.NewUniform(color.RGBA{R: 42, G: 44, B: 52, A: 255}), image.Point{}, draw.Src)
			masked++
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, canvas); err != nil {
		return nil, 0, err
	}
	data = buf.Bytes()
	if annotated {
		identities := make(map[string]RefIdentity, len(elements))
		for i := range elements {
			identities[elements[i].Ref] = RefIdentity{Role: elements[i].Role, Name: elements[i].Name}
			elements[i].Name = redact(elements[i].Name)
			elements[i].Value = redact(elements[i].Value)
		}
		if !full {
			visible := elements[:0]
			for _, element := range elements {
				if element.Visible {
					visible = append(visible, element)
				}
			}
			elements = visible
		}
		var refs map[string]RefIdentity
		data, refs, err = annotateScreenshotPNG(data, elements, scale, MaxScreenshotWidth)
		if err != nil || identity != s.ExecutionIdentity(ctx) {
			return nil, 0, errors.New("credential_redaction_stale_annotation")
		}
		// Display redaction must not change the private DOM identity used
		// to validate a ref before input. These names are not image labels.
		for ref := range refs {
			refs[ref] = identities[ref]
		}
		s.mu.Lock()
		s.screenshotIdentity, s.screenshotReferences = identity, refs
		s.mu.Unlock()
	}
	return data, masked, nil
}
