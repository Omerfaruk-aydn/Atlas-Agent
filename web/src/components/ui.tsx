// UI primitives. Buttons, icons, tooltips, switches and the spinner come
// from Atlas Agent's component library (vendored under src/vendor); dialogs,
// menus and toasts are small Atlas wrappers over Kobalte.

import { Dialog as KDialog } from "@kobalte/core/dialog"
import { DropdownMenu } from "@kobalte/core/dropdown-menu"
import { createSignal, For, type JSX, Show, splitProps } from "solid-js"
import { t } from "../i18n"

export { Button } from "../vendor/atlas-ui/actions/button/button"
export { IconButton } from "../vendor/atlas-ui/actions/icon-button/icon-button"
export { Icon } from "../vendor/atlas-ui/icons/icon/icon"
export { Tooltip } from "../vendor/atlas-ui/overlays/tooltip/tooltip"
export { Switch } from "../vendor/atlas-ui/forms/switch/switch"
export { Spinner } from "../vendor/atlas-ui/components/spinner"
export { ResizeHandle } from "../vendor/atlas-ui/components/resize-handle"
export { DiffChanges } from "../vendor/atlas-ui/data-display/diff-changes/diff-changes"

// Icons the vendored catalog lacks. 16×16, stroked with currentColor.
const extra = {
  send: `<path d="M8 13V3M3.5 7.5 8 3l4.5 4.5" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" stroke-linejoin="round"/>`,
  stop: `<rect x="4" y="4" width="8" height="8" rx="1.5" fill="currentColor"/>`,
  paperclip: `<path d="M10.5 4.5 5.8 9.2a1.5 1.5 0 0 0 2.1 2.1l5-5a3 3 0 0 0-4.2-4.2l-5.2 5.2a4.5 4.5 0 0 0 6.4 6.4L13.5 10" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"/>`,
  terminal: `<rect x="1.5" y="2.5" width="13" height="11" rx="1.5" stroke="currentColor"/><path d="m4.5 6 2 2-2 2M8 10.5h3.5" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"/>`,
  file: `<path d="M3.5 1.5h6l3 3v10h-9z" stroke="currentColor" stroke-linejoin="round"/><path d="M9.5 1.5v3h3" stroke="currentColor" stroke-linejoin="round"/>`,
  image: `<rect x="1.5" y="2.5" width="13" height="11" rx="1.5" stroke="currentColor"/><circle cx="5.5" cy="6.5" r="1.25" stroke="currentColor"/><path d="m2 12 3.5-3.5 2.5 2.5 2.5-3 3.5 4" stroke="currentColor" stroke-linejoin="round"/>`,
  sun: `<circle cx="8" cy="8" r="3" stroke="currentColor"/><path d="M8 1v1.5M8 13.5V15M1 8h1.5M13.5 8H15M3 3l1 1M12 12l1 1M3 13l1-1M12 4l1-1" stroke="currentColor" stroke-linecap="round"/>`,
  moon: `<path d="M13.5 9.5A5.5 5.5 0 0 1 6.5 2.5a5.5 5.5 0 1 0 7 7Z" stroke="currentColor" stroke-linejoin="round"/>`,
  bolt: `<path d="M9 1.5 3.5 9H8l-1 5.5L12.5 7H8z" stroke="currentColor" stroke-linejoin="round"/>`,
  question: `<circle cx="8" cy="8" r="6.5" stroke="currentColor"/><path d="M6.2 6.2a1.9 1.9 0 1 1 2.6 1.8c-.5.2-.8.6-.8 1.1v.4M8 11.6v.1" stroke="currentColor" stroke-linecap="round"/>`,
  shield: `<path d="M8 1.5 2.5 3.5V8c0 3 2.4 5.6 5.5 6.5 3.1-.9 5.5-3.5 5.5-6.5V3.5z" stroke="currentColor" stroke-linejoin="round"/>`,
  robot: `<rect x="2.5" y="5" width="11" height="8" rx="2" stroke="currentColor"/><path d="M8 2.5V5M5.75 9h.01M10.25 9h.01" stroke="currentColor" stroke-linecap="round" stroke-width="1.5"/>`,
  pipeline: `<circle cx="3.5" cy="8" r="2" stroke="currentColor"/><circle cx="12.5" cy="3.5" r="2" stroke="currentColor"/><circle cx="12.5" cy="12.5" r="2" stroke="currentColor"/><path d="M5.5 8h2.5m0 0V4.5c0-.6.4-1 1-1h1.5M8 8v3.5c0 .6.4 1 1 1h1.5" stroke="currentColor"/>`,
  cursor: `<path d="m3 2 9 4.5-4 1.2L6.5 12z" stroke="currentColor" stroke-linejoin="round"/>`,
  brain: `<path d="M6 2.5a2 2 0 0 0-2 2 2 2 0 0 0-1.5 3.3A2.3 2.3 0 0 0 4 12a2 2 0 0 0 4 .5V3.8A1.8 1.8 0 0 0 6 2.5Zm4 0a2 2 0 0 1 2 2 2 2 0 0 1 1.5 3.3A2.3 2.3 0 0 1 12 12a2 2 0 0 1-4 .5" stroke="currentColor" stroke-linejoin="round"/>`,
  sidebar: `<rect x="1.5" y="2.5" width="13" height="11" rx="1.5" stroke="currentColor"/><path d="M6 2.5v11" stroke="currentColor"/>`,
} as const

