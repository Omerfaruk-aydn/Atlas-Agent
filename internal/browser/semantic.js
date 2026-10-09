(async () => {
  const p = __PARAMS__;
  if (p.condition === 'url' || p.condition === 'title') {
    const actual = p.condition === 'url' ? location.href : document.title;
    return {passed:actual === p.expected,actual,count:0};
  }
  __TARGET_LIB__
  const T = atlasTargets(p);
  if (T.error === 'target_scan_limit') return {passed:false,count:0,reason:'target_scan_limit'};
  if (T.error === 'scope_not_unique') return {count:T.count,passed:false,reason:'scope_not_unique'};
  if (T.error) return {count:T.count,passed:false,reason:T.error,candidates:T.candidates||[],available:T.available||[]};
  const {name, role, visible, enabled, norm, context} = T;
  let matches = T.matches;
  const describe = e => ({role:role(e),name:e.type==='password'?'[password]':(name(e)||T.hint(e)).slice(0,80),context:context(e)});
  if (p.action === 'find') return {count:matches.length,truncated:matches.length>100,matches:matches.slice(0,100).map(e => {
    const r=e.getBoundingClientRect();
    let ref=e.getAttribute('data-atlas-ref');
    if(!ref){window.__atlasRefCounter=(window.__atlasRefCounter||0)+1;ref='e'+window.__atlasRefCounter;e.setAttribute('data-atlas-ref',ref)}
    return {ref,role:role(e),name:e.type==='password'?'[password]':name(e)||T.hint(e),context:context(e),visible:visible(e),disabled:!enabled(e),tag:e.tagName,rect:{x:r.x,y:r.y,width:r.width,height:r.height}};
  })};
  if (p.condition === 'hidden') return {count:matches.length,passed:matches.every(e=>!visible(e))};
  if (p.condition === 'count') return {count:matches.length,actual:String(matches.length),passed:String(matches.length)===p.expected};
  if (p.action !== 'assert') matches = matches.filter(visible);
  if (matches.length !== 1) return {count:matches.length,passed:false,candidates:matches.slice(0,6).map(describe)};
  const e=matches[0];
  if (p.action === 'assert') {
    if (p.condition==='value' && e.type==='password') return {count:1,passed:false,reason:'password inspection prohibited'};
    const value = () => ('value' in e && typeof e.value === 'string') ? e.value : (e.getAttribute('aria-valuetext') || e.getAttribute('aria-valuenow') || norm(e.innerText));
    const actual = p.condition==='text'||p.condition==='text_contains'?norm(e.innerText):p.condition==='value'||p.condition==='value_not'?value():
      p.condition==='checked'?String(!!e.checked||e.getAttribute('aria-checked')==='true'):
      p.condition==='selected'?String(!!e.selected||e.getAttribute('aria-selected')==='true'):
      p.condition==='not_busy'?String(e.getAttribute('aria-busy')==='true'):null;
    const passed = p.condition==='visible'?visible(e):p.condition==='enabled'?enabled(e):
      p.condition==='text_contains'?norm(actual).toLowerCase().includes(norm(p.expected).toLowerCase()):
      p.condition==='value_not'?actual!==p.expected:
      p.condition==='not_busy'?actual!=='true':actual===p.expected;
    return {count:1,passed,actual,target:describe(e)};
  }
  e.scrollIntoView({block:'center',inline:'center'});
  const a=e.getBoundingClientRect();
  // Hidden tabs never run animation frames; a timer bounds the wait.
  await new Promise(resolve=>{requestAnimationFrame(()=>requestAnimationFrame(resolve));setTimeout(resolve,200)});
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
  let blocker='';
  // A blocker is named so it can be acted on: its role (presentational
  // roles say nothing, so the tag), id, name and the container it is in.
  const blockerOf=h=>{const r=role(h);const what=(r&&r!=='none'&&r!=='presentation'?r:h.tagName.toLowerCase())+(h.id?'#'+h.id.slice(0,40):'');
    const n=name(h);const where=context(h);return what+(n?' "'+n.slice(0,40)+'"':'')+(where?' in '+where:'')};
  if(!passed){blocker=!enabled(e)?'disabled':!stable?'moving':!editable?'not editable':(hit&&hit!==e&&!e.contains(hit))?'covered by '+blockerOf(hit):'not visible'}
  return {count:1,passed,x,y,target:describe(e),blocker};
})()
