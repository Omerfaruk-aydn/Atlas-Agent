package browser

import (
	"context"
	"errors"
	"time"
	"unicode/utf16"
)

// SecretTarget pins a password fill to the exact page target observed when
// the user authorized it.
type SecretTarget struct {
	Origin    string
	TabID     string
	Document  string
	FrameID   string
	Selector  string
	NodeToken string
}

// Secret fill statuses. Filling a field and completing a sign-in are
// separate outcomes; none of these claims the login succeeded.
const (
	SecretNotSent         = "not_sent"
	SecretFillDispatched  = "fill_dispatched"
	SecretFillUncertain   = "fill_uncertain"
	SecretTargetStale     = "target_stale"
	SecretTargetMissing   = "target_missing"
	SecretOriginMismatch  = "origin_mismatch"
	SecretTargetAmbiguous = "ambiguous_target"
)

// SecretFillResult reports what reached the page. Dispatched is true once
// text input was sent, even when the follow-up check is inconclusive.
type SecretFillResult struct {
	Status     string `json:"status"`
	Dispatched bool   `json:"dispatched"`
	Reason     string `json:"reason,omitempty"`
}

// PasswordTarget is a visible password field found for a credential card.
type PasswordTarget struct {
	Found     bool   `json:"found"`
	Count     int    `json:"count"`
	Origin    string `json:"origin"`
	TabID     string `json:"tab_id"`
	Document  string `json:"document"`
	FrameID   string `json:"frame_id,omitempty"`
	Selector  string `json:"selector,omitempty"`
	NodeToken string `json:"node_token,omitempty"`
	Reason    string `json:"reason,omitempty"`
}

// SecretFiller is implemented by drivers that can place a secret into one
// password field without exposing it to page scripts' return values.
type SecretFiller interface {
	PasswordTarget(ctx context.Context, frameID, selector string) (PasswordTarget, error)
	FillSecret(ctx context.Context, target SecretTarget, secret string) (SecretFillResult, error)
}

// LoginState is a structural observation of a sign-in page. It never
// contains field values.
type LoginState struct {
	URL              string `json:"url"`
	Origin           string `json:"origin"`
	PasswordVisible  bool   `json:"password_visible"`
	PasswordInvalid  bool   `json:"password_invalid"`
	PasswordFilled   bool   `json:"password_filled"`
	AlertVisible     bool   `json:"alert_visible"`
	OneTimeCode      bool   `json:"one_time_code"`
	Captcha          bool   `json:"captcha"`
	Passkey          bool   `json:"passkey"`
	DeviceApproval   bool   `json:"device_approval"`
	CredentialChange bool   `json:"credential_change"`
	ExpectedText     bool   `json:"expected_text"`
	AuthenticatedUI  bool   `json:"authenticated_ui"`
	PostLoginUI      bool   `json:"post_login_ui"`
}

// LoginProber observes sign-in state without dispatching input.
type LoginProber interface {
	LoginState(ctx context.Context, expectedText string) (LoginState, error)
}

var (
	_ SecretFiller = (*chromedpSession)(nil)
	_ LoginProber  = (*chromedpSession)(nil)
)

// passwordTargetScript finds exactly one visible, enabled password input.
// It runs in an isolated world: page scripts cannot read its globals.
const passwordTargetScript = `function(selector){
const roots=[document];const all=[];
for(let i=0;i<roots.length&&i<100;i++){for(const e of roots[i].querySelectorAll('*'))if(e.shadowRoot)roots.push(e.shadowRoot);
all.push(...roots[i].querySelectorAll(selector||'input[type="password"]'))}
const usable=e=>e instanceof HTMLInputElement&&e.type==="password"&&!e.disabled&&!e.readOnly&&e.getClientRects().length>0&&getComputedStyle(e).visibility==="visible"&&Number(getComputedStyle(e).opacity)>0;
const hits=all.filter(usable);
if(selector&&all.length&&!hits.length){return {count:0,reason:all.some(e=>!(e instanceof HTMLInputElement)||e.type!=="password")?"not_password_field":"target_hidden",origin:location.origin}}
let path="";
if(hits.length===1){const e=hits[0];if(e.id&&document.querySelectorAll('#'+CSS.escape(e.id)).length===1)path='#'+CSS.escape(e.id);else if(e.name&&document.querySelectorAll('input[type="password"][name="'+CSS.escape(e.name)+'"]').length===1)path='input[type="password"][name="'+CSS.escape(e.name)+'"]';else path=selector||'input[type="password"]';}
let token="";
if(hits.length===1){const targets=globalThis.__atlasSecretTargets||=(new Map());
for(const [key,node] of targets){if(!node.isConnected)targets.delete(key);else if(node===hits[0])token=key;}
if(!token){token=crypto.randomUUID();if(targets.size>=64)targets.delete(targets.keys().next().value);targets.set(token,hits[0]);}}
return {count:hits.length,origin:location.origin,selector:path,node_token:token}}`

