# Phone voice assistant

Atlas can talk to you on the phone. You call its number and ask for
something ("summarize my last email"), or Atlas calls you: a scheduled
morning call, a site that went down, anything a routine or session decides
is worth a call through the `call_owner` tool.

It is off by default and does nothing until you configure it and start
`atlas-agent phone serve`.

## How it fits together

```
caller ⇄ Twilio (PSTN) ⇄ media stream ⇄ VoiceGateway ⇄ ElevenLabs STT/TTS
                                              │
                                              └─ Atlas agent turn (same
                                                 tools, permissions, MCP)
```

- **Transport** (`internal/voice/transport/twilio`): signed webhooks, the
  bidirectional Media Stream (mu-law 8 kHz), outbound calls, SMS/WhatsApp
  written fallback.
- **Speech provider** (`internal/voice/providers/elevenlabs`): Scribe
  Realtime for speech-to-text, the streaming text-to-speech endpoint for
  replies. ElevenLabs only converts speech; it is not the assistant.
- **Gateway** (`internal/voice`): call state machine, turn handling,
  barge-in, spoken confirmations, idempotency, budgets, cleanup.
- **Agent**: every final utterance becomes an ordinary agent turn in a
  session created for the call.

Only final transcripts start a turn. Speaking while Atlas talks stops its
audio at once; a new utterance cancels the turn in flight. Hanging up
cancels the turn, its tools, recognition, synthesis and the stream.

## Setup

1. **Twilio**: buy a voice-capable number. Note the account SID and auth
   token.
2. **ElevenLabs**: create an API key and choose a voice id.
3. **Public HTTPS**: Twilio must reach `listen.public_addr` (default
   `127.0.0.1:8787`) over HTTPS. Put it behind a TLS reverse proxy or a
   tunnel that maps the public origin to that address with no path prefix.
   The public URL goes in `twilio.public_base_url`.
4. **Twilio number webhook**: set "A call comes in" to
   `POST https://<public_base_url>/voice/twilio/incoming`.
5. **Credentials** go into environment variables, never into the config:

   ```bash
   export ELEVENLABS_API_KEY=...
   export TWILIO_ACCOUNT_SID=AC...
   export TWILIO_AUTH_TOKEN=...
   export ATLAS_OWNER_PHONE=+905551112233
   ```

6. **Configure** `tools.voice_calls` in the global `atlas.json` (the
   `atlasrc` format has no builtin for this block yet):

   ```json
   {
     "tools": {
       "voice_calls": {
         "enabled": true,
         "language": "tr-TR",
         "owner": { "phone_number": "$ATLAS_OWNER_PHONE", "timezone": "Europe/Istanbul" },
         "elevenlabs": {
           "api_key": "$ELEVENLABS_API_KEY",
           "voice_id": "<voice id>",
           "keyterms": ["Atlas", "Twilio"]
         },
         "twilio": {
           "account_sid": "$TWILIO_ACCOUNT_SID",
           "auth_token": "$TWILIO_AUTH_TOKEN",
           "phone_number": "+15550001111",
           "public_base_url": "https://voice.example.com"
         },
         "outbound": { "fallback": ["sms"] },
         "tool_policy": { "allow": ["mcp_gmail_search*", "mcp_gmail_read*", "mcp_calendar_list*"] }
       }
     }
   }
   ```

7. Check it without contacting anyone, then start the service:

   ```bash
   atlas-agent phone check
   atlas-agent phone serve
   ```

8. Optionally place one real, billed test call:

   ```bash
   atlas-agent phone test-call --yes
   ```

`/integrations` shows the assistant as **Phone voice assistant**; its check
validates the configuration and asks the running service for status. It
never places a call.

## WhatsApp instead of Twilio

Set `"transport": "whatsapp"` to take and place calls over the WhatsApp
Business Calling API. Calls to you cost only Meta's per-minute connection
fee (see the [calling pricing page][wa-pricing]); calls you place to the
assistant are free for the business. Audio travels over WebRTC as G.711
mu-law, so the gateway, safety rules and tools are the same as with
Twilio.

What you need:

