// The panel's two project tools: Changes (what this chat changed, with a diff
// and a tree of files) and Files (the project's tree and a file viewer). Both
// have the shape of the reference: the work on the left, a filterable tree on
// the right.

import { useEffect, useMemo, useState } from "react";
import { http } from "../../api/http";
import type { DirectoryEntry, DirectoryListing } from "../../api/types";
import { baseName } from "../../lib/format";
import { t } from "../../lib/i18n";
import { IconBranch, IconChevron, IconCopy, IconDiff, IconFolder, IconList, IconPanelRight, IconRetry, IconSearch, IconWrap } from "../../primitives/icons";
import { Tip, useDebounced } from "../../primitives/ui";
import { useApp } from "../../stores/app";
import { usePanel } from "../../stores/panel";
import { Diff, diffLines } from "../thread/ToolRow";
import { FileIcon } from "./FileIcon";
import { FileViewer, inProject, TreeNode, useSessionChanges, type FileChange } from "./Tools";

const api = (suffix: string) => `/api/workspaces/${encodeURIComponent(useApp.getState().workspace?.id ?? "")}${suffix}`;

interface Folder {
  name: string;
  path: string;
  folders: Folder[];
  files: FileChange[];
}

/** The changed files as folders, relative to the project. */
export function changeTree(changes: FileChange[], relative: (path: string) => string): Folder {
  const root: Folder = { name: "", path: "", folders: [], files: [] };
  for (const change of changes) {
    const parts = relative(change.path).split("/").filter(Boolean);
    let at = root;
    for (const part of parts.slice(0, -1)) {
      let next = at.folders.find((f) => f.name === part);
      if (!next) {
        next = { name: part, path: at.path ? `${at.path}/${part}` : part, folders: [], files: [] };
        at.folders.push(next);
      }
      at = next;
    }
    at.files.push(change);
  }
  const sort = (f: Folder) => {
    f.folders.sort((a, b) => a.name.localeCompare(b.name));
    f.files.sort((a, b) => baseName(a.path).localeCompare(baseName(b.path)));
    f.folders.forEach(sort);
  };
  sort(root);
  return root;
}

function Counts({ add, del }: { add: number; del: number }) {
  return (
    <span className="tree-counts">
      {add > 0 && <span className="add">+{add}</span>}
      {del > 0 && <span className="del">−{del}</span>}
    </span>
  );
}

function FolderRows({ folder, depth, selected, onSelect, closed, toggle }: { folder: Folder; depth: number; selected: string; onSelect(path: string): void; closed: Set<string>; toggle(path: string): void }) {
  return (
    <>
      {folder.folders.map((child) => (
        <div key={child.path}>
          <button type="button" className="tree-row" aria-expanded={!closed.has(child.path)} style={{ paddingInlineStart: 8 + depth * 14 }} onClick={() => toggle(child.path)}>
            <IconChevron className="chevron" />
            <span className="tree-name">{child.name}</span>
          </button>
          {!closed.has(child.path) && <FolderRows folder={child} depth={depth + 1} selected={selected} onSelect={onSelect} closed={closed} toggle={toggle} />}
        </div>
      ))}
      {folder.files.map((file) => (
        <button key={file.path} type="button" className="tree-row" data-selected={selected === file.path || undefined} title={file.path} style={{ paddingInlineStart: 8 + depth * 14 + 14 }} onClick={() => onSelect(file.path)}>
          <FileIcon name={baseName(file.path)} />
          <span className="tree-name">{baseName(file.path)}</span>
          <Counts add={file.additions} del={file.removals} />
          <span className="status-mark" data-status={file.status} title={t(file.status === "added" ? "work.added" : "work.modified")} />
        </button>
      ))}
    </>
  );
}

