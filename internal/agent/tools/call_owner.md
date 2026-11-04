Phone the owner through the Atlas voice service (`atlas phone serve`).

The number always comes from the voice configuration; you cannot choose who is called. Use it only when a call is clearly worth more than a written message: something the owner asked to be called about, an urgent alert, or a scheduled morning call.

- `reason` is spoken right after the greeting and reused as the written fallback, so make it short, factual and self-contained.
- `event_id` makes the call idempotent: the same id never rings twice. Use a stable id for the event (a monitor incident, a routine occurrence). In routines it defaults to the routine and the day.
- Rate limits apply (calls per hour, minimum gap). An "urgent" priority does not lift the hourly cap.
- Results: `answered` (the conversation continues on the phone), `no_answer`, `rejected`, `failed`, `rate_limited`, `duplicate`. When the owner is not reached, a written message is sent on the configured fallback channel and listed in `fallback`.
- Never call again just because a call was not answered or the outcome is unclear; report what happened instead.
