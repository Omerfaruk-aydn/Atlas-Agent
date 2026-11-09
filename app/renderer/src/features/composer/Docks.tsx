// When the agent needs an answer, the question takes the composer's place, so
// nothing can be typed past it. The draft is kept and comes back afterwards.

import { useEffect, useRef, useState, type CSSProperties, type KeyboardEvent } from "react";
import type { PermissionRequest, QuestionItem, QuestionRequest, QuestionResponse } from "../../api/types";
import { t } from "../../lib/i18n";
import { permissionSentence } from "../../lib/chat";
import { toolVerb } from "../../lib/tools";
import { IconShield } from "../../primitives/icons";
import { Kbd } from "../../primitives/ui";
import { useApp } from "../../stores/app";

export function PermissionDock({ request }: { request: PermissionRequest }) {
  const answer = useApp((s) => s.answerPermission);
  const waiting = useApp((s) => s.permissions.filter((p) => p.session_id === request.session_id).length);
  const sentence = permissionSentence(request);
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
    <div className="dock permission-dock" role="alertdialog" aria-label={sentence.title} aria-describedby={`${request.id}-scope`} onKeyDown={onKey}>
      <div className="dock-head">
        <IconShield />
        <span className="permission-title">{sentence.title}</span>
        <span className="dock-tool">{toolVerb(request.tool_name, false)}</span>
        {waiting > 1 && <span className="permission-more">+{waiting - 1}</span>}
      </div>
      {sentence.detail && <pre className="dock-code">{sentence.detail}</pre>}
      {request.description && !sentence.detail.includes(request.description) && request.description !== sentence.title && <div className="dock-hint">{request.description}</div>}
      <div className="dock-actions">
        <button type="button" className="button" data-action="deny" onClick={() => void answer(request.id, "deny")}>
          {t("approve.deny")}
          <Kbd keys="Esc" />
        </button>
        <span className="composer-gap" />
        <button type="button" className="button" data-action="always" onClick={() => void answer(request.id, "allow_session")}>
          {t("approve.always")}
          <Kbd keys="Ctrl+Enter" />
        </button>
        <button ref={primary} type="button" className="button" data-variant="primary" data-action="once" onClick={() => void answer(request.id, "allow")}>
          {t("approve.once")}
          <Kbd keys="Enter" />
        </button>
      </div>
      <div className="dock-scope" id={`${request.id}-scope`}>
        {sentence.scope}
        {request.path && <span className="dock-path">{request.path}</span>}
      </div>
    </div>
  );
}

function Question({ item, value, onChange, index, total }: { item: QuestionItem; value: QuestionResponse; onChange(next: QuestionResponse): void; index: number; total: number }) {
  const selected = value.selected_ids ?? [];
  const multi = item.type === "multi_choice";
  return (
    <div className="question" style={{ "--i": index } as CSSProperties}>
      {total > 1 && <div className="question-step">{t("question.step").replace("{0}", String(index + 1)).replace("{1}", String(total))}</div>}
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
      {request.questions.map((item, i) => (
        <Question key={item.id} item={item} index={i} total={request.questions.length} value={valueOf(item)} onChange={(next) => setResponses({ ...responses, [item.id]: next })} />
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
