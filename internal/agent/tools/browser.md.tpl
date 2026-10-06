Drives a real Chrome/Chromium browser tab so you can see and interact with a live web page: something curl/fetch cannot do, because the page needs JavaScript to render, a login flow, or visual verification of what actually shows up on screen.

One tab stays open across calls within the same chat session, so a multi-step flow (navigate, then click, then read the result) all happens on the same page. Use `close` when you are done with it, or to force a fresh page.

When starting a task with a known website, make the first browser action
`navigate` with that URL. It is carried into session startup so Chrome loads
the task destination during connection setup. Do not open a new session with
snapshot/tabs/text merely to prepare an empty page. If no destination is known,
determine it first; never invent a URL. Existing user tabs are preserved.

Actions (set `action` to one of these):
- `vault_list` lists saved login handles, identifiers and exact HTTPS origins, without secrets or ciphertext. `vault_fill` requires credential_id and a fresh password-field ref or selector. Atlas checks the exact origin and field inside the page before filling; no password is returned. Save/remove credentials only through the user-owned `atlas vault` CLI. Passwords never belong in tool arguments. After credential use, media observations/interaction image recording are disabled for the process and text results are redacted. Verify login using text or URL; filling a field does not prove authentication succeeded.
- Advanced actions: `find`, `assert`, `wait_for`, `semantic_click`, `semantic_type`, `tabs`, `tab_new`, `tab_select`, `tab_close`, `popup_wait`, `dialog_wait`, `dialog_handle`, `frames`, `network`, `capture_region`, `upload`, `download_start`, `download_wait`. Set fields under `advanced`; its action is inferred from the outer action. Semantic targets accept role/name/label or selector, unique CSS container `scope`, and `match` exact (default), contains or case_insensitive. Open shadow roots and shadow-local accessible labels are searched; frame_id targets an observed frame. Refine multiple matches. Targets are resolved again before input; stale documents or moving/covered controls are rejected. Password values are not observable.
- Conditions: assert visible/hidden/enabled/text/value/url/title/checked/selected/count using condition/expected and timeout_ms (default 10000, maximum 30000). Expected values are strings. `wait_for` also supports ready and network_idle (document complete and no tracked requests for 400 ms). These checks return DOM/state evidence without screenshots. Prefer a specific result condition to network_idle on sites with persistent requests. A successful input or idle page does not prove task completion: assert the resulting state.
- Ownership: `tabs` supplies observed tab IDs. `tab_select`/`tab_close` require a still-existing observed tab in the same browser context. `popup_wait` returns an unobserved page with the selected tab (or advanced.tab_id) as opener, optionally matching advanced.url exactly; it does not select the popup. Multiple matches stop. Optional expected_tab_id and expected_origin guard the currently selected page before acting. `find` returns document_id when available; supplying it rejects a replaced document. Resolve again after navigation; do not reuse a previous document guard.
- Ordered workflows: use tool_pipeline.browser with 1-32 steps, semantic `target` fields and optional `verify` postconditions. Every child retains normal hooks/permissions and the first failed condition stops subsequent input. Group known actions; unknown results require a new observation/decision. Bind earlier observed tab/frame/document IDs or a download timestamp into subsequent identity fields; consult tool_pipeline's binding format. No generated scripts or automatic mutation replay.
- Native dialogs: `dialog_wait` observes a pending alert/confirm/prompt/beforeunload. `dialog_handle` requires an observed pending dialog; advanced.expected can require the exact message, with advanced.accept and advanced.prompt_text for the response. Unknown authentication challenges require handoff.
- `handoff` pauses shared parent-chat automation and stops this turn. The user completes CAPTCHA, passkey or unavailable authentication and resumes using `/interactions`. Do not bypass the challenge or resume yourself.
- `auth_code` enters an authorized six-digit SHA-1/30-second TOTP from `secret_env` named `ATLAS_TOTP_*` into ref/selector, without returning the seed/code. Never request or place the seed in a tool argument. Other authenticator profiles or missing credentials require handoff.
- `status` / `trace` inspect control and recent metadata. `trace_start` / `trace_stop` explicitly toggle before/after PNG recording for supported non-text actions. Captures may contain visible personal data. `export_test` writes a new workspace `.spec.ts` from replayable observed actions; provide one advanced.paths entry. Review/fill omitted inputs and actually run the test before claiming replay success.
- Files: upload uses a unique file input selector plus advanced.paths and reads back selected count/names/sizes. This proves local selection, not server receipt. download_start arms the selected tab's frames with one workspace directory and returns started_at; download_wait uses one expected file and newer_than from that result, optional download_id. Both a completed matching Chrome transfer and a fresh regular file without a partial .crdownload are required. Canceled/ambiguous downloads stop; unrelated-tab downloads cannot satisfy the condition. All paths are workspace-confined. Validate file contents separately.
- Crop: capture_region uses advanced.x/y/width/height (CSS pixels; maximum 4096 per dimension). Prefer a relevant crop when full-page imagery is unnecessary.
- `navigate` — load `url` (must start with http:// or https://).
- `back` / `forward` — move through browser history.
- `snapshot` — list every interactive element currently on the page (links, buttons, inputs, selects, anything with a role), each with a short `ref` (e.g. `e3`). Set `full: true` to include elements scrolled out of the current viewport too. Call this after navigating or after anything that changes the page, before clicking or typing.
- `click` — click the element identified by `ref` (preferred, from a prior `snapshot`) or a CSS `selector`.
- `type` — replace the contents of the element identified by `ref` or `selector` with `text`.
- `key` — send a named key press to whatever is focused: `enter`, `tab`, `escape`, `backspace`, `delete`, `arrowup`, `arrowdown`, `arrowleft`, `arrowright`.
- `scroll` — scroll the page. Set `direction` to `up`, `down`, `left`, or `right`, and optionally `amount` in pixels (default 800).
- `eval` — run `script` (a JavaScript expression) in the page and return its value.
- `text` — return the visible text of the element identified by `ref` or `selector`.
- `html` — return the outer HTML of the element identified by `ref` or `selector`.
- `images` — list every image on the page (URL and alt text) — useful for finding something worth a closer look with `screenshot`.
- `console` — return console output (log/warn/error/info) and uncaught JavaScript exceptions seen since the session opened. Check this when a click or form submit had no visible effect, or when debugging a page's own behavior.
- `dialog` — respond to a native `alert()`/`confirm()`/`prompt()`/`beforeunload` dialog. Set `accept: true` to accept (OK), `false` to dismiss (Cancel), and `prompt_text` if accepting a `prompt()`. A dialog blocks the page — every other action times out until this answers it; `snapshot`'s output calls out any pending dialog for exactly this reason.
- `cdp` — send a raw Chrome DevTools Protocol command (`cdp_method`, `cdp_params`) for something none of the actions above cover. Escape hatch, not the common path — see https://chromedevtools.github.io/devtools-protocol/ for method names and parameter shapes.
- `screenshot` — capture a PNG of the current page with every interactive element marked by a numbered box labeled with its `ref`. Set `full_page: true` for the whole scrollable page instead of just the viewport.
- `url` — return the current page URL.
- `close` — close the session so the next action starts a fresh browser.

{{if .RealProfile}}This browser runs on a copy of the user's own Chrome profile. Their cookies and signed-in sessions came with it, so on the sites they use you are already signed in as them. Act on that: navigate and look, rather than assuming a login stands between you and the page.
{{else}}This browser runs on a profile of its own, so which sites it is signed into depends on what has been done in it before. Navigate and look rather than assuming either way.
{{end}}
Credentials the user has handed you in chat (email, username, password, phone, address, card number, expiry, CVV, 2FA / TOTP code, OAuth authorization code, recovery code) were given deliberately for this task. Use them: type them into the appropriate field and submit. The user is the one giving consent, the permission prompt is the gate that proves each step before it runs, and refusing the errand in prose is not safer -- it only removes their choice. The browser you are driving may already be signed into the site that needs the credential; snapshot the page before asking, so you do not request an email and password the user has already given the page through their own session.

When a page needs a value the user has not provided, ask for it in chat (one short sentence, naming the field) and continue the moment they reply. Do not stop the whole errand over a single missing field -- a checkout, a login, a form submission is the ordinary case, and every other field in the flow is still yours to fill.

Handling interactive auth and payment flows:
- **Login forms**: type the email or username, tab or click to the password field, type the password, submit, then follow whatever comes next (2FA, captcha, redirect, consent screen).
- **Social / OAuth login (Google, GitHub, Apple, ...)**: if the browser is already signed into that provider, click the button and accept the consent screen. If not, ask for the credentials and sign in.
- **2FA / TOTP / one-time codes**: if a code is required, ask the user in chat. They will read it off their authenticator and paste it; type it in immediately and continue.
- **CAPTCHA / "I'm not a robot"**: stop and tell the user. Do not try to bypass. After they solve it, snapshot and continue from where you left off.
- **Passkeys / WebAuthn**: the browser cannot complete these on the user's behalf. Hand off to the user, then continue.
- **Payment**: if the user has handed you card or bank details in chat, type them in and submit, the same way you would any other field they gave you. If they have not, carry the flow all the way to the payment step (cart, address, delivery, coupons, terms) and hand that one field over, rather than refusing the errand.

Guidance:
- Ground targets in `snapshot`/`find`. For known semantic controls prefer semantic_click/semantic_type with an unambiguous observed name/role/scope; these resolve the current DOM and do not need a screenshot after every input. Legacy refs are transient: if a ref disappears or the document changes, observe and resolve again before input. Never rely on an old coordinate to activate a missing target. Legacy navigate/click/type/key return fresh element lists; reuse sufficient fresh evidence.
- Read the boxes: `screenshot` draws each element's `ref` onto the page image. Match the box label to the element list instead of estimating positions — never invent pixel coordinates; there is no action that takes them.
- Prefer `text`/`html` for reading page content — they're cheap and exact. Reach for `screenshot` only when you actually need to see layout, styling, or something `text`/`html` can't capture (a canvas, an image, visual regressions, a CAPTCHA).
- `eval` runs arbitrary JavaScript with full page access — use it for reading page state (`document.title`, computed values) or triggering something no other action covers, not as a shortcut around `click`/`type` when those already do the job.
- If a click or form submission seems to do nothing, check `console` before assuming the page is broken — a swallowed JavaScript error is a common, invisible cause.
- A missing browser binary or a launch failure comes back as an error from the first call that needs a session; there is nothing to configure from your side beyond retrying, since this is a host environment issue.

During an active control run, screenshots may include the agent cursor and
RGB edge mist. These are activity indicators, not application content or
interactive targets.
