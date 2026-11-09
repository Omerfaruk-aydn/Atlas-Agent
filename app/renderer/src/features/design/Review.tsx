// Comments pinned on the canvas, and the file's history.

import { useEffect, useState } from "react";
import { http } from "../../api/http";
import { t } from "../../lib/i18n";
import { useApp } from "../../stores/app";
import { useDesign } from "../../stores/design";
import { type Comment, type DesignDoc, locate, newId, pageOf, sanitizeDoc } from "./model";
import { worldBounds } from "./geometry";
import { sizeOf } from "./text";

const iso = () => new Date().toISOString();

function withComments(doc: DesignDoc, fn: (list: Comment[]) => Comment[]): DesignDoc {
  const next = fn(doc.comments ?? []);
  return { ...doc, comments: next.length ? next : undefined };
}

/** Starts a comment at a page point (or on the layer under it); returns its id. */
export function newCommentAt(pageId: string, at: { x: number; y: number }, node?: string): string | null {
  const s = useDesign.getState();
  if (!s.doc) return null;
  const id = newId();
  const c: Comment = { id, page: pageId, x: Math.round(at.x), y: Math.round(at.y), author: "person", text: "", created: iso(), ...(node ? { node } : {}) };
  // Not in history until it has words: an empty pin is dropped when the thread closes.
  useDesign.setState({ doc: withComments(s.doc, (l) => [...l, c]) });
  return id;
}

function edit(fn: (list: Comment[]) => Comment[]) {
  const s = useDesign.getState();
  if (s.doc) s.commit(withComments(s.doc, fn));
}

/** Numbered pins over the canvas; a click opens the thread. */
export function CommentPins() {
  const doc = useDesign((s) => s.doc);
  const pageId = useDesign((s) => s.pageId);
  const camera = useDesign((s) => s.camera);
  const active = useDesign((s) => s.activeComment);
  if (!doc?.comments) return null;
  const page = pageOf(doc, pageId);
  const mine = doc.comments.filter((c) => c.page === page.id);
  return (
    <div className="design-pins">
      {mine.map((c, i) => {
        // A pin on a layer follows it when it moves.
        const at = c.node ? locate(page, c.node) : null;
        const b = at ? worldBounds(at.path, sizeOf) : null;
        const x = b ? b.x : c.x;
        const y = b ? b.y : c.y;
        return (
          <button
            key={c.id}
            type="button"
            className="design-pin"
            data-on={active === c.id || undefined}
            data-resolved={c.resolved || undefined}
            style={{ left: x * camera.zoom + camera.x, top: y * camera.zoom + camera.y }}
            aria-label={`${t("design.comments")} ${i + 1}: ${c.text || t("design.writeComment")}`}
            onPointerDown={(e) => e.stopPropagation()}
            onClick={(e) => {
              e.stopPropagation();
              useDesign.getState().setActiveComment(c.id);
            }}
          >
            {i + 1}
          </button>
        );
      })}
    </div>
  );
}

export function CommentsPanel() {
  const doc = useDesign((s) => s.doc);
  const pageId = useDesign((s) => s.pageId);
  const active = useDesign((s) => s.activeComment);
  const setActive = useDesign((s) => s.setActiveComment);
  const [showResolved, setShowResolved] = useState(false);
  const [draft, setDraft] = useState("");
  const [reply, setReply] = useState("");
  const list = (doc?.comments ?? []).filter((c) => c.page === pageId);
  const shown = list.filter((c) => showResolved || !c.resolved || c.id === active);

  // An empty new pin is dropped when another thread opens or the panel closes.
  useEffect(
    () => () => {
      const s = useDesign.getState();
      if (s.doc?.comments?.some((c) => !c.text)) useDesign.setState({ doc: withComments(s.doc, (l) => l.filter((c) => c.text)) });
    },
    [],
  );
  useEffect(() => {
    setDraft("");
    setReply("");
  }, [active]);

  const go = (id: string) => {
    const c = list.find((x) => x.id === id);
    setActive(id);
    if (c && c.node) useDesign.getState().select([c.node]);
  };

  return (
    <div className="design-comments">
      <label className="design-check">
        <input type="checkbox" checked={showResolved} onChange={(e) => setShowResolved(e.target.checked)} />
        {t("design.showResolved")}
      </label>
      {shown.length === 0 && <p className="design-muted">{t("design.noComments")}</p>}
      {shown.map((c, i) => {
        const open = c.id === active;
        return (
          <article key={c.id} className="design-thread" data-on={open || undefined} data-resolved={c.resolved || undefined}>
            <button type="button" className="design-thread-head" onClick={() => (open ? setActive(null) : go(c.id))}>
              <span className="design-pin-num">{list.indexOf(c) + 1 || i + 1}</span>
              <span className="design-row-name">{c.text || t("design.writeComment")}</span>
              <span className="design-muted">{c.author}</span>
            </button>
            {open && (
              <div className="design-thread-body">
                {!c.text ? (
                  <>
                    <textarea
                      className="design-comment-input"
                      aria-label={t("design.writeComment")}
                      placeholder={t("design.writeComment")}
                      autoFocus
                      rows={3}
                      value={draft}
                      onChange={(e) => setDraft(e.target.value)}
                      onKeyDown={(e) => {
                        if (e.key === "Enter" && !e.shiftKey && draft.trim()) {
                          e.preventDefault();
                          edit((l) => l.map((x) => (x.id === c.id ? { ...x, text: draft.trim() } : x)));
                        }
                      }}
                    />
                    <button type="button" className="design-chip" disabled={!draft.trim()} onClick={() => edit((l) => l.map((x) => (x.id === c.id ? { ...x, text: draft.trim() } : x)))}>
                      {t("design.comments")}
                    </button>
                  </>
                ) : (
                  <>
                    <p className="design-comment-text">{c.text}</p>
                    {(c.replies ?? []).map((r, k) => (
                      <p key={k} className="design-comment-reply">
                        <strong>{r.author}</strong> {r.text}
                      </p>
                    ))}
                    <input className="design-name" aria-label={t("design.reply")} placeholder={t("design.reply")} value={reply} maxLength={4000} onChange={(e) => setReply(e.target.value)} onKeyDown={(e) => e.key === "Enter" && reply.trim() && (edit((l) => l.map((x) => (x.id === c.id ? { ...x, replies: [...(x.replies ?? []), { author: "person", text: reply.trim(), created: iso() }] } : x))), setReply(""))} />
                    <div className="design-row">
                      <button type="button" className="design-chip" onClick={() => edit((l) => l.map((x) => (x.id === c.id ? { ...x, resolved: x.resolved ? undefined : true } : x)))}>
                        {c.resolved ? t("design.reopen") : t("design.resolve")}
                      </button>
                      <button
                        type="button"
                        className="design-chip"
                        onClick={() => {
                          setActive(null);
                          edit((l) => l.filter((x) => x.id !== c.id));
                        }}
                      >
                        {t("design.deleteComment")}
                      </button>
                    </div>
                  </>
                )}
              </div>
            )}
          </article>
        );
      })}
    </div>
  );
}

