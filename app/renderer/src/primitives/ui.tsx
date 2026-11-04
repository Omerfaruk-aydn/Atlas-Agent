// The app's shared controls, built on Radix for focus, keyboard and screen
// reader behaviour. Styles live in design/surfaces.css.

import { Dialog, DropdownMenu, ContextMenu, HoverCard, Popover, Switch as RadixSwitch, Tooltip as RadixTooltip } from "radix-ui";
import { useCallback, useEffect, useLayoutEffect, useRef, type PointerEvent as ReactPointerEvent, type ReactElement, type ReactNode } from "react";
import { displayKeys } from "../lib/shortcuts";
import { useApp } from "../stores/app";

export function Kbd({ keys }: { keys: string }) {
  return (
    <span className="kbds">
      {displayKeys(keys)
        .split("+")
        .map((k, i) => (
          <kbd key={i}>{k}</kbd>
        ))}
    </span>
  );
}

export function Tip({ label, keys, children }: { label: string; keys?: string; children: ReactElement }) {
  return (
    <RadixTooltip.Root>
      <RadixTooltip.Trigger asChild>{children}</RadixTooltip.Trigger>
      <RadixTooltip.Portal>
        <RadixTooltip.Content className="tip" sideOffset={6} collisionPadding={8}>
          {label}
          {keys && <Kbd keys={keys} />}
        </RadixTooltip.Content>
      </RadixTooltip.Portal>
    </RadixTooltip.Root>
  );
}

export const TipProvider = RadixTooltip.Provider;

interface ModalProps {
  open: boolean;
  onClose(): void;
  title: string;
  description?: string;
  className?: string;
  hideTitle?: boolean;
  children: ReactNode;
}