// secretFocusScript validates the target again and focuses it, clearing a
// previous value. The secret is not an argument of this function.
const secretFocusScript = `function(origin,selector,token){
if(location.origin!==origin)return "origin_mismatch";
const roots=[document];const all=[];
for(let i=0;i<roots.length&&i<100;i++){for(const e of roots[i].querySelectorAll('*'))if(e.shadowRoot)roots.push(e.shadowRoot);all.push(...roots[i].querySelectorAll(selector))}
const usable=e=>e instanceof HTMLInputElement&&e.type==="password"&&!e.disabled&&!e.readOnly&&e.getClientRects().length>0&&getComputedStyle(e).visibility==="visible"&&Number(getComputedStyle(e).opacity)>0;
const hits=all.filter(usable);
if(hits.length===0)return all.length?"not_password_field":"target_missing";
if(hits.length>1)return "ambiguous_target";
const e=hits[0];
if(!token||globalThis.__atlasSecretTargets?.get(token)!==e||!e.isConnected)return "target_stale";
e.focus();
if(!e.isConnected||globalThis.__atlasSecretTargets?.get(token)!==e)return "target_stale";
const root=e.getRootNode();
if(document.activeElement!==e&&root.activeElement!==e)return "focus_failed";
globalThis.__atlasSecretTarget=new WeakRef(e);return "focused"}`

// Secret insertion resolves the pinned element again inside one script task.
// It never sends keystrokes to whichever unrelated element holds focus.
const secretInsertScript = `function(origin,token,secret){
const e=globalThis.__atlasSecretTargets?.get(token);
if(location.origin!==origin||!e||!e.isConnected||e.type!=="password"||e.disabled||e.readOnly||!e.getClientRects().length||getComputedStyle(e).visibility!=="visible"||Number(getComputedStyle(e).opacity)<=0)return "target_stale";
const set=Object.getOwnPropertyDescriptor(HTMLInputElement.prototype,"value").set;
globalThis.__atlasSecretTargets.delete(token);
set.call(e,secret);e.dispatchEvent(new Event("input",{bubbles:true}));e.dispatchEvent(new Event("change",{bubbles:true}));
return "sent"}`

// secretVerifyScript confirms the focused field received the expected
// number of characters. It returns a boolean, never the value.
const secretVerifyScript = `function(n){const e=globalThis.__atlasSecretTarget?.deref();globalThis.__atlasSecretTarget=null;return !!e&&e.isConnected&&e.type==="password"&&e.value.length===n}`