export type ExtraIconName = keyof typeof extra

export function AIcon(props: { name: ExtraIconName; size?: number; class?: string; label?: string }) {
  const size = () => props.size ?? 16
  return (
    <svg
      class={props.class}
      width={size()}
      height={size()}
      viewBox="0 0 16 16"
      fill="none"
      role={props.label ? "img" : undefined}
      aria-label={props.label}
      aria-hidden={props.label ? undefined : "true"}
      // The artwork is a fixed constant above, never user content.
      innerHTML={extra[props.name]}
    />
  )
}

/** Dialog is a modal with a title, focus trap and Escape to close. */
export function Dialog(props: {
  open: boolean
  onOpenChange: (open: boolean) => void
  title: string
  description?: string
  size?: "small" | "medium" | "large"
  children: JSX.Element
  footer?: JSX.Element
}) {
  return (
    <KDialog open={props.open} onOpenChange={props.onOpenChange} modal>
      <KDialog.Portal>
        <KDialog.Overlay class="dialog-overlay" />
        <div class="dialog-positioner">
          <KDialog.Content class="dialog" data-size={props.size ?? "medium"}>
            <header class="dialog-header">
              <KDialog.Title class="dialog-title">{props.title}</KDialog.Title>
              <KDialog.CloseButton class="ghost-icon" aria-label={t("common.close")}>
                <span aria-hidden="true">×</span>
              </KDialog.CloseButton>
            </header>
            <Show when={props.description}>
              <KDialog.Description class="dialog-description">{props.description}</KDialog.Description>
            </Show>
            <div class="dialog-body">{props.children}</div>
            <Show when={props.footer}>
              <footer class="dialog-footer">{props.footer}</footer>
            </Show>
          </KDialog.Content>
        </div>
      </KDialog.Portal>
    </KDialog>
  )
}

export interface MenuItem {
  label: string
  onSelect: () => void
  danger?: boolean
  disabled?: boolean
}

/** Menu is a keyboard-accessible dropdown of actions. */
export function Menu(props: { label: string; items: MenuItem[]; trigger: JSX.Element; class?: string }) {
  return (
    <DropdownMenu gutter={4} placement="bottom-end">
      <DropdownMenu.Trigger class={props.class ?? "ghost-icon"} aria-label={props.label}>
        {props.trigger}
      </DropdownMenu.Trigger>
      <DropdownMenu.Portal>
        <DropdownMenu.Content class="menu">
          <For each={props.items}>
            {(item) => (
              <DropdownMenu.Item class="menu-item" data-danger={item.danger ? "" : undefined} disabled={item.disabled} onSelect={item.onSelect}>
                {item.label}
              </DropdownMenu.Item>
            )}
          </For>
        </DropdownMenu.Content>
      </DropdownMenu.Portal>
    </DropdownMenu>
  )
}

// --- Toasts ---

export interface Toast {
  id: number
  kind: "info" | "error" | "success"
  text: string
}

const [toasts, setToasts] = createSignal<Toast[]>([])
let nextToast = 1

export function toast(text: string, kind: Toast["kind"] = "info", ms = 5000) {
  const id = nextToast++
  setToasts((l) => [...l.slice(-3), { id, kind, text }])
  if (ms > 0) setTimeout(() => dismissToast(id), ms)
}

export function dismissToast(id: number) {
  setToasts((l) => l.filter((x) => x.id !== id))
}

export function Toasts() {
  return (
    <div class="toasts" role="region" aria-live="polite" aria-label="Notifications">
      <For each={toasts()}>
        {(item) => (
          <div class="toast" data-kind={item.kind} role={item.kind === "error" ? "alert" : "status"}>
            <span>{item.text}</span>
            <button class="ghost-icon" aria-label={t("common.dismiss")} onClick={() => dismissToast(item.id)}>
              <span aria-hidden="true">×</span>
            </button>
          </div>
        )}
      </For>
    </div>
  )
}

/** Segmented radio group for a few mutually exclusive options. */
export function Segmented<T extends string>(props: {
  label: string
  value: T
  options: { value: T; label: string }[]
  onChange: (v: T) => void
}) {
  const [local] = splitProps(props, ["label", "value", "options", "onChange"])
  return (
    <div class="segmented" role="radiogroup" aria-label={local.label}>
      <For each={local.options}>
        {(o) => (
          <button
            type="button"
            role="radio"
            aria-checked={local.value === o.value}
            data-active={local.value === o.value ? "" : undefined}
            onClick={() => local.onChange(o.value)}
          >
            {o.label}
          </button>
        )}
      </For>
    </div>
  )
}
