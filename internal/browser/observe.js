(function(p) {
  // Interactive-element observation shared by snapshot, annotated
  // screenshots and change-focused observations. Refs persist on the node
  // (data-atlas-ref) and never survive a navigation.
  var counter = window.__atlasRefCounter || 0;
  var vw = window.innerWidth, vh = window.innerHeight;
  var host = window[Symbol.for('atlas.agent.activity.v1')];
  host = host && host.host;
  var selector = 'a[href], button, input, textarea, select, summary, iframe, [role], [contenteditable=""], [contenteditable="true"], [onclick], [tabindex], dialog[open], [aria-invalid="true"], [aria-live]';
  var roots = [document], shadowOf = new Map(), scanned = 0, scanLimit = false;
  for (var r = 0; r < roots.length && r < 200; r++) {
    var all = roots[r].querySelectorAll('*');
    scanned += all.length;
    if (scanned > 60000) { scanLimit = true; break; }
    for (var j = 0; j < all.length; j++) {
      if (all[j].shadowRoot) { roots.push(all[j].shadowRoot); shadowOf.set(all[j].shadowRoot, true); }
    }
  }
  var norm = function(s) { return String(s || '').replace(/\s+/g, ' ').trim(); };
  var clip = function(s, n) { s = norm(s); return s.length > n ? s.slice(0, n - 1) + '…' : s; };
  var parentOf = function(n) { return n.parentElement || (n.getRootNode && n.getRootNode().host) || null; };
  var byId = function(el, id) { var root = el.getRootNode(); return (root.getElementById && root.getElementById(id)) || document.getElementById(id); };
  var labelled = function(el) {
    var ids = (el.getAttribute('aria-labelledby') || '').split(/\s+/).filter(Boolean);
    return norm(ids.map(function(id) { var n = byId(el, id); return n ? n.textContent : ''; }).join(' '));
  };
  var roleOf = function(el) {
    var role = el.getAttribute('role');
    if (role) return role.split(/\s+/)[0];
    var tag = el.tagName.toLowerCase();
    if (tag === 'a') return 'link';
    if (tag === 'select') return 'combobox';
    if (tag === 'textarea') return 'textbox';
    if (tag === 'dialog') return 'dialog';
    if (tag === 'iframe') return 'iframe';
    if (tag === 'summary') return 'button';
    if (tag === 'input') {
      var t = (el.getAttribute('type') || 'text').toLowerCase();
      if (t === 'checkbox' || t === 'radio') return t;
      if (t === 'submit' || t === 'button' || t === 'reset' || t === 'image') return 'button';
      if (t === 'range') return 'slider';
      if (t === 'number') return 'spinbutton';
      return 'textbox';
    }
    if (el.isContentEditable) return 'textbox';
    return tag;
  };
  var nameOf = function(el) {
    var tag = el.tagName.toLowerCase();
    var name = el.getAttribute('aria-label') || labelled(el);
    if (!name && el.labels && el.labels.length) name = Array.from(el.labels).map(function(l) {
      if (!l.contains(el)) return l.innerText;
      var clone = l.cloneNode(true);
      clone.querySelectorAll('select,textarea,input,option,button').forEach(function(c) { c.remove(); });
      return clone.textContent;
    }).join(' ');
    if (!name) name = el.getAttribute('placeholder') || el.getAttribute('alt') || el.getAttribute('title') || '';
    if (!name && tag === 'iframe') {
      try { name = el.getAttribute('name') || new URL(el.src, location.href).origin; } catch (e) { name = ''; }
    }
    if (!name && tag !== 'input' && tag !== 'textarea' && tag !== 'select') name = el.innerText || '';
    if (!name && (el.type === 'submit' || el.type === 'button')) name = el.value || '';
    return clip(name, 120);
  };
  var CONTAINERS = {dialog: 1, alertdialog: 1, region: 1, form: 1, navigation: 1, complementary: 1, group: 1, toolbar: 1, menu: 1, listbox: 1, tabpanel: 1, tree: 1, grid: 1, row: 1, banner: 1, contentinfo: 1, main: 1, alert: 1, status: 1, article: 1, listitem: 0};
  var containerName = function(n) {
    var name = n.getAttribute('aria-label') || labelled(n);
    if (!name) {
      var legend = n.querySelector && n.querySelector(':scope > legend, :scope > h1, :scope > h2, :scope > h3, :scope > h4, :scope > header h1, :scope > header h2, :scope > header h3');
      if (legend) name = legend.textContent;
    }
    if (!name) name = n.getAttribute('title') || n.getAttribute('data-testid') || '';
    return clip(name, 40);
  };
  var contextOf = function(el) {
    var parts = [], depth = 0;
    for (var n = parentOf(el); n && n.nodeType === 1 && depth < 40 && parts.length < 2; n = parentOf(n), depth++) {
      var tag = n.tagName.toLowerCase(), role = n.getAttribute('role');
      if (!role) role = {dialog: 'dialog', form: 'form', nav: 'navigation', aside: 'complementary', fieldset: 'group', section: 'region', header: 'banner', footer: 'contentinfo', main: 'main', article: 'article', li: 'listitem'}[tag] || '';
      if (!role || !(role in CONTAINERS)) continue;
      var name = containerName(n);
      if (!name && role !== 'dialog' && role !== 'alertdialog' && role !== 'form') continue;
      parts.unshift(role + (name ? ' "' + name + '"' : ''));
    }
    return parts.join(' > ');
  };
  var hintOf = function(el) {
    for (var n = parentOf(el), depth = 0; n && n.nodeType === 1 && depth < 3; n = parentOf(n), depth++) {
      var attr = n.getAttribute('aria-label') || n.getAttribute('data-tooltip') || n.getAttribute('data-label') || n.getAttribute('title');
      if (attr) return clip(attr, 40);
      var prev = n === parentOf(el) ? el.previousElementSibling : null;
      if (prev && prev.innerText && norm(prev.innerText).length <= 30) return clip(prev.innerText, 40);
      var text = '';
      for (var c = n.firstChild; c; c = c.nextSibling) if (c.nodeType === 3) text += c.textContent;
      if (norm(text)) return clip(text, 40);
    }
    return '';
  };
  var errorOf = function(el) {
    var msg = '';
    if (el.willValidate && el.validity && !el.validity.valid) msg = el.validationMessage;
    if (!msg && el.getAttribute('aria-invalid') === 'true') {
      var ids = (el.getAttribute('aria-errormessage') || el.getAttribute('aria-describedby') || '').split(/\s+/).filter(Boolean);
      msg = ids.map(function(id) { var n = byId(el, id); return n ? n.textContent : ''; }).join(' ') || 'invalid';
    }
    return clip(msg, 120);
  };
  if (p.identify) {
    // Identity of already-issued refs, named exactly as an observation
    // names them, so a node reused for another item is not mistaken for
    // the one the model saw.
    var ids = {};
    p.identify.forEach(function(ref) {
      var el = null;
      for (var q = 0; q < roots.length && !el; q++) el = roots[q].querySelector('[data-atlas-ref="' + ref + '"]');
      ids[ref] = el && el.isConnected ? {role: roleOf(el), name: nameOf(el)} : null;
    });
    return JSON.stringify({identities: ids});
  }
  var scope = null;
  if (p.scope) {
    var scopes = [];
    roots.forEach(function(root) { scopes.push.apply(scopes, Array.from(root.querySelectorAll(p.scope))); });
    if (scopes.length !== 1) return JSON.stringify({error: 'scope_not_unique', count: scopes.length});
    scope = scopes[0];
  }
  var inScope = function(el) {
    if (!scope) return true;
    for (var n = el; n; n = parentOf(n)) if (n === scope) return true;
    return false;
  };
  var out = [], seen = new Set();
  // Paint bounds stop at clipping ancestors. Long layer labels can have a
  // layout box wider than their sidebar; annotating that raw box obscures
  // the canvas and suggests coordinates that are not visible targets.
  var paintRect = function(el) {
    var b = el.getBoundingClientRect();
    var left = b.left, top = b.top, right = b.right, bottom = b.bottom;
    for (var n = parentOf(el); n && n.nodeType === 1; n = parentOf(n)) {
      var s = window.getComputedStyle(n);
      if (s.display === 'contents') continue;
      var clipX = /^(hidden|clip|auto|scroll)$/.test(s.overflowX);
      var clipY = /^(hidden|clip|auto|scroll)$/.test(s.overflowY);
      if (!clipX && !clipY) continue;
      var r = n.getBoundingClientRect();
      var sx = n.offsetWidth ? r.width / n.offsetWidth : 1;
      var sy = n.offsetHeight ? r.height / n.offsetHeight : 1;
      var x = r.left + n.clientLeft * sx, y = r.top + n.clientTop * sy;
      if (clipX) { left = Math.max(left, x); right = Math.min(right, x + n.clientWidth * sx); }
      if (clipY) { top = Math.max(top, y); bottom = Math.min(bottom, y + n.clientHeight * sy); }
    }
    return {left: left, top: top, right: right, bottom: bottom, width: Math.max(0, right-left), height: Math.max(0, bottom-top)};
  };
  var active = document.activeElement;
  while (active && active.shadowRoot && active.shadowRoot.activeElement) active = active.shadowRoot.activeElement;
  for (var k = 0; k < roots.length; k++) {
    var list = roots[k].querySelectorAll(selector);
    for (var i = 0; i < list.length; i++) {
      var el = list[i];
      if (seen.has(el) || el === host || (host && host.contains && host.contains(el))) continue;
      seen.add(el);
      if (!inScope(el)) continue;
      var style = window.getComputedStyle(el);
      if (style.display === 'none' || style.visibility === 'hidden') continue;
      var rect = paintRect(el);
      if (rect.width === 0 || rect.height === 0) continue;
      var visible = !(rect.bottom < 0 || rect.top > vh || rect.right < 0 || rect.left > vw);
      if (!p.full && !visible) continue;
      var ref = el.getAttribute('data-atlas-ref');
      if (!ref) { counter++; ref = 'e' + counter; el.setAttribute('data-atlas-ref', ref); }
      var tag = el.tagName.toLowerCase();
      var role = roleOf(el);
      var item = {ref: ref, role: role, tag: tag, name: nameOf(el),
        rect: {x: rect.left, y: rect.top, width: rect.width, height: rect.height}, visible: visible};
      if (tag === 'input' || tag === 'textarea' || tag === 'select') {
        item.value = el.type === 'password' ? (el.value ? '[password]' : '') : clip(el.value || '', 200);
        if (el.type === 'checkbox' || el.type === 'radio') item.checked = el.checked ? 'true' : 'false';
      } else if (el.isContentEditable && el.innerText && el.innerText.length <= 200) {
        item.value = clip(el.innerText, 200);
      }
      var ac = el.getAttribute('aria-checked'); if (ac) item.checked = ac;
      if (el.getAttribute('aria-selected') === 'true' || (tag === 'option' && el.selected)) item.selected = true;
      var ae = el.getAttribute('aria-expanded'); if (ae) item.expanded = ae;
      if ((el.matches && el.matches(':disabled')) || el.getAttribute('aria-disabled') === 'true') item.disabled = true;
      if (el === active) item.focused = true;
      var err = errorOf(el); if (err) { item.invalid = true; item.error = err; }
      if (!item.name && (role === 'textbox' || role === 'spinbutton' || role === 'combobox' || role === 'slider')) {
        var hint = hintOf(el); if (hint) item.hint = hint;
      }
      if ((role === 'alert' || role === 'status' || el.hasAttribute('aria-live')) && !item.name) item.name = clip(el.innerText, 120);
      var ctx = contextOf(el); if (ctx) item.context = ctx;
      if (el.getRootNode() !== document) item.shadow = true;
      out.push(item);
    }
  }
  window.__atlasRefCounter = counter;
  return JSON.stringify({doc: String(performance.timeOrigin), url: location.href, title: document.title,
    elements: out, scan_limited: scanLimit, roots: roots.length});
})(__PARAMS__)
