   never as a raw URL prefix. One-time codes, security keys and approvals on
   another device are always yours to complete.

**Temporary by default.** The usable value lives only in Atlas' memory, is used at
most once, and is erased when used, after 2 minutes unused (the card itself
expires after 5 minutes), when the card is canceled, when the task is
stopped, when the phone is unpaired, and when Atlas exits. It is never placed
in model requests, tool arguments or results, the database, prompt history,
URLs, browser storage on the phone, notifications or logs. Go cannot promise
that freed memory is overwritten immediately. Timers release the owned
credential buffer without waiting for another request. Redaction retains keyed
fingerprints, not a plaintext copy, to protect later observations. Explicitly
saved vault entries follow their separate encrypted-storage lifecycle.

**Saving is opt-in.** The card shows "Also save it in this computer's
protected vault for this site and account" only for an HTTPS site with a
known account name.
It is off by default. A saved credential is bound to that origin and account
and is used later through `vault_fill` with its vault entry name.

The outcome the agent sees is one of: not sent, sent (dispatched), uncertain
(the browser may have received it; it is never re-sent automatically),
failed, waiting for your verification, or verified by `login_check`.

If you typed a password into chat anyway and a tool refused it, that value is
removed from every later model request and from the stored tool call. It is
still in your own chat message; delete the chat if that matters.

## YOLO (bypass) and other permission modes

There is one permission system. Credential actions go through it like every
other tool call:

| Mode | Credential card | Fill with a handle you provided | Other password typing |
| --- | --- | --- | --- |
| Ask (manual) | opens without a prompt | no extra prompt | refused |
| Auto-accept edits | opens without a prompt | no extra prompt | refused |
| Plan (read-only) | refused (no card opens) | refused, the value is not consumed | refused |
| Bypass / YOLO | opens without a prompt | no extra prompt | refused |

Typing your password into the card is the authorization for that one
sign-in. It is scoped to the task (session), the request, the origin, the
account and the handle, and expires as above; it does not authorize any
other site, account or later sign-in. YOLO does not let the agent invent,
reuse or move a password between sites, and it does not answer the card for
you. A handle from another task, an expired or used handle, or one bound to a
different origin is refused before anything is typed.

## Returning control to Atlas

When the browser or desktop is handed to you (for example for a CAPTCHA, a
one-time code or a device approval), the task pauses. On the phone the task
shows **Return control to Atlas**. Press it after you finished the step.

- It uses the same workflow control as the computer (`interaction_resume`),
  checks that this is the same task and that it is still paused, and refuses
  a stale state, so a double tap or two phones cannot start two runs.
- A task that was stopped stays stopped; returning control does not revive
  it.
- Atlas observes the page again before doing anything and does not assume
  your step succeeded.
- Typing `/resume-use` in the phone chat does the same thing. It is never
  sent to the model as text and does not run any command on the computer.

## Live screen (view only)

With previews allowed for the phone (on the computer: `/mobile`, select the
phone, press `p`), open a task and tap the screen button in the top bar, or
**Live screen** in the `⋯` menu. **Chat / Screen** switches views; the stream
pauses when the screen view is hidden or the phone locks, and stops with
**Stop**.

- The phone can zoom (−, +, fit, double tap) and expand the view. It cannot
  click, type or scroll on the computer: there is no remote input.
- Frames travel as JPEG on one authenticated HTTPS stream (`GET
  /api/screen/stream`, the phone's cookie plus a CSRF header). One capture
  loop serves every phone; each phone keeps only the newest frame, so a slow
  phone or network skips frames instead of falling behind. Frames are never
  written to disk or cached.
- The panel shows measured numbers: frames per second actually drawn on the
  phone, the computer's capture and sent rate, data rate and frame age. A
  frame older than two seconds dims the picture.
- The target is 24 frames per second. On the development computer (Windows
  10, 1920×1080, GDI capture) 60-second runs measured 23.4–24.1 frames per
  second sent at 960×540, capture about 36–39 ms per frame, JPEG encoding
  about 9 ms, roughly 1.1 MB/s, and about 77% of one CPU core (of 12) for
  the gateway. Resolution steps down (1600, 1280, 960, 720 pixels wide) when
  capture or encoding cannot keep up, and back up with headroom. Slower
  computers, more monitors or a remote connection (Funnel, relay) give lower
  numbers; the panel shows what you actually get.
- A successfully captured black frame keeps streaming with an informational
  notice; black pixels alone do not prove the desktop is locked. Actual capture
  failures are reported separately. Monitor,
  resolution and orientation changes apply to the next frame.
- Turning previews off or unpairing ends a running stream within about a
  second. At most four phones can watch at once.

## Adding the panel to the Home Screen

**iPhone (Safari).** iOS has no install button for websites. Open the panel
in Safari (not inside another app's browser), tap **Share**, then **Add to
Home Screen**. The Home Screen app has separate storage from Safari, so it
starts unpaired: in Safari, open Settings › Home Screen and tap **Connect
the Home Screen app**, then type the eight-character code in the Home Screen
app within 3 minutes. The code works once. If you open the panel inside another app
(Instagram, WhatsApp, Gmail…), the panel asks you to open it in Safari first.

**Android (Chrome).** The **Install Atlas** button appears only when Chrome offers
installation for this site; otherwise use Chrome's menu → **Add to Home
screen** / **Install app**. Chrome shares cookies with the installed app, so
no code is needed.

The install manifest starts the app at `/` and contains no pairing nonce. The
service worker caches only the panel's own files; API responses, chat
content, credential cards and screen frames are never cached. A new panel
version shows "A new version is ready." with **Reload**.

## Not verified on physical devices

Verified with real Chrome and Playwright WebKit (iPhone 13 profile) against
the real gateway over TLS, and with a real Chrome browser on a synthetic
sign-in page. Not verified: a physical iPhone or Android phone, Safari's
actual Add to Home Screen flow and Home Screen storage behavior, Android's
real install prompt, live streaming over Tailscale Funnel or the relay, and
sign-in to any real account.

## Protected browser observations after sign-in

Using a credential no longer disables every browser screenshot. The native
browser screenshot path masks editable controls, registered plaintext secrets
and opaque embedded media in the image, without editing the page or actual
form values. Cross-frame/opaque contents may therefore be unavailable visually;
ordinary inspectable page text and layout remain visible. Full-page and target
annotations are supported. Protected screenshots do not grant new pixel
coordinates; use current semantic refs. The pipeline relays the selected image's
redaction attestation instead of losing it. Unprotected media remains blocked.

Capture compares the document and layout before and after the shot. A changing
page returns an explicit stale-layout error with no image, rather than sending
a possibly misaligned mask. Native dialogs disappear from observations when
Chrome reports them closed or the top-level page commits a new document.
A failed dialog-handling command does not discard a still-pending dialog.
