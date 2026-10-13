
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
)

// DesignPrototypeStep is deliberately flat; arbitrary scripts are not steps.
type DesignPrototypeStep struct {
	Action   string `json:"action" enum:"click,key"`
	Selector string `json:"selector,omitempty"`
	Key      string `json:"key,omitempty"`
}

type designAudit struct {
	ID               string            `json:"id"`
	URL              string            `json:"url"`
	Document         string            `json:"document"`
	Width            int               `json:"width"`
	Height           int               `json:"height"`
	Overflow         int               `json:"overflow"`
	SmallTargets     int               `json:"small_targets"`
	UnnamedTargets   int               `json:"unnamed_targets"`
	LowContrast      int               `json:"low_contrast"`
	UnknownContrast  int               `json:"unknown_contrast"`
	Truncated        bool              `json:"truncated"`
	AssertionsPassed bool              `json:"assertions_passed"`
	PrototypeSteps   int               `json:"prototype_steps"`
	AssertionCount   int               `json:"assertion_count"`
	SourceHashes     map[string]string `json:"source_hashes,omitempty"`
	Screenshot       string            `json:"screenshot"`
	ScreenshotSHA256 string            `json:"screenshot_sha256"`
	CapturedAt       int64             `json:"captured_at"`
	Passed           bool              `json:"passed"`
}

const designAuditScript = `(()=>{
 const all=[...document.querySelectorAll('body *')];let overflow=0,small=0,unnamed=0,low=0,unknown=0;
 const rgb=s=>{const m=s.match(/^rgba?\(([^)]+)\)$/);if(!m)return null;const a=m[1].split(/[, ]+/).filter(Boolean).map(Number);return a.length>=3&&a.slice(0,3).every(Number.isFinite)?a:null};
 const luminance=c=>c.slice(0,3).map(x=>{x/=255;return x<=.04045?x/12.92:((x+.055)/1.055)**2.4}).reduce((s,x,i)=>s+x*[.2126,.7152,.0722][i],0);
 for(const e of all.slice(0,1200)){
  const r=e.getBoundingClientRect(),s=getComputedStyle(e);if(r.width<=0||r.height<=0||s.visibility==='hidden'||s.display==='none')continue;
  if(r.left<-.5||r.right>innerWidth+.5)overflow++;
  if(e.matches('button,a[href],input,select,textarea,[role="button"]')){
   const hit=e.closest('label')?.getBoundingClientRect()||r;if(hit.width<44||hit.height<44)small++;
   if(!(e.getAttribute('aria-label')||e.getAttribute('aria-labelledby')||e.labels?.length||e.innerText?.trim()||e.getAttribute('title')||e.getAttribute('alt')))unnamed++;
  }
  if(![...e.childNodes].some(n=>n.nodeType===3&&n.textContent.trim()))continue;
  const fg=rgb(s.color);let bg=null,node=e,uncertain=Number(s.opacity)<1;
  while(node){const style=getComputedStyle(node),c=rgb(style.backgroundColor);if(style.backgroundImage!=='none')uncertain=true;if(c&&c[3]!==0){if(c[3]===undefined||c[3]===1){bg=c;break}uncertain=true}node=node.parentElement}
  if(!fg||!bg||(fg[3]!==undefined&&fg[3]<1)||uncertain){unknown++;continue}
  const a=luminance(fg),b=luminance(bg),ratio=(Math.max(a,b)+.05)/(Math.min(a,b)+.05),size=parseFloat(s.fontSize),large=size>=24||(size>=18.66&&Number(s.fontWeight)>=700);if(ratio<(large?3:4.5))low++;
 }
 return {url:location.href,document:String(performance.timeOrigin),width:innerWidth,height:innerHeight,overflow:overflow,small_targets:small,unnamed_targets:unnamed,low_contrast:low,unknown_contrast:unknown,truncated:all.length>1200};
})()`

