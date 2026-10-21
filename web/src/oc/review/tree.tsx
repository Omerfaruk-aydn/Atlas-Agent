// Atlas Agent's review change tree and flat result list, copied from
// gui-extensions/src/file/{tree-v2,list}.tsx (MIT, 2.0.32). The workspace-file
// model, drag and open-in-app pieces are dropped: Atlas lists only the files
// a session changed, so the tree always renders a fixed set of paths.

import "@atlas/ui/file-tree.css"
import { createMemo, createSignal, For, Show, splitProps, type ComponentProps, type ParentProps } from "solid-js"
import { Dynamic } from "solid-js/web"
import { createVirtualizer, defaultRangeExtractor } from "@tanstack/solid-virtual"
import { FileIcon } from "@atlas/ui/file-icon"
import { Icon } from "@atlas/ui/icon"
import { getDirectory, getFilename } from "@atlas/util/path"
import {
  buildFileTreeV2Model,
  flattenFileTreeV2,
  normalizeFileTreeV2Path,
  type FileNode,
  type FileTreeV2Node,
} from "./tree-model"

export type ChangeKind = "add" | "del" | "mix"

const INDENT_STEP = 16

function virtualScrollElement(root: HTMLElement | undefined) {
  if (!root?.isConnected) return null
  return root.closest<HTMLDivElement>(".scroll-view__viewport")
}

function rowPaddingStart(level: number, type: FileNode["type"]) {
  if (type === "directory") return 8 + level * INDENT_STEP
  if (level === 0) return 8
  return 8 + level * INDENT_STEP - INDENT_STEP
}

function guideLineStart(level: number) {
  return rowPaddingStart(level, "directory") + 8
}

const kindLabel = (kind: ChangeKind) => (kind === "add" ? "A" : kind === "del" ? "D" : "M")
const kindChange = (kind: ChangeKind) => (kind === "add" ? "added" : kind === "del" ? "deleted" : "modified")

/** Drives the highlight/selection of the flat result list from the filter input's keys. */
export function applyFileListKeyDown(
  event: KeyboardEvent,
  files: readonly string[],
  highlighted: string | undefined,
  options: { onHighlight: (path: string) => void; onSelect: (path: string) => void },
) {
  if (files.length === 0) return
  if (event.key === "ArrowDown" || event.key === "ArrowUp") {
    const currentIndex = highlighted ? files.indexOf(highlighted) : -1
    const delta = event.key === "ArrowDown" ? 1 : -1
    const start = currentIndex === -1 ? (delta > 0 ? 0 : files.length - 1) : currentIndex + delta
    options.onHighlight(files[Math.max(0, Math.min(files.length - 1, start))]!)
    event.preventDefault()
    return
  }
  if (event.key !== "Enter") return
  const target = highlighted ?? files[0]
  if (!target) return
  options.onSelect(target)
  event.preventDefault()
}

/** Depth-first file order with directories before sibling files, as the tree lists them. */
export function sortReviewPaths(paths: readonly string[]) {
  const model = buildFileTreeV2Model(paths)
  return flattenFileTreeV2(model, () => true)
    .filter((row) => row.node.type === "file")
    .map((row) => row.node.originalPath)
}

function syncWidth(element?: HTMLDivElement) {
  if (!element) return
  queueMicrotask(() => {
    if (!element.isConnected) return
    const width = Math.max(element.clientWidth, ...Array.from(element.children, (child) => child.scrollWidth))
    if (width <= element.clientWidth) return
    element.style.width = `${width}px`
  })
}

const Row = (
  p: ParentProps &
    ComponentProps<"button"> & {
      node: FileTreeV2Node
      level: number
      active?: string
      kinds?: ReadonlyMap<string, ChangeKind>
    },
) => {
  const [local, rest] = splitProps(p, ["node", "level", "active", "kinds", "children", "class"])
  const kind = () => local.kinds?.get(normalizeFileTreeV2Path(local.node.path))
  return (
    <Dynamic
      component="button"
      type="button"
      data-slot="file-tree-v2-row"
      data-path={local.node.path}
      data-selected={local.node.path === local.active ? "" : undefined}
      class={`relative ${local.class ?? ""}`}
      style={`padding-inline-start: ${rowPaddingStart(local.level, local.node.type)}px`}
      {...rest}
    >
      {local.children}
      <span data-slot="file-tree-v2-label" class="flex-1 shrink-0 text-start text-12-medium whitespace-nowrap">
        <bdi dir="auto">
          {local.node.type === "directory" ? normalizeFileTreeV2Path(local.node.path).split("/").at(-1) : local.node.name}
        </bdi>
      </span>
      {(() => {
        const value = kind()
        if (!value || local.node.type !== "file") return <span data-slot="file-tree-v2-end" />
        return (
          <span data-slot="file-tree-v2-change" data-change={kindChange(value)}>
            {kindLabel(value)}
          </span>
        )
      })()}
    </Dynamic>
  )
}

