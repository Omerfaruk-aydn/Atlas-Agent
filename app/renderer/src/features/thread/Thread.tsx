// The conversation column. It stays pinned to the bottom while content grows,
// until the reader scrolls up; then it holds still and offers a way back.
//
// Long chats mount only their newest turns; older ones join as the reader
// scrolls up, and the browser skips the layout of whatever is off screen
// (content-visibility on .turn). That keeps the DOM small without giving turns
// a fixed height, which their folding and streaming would break.

import { useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import type { Message } from "../../api/types";
import { t } from "../../lib/i18n";
import { onThread } from "../../lib/thread-bus";
import { buildTurns, turnPreview, type Turn } from "../../lib/turns";
import { IconArrowDown } from "../../primitives/icons";
import { Modal } from "../../primitives/ui";
import { useApp } from "../../stores/app";
import { CompactionRunning, TurnView, UserBubble, Working } from "./Turn";

const PIN_DISTANCE = 48;
const TO_BOTTOM_DISTANCE = 120;
const PAGE = 24;
const LOAD_MARGIN = 600;
const MAX_TICKS = 72;
const CARD_HEIGHT = 92;
const NO_MESSAGES: Message[] = [];

function Scrubber({ turns, active, onJump }: { turns: Turn[]; active: number; onJump(index: number): void }) {
  const [hover, setHover] = useState<{ index: number; top: number } | null>(null);
  const shown = useMemo(() => {
    const all = turns.flatMap((turn, index) => (turn.user ? [{ index, ...turnPreview(turn) }] : []));
    if (all.length <= MAX_TICKS) return all;
    const at = Math.max(0, all.findIndex((x) => x.index >= active));
    const from = Math.min(Math.max(0, at - MAX_TICKS / 2), all.length - MAX_TICKS);
    return all.slice(from, from + MAX_TICKS);
  }, [turns, active]);
  if (shown.length < 2) return null;
  const card = hover ? shown.find((x) => x.index === hover.index) : undefined;

  return (
    <nav className="scrubber" aria-label={t("thread.turns")} onMouseLeave={() => setHover(null)}>
      {shown.map(({ index, title }) => (
        <button
          key={index}
          type="button"
          className="tick"
          data-active={index === active || undefined}
          aria-label={title.slice(0, 80)}
          onMouseEnter={(e) => {
            const nav = e.currentTarget.parentElement as HTMLElement;
            setHover({ index, top: Math.max(0, Math.min(e.currentTarget.offsetTop - 12, nav.clientHeight - CARD_HEIGHT)) });
          }}
          onClick={() => onJump(index)}
        />
      ))}
      {hover && card && (
        <div className="tick-card" style={{ insetBlockStart: hover.top }}>
          <div className="tick-title">{card.title}</div>
          {card.body && <div className="tick-body">{card.body}</div>}
        </div>
      )}
    </nav>
  );
}

export function RewindDialog() {
  const ask = useApp((s) => s.rewind);
  const close = useApp((s) => s.closeRewind);
  const confirm = useApp((s) => s.confirmRewind);
  const write = ask?.preview.files_to_write ?? 0;
  const remove = ask?.preview.files_to_delete ?? 0;
  return (
    <Modal open={!!ask} onClose={close} title={t(ask?.edit ? "edit.title" : "rewind.title")} description={t(ask?.edit ? "edit.body" : "rewind.body")} className="modal-sm">
      {write + remove > 0 ? (
        <p className="modal-text">
          {write} {t("rewind.write")}, {remove} {t("rewind.delete")}
        </p>
      ) : (
        <p className="modal-text">{t("rewind.noFiles")}</p>
      )}
      <div className="modal-actions">
        <button type="button" className="button" onClick={close}>
          {t("common.cancel")}
        </button>
        <button type="button" className="button" data-variant="primary" onClick={() => void confirm()}>
          {t(ask?.edit ? "edit.confirm" : "rewind.confirm")}
        </button>
      </div>
    </Modal>
  );
}

export function Thread({ sessionId }: { sessionId: string }) {
  const messages = useApp((s) => s.messages[sessionId] ?? NO_MESSAGES);
  const busy = useApp((s) => !!s.busy[sessionId]);
  const optimistic = useApp((s) => s.optimistic[sessionId]);
  const permissions = useApp((s) => s.permissions);
  const compacting = useApp((s) => !!s.compacting[sessionId]);
  const turns = useMemo(() => buildTurns(messages), [messages]);
  const waiting = useMemo(() => new Set(permissions.filter((p) => p.session_id === sessionId).map((p) => p.tool_call_id)), [permissions, sessionId]);

  const scroller = useRef<HTMLDivElement>(null);
  const content = useRef<HTMLDivElement>(null);
  const pinned = useRef(true);
  const [away, setAway] = useState(false);
  // How many of the newest turns are mounted.
  const [mounted, setMounted] = useState(PAGE);
  const [activeTurn, setActiveTurn] = useState(-1);
  const [unfoldAll, setUnfoldAll] = useState(false);
  const anchorHeight = useRef<number | null>(null);
  const pendingJump = useRef<number | null>(null);

  const first = Math.max(0, turns.length - mounted);
  const shownActive = activeTurn < 0 ? turns.length - 1 : activeTurn;

  // New turns must not push out what a reader further up is looking at.
  const known = useRef(turns.length);
  useEffect(() => {
    const added = turns.length - known.current;
    known.current = turns.length;
    if (added > 0 && !pinned.current) setMounted((n) => n + added);
  }, [turns.length]);

  const toBottom = () => {
    const el = scroller.current;
    if (el) el.scrollTop = el.scrollHeight;
  };

  useLayoutEffect(() => {
    pinned.current = true;
    setAway(false);
    toBottom();
  }, [sessionId]);

  useEffect(() => {
    if (!content.current) return;
    const observer = new ResizeObserver(() => {
      if (pinned.current) toBottom();
    });
    observer.observe(content.current);
    return () => observer.disconnect();
  }, []);

  // Older turns went in above: keep what the reader is looking at in place.
  useLayoutEffect(() => {
    const el = scroller.current;
    if (el && anchorHeight.current !== null) {
      el.scrollTop += el.scrollHeight - anchorHeight.current;
      anchorHeight.current = null;
    }
    if (pendingJump.current !== null) {
      const target = el?.querySelector(`[data-turn="${pendingJump.current}"]`);
      pendingJump.current = null;
      target?.scrollIntoView({ block: "start" });
    }
  }, [first]);

  const jump = (index: number) => {
    pinned.current = false;
    setAway(true);
    const target = scroller.current?.querySelector(`[data-turn="${index}"]`);
    if (target) {
      target.scrollIntoView({ block: "start" });
      setActiveTurn(index);
      return;
    }
    pendingJump.current = index;
    setMounted(turns.length - index);
    setActiveTurn(index);
  };
  const jumpRef = useRef(jump);
  jumpRef.current = jump;

  const activeRef = useRef(shownActive);
  activeRef.current = shownActive;
  const lengthRef = useRef(turns.length);
  lengthRef.current = turns.length;

  useEffect(
    () =>
      onThread((command) => {
        const last = lengthRef.current - 1;
        if (command === "fold") setUnfoldAll((open) => !open);
        else if (command === "prev") jumpRef.current(Math.max(0, activeRef.current - 1));
        else jumpRef.current(Math.min(last, activeRef.current + 1));
      }),
    [],
  );

  const onScroll = () => {
    const el = scroller.current;
    if (!el) return;
    const distance = el.scrollHeight - el.scrollTop - el.clientHeight;
    pinned.current = distance < PIN_DISTANCE;
    setAway(distance > TO_BOTTOM_DISTANCE);
    if (el.scrollTop < LOAD_MARGIN && first > 0) {
      anchorHeight.current = el.scrollHeight;
      setMounted((n) => n + PAGE);
    }
    // The turn at the reading line, for the scrubber.
    const top = el.getBoundingClientRect().top + 80;
    let found = activeRef.current;
    for (const section of el.querySelectorAll<HTMLElement>("[data-turn]")) {
      if (section.getBoundingClientRect().top <= top) found = Number(section.dataset.turn);
      else break;
    }
    if (found !== activeRef.current) setActiveTurn(found);
  };

  // The last turn is live while the session runs and no newer message waits.
  const lastIndex = turns.length - 1;

  return (
    <div className="thread">
      <Scrubber turns={turns} active={shownActive} onJump={jump} />
      <div className="thread-scroll" ref={scroller} onScroll={onScroll}>
        <div className="thread-column" ref={content}>
          {turns.slice(first).map((turn, i) => {
            const index = first + i;
            return <TurnView key={turn.id} turn={turn} index={index} active={busy && !optimistic && index === lastIndex} last={index === lastIndex} waiting={waiting} unfoldAll={unfoldAll} />;
          })}
          {optimistic && (
            <section className="turn" data-live="">
              <UserBubble text={optimistic.text} pendingFiles={optimistic.files} enter />
              <div className="turn-body">
                <Working from={optimistic.at} />
              </div>
            </section>
          )}
          {compacting && <CompactionRunning />}
        </div>
      </div>
      {away && (
        <button
          type="button"
          className="to-bottom"
          aria-label={t("thread.toBottom")}
          title={t("thread.toBottom")}
          onClick={() => {
            pinned.current = true;
            setAway(false);
            setActiveTurn(-1);
            scroller.current?.scrollTo({ top: scroller.current.scrollHeight, behavior: "smooth" });
          }}
        >
          <IconArrowDown />
        </button>
      )}
    </div>
  );
}
