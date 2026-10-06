(async () => {
  const p = __PARAMS__, roots = [document];
  if (p.condition === 'url' || p.condition === 'title') {
    const actual = p.condition === 'url' ? location.href : document.title;
    return {passed:actual === p.expected,actual,count:0};
  }
  let nodes = [];
  for (let i = 0; i < roots.length && i < 100; i++) {
    const all = Array.from(roots[i].querySelectorAll('*'));
    for (const e of all) if (e.shadowRoot) roots.push(e.shadowRoot);
    nodes.push(...all);
    if (nodes.length > 20000) return {passed:false,count:0,reason:'target_scan_limit'};
  }
  if (roots.length > 100) return {passed:false,count:0,reason:'target_scan_limit'};
  const norm = s => String(s || '').replace(/\s+/g, ' ').trim();
  const compare = (a, b) => p.match === 'contains' ? norm(a).includes(norm(b)) :
    p.match === 'case_insensitive' ? norm(a).toLowerCase() === norm(b).toLowerCase() : norm(a) === norm(b);
  const name = e => norm(e.getAttribute('aria-label')) ||
    norm((e.getAttribute('aria-labelledby') || '').split(/\s+/).map(id =>
      (e.getRootNode().getElementById?.(id) || document.getElementById(id))?.textContent || '').join(' ')) ||
    norm(Array.from(e.labels || []).map(l => l.textContent).join(' ')) ||
    norm(e.innerText) || norm(e.getAttribute('alt')) || norm(e.getAttribute('title')) ||
    norm(e.getAttribute('placeholder')) || (['button','submit','reset'].includes(e.type) ? norm(e.value) : '');
  const role = e => e.getAttribute('role') ||
    ({BUTTON:'button',A:e.hasAttribute('href')?'link':'',SELECT:'combobox',TEXTAREA:'textbox',OPTION:'option'}[e.tagName]) ||
    (e.tagName === 'INPUT' ? ({checkbox:'checkbox',radio:'radio',submit:'button',button:'button',reset:'button',range:'slider',number:'spinbutton'}[e.type] || 'textbox') : e.isContentEditable ? 'textbox' : '');
  const contains = (container, e) => {
    for (let n = e; n; n = n.parentElement || n.getRootNode()?.host) if (n === container) return true;
    return false;
  };
  const visible = e => e.isConnected && e.getClientRects().length > 0 &&
    !['hidden','collapse'].includes(getComputedStyle(e).visibility) && !e.closest('[inert]');
  const enabled = e => !e.matches(':disabled') && e.getAttribute('aria-disabled') !== 'true';
  let scope;
  if (p.scope) {
    const scopes = roots.flatMap(r => Array.from(r.querySelectorAll(p.scope))).filter(visible);
    if (scopes.length !== 1) return {count:scopes.length,passed:false,reason:'scope_not_unique'};
    scope = scopes[0];
  }
  let matches = p.selector ? roots.flatMap(r => Array.from(r.querySelectorAll(p.selector))) :
    nodes.filter(e => (!p.role || role(e) === p.role) && (!p.name || compare(name(e), p.name)) &&
      (!p.label || Array.from(e.labels || []).some(l => compare(l.textContent, p.label))));
  matches = matches.filter(e => (!scope || contains(scope,e)) && e !== window[Symbol.for('atlas.agent.activity.v1')]?.host);
  if (p.action === 'find') return {count:matches.length,truncated:matches.length>100,matches:matches.slice(0,100).map(e => {
    const r=e.getBoundingClientRect();
    return {role:role(e),name:e.type==='password'?'[password]':name(e),visible:visible(e),disabled:!enabled(e),tag:e.tagName,rect:{x:r.x,y:r.y,width:r.width,height:r.height}};
  })};
  if (p.condition === 'hidden') return {count:matches.length,passed:matches.every(e=>!visible(e))};
  if (p.condition === 'count') return {count:matches.length,actual:String(matches.length),passed:String(matches.length)===p.expected};
  if (p.action !== 'assert') matches = matches.filter(visible);
  if (matches.length !== 1) return {count:matches.length,passed:false};
  const e=matches[0];
  if (p.action === 'assert') {
    if (p.condition==='value' && e.type==='password') return {count:1,passed:false,reason:'password inspection prohibited'};
    const actual = p.condition==='text'?norm(e.innerText):p.condition==='value'?e.value:
      p.condition==='checked'?String(!!e.checked||e.getAttribute('aria-checked')==='true'):
      p.condition==='selected'?String(!!e.selected||e.getAttribute('aria-selected')==='true'):null;
    return {count:1,passed:p.condition==='visible'?visible(e):p.condition==='enabled'?enabled(e):actual===p.expected,actual};
  }
  e.scrollIntoView({block:'center',inline:'center'});
  const a=e.getBoundingClientRect();
  await new Promise(resolve=>requestAnimationFrame(()=>requestAnimationFrame(resolve)));
  const b=e.getBoundingClientRect(),x=b.x+b.width/2,y=b.y+b.height/2;
  const hit=e.getRootNode().elementFromPoint(x,y);
  const stable=Math.abs(a.x-b.x)<.5&&Math.abs(a.y-b.y)<.5&&Math.abs(a.width-b.width)<.5&&Math.abs(a.height-b.height)<.5;
  const editable=p.action!=='semantic_type'||(!e.readOnly&&(e.tagName==='INPUT'||e.tagName==='TEXTAREA'||e.isContentEditable));
  const passed=visible(e)&&stable&&enabled(e)&&editable&&(hit===e||e.contains(hit));
  if (passed&&p.action==='semantic_type') {
    e.focus();
    if(e.select)e.select();
    else if(e.isContentEditable){const range=document.createRange();range.selectNodeContents(e);const selection=getSelection();selection.removeAllRanges();selection.addRange(range)}
  }
  return {count:1,passed,x,y};
})()
