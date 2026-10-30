# Kimi Code subscription login

Sign in with a Kimi Code membership instead of an API key. This uses the same
OAuth device-code flow as the official Kimi Code CLI.

```sh
atlas-agent login kimi        # aliases: kimi-code, kimi-plan, kimi-membership
atlas-agent login -f kimi     # re-authenticate
atlas-agent logout kimi
```

The command copies the one-time code to the clipboard, opens the Kimi
verification page, and waits for approval. The provider is `kimi-code`
("Kimi Code (subscription)"). Picking one of its models in the TUI model
picker opens the same device-code dialog instead of an API-key prompt.

## Kimi subscription vs. API key

| Command | Provider | Auth |
| --- | --- | --- |
| `login kimi` | `kimi-code` | Membership OAuth, no API key |
| `login kimi-coding` | `kimi-coding` | API key (`KIMI_CODING_API_KEY`) |
| `login kimi-coding-global` | `kimi-coding-global` | API key |

`login --list` shows both. The API-key providers are unchanged.

## How it works

- Authorization host: `https://auth.kimi.com` (override with
  `KIMI_CODE_OAUTH_HOST` or `KIMI_OAUTH_HOST`).
- Device flow: `POST /api/oauth/device_authorization`, then polling
  `POST /api/oauth/token` with the device-code grant. No scope is sent.
- Refresh uses the `refresh_token` grant; 429/5xx are retried, while a
  rejected token (401/403) asks you to log in again.
- Inference goes to `https://api.kimi.com/coding/v1` (OpenAI-compatible) with
  the OAuth access token as the bearer credential. No key is minted.
- Requests carry the `X-Msh-*` identification headers (platform, version,
  device name/model/OS, and a random device id stored as `kimi_device_id`
  in the Atlas data directory).

Package: `internal/oauth/kimi`.

## Status

The flow follows the public Kimi Code CLI source. It has been exercised
against local test servers only, not against the live Kimi service.
