
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
