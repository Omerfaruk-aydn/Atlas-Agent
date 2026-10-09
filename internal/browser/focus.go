package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// FocusInfo describes the element that receives keyboard text, followed
// through open shadow roots and same-origin frames.
type FocusInfo struct {
	Tag      string `json:"tag"`
	Role     string `json:"role,omitempty"`
	Name     string `json:"name,omitempty"`
	Editable bool   `json:"editable"`
	// Kind is input, textarea, contenteditable, textbox or none.
	Kind string `json:"kind"`
	// CanvasApp: a visible canvas covers much of the viewport (a design or
	// drawing editor). Text there is entered through a DOM editor the
	// application positions over or hides behind the canvas.
	CanvasApp bool `json:"canvas_app,omitempty"`
	// CanvasEditor: the focused editor is such a hidden or canvas-overlaid
	// editor. It can stay focused after the application left text editing,
	// so its focus alone does not show which text object receives input.
	CanvasEditor bool `json:"canvas_editor,omitempty"`
	// Text is the editor's current text (bounded), reported only for
	// canvas editors and never for passwords.
	Text string `json:"text,omitempty"`
	// Key identifies the focused node (tag, id, class, role, name).
	Key string `json:"key,omitempty"`
}

// FocusSession is implemented by drivers that can report the focused
// element before text is inserted at it.
type FocusSession interface {
	Focus(ctx context.Context) (FocusInfo, error)
}

// focusScript reports the deepest focused element. Text-like inputs,
// textareas, contenteditable hosts and role=textbox count as editable;
// the body, the document or a button never do.
const focusScript = `(() => {
  let e = document.activeElement;
  for (let i = 0; i < 16 && e; i++) {
    if (e.shadowRoot && e.shadowRoot.activeElement) { e = e.shadowRoot.activeElement; continue; }
    if ((e.tagName === 'IFRAME' || e.tagName === 'FRAME')) {
      try { const d = e.contentDocument; if (d && d.activeElement) { e = d.activeElement; continue; } } catch (_) {}
    }
    break;
  }
  if (!e || e === document.body || e === document.documentElement) return {tag: e ? e.tagName.toLowerCase() : '', editable: false, kind: 'none'};
  const tag = e.tagName.toLowerCase();
  const role = e.getAttribute('role') || '';
  const name = (e.getAttribute('aria-label') || e.getAttribute('name') || e.getAttribute('placeholder') || '').slice(0, 60);
  const textTypes = ['', 'text', 'search', 'email', 'url', 'tel', 'number', 'password'];
  let kind = 'none';
  if (tag === 'input' && textTypes.includes((e.getAttribute('type') || '').toLowerCase())) kind = 'input';
  else if (tag === 'textarea') kind = 'textarea';
  else if (e.isContentEditable) kind = 'contenteditable';
  else if (role === 'textbox' || role === 'searchbox' || role === 'combobox') kind = 'textbox';
  const editable = kind !== 'none' && !e.disabled && !e.readOnly;
  const password = kind === 'input' && e.type === 'password';
  const key = [tag, e.id, typeof e.className === 'string' ? e.className.slice(0, 80) : '', role, name].join('|');
  const out = {tag, role, name: password ? '[password]' : name, editable, kind, key};
  // A canvas application: a visible canvas covering at least 40% of the
  // viewport. Its text editor is either a hidden proxy (tiny, off screen,
  // transparent) or an element laid over the canvas.
  const area = innerWidth * innerHeight;
  const canvases = Array.from(document.querySelectorAll('canvas')).map(c => c.getBoundingClientRect())
    .filter(b => b.width * b.height >= 0.4 * area);
  if (!canvases.length || !editable || password) return out;
  out.canvas_app = true;
  const r = e.getBoundingClientRect(), cs = getComputedStyle(e);
  const onScreen = r.width >= 2 && r.height >= 2 && r.right > 0 && r.bottom > 0 && r.left < innerWidth && r.top < innerHeight;
  const hidden = !onScreen || parseFloat(cs.opacity) < 0.05 || cs.visibility === 'hidden';
  const cx = r.left + r.width / 2, cy = r.top + r.height / 2;
  const over = canvases.some(b => cx >= b.left && cx <= b.right && cy >= b.top && cy <= b.bottom);
  out.canvas_editor = hidden || over;
  if (out.canvas_editor) {
    const text = kind === 'input' || kind === 'textarea' ? String(e.value || '') : String(e.innerText || '');
    out.text = text.length > 200 ? text.slice(0, 199) + '…' : text;
  }
  return out;
})()`

// Focus reports the focused element of the selected tab.
func (s *chromedpSession) Focus(ctx context.Context) (FocusInfo, error) {
	raw, err := s.EvalContext(ctx, focusScript, 5*time.Second)
	if err != nil {
		return FocusInfo{}, err
	}
	var info FocusInfo
	if err := json.Unmarshal([]byte(raw), &info); err != nil {
		return FocusInfo{}, fmt.Errorf("focus report unreadable: %w", err)
	}
	return info, nil
}
