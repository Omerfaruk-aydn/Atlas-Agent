# Automatic model router

Off by default. When enabled, each agent step of the main session runs on a
cheap fast model or on the strong model, chosen by a deterministic heuristic.
Sub-agents are not routed (they already follow model roles).

```json
{
  "options": {
    "model_roles": { "fast": { "provider": "anthropic", "model": "claude-haiku-4-5" } },
    "router": { "enabled": true }
  }
}
```

| Setting | Meaning |
| --- | --- |
| `enabled` | Turn the router on. |
| `fast_role` | Role for cheap steps. Default `fast`, then the `small` model. |
| `strong_role` | Role for demanding steps. Default: the session's large model. |
| `min_dwell_steps` | Fewest steps between switches (default 3). |
| `downshift_streak` | Read-only steps in a row before returning to fast (default 2). |
| `error_streak` | Failing steps in a row that escalate to strong (default 2). |

## How it decides

- Turn start: prompt keywords in English and Turkish. Design, debugging,
  review, security and failing tests (`mimari`, `hata ayıkla`, `güvenlik`,
  `başarısız test`, ...) go strong. Reading, searching, listing, summarizing
  and renames (`oku`, `listele`, `özetle`, ...) go fast. No clear signal keeps
  the current model; sessions start on the strong model.
- Mid turn: two failing steps in a row, or failing test or build output from
  `bash`, escalate immediately. Returning to fast needs a streak of read-only
  tool steps, the dwell time, and a saving that outweighs the prompt cache
  given up (when prices are known). A turn that opened with strong work never
  downshifts, and a cross-provider downshift only happens at turn start.
- Always strong: context above 70% of the fast model's window, or images the
  fast model cannot read.

Switches happen only between steps, after all tool results are in the history,
so tool call and result pairing is never split. Reasoning blocks and provider
options are re-resolved for the new model through the same path as 429
failover, and a 429 failover overrides routing for the rest of the turn.

## Savings

Every step run on the fast model records the estimated saving against the
strong model at the same token counts (`router_step` events); switches are
recorded as `router_switch`. They appear as "router saved ~$X" in the TUI Usage
dialog (this session) and in `atlas-agent stats harness` (all sessions). The
figure is an estimate: it ignores cache-hit differences between models.
