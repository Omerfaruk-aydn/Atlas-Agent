# Phone panel browser checks

`iphone.test.cjs` runs the actual panel HTML, CSS and JavaScript in
Playwright WebKit with the iPhone 13 device profile. API responses and event
streams are controlled fixtures; the Go mobile tests cover the real gateway,
TLS, cookies, pairing and authorization separately. Browser emulation is
not a claim of a physical iPhone or Home Screen installation test.

`android.test.cjs` uses Chromium and the Pixel 7 device profile to check
browser-only access, narrow layouts, network return, draft preservation,
session expiry and unavailable storage. No Android package or APK is used.

With Node.js, Playwright and its WebKit browser installed:

```powershell
node --test internal/mobile/web_tests/iphone.test.cjs
node --test internal/mobile/web_tests/android.test.cjs
```

The `playwright` package must resolve from the test directory, an ancestor
`node_modules`, or `NODE_PATH`. These checks require no production service,
account, phone or private data. The tests use a loopback HTTP fixture only;
the production gateway continues to require HTTPS.

Install Chromium with `npx playwright install chromium`, or set
`ATLAS_CHROMIUM_CHANNEL=chrome` to use an installed Chrome build. These are
developer browser checks, not dependencies for phone users.

`gateway.test.cjs` drives the real gateway instead of fixtures: it starts
`TestBrowserAcceptanceServer` (TLS, cookies, CSRF, pairing approved by a
simulated computer, a synthetic screen source) and checks the credential
card, returning control, the live screen stream and revocation. It needs Go
on `PATH` and takes about a minute; set `ATLAS_ENGINE=webkit` for WebKit:

```powershell
node --test internal/mobile/web_tests/gateway.test.cjs
```