1. A Meta developer app with the WhatsApp product, and a business number on
   the **Cloud API** (not the WhatsApp Business app). Calling must be
   enabled for the number. Meta's free test number works for a first trial.
2. Business-initiated calls (the assistant calling you) are not offered for
   business numbers with a US, Canada, Egypt, Vietnam or Nigeria country
   code, and each user must have granted the number permission to call
   them. Calls you place to the assistant need neither.
3. A public HTTPS address that reaches `listen.public_addr` (a tunnel is
   fine). In the app dashboard set the webhook callback URL to
   `https://<public host>/voice/whatsapp/webhook`, enter your verify token,
   and subscribe to the `calls` field.
4. Credentials in environment variables, never in the config:

   ```bash
   export WHATSAPP_ACCESS_TOKEN=...
   export WHATSAPP_APP_SECRET=...
   export WHATSAPP_VERIFY_TOKEN=...   # any string you also give Meta
   ```

5. Configure (the `twilio` block is not needed):

   ```json
   {
     "tools": {
       "voice_calls": {
         "enabled": true,
         "transport": "whatsapp",
         "owner": { "phone_number": "$ATLAS_OWNER_PHONE" },
         "elevenlabs": { "api_key": "$ELEVENLABS_API_KEY", "voice_id": "<voice id>" },
         "whatsapp": {
           "access_token": "$WHATSAPP_ACCESS_TOKEN",
           "app_secret": "$WHATSAPP_APP_SECRET",
           "verify_token": "$WHATSAPP_VERIFY_TOKEN",
           "phone_number_id": "<Cloud API phone number id>"
         },
         "outbound": { "fallback": ["telegram"] }
       }
     }
   }
   ```

The owner number is the number your WhatsApp account uses, in E.164 form.
Inbound calls are accepted only from the owner and `owner.allowed_callers`;
anyone else is rejected before the call is answered. Webhooks without a
valid `X-Hub-Signature-256` are refused. The SMS and Twilio WhatsApp
fallbacks need the Twilio transport; use `telegram` here.

The media path needs UDP. `whatsapp.stun_servers` (default: Google's public
STUN server) lets the media cross NAT, which shows this machine's public
address to that server and to WhatsApp. Behind a strict firewall or
symmetric NAT the call may connect without audio; a TURN relay is not
supported.

[wa-pricing]: https://developers.facebook.com/documentation/business-messaging/whatsapp/calling/pricing

## Safety rules

- **One person.** Outbound calls only ever go to `owner.phone_number`. The
  agent's `call_owner` tool has no destination parameter. Inbound calls are
  accepted only from the owner and `owner.allowed_callers`; anyone else is
  rejected before the call is answered.
- **Permissions are never bypassed.** Voice turns run without permission
  prompts (nobody is at the terminal). On top of that:
  - read-only tools, and tools you list in `tool_policy.allow`, run through
    the normal permission checks;
  - local execution, file edits, desktop control, `call_owner` and mail
    sending are blocked on calls (`tool_policy.block` adds more; sending
    mail can be re-enabled with `allow_email_send`, still behind
    confirmation);
  - everything else (calendar writes, drafts, MCP writes) is read back to
    the caller with its date, time, duration, timezone and attendees, and
    runs only after a clear "evet"/"yes". Any other answer, silence, a new
    request or hanging up means no. A yes approves exactly that one call.
  - Plan mode stays read-only even after a spoken yes.
- **Untrusted content.** The turn prompt marks email, calendar, web and
  tool text as data, never instructions.
- **Secrets** are only `$ENV` references in the config; resolved values are
  redacted in every string, JSON and log form. Phone numbers are masked in
  logs and records. Raw audio is never stored (`record_calls` must stay
  false); transcripts are stored locally unless `store_transcripts` is
  false.
- **Costs are bounded.** `max_call_seconds` (180), `max_daily_minutes`
  (30), `max_concurrent_calls` (1), `silence_timeout_seconds` (20),
  `max_tool_calls_per_turn` (8), and for outbound calls `max_per_hour` (3)
  and `min_interval_seconds` (600). An urgent call skips the minimum gap,
  never the hourly cap.
