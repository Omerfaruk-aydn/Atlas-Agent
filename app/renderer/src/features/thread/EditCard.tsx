// The files a turn changed, with a decision for each: keep the edit or undo
// it. Undo asks the gateway to put the old text back; it refuses when the file
// has changed since, so a later edit of the person's is never overwritten.
// "Kept" only records that the edit was looked at, on this computer.

import { useContext, useEffect, useState } from "react";
import { SideScope } from "../../lib/scope";
import { fileEdits, loadReviews, reviewKey, saveReview, type FileEdit, type Review } from "../../lib/chat";
import { baseName } from "../../lib/format";
import { t } from "../../lib/i18n";
import { userText, type Turn } from "../../lib/turns";
import { IconChevron, IconSearch } from "../../primitives/icons";
import { useReview } from "../../stores/review";
import { ReviewPanel } from "./ReviewPanel";
import { Tip } from "../../primitives/ui";
import { useApp } from "../../stores/app";
import { usePanel } from "../../stores/panel";
import { IconUndo } from "./chatIcons";

function Row({ file, state, busy, onKeep, onUndo }: { file: FileEdit; state: Review | undefined; busy: boolean; onKeep(): void; onUndo(): void }) {
  return (
    <div className="edit-row" title={file.path} data-state={state}>
      <span className="edit-name">{baseName(file.path)}</span>
      {file.created && <span className="edit-new">{t("edits.created")}</span>}
      {!!file.additions && <span className="add">+{file.additions}</span>}
      {!!file.removals && <span className="del">−{file.removals}</span>}
      <span className="edit-decision">
        {state ? (
          <span className="edit-state" key={state} role="status">
            {state === "kept" ? t("edits.kept") : t("edits.undone")}
          </span>
        ) : (
          <>
            <Tip label={file.undoable ? t("edits.undo") : t("edits.noUndo")}>
              <button type="button" className="edit-button" data-action="undo" disabled={busy || !file.undoable} aria-label={`${t("edits.undo")} ${baseName(file.path)}`} onClick={onUndo}>
                <IconUndo />
                <span>{t("edits.undo")}</span>
              </button>
            </Tip>
            <button type="button" className="edit-button" data-action="keep" disabled={busy} aria-label={`${t("edits.keep")} ${baseName(file.path)}`} onClick={onKeep}>
              {t("edits.keep")}
            </button>
          </>
        )}
      </span>
    </div>
  );
}

export function EditCard({ turn }: { turn: Turn }) {
  const openTool = usePanel((s) => s.openTool);
  const revert = useApp((s) => s.revertFile);
  const startReview = useReview((s) => s.start);
  const hydrate = useReview((s) => s.hydrate);
  const phase = useReview((s) => s.byTurn[turn.id]?.phase ?? "idle");
  const sessionId = useApp((s) => s.currentId);
  const side = useContext(SideScope);
  const busy = useApp((s) => !!s.currentId && !!s.busy[s.currentId]);
  const canReview = !side && !busy && !!sessionId && phase !== "starting" && phase !== "running";
  const edits = fileEdits(turn.steps);
  const [reviews, setReviews] = useState(loadReviews);
  const [working, setWorking] = useState(false);
  const [confirming, setConfirming] = useState(false);
  const hasEdits = edits.length > 0;
  useEffect(() => {
    if (hasEdits && sessionId) hydrate(turn.id, sessionId);
  }, [hasEdits, hydrate, sessionId, turn.id]);
  if (!hasEdits) return null;

  const review = () => {
    if (!sessionId) return;
    void startReview(turn.id, sessionId, edits.map((f) => f.path), turn.user ? userText(turn.user) : "");
  };

  const stateOf = (f: FileEdit) => reviews[reviewKey(turn.id, f.path)];
  const open = edits.filter((f) => !stateOf(f));
  const mark = (f: FileEdit, value: Review) => setReviews(saveReview(reviewKey(turn.id, f.path), value));

  const undo = async (f: FileEdit): Promise<boolean> => {
    const ok = await revert({ path: f.path, before: f.before, after: f.after });
    if (ok) mark(f, "undone");
    return ok;
  };

  const undoAll = async () => {
    setConfirming(false);
    setWorking(true);
    for (const f of open) if (f.undoable) await undo(f);
    setWorking(false);
  };

  const n = edits.length;
  return (
    <div className="files-card edit-card">
      <div className="files-head edit-head">
        <button type="button" className="edit-title" onClick={() => openTool("changes")} title={t("edits.open")}>
          <span>
            {n} {n === 1 ? t("thread.fileChanged") : t("thread.filesChanged")}
          </span>
          <IconChevron className="chevron" />
        </button>
        {canReview && (
          <Tip label={t("review.hint")}>
            <button type="button" className="edit-button" data-action="review" onClick={review}>
              <IconSearch />
              <span>{t(phase === "idle" ? "review.action" : "review.again")}</span>
            </button>
          </Tip>
        )}
        {open.length > 0 && (
          <span className="edit-bulk">
            {confirming ? (
              <>
                <span className="edit-confirm">{t("edits.confirmAll")}</span>
                <button type="button" className="edit-button" data-action="undo-all-confirm" onClick={() => void undoAll()}>
                  {t("edits.undoAll")}
                </button>
                <button type="button" className="edit-button" onClick={() => setConfirming(false)}>
                  {t("common.cancel")}
                </button>
              </>
            ) : (
              <>
                <button type="button" className="edit-button" data-action="undo-all" disabled={working || !open.some((f) => f.undoable)} onClick={() => setConfirming(true)}>
                  <IconUndo />
                  <span>{t("edits.undoAll")}</span>
                </button>
                <button type="button" className="edit-button" data-action="keep-all" disabled={working} onClick={() => open.forEach((f) => mark(f, "kept"))}>
                  {t("edits.keepAll")}
                </button>
              </>
            )}
          </span>
        )}
      </div>
      {edits.map((f) => (
        <Row key={f.path} file={f} state={stateOf(f)} busy={working} onKeep={() => mark(f, "kept")} onUndo={() => void undo(f)} />
      ))}
      <ReviewPanel turnId={turn.id} onRetry={review} />
    </div>
  );
}
