# CAPTCHA Solving

Atlas detects and answers page verification widgets so browser
automation can finish sign-in and form flows without a human handoff.

**No external API is required.** The model reads image-text CAPTCHAs
with its own vision; math challenges are computed offline. A remote
token farm is only needed for reCAPTCHA / hCaptcha / Turnstile response
tokens.

## Browser actions

| Action | What it does |
|---|---|
| `captcha_detect` | Reports the visible verification widget: kind, site key, question, answer field, verify button. |
| `captcha_solve` | Answers math offline. For image-text it returns the challenge **crop** so you can read it. For token widgets it reports what is needed. |
| `captcha_fill` | Types the answer into the challenge's answer box. `advanced.verify` clicks the submit button too. |

Supported kinds: `image_text`, `math`, `checkbox`, `recaptcha_v2`,
`recaptcha_v3`, `hcaptcha`, `turnstile`.

`checkbox` widgets are reported as `needs_click` -- a single `click`
clears them.

## How image-text CAPTCHAs are solved

1. `captcha_solve` captures **only the challenge element** (never the
   full page) and returns it as an image with the
   `"captcha_challenge":true` metadata mark.
2. The credential media gate lets that crop through: a CAPTCHA picture
   is a public widget, not a user secret. Ordinary screenshots stay
   blocked in credential-bearing sessions.
3. The model reads the characters from the image and calls
   `captcha_fill` with the answer.

No OCR engine, no vision API, no token farm. The model already knows
how to read a CAPTCHA.

## Typical flow (OİS-style login)

```
browser { action: navigate, url: "https://ois.example/auth" }
browser { action: set_field, ref: "e1", value: "omerfarukaydin1" }
browser { action: vault_fill, credential_id: "cr_...", ref: "e2" }
browser { action: captcha_solve }
  -> image of the "Güvenlik Resmi" challenge
  -> (model reads it)
browser { action: captcha_fill, value: "a7k2m" }
browser { action: login_check, ... }
```

## Token widgets (reCAPTCHA, hCaptcha, Turnstile)

These produce a response token through a provider API. A remote solver
is **optional** and only needed for these:

```json
{
  "tools": {
    "captcha": {
      "remote_provider": "2captcha",
      "remote_api_key": "your-key-or-$ENV_VAR"
    }
  }
}
```

Without a remote solver, `captcha_solve` reports `needs_user` and the
user completes the widget by hand.

## Security model

- **Element-scoped capture.** Only the challenge image element is
  captured -- never the full page, never surrounding form fields.
- **Challenge crops are not secrets.** They carry the
  `captcha_challenge:true` mark so the credential media gate allows them;
  every other image stays blocked in credential sessions.
- **No pixels leave the machine** unless a remote token solver is
  explicitly configured for a token widget.
- **Answers are challenge responses**, not user passwords; they go
  through the normal field path.

## Configuration

All optional. The default chain works offline with zero configuration.

```json
{
  "tools": {
    "browser": { "enabled": true },
    "captcha": {
      "enabled": true,
      "remote_provider": "2captcha",
      "remote_api_key": "$CAPTCHA_API_KEY"
    }
  }
}
```

| Field | Purpose |
|---|---|
| `enabled` | Default `true`. |
| `solvers` | Order; default `math, remote` (remote only when a key is set). |
| `remote_provider` | `2captcha`, `capsolver` or `anticaptcha`. |
| `remote_api_key` | Raw key or `$VAR` reference. Fallback: `ATLAS_CAPTCHA_API_KEY`. |
| `attempt_timeout` | Per-solver deadline (default `30s`). |