function GuideLines(props: { level: number }) {
  return (
    <For each={Array.from({ length: props.level })}>
      {(_, index) => <div data-slot="file-tree-v2-guide" style={`inset-inline-start: ${guideLineStart(index())}px`} />}
    </For>
  )
}

const itemStyle = (item: { size: number; start: number }) => ({
  position: "absolute" as const,
  top: "0",
  "inset-inline-start": "0",
  width: "100%",
  "min-width": "max-content",
  height: `${item.size}px`,
  transform: `translateY(${item.start}px)`,
})

/** The change tree over exactly the allowed paths. */
export function ReviewTree(props: {
  allowed: readonly string[]
  kinds?: ReadonlyMap<string, ChangeKind>
  active?: string
  onFileClick: (path: string) => void
}) {
  const active = () => normalizeFileTreeV2Path(props.active ?? "")
  const model = createMemo(() => buildFileTreeV2Model(props.allowed))
  const [collapsed, setCollapsed] = createSignal<ReadonlySet<string>>(new Set())
  const expanded = (path: string) => !collapsed().has(path)
  const rows = createMemo(() => flattenFileTreeV2(model(), expanded))
  const [root, setRoot] = createSignal<HTMLDivElement>()
  const [focused, setFocused] = createSignal<string>()

  const virtualizer = createVirtualizer<HTMLDivElement, HTMLDivElement>({
    get count() {
      return rows().length
    },
    getScrollElement: () => virtualScrollElement(root()),
    initialRect: { width: 0, height: 600 },
    estimateSize: () => 28,
    gap: 2,
    overscan: 10,
    get getItemKey() {
      const current = rows()
      return (index: number) => current[index]?.node.path ?? index
    },
    rangeExtractor: (range) => {
      const indexes = defaultRangeExtractor(range)
      const path = focused()
      const index = path ? rows().findIndex((row) => row.node.path === path) : -1
      if (index < 0 || indexes.includes(index)) return indexes
      return [...indexes, index].sort((a, b) => a - b)
    },
  })

  const rowByKey = createMemo(() => new Map<string | number | bigint, ReturnType<typeof rows>[number]>(rows().map((row) => [row.node.path, row])))
  const itemByKey = createMemo(() => new Map(virtualizer.getVirtualItems().map((item) => [item.key, item] as const)))
  const keys = createMemo(() => virtualizer.getVirtualItems().map((item) => item.key))

  const toggle = (path: string) =>
    setCollapsed((prev) => {
      const next = new Set(prev)
      if (next.has(path)) next.delete(path)
      else next.add(path)
      return next
    })

  return (
    <div
      ref={(el) => {
        setRoot(el)
        syncWidth(el)
      }}
      data-component="file-tree-v2"
      data-total-rows={model().total}
      class="group/file-tree-v2"
      style={{ position: "relative", height: `${virtualizer.getTotalSize()}px` }}
    >
      <For each={keys()}>
        {(key) => (
          <Show when={itemByKey().get(key)}>
            {(item) => (
              <div style={itemStyle(item())}>
                <Show when={rowByKey().get(key)}>
                  {(row) => (
                    <Show
                      when={row().node.type === "directory"}
                      fallback={
                        <Row
                          node={row().node}
                          level={row().level}
                          active={active()}
                          kinds={props.kinds}
                          onFocus={() => setFocused(row().node.path)}
                          onBlur={() => setFocused(undefined)}
                          onClick={() => props.onFileClick(row().node.originalPath)}
                        >
                          <GuideLines level={row().level} />
                          <Show when={row().level > 0}>
                            <div class="w-4 shrink-0" />
                          </Show>
                          <span class="filetree-iconpair size-4">
                            <FileIcon node={row().node} class="size-4 filetree-icon filetree-icon--color" />
                            <FileIcon node={row().node} class="size-4 filetree-icon filetree-icon--mono" mono />
                          </span>
                        </Row>
                      }
                    >
                      <Row
                        node={row().node}
                        level={row().level}
                        active={active()}
                        kinds={props.kinds}
                        onFocus={() => setFocused(row().node.path)}
                        onBlur={() => setFocused(undefined)}
                        aria-expanded={expanded(row().node.path)}
                        onClick={() => toggle(row().node.path)}
                      >
                        <GuideLines level={row().level} />
                        <div
                          data-slot="file-tree-v2-chevron"
                          data-expanded={expanded(row().node.path) ? "" : undefined}
                          class="size-4 flex items-center justify-center"
                        >
                          <Icon name="chevron-down" />
                        </div>
                      </Row>
                    </Show>
                  )}
                </Show>
              </div>
            )}
          </Show>
        )}
      </For>
    </div>
  )
}

