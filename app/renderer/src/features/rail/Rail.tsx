// The right rail: what the agent changed, the project's files, and background
// jobs. It only reads; everything it shows comes from the session and gateway.

import { Tabs } from "radix-ui";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { http } from "../../api/http";
import type { DirectoryEntry, DirectoryListing, FileContent, Job, JobOutput, Message } from "../../api/types";
import { baseName } from "../../lib/format";
import { languageOfFile } from "../../lib/highlight";
import { t } from "../../lib/i18n";
import type { RailTab } from "../../lib/prefs";
import { category, toolFile, toolMeta, type ToolMeta } from "../../lib/tools";
import { buildTurns, toolCalls } from "../../lib/turns";
import { IconChevron, IconClose, IconDiff, IconFile, IconFolder, IconSearch, IconStop, IconTerminal } from "../../primitives/icons";
import { Collapse } from "../../primitives/motion";
import { ResizeHandle, Tip, useDebounced } from "../../primitives/ui";
import { useApp } from "../../stores/app";
import { Code } from "../thread/CodeView";
import { Diff } from "../thread/ToolRow";

const NO_MESSAGES: Message[] = [];
const JOB_POLL_MS = 1500;

const api = (suffix: string) => `/api/workspaces/${encodeURIComponent(useApp.getState().workspace?.id ?? "")}${suffix}`;

interface FileChange {
  path: string;
  additions: number;
  removals: number;
  edits: ToolMeta[];
}

function useSessionChanges(): FileChange[] {
  const messages = useApp((s) => (s.currentId ? (s.messages[s.currentId] ?? NO_MESSAGES) : NO_MESSAGES));
  return useMemo(() => {
    const files = new Map<string, FileChange>();
    for (const turn of buildTurns(messages)) {
      for (const { call, result } of toolCalls(turn.steps)) {
        const cat = category(call.name);
        if ((cat !== "edit" && cat !== "write") || !result || result.is_error) continue;
        const path = toolFile(call);
        if (!path) continue;
        const meta = toolMeta(result);
        const entry = files.get(path) ?? { path, additions: 0, removals: 0, edits: [] };
        entry.additions += meta.additions ?? 0;
        entry.removals += meta.removals ?? 0;
        entry.edits.push(meta);
        files.set(path, entry);
      }
    }
    return [...files.values()];
  }, [messages]);
}

/** A path inside the project, relative to it. */
function inProject(path: string): string {
  const root = (useApp.getState().workspace?.path ?? "").replaceAll("\\", "/").replace(/\/$/, "");
  const file = path.replaceAll("\\", "/");
  return root && file.toLowerCase().startsWith(`${root.toLowerCase()}/`) ? file.slice(root.length + 1) : file;
}

function ChangeRow({ change }: { change: FileChange }) {
  const [open, setOpen] = useState(false);
  const relative = inProject(change.path);
  return (
    <div className="change">
      <button type="button" className="change-row" aria-expanded={open} title={change.path} onClick={() => setOpen(!open)}>
        <IconChevron className="chevron" />
        <span className="change-name">{baseName(change.path)}</span>
        <span className="change-dir">{relative.slice(0, -baseName(relative).length)}</span>
        {!!change.additions && <span className="add">+{change.additions}</span>}
        {!!change.removals && <span className="del">−{change.removals}</span>}
      </button>
      <Collapse open={open}>
        {open && (
          <div className="change-diffs">
            {change.edits.map((meta, i) => (
              <Diff key={i} meta={meta} full />
            ))}
          </div>
        )}
      </Collapse>
    </div>
  );
}

function Changes() {
  const changes = useSessionChanges();
  if (changes.length === 0) return <div className="rail-empty">{t("rail.noChanges")}</div>;
  const additions = changes.reduce((n, c) => n + c.additions, 0);
  const removals = changes.reduce((n, c) => n + c.removals, 0);
  return (
    <>
      <div className="rail-summary">
        <span>
          {changes.length} {changes.length === 1 ? t("thread.fileChanged") : t("thread.filesChanged")}
        </span>
        {additions > 0 && <span className="add">+{additions}</span>}
        {removals > 0 && <span className="del">−{removals}</span>}
      </div>
      <div className="rail-scroll">
        {changes.map((c) => (
          <ChangeRow key={c.path} change={c} />
        ))}
      </div>
    </>
  );
}

