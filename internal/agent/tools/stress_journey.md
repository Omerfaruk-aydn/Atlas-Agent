Run a declared web journey under adverse conditions and record observed results.

Read or write a project-relative JSON manifest before calling this tool. Version
1 has `url` and 1-12 `cases`. Each case requires a unique `name`, viewport `width`
and `height`, and 1-48 `steps`. Optional `latency_ms`, download/upload bytes per
second and `blocked_urls` inject actual CDP network faults. Cache and service
worker bypass make those conditions observable. Each case owns a new browser
tab and restores the original tab on completion or cancellation.

Steps support click/type (CSS selector), key, network (offline/online), assert
and wait_for (browser condition/expected/selector, timeout_ms). `repeat: 2` is
an explicit duplicate click test, never an automatic mutation retry. To test
malformed form input, specify its literal invalid text in a type step. Network
response bodies are not replaced. Each case requires at least one explicit
assertion. Use fixtures or an authorized test account: a new tab does not
isolate the site's cookies, server data or purchases.

Assertions report actual browser outcomes; denied tools, invalid results,
timeouts and cleanup failures cannot pass. Later steps after failure are
blocked rather than blindly executed. Cases run sequentially, without automatic
retries. Permission and hook checks apply to every browser action, including
cleanup. Reports are persisted per session. This tests the declared journey,
not all behavior, accessibility or visual quality.

For controlled main-frame dates/timers, an optional clock configuration and
advance_time steps use the time_lab contract. See time_lab for its real-clock
limitations. Never claim that a virtual client clock changes server-side time.
