package webverify

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

//go:embed builtin_a11y.js
var builtinA11yScript string

// loadTimingScript reads the navigation timing of the current document.
const loadTimingScript = `(()=>{const n=performance.getEntriesByType('navigation')[0];if(!n)return JSON.stringify(null);return JSON.stringify({ttfb:Math.round(n.responseStart||0),dom_content_loaded:Math.round(n.domContentLoadedEventEnd||0),load:Math.round(n.loadEventEnd||0),transfer_bytes:Math.round(n.transferSize||0)})})()`

// layoutScript reports horizontal page overflow and a blank page. Only a
// document that really scrolls sideways counts; an element clipped by an
// overflow:hidden ancestor is not a defect the user can see.
const layoutScript = `(()=>{
 const excerpt=(e)=>{const s=(e.outerHTML||'').replace(/\s+/g,' ');return s.length>240?s.slice(0,240)+'...':s};
 const path=(e)=>{const p=[];while(e&&e.nodeType===1&&p.length<4){let s=e.tagName.toLowerCase();if(e.id)s+='#'+e.id;else if(e.classList&&e.classList.length)s+='.'+e.classList[0];p.unshift(s);e=e.parentElement}return p.join(' > ')};
 const de=document.documentElement,w=window.innerWidth;
 const scrolls=de.scrollWidth>w+1;
 const wide=[];
 if(scrolls){
  for(const e of document.body?document.body.querySelectorAll('*'):[]){
   const r=e.getBoundingClientRect();if(r.width<=0||r.height<=0)continue;
   if(r.right<=w+1&&r.left>=-1)continue;
   const cs=getComputedStyle(e);if(cs.visibility==='hidden'||cs.display==='none'||cs.position==='fixed')continue;
   let p=e.parentElement,inside=false;
   while(p&&p!==document.body){const o=getComputedStyle(p).overflowX;if(o==='auto'||o==='scroll'||o==='hidden'||o==='clip'){inside=true;break}p=p.parentElement}
   if(inside)continue;
   wide.push({target:path(e),html:excerpt(e),right:Math.round(r.right)});
   if(wide.length>=4)break;
  }
 }
 const text=(document.body&&document.body.innerText||'').trim();
 const media=document.querySelectorAll('img,canvas,video,svg,iframe').length;
 return JSON.stringify({inner_width:w,scroll_width:de.scrollWidth,scrolls:scrolls,wide:wide,blank:text.length===0&&media===0,body:excerpt(document.body||de)});
})()`

// bodyExcerptScript returns the start of the visible page text, used as DOM
// evidence when a flow step fails.
const bodyExcerptScript = `(()=>{const t=((document.body&&document.body.innerText)||'').replace(/\s+/g,' ').trim();return JSON.stringify({url:location.href,title:document.title,text:t.length>400?t.slice(0,400)+'...':t})})()`

// axeRunScript runs a previously injected axe-core.
const axeRunScript = `(async()=>{const r=await window.axe.run(document,{resultTypes:['violations']});return JSON.stringify(r.violations.map(v=>({id:v.id,impact:v.impact||'minor',help:v.help,helpUrl:v.helpUrl,nodes:v.nodes.slice(0,3).map(n=>({target:(n.target||[]).join(' '),html:(n.html||'').slice(0,300)}))})))})()`

// maxAxeBytes bounds an axe-core bundle read from disk.
const maxAxeBytes = 4 << 20

// AxeScript is the accessibility engine to inject into pages.
type AxeScript struct {
	// Source is the JavaScript that defines window.axe, empty when axe-core
	// is not available and the built-in subset runs instead.
	Source string
	// Path is where the script was read from.
	Path string
}

// FindAxe looks for axe-core in the places a project keeps it: an explicit
// path, node_modules and .atlas/vendor. axe-core is MPL-2.0 licensed and is
// never redistributed with Atlas; a project that wants the full rule set
// installs it (npm i -D axe-core) and Atlas reads the file in place.
func FindAxe(root, explicit string) AxeScript {
	var candidates []string
	if explicit != "" {
		if filepath.IsAbs(explicit) {
			candidates = append(candidates, explicit)
		} else {
			candidates = append(candidates, filepath.Join(root, explicit))
		}
	}
	if root != "" {
		candidates = append(candidates,
			filepath.Join(root, "node_modules", "axe-core", "axe.min.js"),
			filepath.Join(root, ".atlas", "vendor", "axe.min.js"),
		)
	}
	for _, path := range candidates {
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() || info.Size() == 0 || info.Size() > maxAxeBytes {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil || !strings.Contains(string(data), "axe") {
			continue
		}
		return AxeScript{Source: string(data), Path: path}
	}
	return AxeScript{}
}

// injectScript defines window.axe from the script source and reports the
// type found, so a bundle that did not install itself is detected.
func (a AxeScript) injectScript() string {
	return "(function(){" + a.Source + "\n;return JSON.stringify(typeof window.axe)})()"
}

// Name labels the engine in the report.
func (a AxeScript) Name() string {
	if a.Source != "" {
		return "axe-core"
	}
	return "built-in subset"
}

// A11yViolation is one rule that failed on the page.
type A11yViolation struct {
	ID      string     `json:"id"`
	Impact  string     `json:"impact"`
	Help    string     `json:"help"`
	HelpURL string     `json:"helpUrl"`
	Nodes   []A11yNode `json:"nodes"`
}

// A11yNode is an element that violates a rule.
type A11yNode struct {
	Target string `json:"target"`
	HTML   string `json:"html"`
}

// decodeJSONString unwraps the double encoding of a script that returns
// JSON.stringify output: the tool reports the string as a JSON string.
func decodeJSONString(raw string, into any) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fmt.Errorf("empty script result")
	}
	if strings.HasPrefix(raw, `"`) {
		var inner string
		if err := json.Unmarshal([]byte(raw), &inner); err != nil {
			return err
		}
		raw = inner
	}
	return json.Unmarshal([]byte(raw), into)
}

// ParseA11y reads the violations a script returned.
func ParseA11y(raw string) ([]A11yViolation, error) {
	var out []A11yViolation
	if err := decodeJSONString(raw, &out); err != nil {
		return nil, fmt.Errorf("accessibility result: %w", err)
	}
	return out, nil
}
