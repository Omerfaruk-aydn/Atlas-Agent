// The panel's tools that read the project: what the agent changed, the
// project's files, and background jobs. Everything they show comes from the
// session and the gateway.

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { http } from "../../api/http";
import type { DirectoryEntry, DirectoryListing, FileContent, Job, JobOutput, Message } from "../../api/types";
import { languageOfFile } from "../../lib/highlight";
import { t } from "../../lib/i18n";
import { category, toolFile, toolMeta, type ToolMeta } from "../../lib/tools";
import { buildTurns, toolCalls } from "../../lib/turns";
import { IconChevron, IconClose, IconFolder, IconStop } from "../../primitives/icons";
import { Collapse } from "../../primitives/motion";
import { Tip } from "../../primitives/ui";
import { useApp } from "../../stores/app";
import { Code } from "../thread/CodeView";
import { FileIcon } from "./FileIcon";

const NO_MESSAGES: Message[] = [];
const JOB_POLL_MS = 1500;

const api = (suffix: string) => `/api/workspaces/${encodeURIComponent(useApp.getState().workspace?.id ?? "")}${suffix}`;

export interface FileChange {
  path: string;
  additions: number;
  removals: number;
  edits: ToolMeta[];
  /** "added" when the agent wrote the file, "modified" when it edited it. */
  status: "added" | "modified";
}

export function useSessionChanges(): FileChange[] {
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
        const entry = files.get(path) ?? { path, additions: 0, removals: 0, edits: [], status: cat === "write" ? "added" : "modified" };
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
export function inProject(path: string): string {
  const root = (useApp.getState().workspace?.path ?? "").replaceAll("\\", "/").replace(/\/$/, "");
  const file = path.replaceAll("\\", "/");
  return root && file.toLowerCase().startsWith(`${root.toLowerCase()}/`) ? file.slice(root.length + 1) : file;
}

export function TreeNode({ entry, depth, onOpen, selected }: { entry: DirectoryEntry; depth: number; onOpen(path: string): void; selected: string }) {
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
        {entry.is_dir ? <IconFolder /> : <FileIcon name={entry.name} />}
        <span>{entry.name}</span>
      </button>
      {open && children?.map((child) => <TreeNode key={child.path} entry={child} depth={depth + 1} onOpen={onOpen} selected={selected} />)}
    </>
  );
}

export function FileViewer({ path, onClose }: { path: string; onClose(): void }) {
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

export function Jobs() {
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