function TreeNode({ entry, depth, onOpen, selected }: { entry: DirectoryEntry; depth: number; onOpen(path: string): void; selected: string }) {
  const [open, setOpen] = useState(false);
  const [children, setChildren] = useState<DirectoryEntry[] | null>(null);

  const toggle = () => {
    if (!entry.is_dir) {
      onOpen(entry.path);
      return;
    }
    if (!children) {
      http
        .get<DirectoryListing>(api(`/files?path=${encodeURIComponent(entry.path)}`))
        .then((listing) => setChildren(listing.entries ?? []))
        .catch(() => setChildren([]));
    }
    setOpen(!open);
  };

  return (
    <>
      <button
        type="button"
        className="tree-row"
        data-selected={selected === entry.path || undefined}
        aria-expanded={entry.is_dir ? open : undefined}
        style={{ paddingInlineStart: 8 + depth * 14 }}
        onClick={toggle}
      >
        {entry.is_dir ? <IconChevron className="chevron" /> : <span className="tree-spacer" />}
        {entry.is_dir ? <IconFolder /> : <IconFile />}
        <span>{entry.name}</span>
      </button>
      {open && children?.map((child) => <TreeNode key={child.path} entry={child} depth={depth + 1} onOpen={onOpen} selected={selected} />)}
    </>
  );
}

function FileViewer({ path, onClose }: { path: string; onClose(): void }) {
  const [file, setFile] = useState<FileContent | null>(null);
  const [error, setError] = useState("");
  useEffect(() => {
    setFile(null);
    setError("");
    http
      .get<FileContent>(api(`/files/content?path=${encodeURIComponent(path)}`))
      .then(setFile)
      .catch((err: unknown) => setError(err instanceof Error ? err.message : String(err)));
  }, [path]);

  return (
    <div className="file-view">
      <div className="file-head">
        <span className="file-path" title={path}>
          {path}
        </span>
        <button type="button" className="icon-button" data-size="sm" aria-label={t("common.close")} onClick={onClose}>
          <IconClose />
        </button>
      </div>
      {error && <div className="rail-empty">{error}</div>}
      {file?.binary && <div className="rail-empty">{t("rail.binary")}</div>}
      {file && !file.binary && (
        <pre className="file-code">
          <Code code={file.content} language={languageOfFile(path)} delay={0} />
        </pre>
      )}
      {file?.truncated && <div className="file-note">{t("rail.truncated")}</div>}
    </div>
  );
}

function Files() {
  const target = useApp((s) => s.fileTarget);
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
      useApp.setState({ fileTarget: "" });
    }
  }, [target]);

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

  if (open) return <FileViewer path={open} onClose={() => setOpen("")} />;

  return (
    <>
      <label className="search search-boxed rail-search">
        <IconSearch />
        <input value={query} placeholder={t("rail.searchFiles")} aria-label={t("rail.searchFiles")} onChange={(e) => setQuery(e.target.value)} />
      </label>
      <div className="rail-scroll">
        {root?.length === 0 && !hits && <div className="rail-empty">{t("rail.noFiles")}</div>}
        {hits
          ? hits.map((f) => (
              <button key={f} type="button" className="tree-row" title={f} onClick={() => setOpen(f)}>
                <IconFile />
                <span>{baseName(f)}</span>
                <span className="change-dir">{f}</span>
              </button>
            ))
          : root?.map((entry) => <TreeNode key={entry.path} entry={entry} depth={0} onOpen={setOpen} selected={open} />)}
      </div>
    </>
  );
}