interface Snapshot {
  id: string;
  time: string;
  kind: "named" | "agent" | "auto";
  label?: string;
}

const api = (suffix: string) => `/api/workspaces/${encodeURIComponent(useApp.getState().workspace?.id ?? "")}/design${suffix}`;

export function HistoryPanel() {
  const file = useDesign((s) => s.file);
  const rev = useDesign((s) => s.rev);
  const [versions, setVersions] = useState<Snapshot[]>([]);
  const [label, setLabel] = useState("");
  const [note, setNote] = useState("");

  const load = () => {
    if (!file) return;
    http
      .get<{ versions: Snapshot[] }>(api(`/history?name=${encodeURIComponent(file)}`))
      .then((r) => setVersions(r.versions))
      .catch(() => setVersions([]));
  };
  // Reloads when the file is saved (its revision changes).
  useEffect(load, [file, rev]); // eslint-disable-line react-hooks/exhaustive-deps

  const restore = async (id: string) => {
    const s = useDesign.getState();
    if (!file || !s.doc) return;
    try {
      const r = await http.get<{ doc: unknown }>(api(`/version?name=${encodeURIComponent(file)}&id=${encodeURIComponent(id)}`));
      // A restore is an edit like any other: undo brings back what was there.
      s.commit(sanitizeDoc(r.doc));
      setNote(t("design.restored"));
    } catch (err) {
      setNote(err instanceof Error ? err.message : String(err));
    }
  };

  const save = async () => {
    const s = useDesign.getState();
    if (!file || !label.trim()) return;
    // The file must hold what is on screen before it is named.
    await s.save();
    try {
      await http.post(api("/version"), { name: file, label: label.trim() });
      setLabel("");
      load();
    } catch (err) {
      setNote(err instanceof Error ? err.message : String(err));
    }
  };

  return (
    <div className="design-comments">
      <div className="design-row">
        <input className="design-name" aria-label={t("design.nameVersion")} placeholder={t("design.nameVersion")} value={label} maxLength={80} onChange={(e) => setLabel(e.target.value)} onKeyDown={(e) => e.key === "Enter" && void save()} />
        <button type="button" className="design-chip" disabled={!label.trim()} onClick={() => void save()}>
          {t("design.saveVersion")}
        </button>
      </div>
      {note && (
        <p className="design-muted" role="status">
          {note}
        </p>
      )}
      {versions.length === 0 && <p className="design-muted">{t("design.noHistory")}</p>}
      {versions.map((v) => (
        <div className="design-version" key={v.id}>
          <div className="design-grow">
            <div>{v.label?.replace(/-/g, " ") || t(`design.kind_${v.kind}` as never)}</div>
            <div className="design-muted">
              {t(`design.kind_${v.kind}` as never)} · {new Date(v.time).toLocaleString()}
            </div>
          </div>
          <button type="button" className="design-chip" onClick={() => void restore(v.id)}>
            {t("design.restore")}
          </button>
        </div>
      ))}
    </div>
  );
}
