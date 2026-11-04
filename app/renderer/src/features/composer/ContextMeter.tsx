// How full the context window is. Hovering the ring gives the short answer;
// clicking it opens what the window is made of, with a way to free it.

import { HoverCard, Popover } from "radix-ui";
import { useMemo, useState } from "react";
import type { Message } from "../../api/types";
import { contextShares, type ContextPart } from "../../lib/context";
import { compact, money } from "../../lib/format";
import { t, type Key } from "../../lib/i18n";
import { IconMinus } from "../../primitives/icons";
import { useApp } from "../../stores/app";

const LABELS: Record<ContextPart, Key> = {
  system: "context.system",
  summary: "context.summary",
  user: "context.user",
  assistant: "context.assistant",
  tools: "context.tools",
};

const NO_MESSAGES: Message[] = [];
const RING_RADIUS = 6;

export function ContextMeter({ sessionId }: { sessionId: string | null }) {
  const session = useApp((s) => s.sessions.find((x) => x.id === sessionId));
  const messages = useApp((s) => (sessionId ? (s.messages[sessionId] ?? NO_MESSAGES) : NO_MESSAGES));
  const windowSize = useApp((s) => {
    const current = s.catalog?.active ?? s.catalog?.selected?.large;
    return s.catalog?.providers?.find((p) => p.id === current?.provider)?.models.find((m) => m.id === current?.model)?.context_window ?? 0;
  });
  const summarize = useApp((s) => s.summarize);
  const [hover, setHover] = useState(false);
  const [open, setOpen] = useState(false);
  const used = session ? session.prompt_tokens + session.completion_tokens : 0;
  const shares = useMemo(() => (open ? contextShares(messages, used) : []), [open, messages, used]);

  if (!session || !windowSize || used === 0) return null;
  const share = Math.min(1, used / windowSize);
  const percent = Math.round(share * 100);
  const length = 2 * Math.PI * RING_RADIUS;
  const high = share > 0.8 || undefined;

  return (
    <Popover.Root
      open={open}
      onOpenChange={(next) => {
        setOpen(next);
        setHover(false);
      }}
    >
      <HoverCard.Root open={hover && !open} onOpenChange={setHover} openDelay={120} closeDelay={60}>
        <Popover.Trigger asChild>
          <HoverCard.Trigger asChild>
            <button type="button" className="context-ring" aria-label={`${t("context.title")} ${percent}%`} data-high={high}>
              <svg width="16" height="16" viewBox="0 0 16 16" aria-hidden="true">
                <circle cx="8" cy="8" r={RING_RADIUS} className="ring-track" />
                <circle cx="8" cy="8" r={RING_RADIUS} className="ring-fill" strokeDasharray={`${share * length} ${length}`} transform="rotate(-90 8 8)" />
              </svg>
            </button>
          </HoverCard.Trigger>
        </Popover.Trigger>
        <HoverCard.Portal>
          <HoverCard.Content className="pop context-hover" side="top" align="end" sideOffset={8} collisionPadding={12}>
            <div className="context-line context-head">
              <span>{t("context.title")}</span>
              <span>{percent}%</span>
            </div>
            <div className="context-track" data-high={high}>
              <span style={{ inlineSize: `${Math.max(share * 100, 1)}%` }} />
            </div>
            <div className="context-line">
              <span>{t("context.tokens")}</span>
              <span>
                {compact(used)} / {compact(windowSize)}
              </span>
            </div>
            {session.cost > 0 && (
              <div className="context-line">
                <span>{t("context.cost")}</span>
                <span>{money(session.cost)}</span>
              </div>
            )}
          </HoverCard.Content>
        </HoverCard.Portal>
      </HoverCard.Root>
      <Popover.Portal>
        <Popover.Content className="pop context-panel" side="top" align="end" sideOffset={10} collisionPadding={12}>
          <div className="context-panel-head">
            <span className="context-panel-title">{t("context.usage")}</span>
            <button type="button" className="link-button context-action" onClick={() => { setOpen(false); void summarize(); }}>
              {t("context.compact")}
            </button>
            <Popover.Close asChild>
              <button type="button" className="icon-button" data-size="xs" aria-label={t("common.close")}>
                <IconMinus />
              </button>
            </Popover.Close>
          </div>
          <div className="context-panel-sum">
            <span>
              {percent}% {t("context.full")}
            </span>
            <span>
              ~{compact(used)} / {compact(windowSize)} {t("context.tokens")}
            </span>
          </div>
          <div className="context-bar" role="img" aria-label={`${percent}%`}>
            {shares.map((s) => (
              <span key={s.part} data-part={s.part} style={{ inlineSize: `${(s.tokens / windowSize) * 100}%` }} />
            ))}
          </div>
          <ul className="context-legend">
            {shares.map((s) => (
              <li key={s.part}>
                <span className="context-swatch" data-part={s.part} />
                <span className="context-legend-label">{t(LABELS[s.part])}</span>
                <span className="context-legend-value">{compact(s.tokens)}</span>
              </li>
            ))}
          </ul>
          <p className="context-note">{t("context.estimate")}</p>
        </Popover.Content>
      </Popover.Portal>
    </Popover.Root>
  );
}
