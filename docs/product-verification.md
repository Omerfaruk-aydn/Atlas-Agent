# Product verification

Product verification is a completion gate for web and UI work. The evaluator
reads code; this gate opens the running app in Atlas's own `browser` tool,
measures it, drives its user flows and sends what it finds back to the agent
as fix tasks. There is no Playwright or other test-framework dependency: the
checks are small scripts evaluated in the page through the browser tool.

It is off by default and fails open: no browser, a denied permission, no
running app or an error inside the check lets the turn end as it would have
without it. The reason is logged.

## Turning it on

```json
{
  "options": {
    "product_verify": {
      "mode": "auto"
    }
  }
}
```

| Setting | Default | Meaning |
| --- | --- | --- |
| `mode` | `off` | `auto` verifies only web/UI work (UI files changed, or a script changed under a UI request in English or Turkish, or a spec with flows exists). `always` verifies whenever a local app is running. |
| `url` | discovered | Address of the app. Empty reads it from `.atlas/verify.json`, the request, or the dev-server output the agent saw (`Local: http://localhost:5173`). |
| `max_rounds` | 2 | How often one request is sent back (max 5). Afterwards it ends with the findings reported. |
| `block_on` | `medium` | Lowest severity of an evidenced finding that blocks. |
| `load_budget_ms` | 5000 | Longest accepted page load; negative disables. |
| `max_console_errors` | 0 | Console errors tolerated per page. |
| `max_failed_requests` | 0 | Failed requests tolerated per page. |
| `axe_path` | auto | Path to `axe.min.js`. |
| `vision_review` | true | Have a vision model review screenshots. |

The gate runs after the work plan's verification contract passes (so builds
and tests come first) and before the evaluator. The browser tool's normal
permission rules apply to every action; allow the browser for the session to
avoid repeated prompts.

## Local addresses only

Only `localhost`, `127.0.0.0/8`, `::1`, `*.localhost` and `*.test` are
accepted, for the base address, extra pages, flow `navigate` steps and
redirects while probing. Anything else is refused, and a configured remote
`url` is reported to the agent as a setup problem.

## What is checked

For each page (`/` plus `pages` from the spec, at most five) at mobile
(390x844), tablet (768x1024) and desktop (1366x800):

- **Console errors.** `error` and `exception` entries beyond the budget. An
  uncaught exception is high severity, a logged error medium. "Failed to load
  resource" lines are left to the network check.
- **Failed requests.** Status 5xx is high, 4xx and failed requests with a known
  address are medium, a missing `favicon.ico` and aborted requests with no
  recorded address are low. Each finding carries the recorded request.
- **Accessibility.** [axe-core](https://github.com/dequelabs/axe-core) is
  injected with the browser tool's `eval` after each page load when the project
  has it (`node_modules/axe-core/axe.min.js`, `.atlas/vendor/axe.min.js` or
  `axe_path`). axe-core is MPL-2.0 licensed and is not shipped with Atlas;
  install it with `npm i -D axe-core`. Without it a built-in subset runs
  (image alt text, button/link/form labels, `lang`, title, zoom-blocking
  viewport meta, duplicate ids, positive tabindex, empty headings, main
  landmark); the report says which engine ran. Impact maps to severity:
  critical high, serious medium, moderate and minor low.
- **Load time.** Navigation timing against the budget.
- **Layout.** A document that scrolls sideways, and a page that renders blank,
  each with a screenshot and the offending markup.
- **Visual review.** The screenshots (at most six) go to a vision-capable
  model (the evaluator's, else the session's) that reports only visible
  defects: overlap, cut-off or unreadable text, content wider than the screen,
  blank areas, broken images, controls too small to use. Vision findings block
  only at high severity.

## Flows

Flows are driven at the widest viewport after the page checks. They come from:

1. `.atlas/verify.json`, written by the agent (hard flows: a failure blocks):

   ```json
   {
     "url": "http://localhost:5173",
     "pages": ["/pricing"],
     "flows": [
       {
         "name": "sign in",
         "steps": [
           { "action": "navigate", "url": "/login" },
           { "action": "type", "selector": "#email", "text": "user@example.com" },
           { "action": "click", "selector": "button[type=submit]" },
           { "action": "assert", "condition": "text_contains", "selector": "h1", "expected": "Dashboard" }
         ]
       }
     ]
   }
   ```

   Actions: `navigate`, `click`, `type`, `key`, `assert`. Assertions use the
   browser tool's `assert` action; conditions: `visible`, `hidden`, `text`,
   `text_contains`, `value`, `enabled`, `url`, `title`, `checked`, `count`.
   At most 20 flows of 30 steps. Use test data only; never real credentials.

2. The request and `.atlas/work.json` acceptance criteria (soft flows): a
   route named next to "page", "route", "url" or "sayfa" must load, and text
   quoted in a feature's acceptance must appear on the page. These are
   heuristics, so their failures are reported as advisory and never block.

## Evidence and blocking

A finding blocks only when it is evidenced and is not advisory: a screenshot
path, a DOM excerpt, a console line, a recorded request, a measurement or a
step error. Findings are merged across viewports.

Each run writes `.atlas/verify/<run id>/` with screenshots, `report.json` and
`report.md` (also pointed to by `.atlas/verify/latest.md`); the newest five
runs are kept. The summary also goes to the evaluator's review, and findings
that remain after `max_rounds` end the turn with the list and the report path.

## Code

- `internal/webverify`: the engine (driver interface, checks, flows, report).
- `internal/agent/tools/webverify_driver.go`: the driver that calls the
  `browser` tool, in a tab of its own.
- `internal/agent/product_verify.go`: the completion gate.
- `internal/config/product_verify.go`: the settings.
