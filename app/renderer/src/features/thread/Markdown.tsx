// Streaming markdown. The text is cut into top-level blocks and each block is
// memoized, so a token only re-renders the block it landed in. While a message
// streams, words are wrapped in spans whose CSS animation runs once, on mount:
// new words fade in and old ones stay still, with no bookkeeping.

import { marked } from "marked";
import { memo, useEffect, useMemo, useRef, useState, type ComponentProps, type MouseEvent, type ReactNode } from "react";
import ReactMarkdown, { type Components } from "react-markdown";
import remarkGfm from "remark-gfm";
import remend from "remend";
import { t } from "../../lib/i18n";
import { useSmoothedText } from "../../lib/stream-pace";
import { IconCheck, IconCopy, IconDownload, IconList, IconWrap } from "../../primitives/icons";
import { useApp } from "../../stores/app";
import { Code } from "./CodeView";
import { SiteIcon } from "./SiteIcon";

interface HastNode {
  type: string;
  value?: string;
  tagName?: string;
  properties?: Record<string, unknown>;
  children?: HastNode[];
}

function wrapWords(node: HastNode) {
  if (!node.children || node.tagName === "code" || node.tagName === "pre") return;
  node.children = node.children.flatMap((child) => {
    if (child.type !== "text") {
      wrapWords(child);
      return [child];
    }
    return (child.value ?? "")
      .split(/(\s+)/)
      .filter(Boolean)
      .map((word) =>
        /^\s+$/.test(word)
          ? { type: "text", value: word }
          : { type: "element", tagName: "span", properties: { className: ["w"] }, children: [{ type: "text", value: word }] },
      );
  });
}

const rehypeWords = () => (tree: HastNode) => wrapWords(tree);

const remarkPlugins = [remarkGfm];
const liveRehype = [rehypeWords];
const noRehype: [] = [];

function textOf(node: ReactNode): string {
  if (typeof node === "string" || typeof node === "number") return String(node);
  if (Array.isArray(node)) return node.map(textOf).join("");
  if (node && typeof node === "object" && "props" in node) return textOf((node.props as { children?: ReactNode }).children);
  return "";
}

export function CopyButton({ text, announce }: { text: string; announce?: string }) {
  const [done, setDone] = useState(false);
  const timer = useRef(0);
  useEffect(() => () => clearTimeout(timer.current), []);
  return (
    <>
      <button
        type="button"
        className="icon-button"
        data-size="sm"
        data-done={done || undefined}
        aria-label={t("thread.copy")}
        title={done ? t("thread.copied") : t("thread.copy")}
        onClick={() => {
          void navigator.clipboard.writeText(text).then(() => {
            setDone(true);
            clearTimeout(timer.current);
            timer.current = window.setTimeout(() => setDone(false), 1600);
          });
        }}
      >
        {done ? <IconCheck key="check" className="icon-in" /> : <IconCopy key="copy" className="icon-in" />}
      </button>
      <span className="sr-only" role="status" aria-live="polite">
        {done ? (announce ?? t("thread.copied")) : ""}
      </span>
    </>
  );
}

const EXTENSIONS: Record<string, string> = {
  javascript: "js", js: "js", typescript: "ts", ts: "ts", tsx: "tsx", jsx: "jsx", python: "py", py: "py", go: "go", rust: "rs", rs: "rs", java: "java", kotlin: "kt",
  c: "c", cpp: "cpp", "c++": "cpp", csharp: "cs", cs: "cs", ruby: "rb", php: "php", swift: "swift", bash: "sh", sh: "sh", shell: "sh", zsh: "sh", powershell: "ps1", ps1: "ps1",
  json: "json", yaml: "yml", yml: "yml", toml: "toml", html: "html", css: "css", scss: "scss", sql: "sql", markdown: "md", md: "md", xml: "xml", diff: "diff", mermaid: "mmd", dockerfile: "Dockerfile",
};

/** Asks where to keep a block of code; the main process shows the save dialog. */
function saveCode(code: string, language: string) {
  const ext = EXTENSIONS[language.toLowerCase()] ?? "txt";
  const name = ext === "Dockerfile" ? "Dockerfile" : `snippet.${ext}`;
  const text = code.endsWith("\n") ? code : code + "\n";
  void window.atlas?.saveText(name, text).then((saved) => {
    if (saved) useApp.getState().toast(`${t("code.saved")} ${saved}`);
  });
}

let diagramSeq = 0;

/**
 * A mermaid source drawn as a picture. Mermaid loads on first use, in its own
 * chunk. Text that does not parse yet (a half-streamed diagram) leaves the
 * source showing and tries again when it changes.
 */
