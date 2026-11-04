// The app's select box: a searchable, grouped list in a popover, in place of
// the system's dropdown. Arrows move, Enter picks, Esc closes.

import { Popover } from "radix-ui";
import { useEffect, useMemo, useRef, useState, type KeyboardEvent, type ReactElement, type ReactNode } from "react";
import { IconCheck, IconChevronDown, IconSearch } from "./icons";

export interface PickerOption {
  value: string;
  label: string;
  hint?: string;
  icon?: ReactNode;
  group?: string;
}

interface PickerProps {
  value: string;
  options: PickerOption[];
  onChange(value: string): void;
  label: string;
  placeholder?: string;
  /** Shown in the search box. Without it the list has no search box. */
  searchLabel?: string;
  emptyLabel?: string;
  /** Replaces the default button, for controls that already have a look. */
  trigger?: ReactElement;
  side?: "top" | "bottom";
  align?: "start" | "end";
}

export function Picker({ value, options, onChange, label, placeholder, searchLabel, emptyLabel, trigger, side = "bottom", align = "end" }: PickerProps) {
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const [active, setActive] = useState(0);
  const list = useRef<HTMLDivElement>(null);
  const current = options.find((o) => o.value === value);

  const shown = useMemo(() => {
    const q = query.trim().toLocaleLowerCase();
    if (!q) return options;
    return options.filter((o) => `${o.label} ${o.hint ?? ""} ${o.group ?? ""}`.toLocaleLowerCase().includes(q));
  }, [options, query]);

  useEffect(() => {
    if (!open) return;
    setActive(query ? 0 : Math.max(0, shown.findIndex((o) => o.value === value)));
    // The list follows the query; the current value only places the start.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, query]);

  useEffect(() => {
    if (!open) return;
    const frame = requestAnimationFrame(() => list.current?.querySelector('[data-active="true"]')?.scrollIntoView({ block: "nearest" }));
    return () => cancelAnimationFrame(frame);
  }, [active, open]);

  const pick = (option: PickerOption | undefined) => {
    if (!option) return;
    setOpen(false);
    if (option.value !== value) onChange(option.value);
  };

  const onKey = (e: KeyboardEvent) => {
    if (e.key === "ArrowDown" || e.key === "ArrowUp") {
      e.preventDefault();
      if (shown.length) setActive((active + (e.key === "ArrowDown" ? 1 : -1) + shown.length) % shown.length);
    } else if (e.key === "Enter") {
      e.preventDefault();
      pick(shown[active]);
    }
  };

  let lastGroup: string | undefined;

  return (
    <Popover.Root
      modal
      open={open}
      onOpenChange={(next) => {
        setOpen(next);
        if (next) setQuery("");
      }}
    >
      <Popover.Trigger asChild>
        {trigger ?? (
          <button type="button" className="picker-button" aria-label={label}>
            {current?.icon}
            <span className="picker-value" data-empty={!current || undefined}>
              {current?.label ?? placeholder}
            </span>
            <IconChevronDown />
          </button>
        )}
      </Popover.Trigger>
      <Popover.Portal>
        <Popover.Content className="pop picker" side={side} align={align} sideOffset={6} collisionPadding={{ top: 48, bottom: 12, left: 12, right: 12 }} onKeyDown={onKey} aria-label={label}>
          {searchLabel && (
            <label className="picker-search">
              <IconSearch />
              <input autoFocus value={query} placeholder={searchLabel} aria-label={searchLabel} onChange={(e) => setQuery(e.target.value)} />
            </label>
          )}
          <div className="picker-list" role="listbox" aria-label={label} ref={list} tabIndex={searchLabel ? -1 : 0}>
            {shown.map((option, i) => {
              const heading = option.group !== lastGroup ? option.group : undefined;
              lastGroup = option.group;
              return (
                <div key={`${option.group ?? ""}:${option.value}`}>
                  {heading && <div className="picker-group">{heading}</div>}
                  <div
                    role="option"
                    aria-selected={option.value === value}
                    className="picker-row"
                    data-active={i === active}
                    onMouseMove={() => setActive(i)}
                    onClick={() => pick(option)}
                  >
                    {option.icon}
                    <span className="picker-label">{option.label}</span>
                    {option.hint && <span className="picker-hint">{option.hint}</span>}
                    {option.value === value && <IconCheck className="picker-check" />}
                  </div>
                </div>
              );
            })}
            {shown.length === 0 && <div className="picker-empty">{emptyLabel}</div>}
          </div>
        </Popover.Content>
      </Popover.Portal>
    </Popover.Root>
  );
}