// loginStateScript classifies the page structure. Text checks are generic
// signals for verification steps, not site-specific selectors.
//
//nolint:misspell // Turkish page text is intentional.
const loginStateScript = `function(expected){
const vis=e=>e.getClientRects().length>0&&getComputedStyle(e).visibility==="visible";
const pw=[...document.querySelectorAll('input[type="password"]')].filter(vis);
const text=(document.body&&document.body.innerText||"").slice(0,20000).toLowerCase();
const has=(...w)=>w.some(x=>text.includes(x));
const otp=[...document.querySelectorAll('input[autocomplete="one-time-code"],input[name*="otp" i],input[name*="totp" i],input[id*="otp" i],input[name*="code" i][inputmode="numeric"],input[type="tel"][maxlength="6"],input[inputmode="numeric"][maxlength="6"]')].some(vis);
const captcha=[...document.querySelectorAll('iframe[src*="recaptcha"],iframe[src*="hcaptcha"],iframe[src*="turnstile"],iframe[src*="captcha"],.g-recaptcha,.h-captcha,#captcha,img[src*="captcha" i]')].some(vis);
const alerts=[...document.querySelectorAll('[role="alert"],[aria-live="assertive"]')].filter(e=>vis(e)&&e.innerText.trim().length>0);
const signedIn=[...document.querySelectorAll('a,button,[role="button"]')].filter(vis).some(e=>/^(sign out|log out|logout|çıkış yap|oturumu kapat|abmelden|déconnexion|cerrar sesión)$/i.test((e.innerText||e.getAttribute('aria-label')||'').trim()));
const continueBtn=[...document.querySelectorAll('a,button,[role="button"],input[type="submit"],input[type="button"]')].filter(vis).some(e=>/^(devam et|devam|continue|proceed|next|ileri|tamam|ok|onayla|onayla ve devam et|start|başla|dashboard|panel|ana sayfa)$/i.test((e.innerText||e.value||e.getAttribute('aria-label')||'').trim()));
const postLoginText=has("hoş geldiniz","hos geldiniz","welcome","adres doğrulama","adres dogrulama","address verification","mezun takip","öğrenci bilgi","ogrenci bilgi","dashboard","panel","profil","my account","hesabım","hesabim");
return {url:location.href.split(/[?#]/)[0],origin:location.origin,
password_visible:pw.length>0,
password_invalid:pw.some(e=>e.getAttribute("aria-invalid")==="true"||e.matches(":invalid")&&e.value!==""),
password_filled:pw.some(e=>e.value.length>0),
alert_visible:alerts.length>0,
one_time_code:otp||has("verification code","2-step verification","two-factor","doğrulama kodu","2 adımlı doğrulama","authenticator app"),
captcha:captcha||has("i'm not a robot","ben robot değilim","captcha"),
passkey:has("passkey","geçiş anahtarı","security key","güvenlik anahtarı"),
device_approval:has("check your phone","tap yes on","approve sign-in","telefonunuzu kontrol edin","telefonunu kontrol et","oturum açmayı onayla"),
credential_change:[...document.querySelectorAll('input[autocomplete="new-password"]')].some(vis),
authenticated_ui:signedIn,
post_login_ui:continueBtn||postLoginText,
expected_text:!!expected&&(text+'\n'+document.title.toLowerCase()).includes(expected.toLowerCase())}}`

func (s *chromedpSession) boundContext(parent context.Context, d time.Duration) (context.Context, func()) {
	ctx, cancel := context.WithTimeout(s.currentContext(), d)
	stop := context.AfterFunc(parent, cancel)
	return ctx, func() { stop(); cancel() }
}

// isolatedWorld returns an execution context in frameID (the top frame when
// empty) that page scripts cannot observe.
func (s *chromedpSession) isolatedWorld(ctx context.Context, frameID string) (float64, error) {
	if frameID == "" {
		tree, err := s.command(ctx, "Page.getFrameTree", map[string]any{}, false)
		if err != nil {
			return 0, err
		}
		frame := browserFrameInfo(tree, "")
		if frame == nil {
			return 0, errors.New("target_missing: no top frame")
		}
		frameID, _ = frame["id"].(string)
	}
	v, err := s.command(ctx, "Page.createIsolatedWorld", map[string]any{"frameId": frameID, "worldName": "atlas-secret"}, false)
	if err != nil {
		return 0, err
	}
	world, _ := v["executionContextId"].(float64)
	if world == 0 {
		return 0, errors.New("target_missing: frame has no execution context")
	}
	return world, nil
}

func (s *chromedpSession) callIn(ctx context.Context, world float64, fn string, args ...any) (any, error) {
	arguments := make([]map[string]any, 0, len(args))
	for _, a := range args {
		arguments = append(arguments, map[string]any{"value": a})
	}
	v, err := s.command(ctx, "Runtime.callFunctionOn", map[string]any{"functionDeclaration": fn, "executionContextId": world, "arguments": arguments, "returnByValue": true, "awaitPromise": true}, false)
	if err != nil {
		return nil, err
	}
	if v["exceptionDetails"] != nil {
		return nil, errors.New("target_missing: page evaluation failed")
	}
	r, _ := v["result"].(map[string]any)
	return r["value"], nil
}

// PasswordTarget finds the single visible password field for a credential
// card and records the tab, document and frame it lives in.
func (s *chromedpSession) PasswordTarget(parent context.Context, frameID, selector string) (PasswordTarget, error) {
	ctx, done := s.boundContext(parent, 5*time.Second)
	defer done()
	id := s.ExecutionIdentity(ctx)
	world, err := s.isolatedWorld(ctx, frameID)
	if err != nil {
		return PasswordTarget{}, err
	}
	value, err := s.callIn(ctx, world, passwordTargetScript, selector)
	if err != nil {
		return PasswordTarget{}, err
	}
	m, _ := value.(map[string]any)
	count, _ := m["count"].(float64)
	out := PasswordTarget{Count: int(count), TabID: id.TabID, Document: id.Document, FrameID: frameID}
	out.Origin, _ = m["origin"].(string)
	out.Selector, _ = m["selector"].(string)
	out.NodeToken, _ = m["node_token"].(string)
	out.Reason, _ = m["reason"].(string)
	out.Found = out.Count == 1 && out.Selector != ""
	if !out.Found && out.Reason == "" {
		out.Reason = map[bool]string{true: SecretTargetAmbiguous, false: SecretTargetMissing}[out.Count > 1]
	}
	return out, nil
}

