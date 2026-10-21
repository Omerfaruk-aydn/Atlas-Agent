// The composer's context ring: how much of the model's context window the
// session has used, drawn as a progress ring beside the send button. It
// changes colour as the window fills, and its tooltip gives the exact
// figures.

import { createMemo, Show } from "solid-js"
import { ProgressCircle } from "@atlas/ui/progress-circle"
import { Tooltip } from "@atlas/ui/tooltip"
import { t as at } from "../i18n"
import { formatTokens } from "../lib/format"

export type ContextUsage = {
  tokens: number
  window: number | undefined
  cost: number
  messages: number
}

const WARN = 70
const CRITICAL = 90

export function ContextRing(props: { usage: ContextUsage }) {
  const percent = createMemo(() => (props.usage.window ? Math.min(100, (props.usage.tokens / props.usage.window) * 100) : 0))
  const level = createMemo(() => (percent() >= CRITICAL ? "critical" : percent() >= WARN ? "warning" : "normal"))
  const rounded = () => (percent() > 0 && percent() < 1 ? "<1" : String(Math.round(percent())))

  return (
    <Show when={props.usage.window}>
      <Tooltip
        placement="top"
        gutter={8}
        value={
          <div class="flex min-w-[200px] flex-col gap-2 py-0.5" data-component="context-ring-card">
            <div class="flex items-baseline justify-between gap-6">
              <span class="text-[12px] text-v2-text-text-muted">{at("usage.context")}</span>
              <span class="text-[13px] tabular-nums [font-weight:530]" data-level={level()}>
                {rounded()}%
              </span>
            </div>
            <div class="h-1 overflow-hidden rounded-full" style={{ background: "var(--context-ring-track)" }}>
              <div
                class="h-full rounded-full transition-[width] duration-300"
                style={{ width: `${percent()}%`, background: `var(--context-ring-${level()})` }}
              />
            </div>
            <div class="flex items-baseline justify-between gap-6 text-[12px] tabular-nums">
              <span class="text-v2-text-text-muted">{at("usage.tokens")}</span>
              <span>
                {formatTokens(props.usage.tokens)} / {formatTokens(props.usage.window!)}
              </span>
            </div>
            <Show when={props.usage.cost > 0}>
              <div class="flex items-baseline justify-between gap-6 text-[12px] tabular-nums">
                <span class="text-v2-text-text-muted">{at("usage.cost")}</span>
                <span>${props.usage.cost.toFixed(props.usage.cost < 1 ? 3 : 2)}</span>
              </div>
            </Show>
          </div>
        }
      >
        <div
          data-component="context-ring"
          data-level={level()}
          role="img"
          tabIndex={0}
          aria-label={`${at("usage.context")}: ${rounded()}%`}
          class="me-1 flex size-7 shrink-0 items-center justify-center rounded-full outline-none focus-visible:ring-2 focus-visible:ring-[var(--context-ring-normal)]"
        >
          <ProgressCircle
            appearance="indicator"
            size={20}
            strokeWidth={2.5}
            percentage={percent()}
            style={{
              "--progress-circle-background": "var(--context-ring-track)",
              "--progress-circle-progress": `var(--context-ring-${level()})`,
            }}
          />
        </div>
      </Tooltip>
    </Show>
  )
}
