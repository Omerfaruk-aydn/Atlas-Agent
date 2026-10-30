(()=>{
 // A small accessibility audit used when axe-core is not installed in the
 // project. It checks a handful of WCAG failures that are decidable from the
 // DOM alone and returns them in the shape axe-core uses.
 const out=[];
 const excerpt=(e)=>{const s=(e.outerHTML||'').replace(/\s+/g,' ');return s.length>240?s.slice(0,240)+'...':s};
 const path=(e)=>{const p=[];while(e&&e.nodeType===1&&p.length<4){let s=e.tagName.toLowerCase();if(e.id)s+='#'+e.id;else if(e.classList&&e.classList.length)s+='.'+e.classList[0];p.unshift(s);e=e.parentElement}return p.join(' > ')};
 const visible=(e)=>{const r=e.getBoundingClientRect(),s=getComputedStyle(e);return r.width>0&&r.height>0&&s.visibility!=='hidden'&&s.display!=='none'};
 const add=(id,impact,help,url,els)=>{if(!els.length)return;out.push({id:id,impact:impact,help:help,helpUrl:url,nodes:els.slice(0,3).map(e=>({target:path(e),html:excerpt(e)}))})};
 const text=(e)=>(e.innerText||e.textContent||'').trim();
 const labelled=(e)=>e.getAttribute('aria-label')||e.getAttribute('aria-labelledby')||e.getAttribute('title');
 const q=(s)=>[...document.querySelectorAll(s)];
 const hidden=(e)=>e.closest('[aria-hidden="true"]')||!visible(e);

 add('image-alt','critical','Images must have alternate text','https://dequeuniversity.com/rules/axe/4.10/image-alt',
  q('img').filter(e=>!e.hasAttribute('alt')&&!labelled(e)&&e.getAttribute('role')!=='presentation'&&e.getAttribute('role')!=='none'));
 add('button-name','critical','Buttons must have discernible text','https://dequeuniversity.com/rules/axe/4.10/button-name',
  q('button,[role="button"]').filter(e=>!hidden(e)&&!text(e)&&!labelled(e)&&!(e.querySelector('img[alt]:not([alt=""])'))));
 add('link-name','serious','Links must have discernible text','https://dequeuniversity.com/rules/axe/4.10/link-name',
  q('a[href]').filter(e=>!hidden(e)&&!text(e)&&!labelled(e)&&!(e.querySelector('img[alt]:not([alt=""])'))));
 add('label','critical','Form elements must have labels','https://dequeuniversity.com/rules/axe/4.10/label',
  q('input:not([type="hidden"]):not([type="submit"]):not([type="button"]):not([type="reset"]):not([type="image"]),select,textarea')
   .filter(e=>!hidden(e)&&!labelled(e)&&!(e.labels&&e.labels.length)&&!e.closest('label')));
 if(!document.documentElement.getAttribute('lang'))
  add('html-has-lang','serious','The <html> element must have a lang attribute','https://dequeuniversity.com/rules/axe/4.10/html-has-lang',[document.documentElement]);
 if(!(document.title||'').trim())
  add('document-title','serious','Documents must have a <title>','https://dequeuniversity.com/rules/axe/4.10/document-title',[document.documentElement]);
 add('meta-viewport','critical','Zooming and scaling must not be disabled','https://dequeuniversity.com/rules/axe/4.10/meta-viewport',
  q('meta[name="viewport"]').filter(e=>/user-scalable\s*=\s*(no|0)|maximum-scale\s*=\s*1(\.0)?(\s|,|$)/i.test(e.getAttribute('content')||'')));
 const ids={},dup=[];
 q('[id]').forEach(e=>{if(!ids[e.id])ids[e.id]=e;else dup.push(e)});
 add('duplicate-id','minor','id attribute values must be unique','https://dequeuniversity.com/rules/axe/4.10/duplicate-id',dup);
 add('tabindex','serious','Elements should not have tabindex greater than zero','https://dequeuniversity.com/rules/axe/4.10/tabindex',
  q('[tabindex]').filter(e=>parseInt(e.getAttribute('tabindex'),10)>0));
 add('empty-heading','minor','Headings must have discernible text','https://dequeuniversity.com/rules/axe/4.10/empty-heading',
  q('h1,h2,h3,h4,h5,h6,[role="heading"]').filter(e=>!hidden(e)&&!text(e)&&!labelled(e)));
 if(!document.querySelector('main,[role="main"]'))
  add('landmark-one-main','moderate','Document should have one main landmark','https://dequeuniversity.com/rules/axe/4.10/landmark-one-main',[document.body||document.documentElement]);
 return JSON.stringify(out);
})()
