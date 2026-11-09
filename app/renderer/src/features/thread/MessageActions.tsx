// What the reader can do with an answer: copy it as text or as Markdown, rate
// it, run it again, or branch the chat from it. The row shows on hover and on
// keyboard focus. A copy is announced in a live region, not only by the icon.

import { useEffect, useRef, useState } from "react";
import { loadFeedback, plainText, saveFeedback, type Feedback } from "../../lib/chat";
import { t } from "../../lib/i18n";
import { IconBranch, IconCheck, IconCopy, IconRetry } from "../../primitives/icons";
import { Tip } from "../../primitives/ui";
import { IconMarkdown, IconThumbDown, IconThumbUp } from "./chatIcons";

const COPIED_MS = 1400;

interface Props {
  /** The id of the answer's last message: what ratings and branches attach to. */
  messageId: string;
  /** The answer as the model wrote it. */
  markdown: string;
  /** Present on the newest answer of an idle chat. */
  onRegenerate?(): void;
  /** Present when the chat can branch from this answer. */
  onBranch?(): void;
}

export function MessageActions({ messageId, markdown, onRegenerate, onBranch }: Props) {
  const [copied, setCopied] = useState<"text" | "markdown" | null>(null);
  const [rating, setRating] = useState<Feedback | undefined>(() => loadFeedback()[messageId]);
  const timer = useRef(0);
  useEffect(() => () => clearTimeout(timer.current), []);

  const copy = (kind: "text" | "markdown") => {
    void navigator.clipboard.writeText(kind === "text" ? plainText(markdown) : markdown).then(() => {
      setCopied(kind);
      clearTimeout(timer.current);
      timer.current = window.setTimeout(() => setCopied(null), COPIED_MS);
    });
  };

  const rate = (value: Feedback) => {
    const next = rating === value ? null : value;
    saveFeedback(messageId, next);
    setRating(next ?? undefined);
  };

  return (
    <div className="msg-actions" role="group" aria-label={t("msg.actions")}>
      <Tip label={t("msg.copyText")}>
        <button type="button" className="icon-button" data-size="sm" aria-label={t("msg.copyText")} data-action="copy-text" onClick={() => copy("text")}>
          {copied === "text" ? <IconCheck key="check" className="icon-in" /> : <IconCopy key="copy" className="icon-in" />}
        </button>
      </Tip>
      <Tip label={t("msg.copyMarkdown")}>
        <button type="button" className="icon-button" data-size="sm" aria-label={t("msg.copyMarkdown")} data-action="copy-markdown" onClick={() => copy("markdown")}>
          {copied === "markdown" ? <IconCheck key="check" className="icon-in" /> : <IconMarkdown key="md" className="icon-in" />}
        </button>
      </Tip>
      <Tip label={t("msg.good")}>
        <button type="button" className="icon-button" data-size="sm" data-action="good" data-on={rating === "up" || undefined} aria-pressed={rating === "up"} aria-label={t("msg.good")} onClick={() => rate("up")}>
          <IconThumbUp />
        </button>
      </Tip>
      <Tip label={t("msg.bad")}>
        <button type="button" className="icon-button" data-size="sm" data-action="bad" data-on={rating === "down" || undefined} aria-pressed={rating === "down"} aria-label={t("msg.bad")} onClick={() => rate("down")}>
          <IconThumbDown />
        </button>
      </Tip>
      {onRegenerate && (
        <Tip label={t("msg.regenerate")}>
          <button type="button" className="icon-button" data-size="sm" data-action="regenerate" aria-label={t("msg.regenerate")} onClick={onRegenerate}>
            <IconRetry />
          </button>
        </Tip>
      )}
      {onBranch && (
        <Tip label={t("msg.branch")}>
          <button type="button" className="icon-button" data-size="sm" data-action="branch" aria-label={t("msg.branch")} onClick={onBranch}>
            <IconBranch />
          </button>
        </Tip>
      )}
      <span className="sr-only" role="status" aria-live="polite">
        {copied ? t("thread.copied") : ""}
      </span>
    </div>
  );
}