export function Changes() {
  const changes = useSessionChanges();
  const git = useApp((s) => s.git);
  const [query, setQuery] = useState("");
  const [selected, setSelected] = useState("");
  const [closed, setClosed] = useState<Set<string>>(new Set());
  const [wrap, setWrap] = useState(false);
  const [treeOpen, setTreeOpen] = useState(true);
  const workspace = useApp((s) => s.workspace);
  const toast = useApp((s) => s.toast);
  const additions = changes.reduce((n, c) => n + c.additions, 0);
  const removals = changes.reduce((n, c) => n + c.removals, 0);
  const shown = useMemo(() => changes.filter((c) => !query.trim() || inProject(c.path).toLowerCase().includes(query.trim().toLowerCase())), [changes, query]);
  const tree = useMemo(() => changeTree(shown, inProject), [shown]);
  const current = changes.find((c) => c.path === selected) ?? changes[0];

  const toggle = (path: string) =>
    setClosed((prev) => {
      const next = new Set(prev);
      if (!next.delete(path)) next.add(path);
      return next;
    });

  return (
    <div className="work">
      <div className="work-main">
        <div className="work-bar">
          <span className="bar-pill" title={git?.branch}>
            <IconBranch />
            <span>{git?.branch ?? t("work.branch")}</span>
            {(additions > 0 || removals > 0) && <Counts add={additions} del={removals} />}
          </span>
          {git?.branch && (
            <span className="bar-pill pill-upstream" title={git.upstream ? `${git.branch} → ${git.upstream}` : t("work.noUpstream")}>
              <span>{git.branch}</span>
              <span aria-hidden="true">→</span>
              <span>{git.upstream ?? t("work.noUpstream")}</span>
            </span>
          )}
          <span className="work-note">{changes.length === 1 ? t("thread.fileChanged") : t("thread.filesChanged")}: {changes.length}</span>
          <div className="work-tools" role="toolbar" aria-label={t("work.tools")}>
            <Tip label={t("work.refresh")}>
              <button type="button" className="icon-button" aria-label={t("work.refresh")} onClick={() => void useApp.getState().refreshGit()}>
                <IconRetry />
              </button>
            </Tip>
            <Tip label={t("work.copyDiff")}>
              <button
                type="button"
                className="icon-button"
                aria-label={t("work.copyDiff")}
                disabled={changes.length === 0}
                onClick={() => {
                  const text = changes
                    .map((c) => [`--- ${inProject(c.path)}`, ...c.edits.flatMap((meta) => diffLines(meta).map((l) => (l.kind === "add" ? "+" : l.kind === "del" ? "-" : l.kind === "hunk" ? "" : " ") + l.text))].join("\n"))
                    .join("\n\n");
                  void navigator.clipboard.writeText(text).then(() => toast(t("panel.copied")));
                }}
              >
                <IconCopy />
              </button>
            </Tip>
            <Tip label={t("work.wrap")}>
              <button type="button" className="icon-button" data-on={wrap || undefined} aria-pressed={wrap} aria-label={t("work.wrap")} onClick={() => setWrap(!wrap)}>
                <IconWrap />
              </button>
            </Tip>
            <Tip label={t("work.reveal")}>
              <button
                type="button"
                className="icon-button"
                aria-label={t("work.reveal")}
                disabled={!current}
                onClick={() => current && workspace && void window.atlas?.reveal(`${workspace.path.replace(/[\\/]$/, "")}/${inProject(current.path)}`)}
              >
                <IconFolder />
              </button>
            </Tip>
            <Tip label={t("work.tree")}>
              <button type="button" className="icon-button" data-on={treeOpen || undefined} aria-pressed={treeOpen} aria-label={t("work.tree")} onClick={() => setTreeOpen(!treeOpen)}>
                <IconPanelRight />
              </button>
            </Tip>
          </div>
        </div>
        <div className="work-view">
          {!current ? (
            <div className="work-empty">
              <IconDiff />
              <strong>{t("rail.noChanges")}</strong>
            </div>
          ) : (
            <div className="work-diff" data-wrap={wrap || undefined}>
              <div className="work-diff-head">
                <FileIcon name={baseName(current.path)} />
                <span className="work-path" title={current.path}>{inProject(current.path)}</span>
                <Counts add={current.additions} del={current.removals} />
              </div>
              {current.edits.map((meta, i) => (
                <Diff key={i} meta={meta} full />
              ))}
            </div>
          )}
        </div>
      </div>
      <aside className="work-tree" data-closed={!treeOpen || undefined} aria-label={t("work.tree")} inert={!treeOpen}>
        <label className="search search-boxed work-filter">
          <IconSearch />
          <input value={query} placeholder={t("work.filter")} aria-label={t("work.filter")} onChange={(e) => setQuery(e.target.value)} />
        </label>
        <div className="work-tree-scroll">
          {shown.length === 0 ? <div className="rail-empty">{changes.length === 0 ? t("rail.noChanges") : t("sidebar.noResults")}</div> : <FolderRows folder={tree} depth={0} selected={current?.path ?? ""} onSelect={setSelected} closed={closed} toggle={toggle} />}
        </div>
      </aside>
    </div>
  );
}