export function Modal({ open, onClose, title, description, className, hideTitle, children }: ModalProps) {
  // Opened from a shortcut there is no trigger to return to, so focus goes
  // back to wherever it was, or else to the message box.
  const back = useRef<HTMLElement | null>(null);
  useLayoutEffect(() => {
    if (open) back.current = document.activeElement as HTMLElement | null;
  }, [open]);
  const restore = (e: Event) => {
    e.preventDefault();
    const target = back.current?.isConnected && back.current !== document.body ? back.current : document.querySelector<HTMLElement>(".composer textarea");
    target?.focus();
  };
  return (
    <Dialog.Root open={open} onOpenChange={(next) => !next && onClose()}>
      <Dialog.Portal>
        <Dialog.Overlay className="scrim" />
        <Dialog.Content className={`modal ${className ?? ""}`} onCloseAutoFocus={restore}>
          <Dialog.Title className={hideTitle ? "sr-only" : "modal-title"}>{title}</Dialog.Title>
          {description ? <Dialog.Description className="modal-text">{description}</Dialog.Description> : <Dialog.Description className="sr-only">{title}</Dialog.Description>}
          {children}
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}

export interface MenuEntry {
  label: string;
  icon?: ReactNode;
  keys?: string;
  danger?: boolean;
  checked?: boolean;
  hint?: string;
  onSelect(): void;
}

function row(entry: MenuEntry) {
  return (
    <>
      {entry.icon}
      <span className="menu-text">
        <span>{entry.label}</span>
        {entry.hint && <span className="menu-hint">{entry.hint}</span>}
      </span>
      {entry.keys && <Kbd keys={entry.keys} />}
      {entry.checked && <span className="menu-check" aria-hidden="true">✓</span>}
    </>
  );
}

export function Menu({ trigger, items, side = "top", align = "start" }: { trigger: ReactElement; items: MenuEntry[]; side?: "top" | "bottom"; align?: "start" | "end" }) {
  return (
    <DropdownMenu.Root>
      <DropdownMenu.Trigger asChild>{trigger}</DropdownMenu.Trigger>
      <DropdownMenu.Portal>
        <DropdownMenu.Content className="menu" side={side} align={align} sideOffset={6} collisionPadding={{ top: 48, bottom: 12, left: 8, right: 8 }}>
          {items.map((entry) => (
            <DropdownMenu.Item key={entry.label} className="menu-item" data-danger={entry.danger || undefined} onSelect={entry.onSelect}>
              {row(entry)}
            </DropdownMenu.Item>
          ))}
        </DropdownMenu.Content>
      </DropdownMenu.Portal>
    </DropdownMenu.Root>
  );
}

export function RightClick({ items, children }: { items: MenuEntry[]; children: ReactElement }) {
  return (
    <ContextMenu.Root>
      <ContextMenu.Trigger asChild>{children}</ContextMenu.Trigger>
      <ContextMenu.Portal>
        <ContextMenu.Content className="menu" collisionPadding={{ top: 48, bottom: 12, left: 8, right: 8 }}>
          {items.map((entry) => (
            <ContextMenu.Item key={entry.label} className="menu-item" data-danger={entry.danger || undefined} onSelect={entry.onSelect}>
              {row(entry)}
            </ContextMenu.Item>
          ))}
        </ContextMenu.Content>
      </ContextMenu.Portal>
    </ContextMenu.Root>
  );
}

export function Pop({ trigger, children, side = "top", align = "start", className }: { trigger: ReactElement; children: ReactNode; side?: "top" | "bottom"; align?: "start" | "end"; className?: string }) {
  return (
    <Popover.Root>
      <Popover.Trigger asChild>{trigger}</Popover.Trigger>
      <Popover.Portal>
        <Popover.Content className={`pop ${className ?? ""}`} side={side} align={align} sideOffset={6} collisionPadding={8}>
          {children}
        </Popover.Content>
      </Popover.Portal>
    </Popover.Root>
  );
}

export const PopClose = Popover.Close;

export function Hover({ card, children }: { card: ReactNode; children: ReactElement }) {
  return (
    <HoverCard.Root openDelay={450} closeDelay={80}>
      <HoverCard.Trigger asChild>{children}</HoverCard.Trigger>
      <HoverCard.Portal>
        <HoverCard.Content className="pop hover-card" side="right" align="start" sideOffset={10} collisionPadding={8}>
          {card}
        </HoverCard.Content>
      </HoverCard.Portal>
    </HoverCard.Root>
  );
}

export function Switch({ checked, onChange, label }: { checked: boolean; onChange(next: boolean): void; label: string }) {
  return (
    <RadixSwitch.Root className="switch" checked={checked} onCheckedChange={onChange} aria-label={label}>
      <RadixSwitch.Thumb className="switch-thumb" />
    </RadixSwitch.Root>
  );
}

export function Segmented<T extends string>({ value, options, onChange, label }: { value: T; options: { value: T; label: string }[]; onChange(next: T): void; label: string }) {
  return (
    <div className="segmented" role="radiogroup" aria-label={label}>
      {options.map((o) => (
        <button key={o.value} type="button" role="radio" aria-checked={o.value === value} onClick={() => onChange(o.value)}>
          {o.label}
        </button>
      ))}
    </div>
  );
}

export function Toasts() {
  const toasts = useApp((s) => s.toasts);
  const drop = useApp((s) => s.dropToast);
  return (
    <div className="toasts" role="status" aria-live="polite">
      {toasts.map((toast) => (
        <button key={toast.id} type="button" className="toast" data-tone={toast.tone} onClick={() => drop(toast.id)}>
          {toast.text}
        </button>
      ))}
    </div>
  );
}

/** A drag handle that sets a width and reports the final value. Double click resets it. */
export function ResizeHandle({ side, min, max, initial, cssVar, onDone }: { side: "left" | "right"; min: number; max: number; initial: number; cssVar: string; onDone(width: number): void }) {
  const start = useRef<{ x: number; width: number } | null>(null);
  const width = useRef(0);

  const apply = useCallback(
    (value: number) => {
      width.current = value;
      document.documentElement.style.setProperty(cssVar, `${value}px`);
    },
    [cssVar],
  );

  const read = () => parseFloat(getComputedStyle(document.documentElement).getPropertyValue(cssVar)) || initial;

  const onDown = (e: ReactPointerEvent<HTMLDivElement>) => {
    e.currentTarget.setPointerCapture(e.pointerId);
    start.current = { x: e.clientX, width: read() };
    document.documentElement.dataset.resizing = "";
  };
  const onMove = (e: ReactPointerEvent<HTMLDivElement>) => {
    if (!start.current) return;
    const delta = e.clientX - start.current.x;
    apply(Math.min(max, Math.max(min, start.current.width + (side === "left" ? delta : -delta))));
  };
  const onUp = () => {
    if (!start.current) return;
    start.current = null;
    delete document.documentElement.dataset.resizing;
    onDone(width.current || read());
  };

  return (
    <div
      className="resize-handle"
      data-side={side}
      role="separator"
      aria-orientation="vertical"
      onPointerDown={onDown}
      onPointerMove={onMove}
      onPointerUp={onUp}
      onDoubleClick={() => {
        document.documentElement.style.removeProperty(cssVar);
        onDone(side === "left" ? 264 : 380);
      }}
    />
  );
}

/** Calls back after a pause in `value`, for search boxes. */
export function useDebounced<T>(value: T, ms: number, callback: (value: T) => void) {
  const latest = useRef(callback);
  latest.current = callback;
  useEffect(() => {
    const timer = setTimeout(() => latest.current(value), ms);
    return () => clearTimeout(timer);
  }, [value, ms]);
}
