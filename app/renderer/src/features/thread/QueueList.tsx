// Messages written while the agent works wait here, above the composer. Each
// can be edited, removed, steered into the running turn, sent right away
// (which stops the turn), or dragged to a new place. The list is held by this
// window; the server only sees a message when it is sent.

import { useEffect, useRef, useState, type KeyboardEvent, type PointerEvent } from "react";
import { dropIndex, type QueueItem } from "../../lib/chat";
import { t } from "../../lib/i18n";
import { reducedMotion, tokenMs } from "../../lib/motion";
import { IconClose, IconFile, IconPencil } from "../../primitives/icons";
import { Tip } from "../../primitives/ui";
import { useApp } from "../../stores/app";
import { IconGrip, IconSendNow, IconSteer } from "./chatIcons";

interface Drag {
  from: number;
  gap: number;
}

const EMPTY: QueueItem[] = [];

interface RowProps {
  item: QueueItem;
  index: number;
  count: number;
  sid: string;
  drag: Drag | null;
  running: boolean;
  onStart(e: PointerEvent, index: number): void;
  onKey(e: KeyboardEvent, index: number): void;
}

function Row({ item, index, count, sid, drag, running, onStart, onKey }: RowProps) {
  const [editing, setEditing] = useState(false);
  const [value, setValue] = useState(item.text);
  const [leaving, setLeaving] = useState(false);
  const field = useRef<HTMLTextAreaElement>(null);
  const store = useApp.getState;

  useEffect(() => {
    if (!editing) return;
    const el = field.current;
    el?.focus();
    el?.setSelectionRange(el.value.length, el.value.length);
  }, [editing]);

  // The row folds away first, then the store drops it.
  const leave = (then: () => void) => {
    if (reducedMotion()) return then();
    setLeaving(true);
    window.setTimeout(then, tokenMs("--atlas-dur-moderate", 220));
  };

  const save = () => {
    if (value.trim()) store().updateQueued(sid, item.id, value);
    else setValue(item.text);
    setEditing(false);
  };

  return (
    <div className="queue-slot" data-leaving={leaving || undefined} data-dragging={drag?.from === index || undefined}>
      <div className="queue-item" data-index={index}>
        <button type="button" className="queue-grip" aria-label={`${t("queue.drag")}. ${t("queue.move")}`} title={t("queue.drag")} onPointerDown={(e) => onStart(e, index)} onKeyDown={(e) => onKey(e, index)}>
          <IconGrip />
        </button>
        {editing ? (
          <textarea
            ref={field}
            className="queue-edit"
            rows={1}
            value={value}
            aria-label={t("queue.edit")}
            onChange={(e) => setValue(e.target.value)}
            onBlur={save}
            onKeyDown={(e) => {
              if (e.key === "Escape") {
                e.preventDefault();
                e.stopPropagation();
                setValue(item.text);
                setEditing(false);
              } else if (e.key === "Enter" && !e.shiftKey && !e.nativeEvent.isComposing) {
                e.preventDefault();
                save();
              }
            }}
          />
        ) : (
          <span className="queue-text" title={item.text}>
            {item.text}
            {item.attachments.length > 0 && (
              <span className="queue-files">
                <IconFile />
                {item.attachments.length}
                <span className="sr-only"> {t("queue.withFiles")}</span>
              </span>
            )}
          </span>
        )}
        <span className="queue-actions">
          {running && (
            <Tip label={t("queue.steerHint")}>
              <button type="button" className="queue-button" data-action="steer" aria-label={`${t("queue.steer")}: ${t("queue.steerHint")}`} onClick={() => leave(() => void store().steerQueued(sid, item.id))}>
                <IconSteer />
                <span>{t("queue.steer")}</span>
              </button>
            </Tip>
          )}
          <Tip label={running ? t("queue.nowHint") : t("queue.now")}>
            <button type="button" className="queue-button" data-action="now" aria-label={running ? `${t("queue.now")}: ${t("queue.nowHint")}` : t("queue.now")} onClick={() => void store().sendQueuedNow(sid, item.id)}>
              <IconSendNow />
              <span>{t("queue.now")}</span>
            </button>
          </Tip>
          <Tip label={t("queue.edit")}>
            <button type="button" className="icon-button" data-size="xs" data-action="edit" aria-label={t("queue.edit")} onMouseDown={(e) => e.preventDefault()} onClick={() => setEditing(!editing)}>
              <IconPencil />
            </button>
          </Tip>
          <Tip label={t("queue.delete")}>
            <button type="button" className="icon-button" data-size="xs" data-action="delete" aria-label={t("queue.delete")} onClick={() => leave(() => store().removeQueued(sid, item.id))}>
              <IconClose />
            </button>
          </Tip>
        </span>
      </div>
      <span className="sr-only">{`${index + 1}/${count}`}</span>
    </div>
  );
}

