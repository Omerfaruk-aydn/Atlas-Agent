// atlasTargets resolves a semantic target across the document and open
// shadow roots. It returns candidates without choosing among them; callers
// refuse to act on anything but exactly one visible match.
const atlasTargets = (p) => {
  const roots = [document];
  let nodes = [];
  for (let i = 0; i < roots.length && i < 200; i++) {
    const all = Array.from(roots[i].querySelectorAll('*'));
    for (const e of all) if (e.shadowRoot) roots.push(e.shadowRoot);
    nodes.push(...all);
    if (nodes.length > 40000) return {error: 'target_scan_limit', matches: []};
  }
  const norm = s => String(s || '').replace(/\s+/g, ' ').trim();
  const compare = (a, b) => p.match === 'contains' ? norm(a).toLowerCase().includes(norm(b).toLowerCase()) :
    p.match === 'case_insensitive' ? norm(a).toLowerCase() === norm(b).toLowerCase() : norm(a) === norm(b);
  const byId = (e, id) => e.getRootNode().getElementById?.(id) || document.getElementById(id);
  const parentOf = n => n.parentElement || n.getRootNode?.()?.host || null;
  // A wrapping label's text without the control's own content (a select's
  // options, a textarea's text), so "Country" matches its select.
  const labelText = (l, e) => {
    if (!l.contains(e)) return norm(l.textContent);
    const clone = l.cloneNode(true);
    for (const c of clone.querySelectorAll('select,textarea,input,option,button')) c.remove();
    return norm(clone.textContent);
  };
  const name = e => norm(e.getAttribute('aria-label')) ||
    norm((e.getAttribute('aria-labelledby') || '').split(/\s+/).filter(Boolean).map(id => byId(e, id)?.textContent || '').join(' ')) ||
    norm(Array.from(e.labels || []).map(l => labelText(l, e)).join(' ')) ||
    (['INPUT', 'TEXTAREA', 'SELECT'].includes(e.tagName) ? '' : norm(e.innerText)) ||
    norm(e.getAttribute('alt')) || norm(e.getAttribute('title')) || norm(e.getAttribute('placeholder')) ||
    (['button', 'submit', 'reset'].includes(e.type) ? norm(e.value) : '');
  const role = e => (e.getAttribute('role') || '').split(/\s+/)[0] ||
    ({BUTTON: 'button', A: e.hasAttribute('href') ? 'link' : '', SELECT: 'combobox', TEXTAREA: 'textbox', OPTION: 'option', DIALOG: 'dialog', SUMMARY: 'button'}[e.tagName]) ||
    (e.tagName === 'INPUT' ? ({checkbox: 'checkbox', radio: 'radio', submit: 'button', button: 'button', reset: 'button', range: 'slider', number: 'spinbutton'}[e.type] || 'textbox') : e.isContentEditable ? 'textbox' : '');
  // Nearby text only names form-like controls; an icon or label span next
  // to a field must not match the field's own label.
  const fieldLike = e => ['INPUT', 'TEXTAREA', 'SELECT'].includes(e.tagName) || e.isContentEditable ||
    ['textbox', 'spinbutton', 'combobox', 'slider', 'searchbox'].includes((e.getAttribute('role') || '').split(/\s+/)[0]);
  const hint = e => {
    if (!fieldLike(e)) return '';
    for (let n = parentOf(e), d = 0; n && n.nodeType === 1 && d < 3; n = parentOf(n), d++) {
      const attr = n.getAttribute('aria-label') || n.getAttribute('data-tooltip') || n.getAttribute('data-label') || n.getAttribute('title');
      if (attr) return norm(attr);
      const prev = d === 0 ? e.previousElementSibling : null;
      if (prev && norm(prev.innerText).length && norm(prev.innerText).length <= 30) return norm(prev.innerText);
    }
    return '';
  };
  const contains = (container, e) => {
    for (let n = e; n; n = parentOf(n)) if (n === container) return true;
    return false;
  };
  // A box placed before the document's origin, with no scrolled ancestor
  // that could bring it back, can never be seen or clicked: applications
  // park closed menus there (Figma keeps every resizing list at y<0).
  const scrolledAncestor = e => {
    for (let n = parentOf(e); n && n.nodeType === 1; n = parentOf(n)) if (n.scrollTop > 0 || n.scrollLeft > 0) return true;
    return false;
  };
  const parked = e => {
    const r = e.getBoundingClientRect();
    return (r.bottom + scrollY <= 0 || r.right + scrollX <= 0) && !scrolledAncestor(e);
  };
  const visible = e => e.isConnected && e.getClientRects().length > 0 &&
    !['hidden', 'collapse'].includes(getComputedStyle(e).visibility) && !e.closest('[inert]') && !parked(e);
  // A display:contents container (application regions often are) has no
  // box of its own; it is visible when something it contains is.
  const containerVisible = (n, depth = 0) => visible(n) || (depth < 4 && n.isConnected && getComputedStyle(n).display === 'contents' &&
    Array.from(n.children).some(c => containerVisible(c, depth + 1)));
  const enabled = e => !e.matches(':disabled') && e.getAttribute('aria-disabled') !== 'true';
  const containerLabel = n => norm(n.getAttribute('aria-label')) ||
    norm((n.getAttribute('aria-labelledby') || '').split(/\s+/).filter(Boolean).map(id => byId(n, id)?.textContent || '').join(' ')) ||
    norm(n.querySelector?.(':scope > legend, :scope > h1, :scope > h2, :scope > h3, :scope > h4, :scope > h5, :scope > h6, :scope > header h1, :scope > header h2, :scope > header h3, :scope > header h4')?.textContent) ||
    norm(n.getAttribute('title'));
  const CONTAINER = 'dialog,form,section,aside,nav,article,fieldset,li,header,footer,main,[role=dialog],[role=alertdialog],[role=region],[role=group],[role=form],[role=toolbar],[role=menu],[role=listbox],[role=tabpanel],[role=article],[role=listitem],[role=row],[role=complementary],[role=navigation],[role=alert],[role=status],[aria-label],[aria-labelledby]';
  let scope = null;
  if (p.scope) {
    const scopes = roots.flatMap(r => Array.from(r.querySelectorAll(p.scope))).filter(n => containerVisible(n));
    if (scopes.length !== 1) return {error: 'scope_not_unique', count: scopes.length, matches: []};
    scope = scopes[0];
  }
  if (p.within) {
    // Container named by its accessible label or heading, e.g. a dialog
    // titled "Share" or a card headed "Computer". Nested matches keep only
    // the innermost container so "the card inside the panel" is precise.
    // Controls carry labels too ("Horizontal resizing") but never contain
    // the target; only real containers can be named by within.
    const CONTROL_ROLES = ['button', 'combobox', 'textbox', 'searchbox', 'spinbutton', 'slider', 'checkbox', 'radio', 'switch', 'option', 'menuitem', 'tab', 'link'];
    const control = n => ['INPUT', 'SELECT', 'TEXTAREA', 'BUTTON', 'A'].includes(n.tagName) || CONTROL_ROLES.includes(role(n));
    const kind = n => role(n) || n.tagName.toLowerCase();
    // within also accepts the context this library reports for a match,
    // `role "label"` or a chain `region "Right sidebar" > group "Layout"`,
    // so a context copied from find or an error resolves the same
    // container. Plain text is a label, as before.
    const segments = String(p.within).split(/\s+>\s+/).map(s => s.trim().match(/^([a-z][\w-]*)\s+"(.*)"$/i));
    const chain = segments.every(Boolean) ? segments.map(m => ({role: m[1].toLowerCase(), label: m[2]})) : [{role: '', label: p.within}];
    if (p.within_role) chain[chain.length - 1].role = chain[chain.length - 1].role || p.within_role;
    const named = within => nodes.filter(n => n.matches?.(CONTAINER) && !control(n) && containerVisible(n) && (!within || contains(within, n)));
    for (let i = 0; i < chain.length; i++) {
      const want = norm(chain[i].label).toLowerCase();
      let containers = named(scope).filter(n => norm(containerLabel(n)).toLowerCase().includes(want) &&
        (!chain[i].role || kind(n) === chain[i].role));
      // A container named exactly as asked ("Resizing") outranks containers
      // whose names merely include it ("Horizontal resizing").
      const exactly = containers.filter(n => norm(containerLabel(n)).toLowerCase() === want);
      if (exactly.length === 1) containers = exactly;
      containers = containers.filter(c => !containers.some(o => o !== c && contains(c, o)));
      if (containers.length !== 1) {
        // Name what does exist, so the next call can pick a real container
        // instead of waiting for one that is not there.
        const available = containers.length ? [] : named(scope).map(c => ({role: kind(c), name: containerLabel(c).slice(0, 60)}))
          .filter((c, j, all) => c.name && all.findIndex(o => o.role === c.role && o.name === c.name) === j).slice(0, 12);
        return {error: containers.length ? 'within_not_unique' : 'within_not_found', count: containers.length, segment: chain.length > 1 ? i : undefined,
          candidates: containers.slice(0, 5).map(c => ({role: kind(c), name: containerLabel(c).slice(0, 60)})), available, matches: []};
      }
      scope = containers[0];
    }
  }
  // A field's label: a <label> element's text, or for a label without text
  // (an icon) its aria-label or title, or the field's own accessible name.
  const labelOf = (l, e) => labelText(l, e) || norm(l.getAttribute('aria-label')) || norm(l.getAttribute('title'));
  const labelMatches = e => {
    const labels = Array.from(e.labels || []);
    if (labels.some(l => compare(labelOf(l, e), p.label))) return true;
    if (!fieldLike(e)) return false;
    return compare(hint(e), p.label) || compare(norm(e.getAttribute('aria-label')), p.label) ||
      compare(norm((e.getAttribute('aria-labelledby') || '').split(/\s+/).filter(Boolean).map(id => byId(e, id)?.textContent || '').join(' ')), p.label);
  };
  let matches = p.selector ? roots.flatMap(r => Array.from(r.querySelectorAll(p.selector))) :
    nodes.filter(e => (!p.role || role(e) === p.role) && (!p.name || compare(name(e) || hint(e), p.name)) &&
      (!p.label || labelMatches(e)));
  const host = window[Symbol.for('atlas.agent.activity.v1')]?.host;
  matches = matches.filter(e => (!scope || contains(scope, e)) && e !== host);
  if (!p.selector) {
    // Source and metadata elements have text but are never targets.
    const NONRENDERED = ['SCRIPT', 'STYLE', 'NOSCRIPT', 'TEMPLATE', 'HEAD', 'TITLE', 'META', 'LINK'];
    matches = matches.filter(e => !NONRENDERED.includes(e.tagName));
  }
  if (!p.selector && p.name) {
    // An element named only by its text matches whenever a descendant does
    // (html, body, wrappers). It is not a separate target: keep the
    // innermost matches so visible-text targeting is not ambiguous by
    // construction. Elements with their own accessible name stay.
    // An ancestor gives way only to a visible descendant that is itself a
    // target (it has a role, or the ancestor has none); a button keeps
    // its zero-size text wrapper, and that wrapper is then dropped.
    const ownName = e => !!(e.getAttribute('aria-label') || e.getAttribute('aria-labelledby'));
    matches = matches.filter(e => ownName(e) || !matches.some(o => o !== e && contains(e, o) && visible(o) && (role(o) || !role(e))));
    matches = matches.filter(e => ownName(e) || role(e) || !matches.some(a => a !== e && contains(a, e) && role(a)));
    // Some applications draw a label twice in the same box (Figma's panel
    // tabs keep a measuring copy under the visible one). The same box,
    // role and name is one place to click, not two targets.
    const box = e => { const r = e.getBoundingClientRect(); return r.width > 0 && r.height > 0 ? [r.x, r.y, r.width, r.height].map(Math.round).join(',') : ''; };
    // The copy a pointer reaches is kept (the other is covered by it).
    matches = matches.filter(e => {
      const k = box(e);
      const twins = k ? matches.filter(o => box(o) === k && role(o) === role(e) && name(o) === name(e)) : [];
      if (twins.length < 2) return true;
      const r = e.getBoundingClientRect();
      const top = document.elementFromPoint(r.x + r.width / 2, r.y + r.height / 2);
      const hit = twins.find(o => top && (o === top || o.contains(top)));
      return (hit || twins[0]) === e;
    });
  }
  const context = e => {
    const parts = [];
    for (let n = parentOf(e), d = 0; n && n.nodeType === 1 && d < 40 && parts.length < 2; n = parentOf(n), d++) {
      if (!n.matches?.(CONTAINER)) continue;
      const label = containerLabel(n);
      if (label) parts.unshift((role(n) || n.tagName.toLowerCase()) + ' "' + label.slice(0, 40) + '"');
    }
    return parts.join(' > ');
  };
  return {matches, name, role, hint, visible, enabled, context, norm};
};