- **Webhooks** must carry a valid `X-Twilio-Signature`; bodies are capped
  and requests rate limited. Media streams are authenticated by a one-time
  token handed out in the signed webhook's response.
- **Internal API** (`listen.control_addr`) listens on loopback only and
  needs the bearer token from the private `voice/control.json` in the
  global data directory.

## Calling the owner

`call_owner(reason, priority?, preferred_channel?, event_id?)`:

- The same `event_id` never rings twice. In routines it defaults to
  `routine:<id>:<date>`, so a daily routine calls at most once a day.
- An unanswered, rejected or rate-limited call sends `reason` on the first
  working written channel in `outbound.fallback` (`sms`, `whatsapp` through
  Twilio, `telegram`).
- `outbound.allow_retry` permits one controlled retry of an unanswered
  urgent call. A dial whose outcome is uncertain is never retried.
- Answering machines detected by Twilio (`twilio.machine_detection`) are
  hung up on and treated as no answer.
- Outbound WhatsApp calls are not supported (business-initiated WhatsApp
  calling is not available in Turkey); such requests go over the phone
  network.

Read-only companions: `call_status`, `call_history`, `call_transcript`.

### Morning call routine

```bash
atlas-agent routine add --name morning-call --disabled "0 9 * * *" \
  "Bugünün sözünü getir. Kısa ve samimi bir sabah konuşması hazırla. call_owner ile kullanıcıyı ara. Cevap vermezse yazılı fallback kullanılır; aynı gün ikinci arama oluşturma."
```

Routines deny anything that would need a prompt, so allow the call
explicitly with `permissions.allowed_tools: ["call_owner"]` in the
routine's working directory. `--disabled` creates it paused;
`atlas-agent routine run morning-call` runs it once by hand and
`atlas-agent routine enable morning-call` turns the schedule on. The
voice service must be running for the call to be placed, and the run's
outcome is in `atlas-agent routine logs morning-call`.

## Site monitoring

Sites listed under `monitor.sites` are checked by the voice service on
their own interval, without the model:

```json
"monitor": {
  "sites": [
    { "name": "shop", "url": "https://shop.example.com/health", "interval_seconds": 60,
      "failure_threshold": 2, "down_statuses": [403], "priority": "urgent" }
  ]
}
```

States are unknown → healthy → degraded → down → recovered → healthy. The
owner is called only on the transition to down (once per incident, keyed
`monitor:<site>:<incident>:down`) and, if armed, on recovery. Saying "tell
me when it's back" on a call arms the recovery call through the
`site_monitor` tool; the flag clears after it fires. 5xx responses,
timeouts, DNS and TLS failures count as failures; other statuses only when
listed in `down_statuses`.

## What is not verified live

The provider and transport code is tested against fake ElevenLabs and
Twilio servers that follow the public protocol documentation. No real call
has been placed and no real ElevenLabs session opened during development.
In particular, verify on first use:

- Scribe Realtime accepts `audio_format=ulaw_8000`, `commit_strategy=vad`
  and `keyterms` query parameters for your model and plan;
- the message type names (`partial_transcript`, `committed_transcript`,
  error types) match what your account receives;
- the TLS proxy forwards WebSocket upgrades on `/voice/twilio/stream`.

The WhatsApp transport is tested against a fake Graph API and a second
WebRTC peer in the same process (SDP, ICE, DTLS-SRTP and audio in both
directions), but never against WhatsApp itself. Verify on first use:

- your number's SDP offer contains PCMU (G.711 mu-law). The transport
  offers and accepts only PCMU, because there is no pure-Go Opus codec; if
  the offer has no PCMU the call fails with a log line naming the step;
- the Graph version (`whatsapp.graph_version`, default `v23.0`) is current;
- the terminate webhook's `status` and `start_time` match what the
  transport expects. A failed business-initiated call without a start time
  is reported as no answer, so the written fallback runs;
- the webhook is subscribed to `calls`, and Meta's retries after a slow
  acknowledgement do not start a second call (a repeated call id is
  admitted only once).

Run `atlas-agent phone serve` with `--debug` for detailed logs during the
first calls.