// FillSecret types the secret into the bound field. Nothing is sent when
// the tab, document, frame, origin or field no longer match. The value is
// never read back; only its length is compared.
func (s *chromedpSession) FillSecret(parent context.Context, target SecretTarget, secret string) (SecretFillResult, error) {
	ctx, done := s.boundContext(parent, 10*time.Second)
	defer done()
	stale := func(reason string) (SecretFillResult, error) {
		return SecretFillResult{Status: SecretTargetStale, Reason: reason}, errors.New(SecretTargetStale + ": " + reason + "; nothing was sent")
	}
	if s.disconnected() || s.currentTabGone() {
		return stale("the browser tab is gone")
	}
	id := s.ExecutionIdentity(ctx)
	if target.TabID != "" && id.TabID != target.TabID {
		return stale("another tab is selected")
	}
	if target.Document != "" && id.Document != target.Document {
		return stale("the page navigated to a new document")
	}
	world, err := s.isolatedWorld(ctx, target.FrameID)
	if err != nil {
		return stale("the target frame is gone")
	}
	value, err := s.callIn(ctx, world, secretFocusScript, target.Origin, target.Selector, target.NodeToken)
	if err != nil {
		return SecretFillResult{Status: SecretNotSent, Reason: "target check failed"}, errors.New("target_missing: the password field could not be checked; nothing was sent")
	}
	if status, _ := value.(string); status != "focused" {
		switch status {
		case SecretTargetStale:
			return stale("the authorized password element was replaced")
		case SecretOriginMismatch:
			return SecretFillResult{Status: SecretOriginMismatch}, errors.New("scope_mismatch: the frame's origin differs from the authorized origin; nothing was sent")
		case SecretTargetAmbiguous:
			return SecretFillResult{Status: SecretTargetAmbiguous}, errors.New("ambiguous_target: more than one password field matches; nothing was sent")
		default:
			return SecretFillResult{Status: SecretTargetMissing, Reason: status}, errors.New("target_missing: " + status + "; nothing was sent")
		}
	}
	// The document may have changed while the field was focused.
	if again := s.ExecutionIdentity(ctx); again.TabID != id.TabID || again.Document != id.Document {
		return stale("the page changed during the fill")
	}
	inserted, err := s.callIn(ctx, world, secretInsertScript, target.Origin, target.NodeToken, secret)
	if err != nil {
		// The input event may or may not have reached the page.
		return SecretFillResult{Status: SecretFillUncertain, Dispatched: true, Reason: "input dispatch did not confirm"}, nil
	}
	if inserted != "sent" {
		return stale("the authorized password element was replaced")
	}
	ok, err := s.callIn(ctx, world, secretVerifyScript, len(utf16.Encode([]rune(secret))))
	if err != nil || ok != true {
		return SecretFillResult{Status: SecretFillUncertain, Dispatched: true, Reason: "the field did not confirm the expected length"}, nil
	}
	return SecretFillResult{Status: SecretFillDispatched, Dispatched: true}, nil
}

// LoginState observes the current top document's sign-in structure.
func (s *chromedpSession) LoginState(parent context.Context, expected string) (LoginState, error) {
	ctx, done := s.boundContext(parent, 5*time.Second)
	defer done()
	world, err := s.isolatedWorld(ctx, "")
	if err != nil {
		return LoginState{}, err
	}
	value, err := s.callIn(ctx, world, loginStateScript, expected)
	if err != nil {
		return LoginState{}, err
	}
	m, _ := value.(map[string]any)
	b := func(k string) bool { v, _ := m[k].(bool); return v }
	out := LoginState{
		PasswordVisible: b("password_visible"), PasswordInvalid: b("password_invalid"), PasswordFilled: b("password_filled"),
		AlertVisible: b("alert_visible"), OneTimeCode: b("one_time_code"), Captcha: b("captcha"), Passkey: b("passkey"),
		DeviceApproval: b("device_approval"), CredentialChange: b("credential_change"), ExpectedText: b("expected_text"),
		AuthenticatedUI: b("authenticated_ui"), PostLoginUI: b("post_login_ui"),
	}
	out.URL, _ = m["url"].(string)
	out.Origin, _ = m["origin"].(string)
	return out, nil
}
