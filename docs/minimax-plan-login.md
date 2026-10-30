# MiniMax Token Plan subscription login

Sign in with a MiniMax Token Plan (coding plan) subscription instead of an API
key. This uses the same OAuth device-code flow (RFC 8628 with PKCE) as the
official `mmx` CLI.

```sh
atlas-agent login minimax                  # global account; aliases: minimax-plan, minimax-global
atlas-agent login minimax --region cn      # China account; aliases: minimax-cn, minimaxi, minimax-plan-cn
atlas-agent login -f minimax               # re-authenticate
atlas-agent logout minimax                 # or: logout minimax-cn
```

The command copies the one-time code to the clipboard, opens the MiniMax
verification page, and waits for approval. Picking a model of the
`minimax-plan` / `minimax-plan-cn` provider in the TUI model picker opens the
same device-code dialog instead of an API-key prompt.

## Subscription vs. API key

| Command | Provider | Auth |
| --- | --- | --- |
| `login minimax` | `minimax-plan` | Subscription OAuth, global, no API key |
| `login minimax --region cn` / `login minimax-cn` | `minimax-plan-cn` | Subscription OAuth, China, no API key |
| `login minimax-coding` (alias `minimax-token-plan`) | `minimax-coding` | Token Plan API key (`MINIMAX_CODING_API_KEY`) |
| `login minimax-api` | `minimax` | Generic pay-per-token API key (`MINIMAX_API_KEY`) |
| `login minimax-china`, `minimax-m-plan` | same ids | API key |

The bare name `minimax` now means the subscription login. The generic
pay-per-token provider, which used to answer to `login minimax`, is reachable
as `login minimax-api`. All API-key providers are otherwise unchanged and
`minimax-token-plan` still resolves to the API-key provider `minimax-coding`.
`login --list` states this.

## Why two provider ids

MiniMax global and China are separate account systems with different account
and model hosts, so a token is only valid in one region. Atlas models that as
two catalog providers (as it does for `kimi-coding` / `kimi-coding-global` and
the Alibaba/Xiaomi plans) rather than a region field stored with the token: the
provider id then alone selects the refresh host and the inference endpoint, and
both can be logged in at once.

## How it works

- Account host: `https://account.minimax.io` (global) or
  `https://account.minimaxi.com` (China). Override both with
  `MINIMAX_OAUTH_HOST` (for tests or gateways).
- Client id `659cf4c1-615c-45f6-a5f6-4bf15eb476e5`, scope
  `openid profile coding_plan`.
- Device flow: `POST /oauth2/device/code` with `code_challenge` (S256 of a
  random 32-byte base64url verifier), then polling `POST /oauth2/token` with
  the device-code grant and `code_verifier`. `authorization_pending` and
  `slow_down` are honored; `access_denied` and `expired_token` end the login.
- Refresh uses the `refresh_token` grant; 429/5xx are retried, while a
  rejected token asks you to log in again. Atlas refreshes using its standard
  buffer (a tenth of the lifetime, at least 30 seconds); `mmx` uses five
  minutes.
- Inference goes to `https://api.minimax.io/anthropic` (global) or
  `https://api.minimax.cn/anthropic` (China), Anthropic Messages format, with
  the OAuth access token in the `x-api-key` header, as `mmx` does for chat.
  Note the API-key MiniMax providers send their key as `Authorization:
  Bearer`; the subscription providers deliberately do not.
- Models mirror `minimax-coding`.

## Known risk: browser sign-in may return 404

A third-party report says the browser flow returns 404 for some MiniMax
accounts. Atlas surfaces this in the error:

> minimax device code request failed: status 404. MiniMax browser sign-in is
> not available for every account (some return 404); sign in with an API key
> instead: atlas-agent login minimax-coding

In that case use `atlas-agent login minimax-coding`. `mmx` treats OAuth and API
key credentials as mutually exclusive; Atlas keeps them as separate providers,
so both can coexist.

## Status

The flow follows the public `mmx-cli` source (v1.0.28). It has been exercised
against local test servers only, not against the live MiniMax service.

Package: `internal/oauth/minimax`.