function useDiagram(code: string, enabled: boolean): string | null {
  const scheme = useApp((s) => s.scheme);
  const [svg, setSvg] = useState<string | null>(null);
  useEffect(() => {
    if (!enabled) return;
    let cancelled = false;
    const timer = setTimeout(() => {
      void (async () => {
        try {
          const { default: mermaid } = await import("mermaid");
          mermaid.initialize({ startOnLoad: false, securityLevel: "strict", suppressErrorRendering: true, theme: scheme === "dark" ? "dark" : "neutral", fontFamily: "inherit" });
          const out = await mermaid.render(`atlas-diagram-${++diagramSeq}`, code);
          if (!cancelled) setSvg(out.svg);
        } catch {
          if (!cancelled) setSvg(null);
        }
      })();
    }, 350);
    return () => {
      cancelled = true;
      clearTimeout(timer);
    };
  }, [code, enabled, scheme]);
  return svg;
}

function DownloadButton({ code, language }: { code: string; language: string }) {
  if (!window.atlas?.saveText) return null;
  return (
    <button type="button" className="icon-button" data-size="sm" aria-label={t("code.download")} title={t("code.download")} onClick={() => saveCode(code, language)}>
      <IconDownload />
    </button>
  );
}

function CodeBlock({ children }: ComponentProps<"pre">) {
  const code = textOf(children).replace(/\n$/, "");
  const child = Array.isArray(children) ? children[0] : children;
  const className = child && typeof child === "object" && "props" in child ? (child.props as { className?: string }).className : "";
  const language = /language-([\w+-]+)/.exec(className ?? "")?.[1] ?? "";
  const [wrap, setWrap] = useState(false);
  const [source, setSource] = useState(false);
  const diagram = useDiagram(code, language === "mermaid");
  if (diagram && !source) {
    return (
      <div className="code-block">
        <div className="code-head">
          <span>{language}</span>
          <span className="code-tools">
            <button type="button" className="icon-button" data-size="sm" aria-label={t("code.showSource")} title={t("code.showSource")} onClick={() => setSource(true)}>
              <IconList />
            </button>
            <DownloadButton code={code} language={language} />
            <CopyButton text={code} announce={t("code.copied")} />
          </span>
        </div>
        <div className="diagram" role="img" aria-label={t("code.diagram")} dangerouslySetInnerHTML={{ __html: diagram }} />
      </div>
    );
  }
  return (
    <div className="code-block" data-wrap={wrap || undefined}>
      <div className="code-head">
        <span>{language}</span>
        <span className="code-tools">
          {diagram && (
            <button type="button" className="icon-button" data-size="sm" data-on aria-label={t("code.showDiagram")} title={t("code.showDiagram")} onClick={() => setSource(false)}>
              <IconList />
            </button>
          )}
          <button type="button" className="icon-button" data-size="sm" data-on={wrap || undefined} aria-pressed={wrap} aria-label={t(wrap ? "code.unwrap" : "code.wrap")} title={t(wrap ? "code.unwrap" : "code.wrap")} onClick={() => setWrap(!wrap)}>
            <IconWrap />
          </button>
          <DownloadButton code={code} language={language} />
          <CopyButton text={code} announce={t("code.copied")} />
        </span>
      </div>
      <pre>
        <Code code={code} language={language} />
      </pre>
    </div>
  );
}

function hostOf(href: string | undefined): string {
  if (!href || !/^https?:/.test(href)) return "";
  try {
    return new URL(href).hostname.replace(/^www\./, "");
  } catch {
    return "";
  }
}

/** Links leave through the main process, which opens the system browser. */
function openLink(e: MouseEvent<HTMLAnchorElement>, href: string | undefined) {
  e.preventDefault();
  if (href) void window.atlas?.openExternal(href);
}

const components: Components = {
  pre: CodeBlock,
  a: ({ href, children }) => {
    const host = hostOf(href);
    return (
      <a href={href} title={href} onClick={(e) => openLink(e, href)}>
        {host && <SiteIcon host={host} />}
        {children}
      </a>
    );
  },
  // Remote images are blocked by the CSP; a link is what can honestly be shown.
  img: ({ src, alt }) => (
    <a href={typeof src === "string" ? src : undefined} onClick={(e) => openLink(e, typeof src === "string" ? src : undefined)}>
      {alt || String(src)}
    </a>
  ),
};

const Block = memo(function Block({ raw, live }: { raw: string; live: boolean }) {
  return (
    <ReactMarkdown remarkPlugins={remarkPlugins} rehypePlugins={live ? liveRehype : noRehype} components={components}>
      {raw}
    </ReactMarkdown>
  );
});

function splitBlocks(text: string): string[] {
  return marked
    .lexer(text)
    .filter((token) => token.type !== "space")
    .map((token) => token.raw);
}

export function Markdown({ text, streaming }: { text: string; streaming: boolean }) {
  const shown = useSmoothedText(text, streaming);
  const blocks = useMemo(() => splitBlocks(shown), [shown]);
  return (
    <div className="markdown">
      {blocks.map((raw, i) => (
        <Block key={i} raw={streaming && i === blocks.length - 1 ? remend(raw) : raw} live={streaming} />
      ))}
    </div>
  );
}
