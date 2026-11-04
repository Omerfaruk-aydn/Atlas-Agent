# Grok subscription login

`atlas-agent login grok` (also `grok-web`, `supergrok`) signs in to a
SuperGrok or X Premium subscription instead of an `XAI_API_KEY`.

- Flow: xAI's OAuth device-code grant at `https://auth.x.ai/oauth2`, with the
  shared public client xAI offers to third-party tools. The consent page may
  say "Grok Build". Scope: `openid profile email offline_access
  grok-cli:access api:access`.
- Calls: the access token is a bearer credential for `https://api.x.ai/v1`
  through the OpenAI Responses adapter, provider id `grok-web`. Refresh is
  automatic; the refresh token is kept when xAI does not rotate it.
- The same sign-in is in the terminal UI (pick a Grok model), the web
  settings (Models page, "Accounts"), the Dots model sheet and the desktop app.
- xAI decides which accounts get API tokens. An account it refuses answers
  model calls with HTTP 403; an API key on the `xai` provider still works.
- Sign out: `atlas-agent logout grok`.