func auditDesign(ctx context.Context, root string, p DesignSystemParams, call fantasy.ToolCall, deps DesignServices) (fantasy.ToolResponse, error) {
	fail := designFailure
	if p.URL == "" || len(p.PrototypeSteps) > 16 || len(p.Assertions) > 16 {
		return fail(fmt.Errorf("audit needs an actual URL and at most 16 steps/assertions"))
	}
	if p.Width == 0 {
		p.Width = 390
	}
	if p.Height == 0 {
		p.Height = 844
	}
	if p.Width < 240 || p.Width > 3840 || p.Height < 240 || p.Height > 2160 {
		return fail(fmt.Errorf("invalid audit viewport"))
	}
	for i, step := range p.PrototypeSteps {
		if step.Action != "click" && step.Action != "key" || step.Action == "click" && step.Selector == "" || step.Action == "key" && step.Key == "" {
			return fail(fmt.Errorf("invalid prototype step %d", i))
		}
	}
	for _, a := range p.Assertions {
		if a.Selector == "" || len(a.Selector) > 512 || len(a.Text) > 2048 {
			return fail(fmt.Errorf("invalid design assertion"))
		}
	}
	sources, err := prepareDesignSources(root, p.SourceFiles)
	if err != nil {
		return fail(err)
	}
	sourceHashes := map[string]string{}
	for _, s := range sources {
		sourceHashes[s.info.Source] = s.info.SHA256
	}
	if err := designPermission(ctx, p, call, deps, root); err != nil {
		return fail(err)
	}
	closeTab, err := openDesignTab(ctx, deps, call.ID, p.URL)
	if err != nil {
		return fail(err)
	}
	defer closeTab()
	metrics, _ := json.Marshal(map[string]any{"width": p.Width, "height": p.Height, "deviceScaleFactor": 1, "mobile": false})
	if _, err := designBrowser(ctx, deps, call.ID+"-viewport", BrowserParams{Action: "cdp", CDPMethod: "Emulation.setDeviceMetricsOverride", CDPParams: BrowserCDPParams(metrics)}); err != nil {
		return fail(err)
	}
	if _, err := designBrowser(ctx, deps, call.ID+"-navigate", BrowserParams{Action: "navigate", URL: p.URL}); err != nil {
		return fail(err)
	}
	for i, step := range p.PrototypeSteps {
		if step.Action != "click" && step.Action != "key" || step.Action == "click" && step.Selector == "" || step.Action == "key" && step.Key == "" {
			return fail(fmt.Errorf("invalid prototype step %d", i))
		}
		if _, err := designBrowser(ctx, deps, fmt.Sprintf("%s-step-%d", call.ID, i), BrowserParams{Action: step.Action, Selector: step.Selector, Key: step.Key}); err != nil {
			return fail(err)
		}
	}
	assertionsPassed := true
	for i, a := range p.Assertions {
		if a.Selector == "" || len(a.Selector) > 512 || len(a.Text) > 2048 {
			return fail(fmt.Errorf("invalid design assertion"))
		}
		selector, _ := json.Marshal(a.Selector)
		text, _ := json.Marshal(a.Text)
		script := fmt.Sprintf("(()=>{const e=document.querySelector(%s);return !!e&&e.textContent.includes(%s)&&(!%t||document.activeElement===e)&&(!%t||e.getClientRects().length>0)})()", selector, text, a.Focused, a.Visible)
		r, err := designBrowser(ctx, deps, fmt.Sprintf("%s-assert-%d", call.ID, i), BrowserParams{Action: "eval", Script: script})
		if err != nil {
			return fail(err)
		}
		var ok bool
		if json.Unmarshal([]byte(r.Content), &ok) != nil {
			return fail(fmt.Errorf("invalid assertion response"))
		}
		assertionsPassed = assertionsPassed && ok
	}
	r, err := designBrowser(ctx, deps, call.ID+"-measure", BrowserParams{Action: "layout_audit"})
	if err != nil {
		return fail(err)
	}
	var audit designAudit
	if json.Unmarshal([]byte(r.Content), &audit) != nil || audit.URL == "" || audit.Document == "" || audit.Width != p.Width || audit.Height != p.Height {
		return fail(fmt.Errorf("invalid or mismatched DOM measurement"))
	}
	audit.AssertionsPassed = assertionsPassed
	audit.PrototypeSteps = len(p.PrototypeSteps)
	audit.AssertionCount = len(p.Assertions)
	audit.SourceHashes = sourceHashes
	audit.Passed = assertionsPassed && audit.Overflow == 0 && audit.SmallTargets == 0 && audit.UnnamedTargets == 0 && audit.LowContrast == 0 && audit.UnknownContrast == 0 && !audit.Truncated
	annotate := false
	shot, err := designBrowser(ctx, deps, call.ID+"-capture", BrowserParams{Action: "screenshot", Annotate: &annotate})
	if err != nil {
		return fail(err)
	}
	d, err := PrepareImageForDelivery(shot.Data, shot.MediaType)
	if err != nil {
		return fail(err)
	}
	if d.OriginalWidth != p.Width || d.OriginalHeight != p.Height {
		return fail(fmt.Errorf("audit screenshot viewport changed"))
	}
	// Re-check document identity after capture; a navigation cannot supply
	// geometry from one page and the image from another.
	id, err := designBrowser(ctx, deps, call.ID+"-identity", BrowserParams{Action: "eval", Script: "({url:location.href,document:String(performance.timeOrigin)})"})
	if err != nil {
		return fail(err)
	}
	var identity struct {
		URL      string `json:"url"`
		Document string `json:"document"`
	}
	if json.Unmarshal([]byte(id.Content), &identity) != nil || identity.URL != audit.URL || identity.Document != audit.Document {
		return fail(fmt.Errorf("stale_design_document: navigation occurred during audit"))
	}
	after, err := prepareDesignSources(root, p.SourceFiles)
	if err != nil {
		return fail(err)
	}
	for _, s := range after {
		if sourceHashes[s.info.Source] != s.info.SHA256 {
			return fail(fmt.Errorf("design source changed during audit"))
		}
	}
	audit.ID = engineering.Hash(GetSessionFromContext(ctx) + call.ID)[:24]
	audit.CapturedAt = time.Now().UnixMilli()
	audit.Screenshot = ".atlas/design/audits/" + audit.ID + ".png"
	audit.ScreenshotSHA256 = imageDeliveryHash(shot.Data)
	if shot.MediaType != "image/png" {
		return fail(fmt.Errorf("design renderer must return a PNG screenshot"))
	}
	if err := writeDesignArtifact(root, audit.Screenshot, shot.Data); err != nil {
		return fail(err)
	}
	if err := storeDesignRuntime(root, ".atlas/design/audits/"+audit.ID+".json", audit); err != nil {
		return fail(err)
	}
	registerDesignAudit(root, audit)
	data, _ := json.Marshal(map[string]any{"audit": audit, "scope": "measured DOM at this viewport only", "visual_inspection_required": true, "not_measured": []string{"aesthetic quality", "native mobile keyboard", "all component states"}})
	shot.Content = string(data)
	shot.IsError = !audit.Passed
	return shot, nil
}

func validDesignAuditID(id string) bool {
	return len(id) == 24 && strings.IndexFunc(id, func(r rune) bool { return r < '0' || r > '9' && r < 'a' || r > 'f' }) == -1
}