/** The project's drive, like D:, for the chip at the top of Files. */
function driveOf(path: string | undefined): string {
  const match = /^([a-z]):/i.exec(path ?? "");
  return match ? `${match[1].toUpperCase()}:` : (path ?? "").split("/").filter(Boolean)[0] ?? "/";
}

export function Files({ tabId }: { tabId: string }) {
  const target = usePanel((s) => s.fileTarget);
  const setTabTitle = usePanel((s) => s.setTabTitle);
  const openTool = usePanel((s) => s.openTool);
  const workspace = useApp((s) => s.workspace);
  const [root, setRoot] = useState<DirectoryEntry[] | null>(null);
  const [open, setOpen] = useState("");
  const [query, setQuery] = useState("");
  const [hits, setHits] = useState<string[] | null>(null);

  useEffect(() => {
    http
      .get<DirectoryListing>(api("/files"))
      .then((listing) => setRoot(listing.entries ?? []))
      .catch(() => setRoot([]));
  }, []);

  useEffect(() => {
    if (target) {
      setOpen(target);
      usePanel.setState({ fileTarget: "" });
    }
  }, [target]);

  // The tab carries the name of the file that is open, as the reference does.
  useEffect(() => {
    setTabTitle(tabId, open ? baseName(open) : "");
  }, [open, tabId, setTabTitle]);

  useDebounced(query.trim(), 120, (q) => {
    if (!q) {
      setHits(null);
      return;
    }
    http
      .get<{ files: string[] }>(api(`/files/search?limit=60&q=${encodeURIComponent(q)}`))
      .then((r) => setHits(r.files ?? []))
      .catch(() => setHits([]));
  });

  return (
    <div className="work">
      <div className="work-main">
        <div className="work-bar">
          <span className="bar-pill" title={workspace?.path}>
            <span>{driveOf(workspace?.path)}</span>
          </span>
        </div>
        <div className="work-view">
          {open ? (
            <FileViewer path={open} onClose={() => setOpen("")} />
          ) : (
            <div className="work-empty">
              <IconList />
              <strong>{t("work.openFile")}</strong>
              <span>{t("work.pickFile")}</span>
            </div>
          )}
        </div>
      </div>
      <aside className="work-tree" aria-label={t("work.tree")}>
        <label className="search search-boxed work-filter">
          <IconSearch />
          <input value={query} placeholder={t("rail.searchFiles")} aria-label={t("rail.searchFiles")} onChange={(e) => setQuery(e.target.value)} />
        </label>
        <button type="button" className="tree-row work-all" onClick={() => openTool("changes")}>
          <IconList />
          <span className="tree-name">{t("work.allChanges")}</span>
        </button>
        <div className="work-tree-scroll">
          {root?.length === 0 && !hits && <div className="rail-empty">{t("rail.noFiles")}</div>}
          {hits
            ? hits.map((f) => (
                <button key={f} type="button" className="tree-row" title={f} onClick={() => setOpen(f)}>
                  <FileIcon name={baseName(f)} />
                  <span className="tree-name">{baseName(f)}</span>
                  <span className="change-dir">{f}</span>
                </button>
              ))
            : root?.map((entry) => <TreeNode key={entry.path} entry={entry} depth={0} onOpen={setOpen} selected={open} />)}
        </div>
      </aside>
    </div>
  );
}
