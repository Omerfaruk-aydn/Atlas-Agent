(async () => {
  const p = __PARAMS__;
  __TARGET_LIB__
  const tokenAttr = 'data-atlas-field';
  const kindOf = e => {
    if (e.isContentEditable) return 'contenteditable';
    const tag = e.tagName;
    if (tag === 'SELECT') return 'select';
    if (tag === 'TEXTAREA') return 'textarea';
    if (tag === 'INPUT') {
      const t = (e.type || 'text').toLowerCase();
      if (['checkbox', 'radio'].includes(t)) return t;
      if (['date', 'time', 'datetime-local', 'month', 'week', 'color', 'range'].includes(t)) return 'native_' + t.replace('-', '_');
      if (t === 'file') return 'file';
      if (t === 'password') return 'password';
      if (t === 'number') return 'number';
      return 'text';
    }
    const r = (e.getAttribute('role') || '').split(/\s+/)[0];
    if (['combobox', 'listbox', 'spinbutton', 'slider', 'switch', 'checkbox', 'radio'].includes(r)) return 'custom_' + r;
    return 'other';
  };
  const valueOf = e => {
    if (!e) return '';
    const kind = kindOf(e);
    if (kind === 'password') return e.value ? '[password]' : '';
    if (kind === 'checkbox' || kind === 'radio') return String(!!e.checked);
    if (kind === 'select') { const o = e.selectedOptions?.[0]; return o ? (o.textContent.trim() || o.value) : ''; }
    if (kind === 'contenteditable') return e.innerText.replace(/\n$/, '');
    // A button's value is a form attribute, not what it shows; custom
    // comboboxes built on buttons display their choice as text.
    if (e.tagName !== 'BUTTON' && 'value' in e && typeof e.value === 'string') return e.value;
    const checked = e.getAttribute('aria-checked'); if (checked) return checked;
    return e.getAttribute('aria-valuetext') || e.getAttribute('aria-valuenow') || (e.innerText || '').trim();
  };
  // Text an application shows next to a control that holds something else:
  // Figma's W field keeps the number and shows the chosen mode ("Fill")
  // beside it. Read from the nearest wrapper that has visible text.
  const shownOf = e => {
    for (let n = e && e.parentElement, d = 0; n && d < 4; n = n.parentElement, d++) {
      const t = String(n.innerText || '').replace(/\s+/g, ' ').trim();
      if (t) return t.slice(0, 80);
    }
    return '';
  };
  const optionValue = e => e && kindOf(e) === 'select' ? (e.selectedOptions?.[0]?.value ?? '') : null;
  // An element or an ancestor marked aria-busy is still being processed by
  // the application; its value is not an answer yet.
  const busy = e => { for (let n = e; n && n.nodeType === 1; n = n.parentElement || n.getRootNode?.()?.host) if (n.getAttribute('aria-busy') === 'true') return true; return false; };
  const inForm = e => !!(e.form || e.closest?.('form'));
  const deepActive = () => { let a = document.activeElement; while (a && a.shadowRoot && a.shadowRoot.activeElement) a = a.shadowRoot.activeElement; return a; };
  // Keyboard input goes to the focused frame. An element can be the active
  // element of a frame that is not focused, so inside a frame every
  // ancestor document must have the frame leading here as its active
  // element too. (document.hasFocus is false for headless pages.)
  const frameChainFocused = () => {
    try {
      for (let w = window; w !== w.parent; w = w.parent) {
        const owner = w.frameElement;
        if (!owner) return false;
        let a = owner.ownerDocument.activeElement;
        while (a && a.shadowRoot && a.shadowRoot.activeElement) a = a.shadowRoot.activeElement;
        if (a !== owner) return false;
      }
      return true;
    } catch (_) { return false; }
  };
  const hasKeyboard = e => deepActive() === e && frameChainFocused();
  const allRoots = () => {
    const roots = [document];
    for (let i = 0; i < roots.length && i < 200; i++) for (const e of roots[i].querySelectorAll('*')) if (e.shadowRoot) roots.push(e.shadowRoot);
    return roots;
  };
  const byToken = () => {
    for (const r of allRoots()) { const found = r.querySelector(`[${tokenAttr}="${p.token}"]`); if (found) return found; }
    return null;
  };
  const describe = (T, e) => ({role: T.role(e), name: (T.name(e) || T.hint(e)).slice(0, 80), context: T.context(e), kind: kindOf(e), in_form: inForm(e)});
  const validity = e => {
    if (!e) return {valid: true};
    let message = '';
    if (e.willValidate && e.validity && !e.validity.valid) message = e.validationMessage;
    if (!message && e.getAttribute?.('aria-invalid') === 'true') {
      const ids = (e.getAttribute('aria-errormessage') || e.getAttribute('aria-describedby') || '').split(/\s+/).filter(Boolean);
      message = ids.map(id => (e.getRootNode().getElementById?.(id) || document.getElementById(id))?.textContent || '').join(' ').trim() || 'invalid';
    }
    return {valid: !message, message: message.slice(0, 200)};
  };
  // identity describes what the field currently edits beyond its own label:
  // the selected objects on the page (layers, rows, tabs) and the other
  // fields of its panel. A property panel reused for another selection
  // keeps its labels but changes these. The values never leave the browser
  // session in clear; Go keeps only a hash.
  const PANEL = 'fieldset,form,section,aside,dialog,[role=group],[role=region],[role=dialog],[role=tabpanel],[role=complementary],[role=form]';
  // Node identities survive repeated reads but never a new document. Names
  // and values alone cannot distinguish two identically named objects.
  const identityKey = Symbol.for('atlas.agent.field.identity.v1');
  const identities = window[identityKey] || (window[identityKey] = {nodes: new WeakMap(), seq: 0});
  const objectIdentity = n => {
    let id = identities.nodes.get(n);
    if (!id) { id = String(++identities.seq); identities.nodes.set(n, id); }
    // App identities also detect a virtualized row reused for another object.
    const app = ['id', 'data-node-id', 'data-layer-id', 'data-object-id'].map(a => n.getAttribute(a) || '');
    return JSON.stringify([id, ...app]);
  };
  const identity = (T, e) => {
    const selection = [];
    for (const r of allRoots()) {
      for (const n of r.querySelectorAll('[aria-selected="true"],[aria-current]:not([aria-current="false"])')) {
        // Hidden elements (options of a closed dropdown) are not a selection,
        // and their labels often carry sizes that the edit itself changes.
        if (n.contains(e) || selection.length >= 20 || !T.visible(n)) continue;
        selection.push(objectIdentity(n) + ':' + (T.role(n) || n.tagName.toLowerCase()) + ':' + T.norm(T.name(n)).slice(0, 60));
      }
    }
    const scope = [];
    let panel = null;
    for (let n = e.parentElement || e.getRootNode?.()?.host; n && n.nodeType === 1; n = n.parentElement || n.getRootNode?.()?.host) if (n.matches?.(PANEL)) { panel = n; break; }
    if (panel) {
      for (const f of panel.querySelectorAll('input,textarea,select,[contenteditable="true"]')) {
        if (f === e || scope.length >= 40 || kindOf(f) === 'password' || kindOf(f) === 'file') continue;
        scope.push((T.name(f) || T.hint(f)).slice(0, 40) + '=' + valueOf(f).slice(0, 200));
      }
    }
    return {selection: selection.sort(), scope};
  };
  const editable = matches => {
    // A field target names a control, not its label or row: when a name
    // also matches surrounding text, keep only the editable candidates.
    if (!p.target.selector && matches.length > 1) {
      const fields = matches.filter(e => kindOf(e) !== 'other');
      if (fields.length) return fields;
    }
    return matches;
  };
  if (p.op === 'prepare' || p.op === 'resolve') {
    const T = atlasTargets(p.target);
    if (T.error) return {ok: false, reason: T.error, count: T.count || 0, candidates: T.candidates || [], available: T.available || []};
    let matches = editable(T.matches.filter(T.visible));
    if (p.near_token && matches.length > 1) {
      // Options of a list that a control just opened: when several lists
      // offer the same option (horizontal and vertical "Fill container"),
      // take the one the control owns, else the one nearest to it.
      const owner = document.querySelector('[' + tokenAttr + '="' + p.near_token + '"]');
      if (owner) {
        const owned = (owner.getAttribute('aria-controls') || owner.getAttribute('aria-owns') || '').split(/\s+/).filter(Boolean).map(id => owner.getRootNode().getElementById?.(id) || document.getElementById(id)).filter(Boolean);
        let narrowed = matches.filter(m => owned.some(c => c.contains(m)));
        if (narrowed.length !== 1) {
          narrowed = [];
          for (let n = owner.parentElement, d = 0; n && d < 12; n = n.parentElement, d++) {
            const inside = matches.filter(m => n.contains(m));
            if (inside.length === 1) { narrowed = inside; break; }
            if (inside.length > 1) break;
          }
        }
        if (narrowed.length === 1) matches = narrowed;
      }
    }
    if (matches.length !== 1) {
      let similar = [];
      const wanted = T.norm(p.target.label || p.target.name || '').toLowerCase();
      if (!matches.length && wanted) {
        // Fields whose name shares a word with the request: the label may
        // be the field's accessible name or a nearby text.
        const words = wanted.split(' ').filter(w => w.length > 2);
        const loose = atlasTargets({within: p.target.within, within_role: p.target.within_role, scope: p.target.scope});
        if (!loose.error) similar = editable(loose.matches.filter(loose.visible)).filter(e => {
          const n = T.norm(T.name(e) || T.hint(e)).toLowerCase();
          return n && words.some(w => n.includes(w));
        }).slice(0, 6).map(e => describe(T, e));
      }
      return {ok: false, reason: matches.length ? 'ambiguous_target' : 'target_missing', count: matches.length,
        candidates: matches.length ? matches.slice(0, 6).map(e => describe(T, e)) : similar};
    }
    const e = matches[0];
    const info = describe(T, e);
    if (info.kind === 'password') return {ok: false, reason: 'password_field_requires_vault_fill', count: 1};
    const v = validity(e);
    if (p.op === 'resolve') return {ok: true, count: 1, ...info, before: valueOf(e), option_value: optionValue(e), valid: v.valid, message: v.message, busy: busy(e), identity: identity(T, e)};
    if (!T.enabled(e) || e.readOnly) return {ok: false, reason: 'target_not_actionable: field is disabled or read-only', count: 1, ...info};
    e.setAttribute(tokenAttr, p.token);
    e.scrollIntoView({block: 'center', inline: 'center'});
    // Hidden tabs never run animation frames; a timer bounds the wait.
    await new Promise(r => { requestAnimationFrame(() => requestAnimationFrame(r)); setTimeout(r, 200); });
    const b = e.getBoundingClientRect();
    const before = valueOf(e);
    // The value the field should hold once the input lands, so append and
    // replace_selection are checked against the whole field, not the part.
    let expected = null;
    const native = info.kind === 'select' || info.kind.startsWith('native_') || info.kind === 'checkbox' || info.kind === 'radio';
    if (!native && p.mode !== 'click') {
      if (window !== window.parent) window.focus();
      e.focus({preventScroll: true});
      if (info.kind === 'contenteditable') {
        if (p.mode === 'append') expected = before + p.value;
        const range = document.createRange(); range.selectNodeContents(e);
        if (p.mode === 'append') range.collapse(false);
        if (p.mode !== 'replace_selection') { const sel = getSelection(); sel.removeAllRanges(); sel.addRange(range); }
      } else if (e.setSelectionRange && typeof e.value === 'string') {
        try {
          if (p.mode === 'append') { e.setSelectionRange(e.value.length, e.value.length); expected = e.value + p.value; }
          else if (p.mode === 'replace_selection') {
            const s = e.selectionStart, end = e.selectionEnd;
            if (typeof s === 'number' && typeof end === 'number') expected = e.value.slice(0, s) + p.value + e.value.slice(end);
          } else e.select();
        } catch (_) { e.select?.(); }
      }
    }
    // Bind a rename to its parent and child slot. A name appearing elsewhere
    // cannot establish that this object was renamed.
    const renameKey = Symbol.for('atlas.agent.field.rename.v1');
    const renames = window[renameKey] || (window[renameKey] = new Map());
    const renameParent = e.parentElement;
    if (renameParent) renames.set(p.token, {parent: renameParent, slot: Array.from(renameParent.children).indexOf(e)});
    let namedBefore = null;
    if (p.value) { const N = atlasTargets({name: p.value}); if (!N.error) namedBefore = N.matches.filter(N.visible).length; }
    return {ok: true, count: 1, ...info, before, expected, focused: hasKeyboard(e), x: b.x + b.width / 2, y: b.y + b.height / 2, identity: identity(T, e), named_before: namedBefore, shown: shownOf(e), expanded: e.getAttribute('aria-expanded') === 'true'};
  }
  const e = byToken();
  if (p.op === 'option_trigger') {
    if (!e) return {ok: false, reason: 'target_missing: option control disappeared'};
    const T = atlasTargets({selector: `[${tokenAttr}="${p.token}"]`});
    let trigger = null;
    if (e.tagName === 'BUTTON' || T.role(e) === 'button' ||
      (e.tagName !== 'INPUT' && ['combobox', 'listbox'].includes(T.role(e)))) trigger = e;
    // Number fields can have a separate mode button. Search only their
    // immediate wrappers, never a different property in the whole panel.
    for (let n = e.parentElement, d = 0; !trigger && n && d < 2; n = n.parentElement, d++) {
      if (n.querySelectorAll('input,textarea,select,[role="combobox"],[role="listbox"]').length > 1) break;
      const candidates = Array.from(n.querySelectorAll('button,[role="button"],[aria-haspopup="listbox"],[aria-haspopup="menu"],span,div'))
        .filter(c => c !== e && !c.contains(e) && T.visible(c) && !c.disabled && c.getAttribute('aria-disabled') !== 'true')
        .filter(c => c.matches('button,[role="button"],[aria-haspopup="listbox"],[aria-haspopup="menu"]') ||
          (/^[⌄▾▼∨]$/.test((c.textContent || '').trim()) && getComputedStyle(c).cursor === 'pointer'));
      if (candidates.length > 1) return {ok: false, reason: 'ambiguous_target: multiple option triggers beside the field'};
      if (candidates.length === 1) trigger = candidates[0];
    }
    if (!trigger && ['combobox', 'listbox'].includes(T.role(e))) trigger = e;
    if (!trigger) return {ok: false, reason: 'menu_trigger_missing: no unique visible option trigger beside the field; inspect the control'};
    const b = trigger.getBoundingClientRect();
    return {ok: true, count: 1, x: b.x + b.width / 2, y: b.y + b.height / 2};
  }
  if (p.op === 'apply_native') {
    if (!e) return {ok: false, reason: 'target_missing: field disappeared before input'};
    const kind = kindOf(e);
    const setter = (proto, prop, value) => Object.getOwnPropertyDescriptor(proto, prop).set.call(e, value);
    if (kind === 'select') {
      const raw = String(p.value);
      const options = Array.from(e.options);
      // An exact value or label wins; a case-insensitive match is used only
      // when it is the only one.
      let found = options.filter(o => o.value === raw || o.textContent.trim() === raw.trim());
      if (!found.length) {
        const want = raw.trim().toLowerCase();
        found = options.filter(o => o.value.toLowerCase() === want || o.textContent.trim().toLowerCase() === want);
      }
      if (found.length !== 1) return {ok: false, reason: found.length ? 'ambiguous_option' : 'option_missing', options: options.slice(0, 30).map(o => o.textContent.trim())};
      setter(HTMLSelectElement.prototype, 'value', found[0].value);
    } else if (kind === 'checkbox' || kind === 'radio') {
      const want = ['true', '1', 'on', 'checked', 'yes'].includes(String(p.value).toLowerCase());
      if (e.checked !== want) e.click();
      return {ok: true, after: valueOf(e)};
    } else {
      setter(HTMLInputElement.prototype, 'value', String(p.value));
    }
    e.dispatchEvent(new Event('input', {bubbles: true, composed: true}));
    e.dispatchEvent(new Event('change', {bubbles: true, composed: true}));
    return {ok: true, after: valueOf(e)};
  }
  if (p.op === 'blur') {
    if (e) { e.blur(); e.dispatchEvent(new FocusEvent('focusout', {bubbles: true, composed: true})); }
    return {ok: !!e};
  }
  if (p.op === 'read') {
    const out = {connected: !!(e && e.isConnected), value: e ? valueOf(e) : '', option_value: optionValue(e), focused: !!e && deepActive() === e, busy: !!e && busy(e), shown: shownOf(e)};
    if (e) Object.assign(out, validity(e));
    if (e) { const T = atlasTargets({selector: `[${tokenAttr}="${p.token}"]`}); out.label = (T.name(e) || T.hint(e)).slice(0, 80); }
    if (p.target && (p.target.role || p.target.name || p.target.label || p.target.selector)) {
      const T = atlasTargets(p.target);
      const matches = editable((T.matches || []).filter(T.visible));
      out.resolved_count = matches.length;
      if (matches.length === 1) {
        out.resolved_is_token = matches[0] === e;
        out.resolved_value = valueOf(matches[0]);
        out.resolved_option_value = optionValue(matches[0]);
        Object.assign(out, validity(matches[0]));
        out.busy = out.busy || busy(matches[0]);
        if (p.identity) out.identity = identity(T, matches[0]);
      }
    }
    return out;
  }
  if (p.op === 'named') {
    const anchor = window[Symbol.for('atlas.agent.field.rename.v1')]?.get(p.token);
    if (!anchor || !anchor.parent.isConnected) return {ok: false, reason: 'rename_identity_lost'};
    const replacement = anchor.parent.children[anchor.slot];
    const N = atlasTargets({name: p.value});
    if (N.error) return {ok: false, reason: N.error};
    const matching = N.matches.filter(n => N.visible(n) && replacement &&
      (n === replacement || replacement.contains(n)) &&
      ['row', 'treeitem', 'tab', 'button', 'heading'].includes(N.role(n)));
    return {ok: true, count: matching.length, connected: !!(e && e.isConnected)};
  }
  if (p.op === 'cleanup') {
    if (e) e.removeAttribute(tokenAttr);
    window[Symbol.for('atlas.agent.field.rename.v1')]?.delete(p.token);
    return {ok: true};
  }
  return {ok: false, reason: 'unsupported field operation'};
})()
