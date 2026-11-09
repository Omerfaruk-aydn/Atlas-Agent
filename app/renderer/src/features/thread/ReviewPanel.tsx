// The outcome of a review, under the files it was about: a verdict line, the
// findings worst first, and for each one a way to hand it to the agent that
// made the changes or to set it aside. Everything the person decides is kept
// on this computer, per turn.

import { useState } from "react";
import type { ReviewFinding } from "../../api/types";
import { clock } from "../../lib/format";
import { t } from "../../lib/i18n";
import { countBySeverity, findingKey, locationOf, severityRank } from "../../lib/review";
import { IconAlert, IconChevron, IconCheck, IconClose } from "../../primitives/icons";
import { Collapse, Shimmer, ThinkingDots, useElapsed } from "../../primitives/motion";
import { Tip } from "../../primitives/ui";
import { useReview, type Entry } from "../../stores/review";
import { usePanel } from "../../stores/panel";
import { Markdown } from "./Markdown";

function Running({ turnId, entry }: { turnId: string; entry: Entry }) {
  const cancel = useReview((s) => s.cancel);
  const elapsed = useElapsed(entry.review?.started_at ?? Date.now());
  return (
    <div className="review-state" role="status">
      <ThinkingDots />
      <Shimmer text={entry.phase === "starting" ? t("review.starting") : t("review.running")} />
      {elapsed >= 2000 && <span className="working-time">{clock(elapsed)}</span>}
      <button type="button" className="edit-button review-end" data-action="review-cancel" onClick={() => void cancel(turnId)}>
        {t("review.cancel")}
      </button>
    </div>
  );
}

function Finding({ turnId, finding, sent }: { turnId: string; finding: ReviewFinding; sent: boolean }) {
  const [open, setOpen] = useState(finding.severity === "critical" || finding.severity === "high");
  const fix = useReview((s) => s.fix);
  const dismiss = useReview((s) => s.dismiss);
  const openFile = usePanel((s) => s.openFile);
  const where = locationOf(finding);
  const detail = !!finding.explanation || !!finding.suggestion;
  return (
    <div className="finding" data-severity={finding.severity} data-sent={sent || undefined}>
      <div className="finding-head">
        <button type="button" className="finding-toggle" aria-expanded={open} disabled={!detail} onClick={() => setOpen(!open)}>
          <span className="finding-sev" title={t(`review.sev.${finding.severity}`)}>
            <span className="finding-dot" aria-hidden="true" />
            <span className="finding-sev-text">{t(`review.sev.${finding.severity}`)}</span>
          </span>
          <span className="finding-title">{finding.title}</span>
          {detail && <IconChevron className="chevron" />}
        </button>
        <span className="finding-actions">
          {sent ? (
            <span className="finding-sent" role="status">
              <IconCheck /> {t("review.sent")}
            </span>
          ) : (
            <>
              <button type="button" className="edit-button" data-action="fix" onClick={() => void fix(turnId, [finding])}>
                {t("review.fix")}
              </button>
              <Tip label={t("review.dismiss")}>
                <button type="button" className="icon-button" data-size="xs" data-action="dismiss" aria-label={t("review.dismiss")} onClick={() => dismiss(turnId, finding)}>
                  <IconClose />
                </button>
              </Tip>
            </>
          )}
        </span>
      </div>
      {where && (
        <button type="button" className="finding-where" title={t("review.openFile")} onClick={() => finding.file && openFile(finding.file)}>
          {where}
        </button>
      )}
      <Collapse open={open && detail}>
        {open && detail && (
          <div className="finding-body">
            {finding.explanation && <p>{finding.explanation}</p>}
            {finding.suggestion && (
              <div className="finding-fix">
                <span className="finding-fix-label">{t("review.suggestion")}</span>
                <Markdown text={finding.suggestion} streaming={false} />
              </div>
            )}
          </div>
        )}
      </Collapse>
    </div>
  );
}

function Done({ turnId, entry }: { turnId: string; entry: Entry }) {
  const review = entry.review;
  const fix = useReview((s) => s.fix);
  const restore = useReview((s) => s.restore);
  if (!review) return null;
  if (!review.structured) {
    return (
      <div className="review-body">
        <div className="review-note">{t("review.unstructured")}</div>
        <Markdown text={review.summary ?? ""} streaming={false} />
      </div>
    );
  }
  const hidden = new Set(entry.dismissed);
  const shown = [...review.findings].sort((a, b) => severityRank(a.severity) - severityRank(b.severity)).filter((f) => !hidden.has(findingKey(f)));
  const dismissed = review.findings.length - shown.length;
  const open = shown.filter((f) => !entry.sent.includes(findingKey(f)));
  const counts = countBySeverity(shown);
  return (
    <div className="review-body">
      <div className="review-verdict">
        {shown.length === 0 ? (
          <span className="review-clean">
            <IconCheck /> {t("review.clean")}
          </span>
        ) : (
          <span className="review-counts" aria-label={t("review.count").replace("{0}", String(shown.length))}>
            {counts.map((c) => (
              <span key={c.severity} className="review-count" data-severity={c.severity}>
                <span className="finding-dot" aria-hidden="true" /> {c.count} {t(`review.sev.${c.severity}`)}
              </span>
            ))}
          </span>
        )}
        {open.length > 1 && (
          <button type="button" className="edit-button review-fix-all" data-action="fix-all" onClick={() => void fix(turnId, open)}>
            {t("review.fixAll").replace("{0}", String(open.length))}
          </button>
        )}
      </div>
      {review.summary && <p className="review-summary">{review.summary}</p>}
      {shown.length > 0 && (
        <div className="findings">
          {shown.map((f) => (
            <Finding key={findingKey(f)} turnId={turnId} finding={f} sent={entry.sent.includes(findingKey(f))} />
          ))}
        </div>
      )}
      <div className="review-foot">
        {dismissed > 0 && (
          <span>
            {t("review.dismissedCount").replace("{0}", String(dismissed))}{" "}
            <button type="button" className="link-button" onClick={() => restore(turnId)}>
              {t("review.showDismissed")}
            </button>
          </span>
        )}
        {review.model && <span className="review-model">{t("review.model").replace("{0}", review.model)}</span>}
      </div>
    </div>
  );
}

export function ReviewPanel({ turnId, onRetry }: { turnId: string; onRetry(): void }) {
  const entry = useReview((s) => s.byTurn[turnId]);
  if (!entry || entry.phase === "idle") return null;
  return (
    <section className="review" aria-label={t("review.title")} data-phase={entry.phase}>
      {(entry.phase === "starting" || entry.phase === "running") && <Running turnId={turnId} entry={entry} />}
      {entry.phase === "failed" && (
        <div className="review-state" role="alert">
          <IconAlert />
          <div className="review-error">
            <div>{t("review.failed")}</div>
            {entry.error && <div className="turn-error-details">{entry.error}</div>}
          </div>
          <button type="button" className="edit-button review-end" onClick={onRetry}>
            {t("review.retry")}
          </button>
        </div>
      )}
      {entry.phase === "canceled" && (
        <div className="review-state" role="status">
          <span className="review-note">{t("review.stopped")}</span>
          <button type="button" className="edit-button review-end" onClick={onRetry}>
            {t("review.retry")}
          </button>
        </div>
      )}
      {entry.phase === "done" && <Done turnId={turnId} entry={entry} />}
    </section>
  );
}