function JobRow({ job, onStop }: { job: Job; onStop(): void }) {
  const [open, setOpen] = useState(false);
  const [output, setOutput] = useState<JobOutput | null>(null);
  const tail = useRef<HTMLPreElement>(null);

  useEffect(() => {
    if (!open) return;
    let timer = 0;
    let stopped = false;
    const poll = async () => {
      try {
        const next = await http.get<JobOutput>(api(`/jobs/${encodeURIComponent(job.id)}/output`));
        if (stopped) return;
        setOutput(next);
        if (!next.done) timer = window.setTimeout(() => void poll(), JOB_POLL_MS);
      } catch {
        // The job went away; the list will drop it.
      }
    };
    void poll();
    return () => {
      stopped = true;
      clearTimeout(timer);
    };
  }, [open, job.id]);

  useEffect(() => {
    if (tail.current) tail.current.scrollTop = tail.current.scrollHeight;
  }, [output]);

  return (
    <div className="change">
      <div className="job-row">
        <button type="button" className="change-row" aria-expanded={open} title={job.command} onClick={() => setOpen(!open)}>
          <IconChevron className="chevron" />
          <span className="dot" data-state={job.done ? undefined : "busy"} />
          <span className="change-name">{job.description || job.command}</span>
          <span className="change-dir">{job.done ? job.status : ""}</span>
        </button>
        {!job.done && (
          <Tip label={t("rail.stopJob")}>
            <button type="button" className="icon-button" data-size="sm" aria-label={t("rail.stopJob")} onClick={onStop}>
              <IconStop />
            </button>
          </Tip>
        )}
      </div>
      <Collapse open={open}>
        {open && (
          <pre className="tool-output job-output" ref={tail}>
            {output ? output.stdout + (output.stderr ? `\n${output.stderr}` : "") || t("rail.noOutput") : "…"}
          </pre>
        )}
      </Collapse>
    </div>
  );
}

function Jobs() {
  const [jobs, setJobs] = useState<Job[] | null>(null);
  const load = useCallback(() => {
    http
      .get<Job[] | null>(api("/jobs"))
      .then((list) => setJobs(list ?? []))
      .catch(() => setJobs([]));
  }, []);

  useEffect(() => {
    load();
    const timer = setInterval(load, 3000);
    return () => clearInterval(timer);
  }, [load]);

  if (jobs?.length === 0) return <div className="rail-empty">{t("rail.noJobs")}</div>;
  return (
    <div className="rail-scroll">
      {jobs?.map((job) => (
        <JobRow
          key={job.id}
          job={job}
          onStop={() => {
            void http.del(api(`/jobs/${encodeURIComponent(job.id)}`)).finally(load);
          }}
        />
      ))}
    </div>
  );
}

export function Rail() {
  const tab = useApp((s) => s.ui.railTab);
  const width = useApp((s) => s.ui.railW);
  const setUI = useApp((s) => s.setUI);
  return (
    <aside className="rail" aria-label={t("rail.title")}>
      <ResizeHandle side="right" min={320} max={720} initial={width} cssVar="--atlas-rail-w" onDone={(w) => setUI("railW", w)} />
      <Tabs.Root className="rail-tabs" value={tab} onValueChange={(value) => setUI("railTab", value as RailTab)}>
        <Tabs.List className="rail-tablist" aria-label={t("rail.title")}>
          <Tabs.Trigger value="changes" className="rail-tab">
            <IconDiff />
            {t("rail.changes")}
          </Tabs.Trigger>
          <Tabs.Trigger value="files" className="rail-tab">
            <IconFile />
            {t("rail.files")}
          </Tabs.Trigger>
          <Tabs.Trigger value="jobs" className="rail-tab">
            <IconTerminal />
            {t("rail.jobs")}
          </Tabs.Trigger>
        </Tabs.List>
        <Tabs.Content value="changes" className="rail-page">
          <Changes />
        </Tabs.Content>
        <Tabs.Content value="files" className="rail-page">
          <Files />
        </Tabs.Content>
        <Tabs.Content value="jobs" className="rail-page">
          <Jobs />
        </Tabs.Content>
      </Tabs.Root>
    </aside>
  );
}
