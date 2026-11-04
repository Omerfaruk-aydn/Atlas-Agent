// Choosing a model: search across providers, see context size and price, set
// the reasoning effort. A model with effort levels offers them right on its row.

import fuzzysort from "fuzzysort";
import { useEffect, useMemo, useRef, useState } from "react";
import type { ModelView, ProviderView } from "../../api/types";
import { t } from "../../lib/i18n";
import { IconCheck, IconSearch } from "../../primitives/icons";
import { Modal } from "../../primitives/ui";
import { useApp } from "../../stores/app";
import { ProviderLogo } from "./ProviderLogo";

interface Row {
  provider: ProviderView;
  model: ModelView;
  search: string;
}

function contextLabel(tokens?: number): string {
  if (!tokens) return "";
  return tokens >= 1_000_000 ? `${(tokens / 1_000_000).toFixed(tokens % 1_000_000 ? 1 : 0)}M` : `${Math.round(tokens / 1000)}k`;
}

function priceLabel(m: ModelView): string {
  if (!m.cost_per_1m_in && !m.cost_per_1m_out) return "";
  return `$${m.cost_per_1m_in ?? 0} / $${m.cost_per_1m_out ?? 0}`;
}

function Effort({ model, value, onPick }: { model: ModelView; value: string; onPick(effort: string): void }) {
  const levels = model.reasoning_levels ?? [];
  if (!model.can_reason || levels.length === 0) return null;
  return (
    <div className="effort" role="radiogroup" aria-label={t("models.effort")}>
      {levels.map((level) => (
        <button
          key={level}
          type="button"
          role="radio"
          aria-checked={level === value}
          onClick={(e) => {
            e.stopPropagation();
            onPick(level);
          }}
        >
          {level}
        </button>
      ))}
    </div>
  );
}

export function ModelPicker() {
  const open = useApp((s) => s.dialog === "models");
  const close = useApp((s) => s.openDialog);
  const catalog = useApp((s) => s.catalog);
  const selectModel = useApp((s) => s.selectModel);
  const openSettings = useApp((s) => s.openSettings);
  const [query, setQuery] = useState("");
  const [selected, setSelected] = useState(0);
  const list = useRef<HTMLDivElement>(null);
  const current = catalog?.active ?? catalog?.selected?.large;

  useEffect(() => {
    if (open) {
      setQuery("");
      setSelected(0);
    }
  }, [open]);

  const rows = useMemo<Row[]>(() => (catalog?.providers ?? []).flatMap((provider) => provider.models.map((model) => ({ provider, model, search: `${model.name} ${provider.name}` }))), [catalog]);

  const shown = useMemo(() => {
    const q = query.trim();
    if (!q) {
      // The current model and its provider come first.
      return [...rows].sort((a, b) => Number(b.provider.id === current?.provider) - Number(a.provider.id === current?.provider));
    }
    return fuzzysort.go(q, rows, { key: "search", limit: 60, threshold: 0.2 }).map((hit) => hit.obj);
  }, [rows, query, current?.provider]);

  // Start on the current model; typing starts over at the best match.
  useEffect(() => {
    if (!open) return;
    const at = query.trim() ? 0 : shown.findIndex((r) => r.provider.id === current?.provider && r.model.id === current?.model);
    setSelected(Math.max(0, at));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [query, open]);
  useEffect(() => {
    list.current?.querySelector('[data-selected="true"]')?.scrollIntoView({ block: "nearest" });
  }, [selected]);

  const pick = (row: Row | undefined, effort?: string) => {
    if (!row) return;
    const level = effort ?? (row.provider.id === current?.provider && row.model.id === current?.model ? current?.reasoning_effort : row.model.default_reasoning_effort);
    close(null);
    void selectModel(row.provider.id, row.model.id, { effort: row.model.reasoning_levels?.length ? level : undefined, think: row.model.can_reason && !row.model.reasoning_levels?.length ? current?.think : undefined });
  };

  let lastProvider = "";

  return (
    <Modal open={open} onClose={() => close(null)} title={t("composer.model")} hideTitle className="palette">
      <label className="palette-input">
        <IconSearch />
        <input
          autoFocus
          value={query}
          placeholder={t("models.search")}
          aria-label={t("models.search")}
          onChange={(e) => setQuery(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "ArrowDown" || e.key === "ArrowUp") {
              e.preventDefault();
              const step = e.key === "ArrowDown" ? 1 : -1;
              if (shown.length) setSelected((selected + step + shown.length) % shown.length);
            } else if (e.key === "Enter") {
              e.preventDefault();
              pick(shown[selected]);
            }
          }}
        />
      </label>
      <div className="palette-list" role="listbox" ref={list}>
        {shown.map((row, i) => {
          const heading = row.provider.id !== lastProvider ? row.provider.name : "";
          lastProvider = row.provider.id;
          const isCurrent = row.provider.id === current?.provider && row.model.id === current?.model;
          return (
            <div key={`${row.provider.id}:${row.model.id}`}>
              {heading && (
                <div className="palette-group model-group">
                  <ProviderLogo id={row.provider.id} size={14} />
                  {heading}
                </div>
              )}
              <div
                role="option"
                aria-selected={i === selected}
                data-selected={i === selected}
                className="palette-row model-row"
                onMouseMove={() => setSelected(i)}
                onClick={() => pick(row)}
              >
                <span className="palette-title">{row.model.name}</span>
                {isCurrent && <Effort model={row.model} value={current?.reasoning_effort || row.model.default_reasoning_effort || ""} onPick={(e) => pick(row, e)} />}
                <span className="palette-hint model-facts">
                  {row.model.supports_images && <span>{t("models.images")}</span>}
                  {contextLabel(row.model.context_window) && <span>{contextLabel(row.model.context_window)}</span>}
                  {priceLabel(row.model) && <span>{priceLabel(row.model)}</span>}
                </span>
                {isCurrent && <IconCheck />}
              </div>
            </div>
          );
        })}
        {shown.length === 0 && <div className="palette-empty">{rows.length === 0 ? t("models.none") : t("palette.empty")}</div>}
      </div>
      <div className="palette-foot">
        <button type="button" className="link-button" onClick={() => openSettings("models")}>
          {t("models.connect")}
        </button>
      </div>
    </Modal>
  );
}
