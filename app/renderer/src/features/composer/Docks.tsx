// When the agent needs an answer, the question takes the composer's place, so
// nothing can be typed past it. The draft is kept and comes back afterwards.

import { useEffect, useRef, useState, type KeyboardEvent } from "react";
import type { PermissionRequest, QuestionItem, QuestionRequest, QuestionResponse } from "../../api/types";
import { t } from "../../lib/i18n";
import { toolVerb } from "../../lib/tools";
import { IconShield } from "../../primitives/icons";
import { Kbd } from "../../primitives/ui";
import { useApp } from "../../stores/app";

function paramLine(req: PermissionRequest): string {
  const params = req.params as Record<string, unknown> | null;
  const pick = params?.command ?? params?.file_path ?? params?.url ?? params?.path;
  return typeof pick === "string" ? pick : req.path;
}

export function PermissionDock({ request }: { request: PermissionRequest }) {
  const answer = useApp((s) => s.answerPermission);
  const line = paramLine(request);
  const primary = useRef<HTMLButtonElement>(null);

  // Keyboard first: the dock takes focus and answers Enter, Ctrl+Enter and Esc.
  useEffect(() => primary.current?.focus(), []);

  const onKey = (e: KeyboardEvent<HTMLDivElement>) => {
    if (e.key === "Escape") {
      e.preventDefault();
      e.stopPropagation();
      void answer(request.id, "deny");
    } else if (e.key === "Enter" && e.ctrlKey) {
      e.preventDefault();
      void answer(request.id, "allow_session");
    }
  };

  return (
    <div className="dock" role="alertdialog" aria-label={t("perm.title")} onKeyDown={onKey}>
      <div className="dock-head">
        <IconShield />
        <span>{t("perm.title")}</span>
        <span className="dock-tool">{toolVerb(request.tool_name, false)}</span>
      </div>
      {request.description && <div className="dock-text">{request.description}</div>}
      {line && !request.description.includes(line) && <pre className="dock-code">{line}</pre>}
      <div className="dock-actions">
        <button type="button" className="button" onClick={() => void answer(request.id, "deny")}>
          {t("perm.deny")}
          <Kbd keys="Esc" />
        </button>
        <span className="composer-gap" />
        <button type="button" className="button" onClick={() => void answer(request.id, "allow_session")}>
          {t("perm.allowSession")}
          <Kbd keys="Ctrl+Enter" />
        </button>
        <button ref={primary} type="button" className="button" data-variant="primary" onClick={() => void answer(request.id, "allow")}>
          {t("perm.allow")}
          <Kbd keys="Enter" />
        </button>
      </div>
    </div>
  );
}

function Question({ item, value, onChange }: { item: QuestionItem; value: QuestionResponse; onChange(next: QuestionResponse): void }) {
  const selected = value.selected_ids ?? [];
  const multi = item.type === "multi_choice";
  return (
    <div className="question">
      <div className="dock-text">{item.question}</div>
      {item.description && <div className="dock-hint">{item.description}</div>}
      {item.type === "yes_no" && (
        <div className="choices">
          {[true, false].map((yes) => (
            <button key={String(yes)} type="button" className="choice" aria-pressed={value.yes === yes} onClick={() => onChange({ ...value, yes })}>
              {yes ? t("question.yes") : t("question.no")}
            </button>
          ))}
        </div>
      )}
      {(item.choices?.length ?? 0) > 0 && (
        <div className="choices">
          {item.choices?.map((choice) => (
            <button
              key={choice.id}
              type="button"
              className="choice"
              title={choice.description}
              aria-pressed={selected.includes(choice.id)}
              onClick={() => {
                const next = multi ? (selected.includes(choice.id) ? selected.filter((id) => id !== choice.id) : [...selected, choice.id]) : [choice.id];
                onChange({ ...value, selected_ids: next });
              }}
            >
              {choice.label}
            </button>
          ))}
        </div>
      )}
      {item.type === "free_text" && (
        <textarea
          className="question-input"
          rows={2}
          value={value.fill_in_text ?? ""}
          placeholder={t("question.placeholder")}
          onChange={(e) => onChange({ ...value, fill_in_text: e.target.value })}
        />
      )}
    </div>
  );
}

function answered(item: QuestionItem, r: QuestionResponse): boolean {
  if (item.type === "yes_no") return r.yes !== undefined;
  if (item.type === "free_text") return !!r.fill_in_text?.trim();
  return (r.selected_ids?.length ?? 0) > 0;
}

export function QuestionDock({ request }: { request: QuestionRequest }) {
  const answer = useApp((s) => s.answerQuestion);
  const skip = useApp((s) => s.cancelQuestion);
  const [responses, setResponses] = useState<Record<string, QuestionResponse>>({});
  const valueOf = (item: QuestionItem) => responses[item.id] ?? { request_id: item.id };
  const ready = request.questions.every((item) => answered(item, valueOf(item)));

  return (
    <div className="dock" role="dialog" aria-label={request.confirm_title ?? t("question.submit")}>
      {request.confirm_title && <div className="dock-head">{request.confirm_title}</div>}
      {request.questions.map((item) => (
        <Question key={item.id} item={item} value={valueOf(item)} onChange={(next) => setResponses({ ...responses, [item.id]: next })} />
      ))}
      <div className="dock-actions">
        <button type="button" className="button" onClick={() => void skip(request.id)}>
          {t("question.skip")}
        </button>
        <span className="composer-gap" />
        <button
          type="button"
          className="button"
          data-variant="primary"
          disabled={!ready}
          onClick={() => void answer(request.id, request.questions.map(valueOf))}
        >
          {t("question.submit")}
        </button>
      </div>
    </div>
  );
}