export function QueueList({ sessionId }: { sessionId: string | null }) {
  const items = useApp((s) => (sessionId ? (s.queue[sessionId] ?? EMPTY) : EMPTY));
  const paused = useApp((s) => (sessionId ? !!s.queuePaused[sessionId] : false));
  const busy = useApp((s) => (sessionId ? !!s.busy[sessionId] : false));
  const [drag, setDrag] = useState<Drag | null>(null);
  const [said, setSaid] = useState("");
  const list = useRef<HTMLDivElement>(null);
  const dragRef = useRef<Drag | null>(null);

  // The gap the pointer is over: 0 above the first row, n below the last.
  const gapAt = (y: number): number => {
    const rows = [...(list.current?.querySelectorAll<HTMLElement>(".queue-slot") ?? [])];
    for (let i = 0; i < rows.length; i++) {
      const box = rows[i]!.getBoundingClientRect();
      if (y < box.top + box.height / 2) return i;
    }
    return rows.length;
  };

  const announce = (to: number) => setSaid(t("queue.position").replace("{0}", String(to + 1)).replace("{1}", String(items.length)));

  const start = (e: PointerEvent, index: number) => {
    if (e.button !== 0 || !sessionId) return;
    e.preventDefault();
    (e.currentTarget as HTMLElement).focus();
    dragRef.current = { from: index, gap: index };
    setDrag(dragRef.current);
    const move = (ev: globalThis.PointerEvent) => {
      dragRef.current = { from: index, gap: gapAt(ev.clientY) };
      setDrag(dragRef.current);
    };
    const finish = (commit: boolean) => {
      window.removeEventListener("pointermove", move);
      window.removeEventListener("pointerup", up);
      window.removeEventListener("keydown", key, true);
      const done = dragRef.current;
      dragRef.current = null;
      setDrag(null);
      if (!commit || !done) return;
      const to = dropIndex(done.from, done.gap);
      if (to === done.from) return;
      useApp.getState().moveQueued(sessionId, done.from, to);
      announce(to);
    };
    const up = () => finish(true);
    const key = (ev: globalThis.KeyboardEvent) => {
      if (ev.key !== "Escape") return;
      ev.preventDefault();
      ev.stopPropagation();
      finish(false);
    };
    window.addEventListener("pointermove", move);
    window.addEventListener("pointerup", up);
    window.addEventListener("keydown", key, true);
  };

  const keyboard = (e: KeyboardEvent, index: number) => {
    if (!sessionId || !e.altKey || (e.key !== "ArrowUp" && e.key !== "ArrowDown")) return;
    e.preventDefault();
    e.stopPropagation();
    const to = index + (e.key === "ArrowUp" ? -1 : 1);
    if (to < 0 || to >= items.length) return;
    useApp.getState().moveQueued(sessionId, index, to);
    announce(to);
    // The grip keeps focus at the row's new place.
    requestAnimationFrame(() => list.current?.querySelectorAll<HTMLElement>(".queue-grip")[to]?.focus());
  };

  // The 2px line sits in the gap the pointer is over.
  let line: number | null = null;
  if (drag && list.current && dropIndex(drag.from, drag.gap) !== drag.from) {
    const rows = [...list.current.querySelectorAll<HTMLElement>(".queue-slot")];
    const target = rows[drag.gap];
    const frame = list.current.getBoundingClientRect();
    if (target) line = target.getBoundingClientRect().top - frame.top;
    else if (rows.length > 0) line = rows[rows.length - 1]!.getBoundingClientRect().bottom - frame.top;
  }

  if (!sessionId || items.length === 0) return null;

  return (
    <div className="queue-list" role="region" aria-label={t("queue.title")} data-dragging={drag ? "" : undefined}>
      <div className="queue-head">
        <span>
          {t("queue.title")} · {items.length}
        </span>
        {paused && (
          <>
            <span className="queue-paused">{t("queue.paused")}</span>
            <button type="button" className="queue-resume" onClick={() => useApp.getState().resumeQueue(sessionId)}>
              {t("queue.resume")}
            </button>
          </>
        )}
      </div>
      <div className="queue-rows" ref={list}>
        {items.map((item, i) => (
          <Row key={item.id} item={item} index={i} count={items.length} sid={sessionId} drag={drag} running={busy} onStart={start} onKey={keyboard} />
        ))}
        {line !== null && <div className="queue-drop" style={{ transform: `translateY(${line - 1}px)` }} aria-hidden="true" />}
      </div>
      <span className="sr-only" role="status" aria-live="polite">
        {said}
      </span>
    </div>
  );
}