/** The flat result list shown while the filter has text. */
export function ReviewList(props: {
  files: readonly string[]
  kinds?: ReadonlyMap<string, ChangeKind>
  active?: string
  highlighted?: string
  onFileClick: (path: string) => void
}) {
  const active = () => normalizeFileTreeV2Path(props.active ?? "")
  const highlighted = () => normalizeFileTreeV2Path(props.highlighted ?? "")
  const [root, setRoot] = createSignal<HTMLDivElement>()

  const virtualizer = createVirtualizer<HTMLDivElement, HTMLDivElement>({
    get count() {
      return props.files.length
    },
    getScrollElement: () => virtualScrollElement(root()),
    initialRect: { width: 0, height: 600 },
    estimateSize: () => 28,
    gap: 2,
    overscan: 10,
    get getItemKey() {
      const files = props.files
      return (index: number) => files[index] ?? index
    },
  })

  const itemByKey = createMemo(() => new Map(virtualizer.getVirtualItems().map((item) => [item.key, item] as const)))
  const keys = createMemo(() => virtualizer.getVirtualItems().map((item) => item.key))

  return (
    <div
      ref={setRoot}
      data-component="file-tree-v2"
      data-total-rows={props.files.length}
      style={{ position: "relative", height: `${virtualizer.getTotalSize()}px` }}
    >
      <For each={keys()}>
        {(key) => {
          const path = String(key)
          const value = normalizeFileTreeV2Path(path)
          const selected = () => (highlighted() ? highlighted() === value : active() === value)
          const kind = () => props.kinds?.get(value)
          const directory = () => (value.includes("/") ? getDirectory(value) : undefined)
          return (
            <Show when={itemByKey().get(key)}>
              {(item) => (
                <div style={itemStyle(item())}>
                  <button
                    type="button"
                    data-slot="file-tree-v2-row"
                    data-path={path}
                    data-selected={selected() ? "" : undefined}
                    data-highlighted={highlighted() === value ? "" : undefined}
                    style="padding-inline-start: 8px"
                    onClick={() => props.onFileClick(path)}
                  >
                    <span class="filetree-iconpair size-4">
                      <FileIcon node={{ path, type: "file" }} class="size-4 filetree-icon filetree-icon--color" />
                      <FileIcon node={{ path, type: "file" }} class="size-4 filetree-icon filetree-icon--mono" mono />
                    </span>
                    <span data-slot="file-tree-v2-label" class="flex flex-1 shrink-0 items-center whitespace-nowrap">
                      <Show when={directory()}>{(dir) => <span class="text-12-medium text-text-muted shrink-0">{dir()}</span>}</Show>
                      <span class="text-12-medium text-text-base shrink-0">{getFilename(value)}</span>
                    </span>
                    <Show when={kind()} fallback={<span data-slot="file-tree-v2-end" />}>
                      {(k) => (
                        <span data-slot="file-tree-v2-change" data-change={kindChange(k())}>
                          {kindLabel(k())}
                        </span>
                      )}
                    </Show>
                  </button>
                </div>
              )}
            </Show>
          )
        }}
      </For>
    </div>
  )
}
