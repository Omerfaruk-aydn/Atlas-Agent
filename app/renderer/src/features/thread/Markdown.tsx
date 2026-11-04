// Streaming markdown. The text is cut into top-level blocks and each block is
// memoized, so a token only re-renders the block it landed in. While a message
// streams, words are wrapped in spans whose CSS animation runs once, on mount:
// new words fade in and old ones stay still, with no bookkeeping.

import { marked } from "marked";
import { memo, useMemo, useState, type ComponentProps, type MouseEvent, type ReactNode } from "react";
import ReactMarkdown, { type Components } from "react-markdown";
import remarkGfm from "remark-gfm";
import remend from "remend";
import { t } from "../../lib/i18n";
import { IconCheck, IconCopy } from "../../primitives/icons";
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

export function CopyButton({ text }: { text: string }) {
  const [done, setDone] = useState(false);
  return (
    <button
      type="button"
      className="icon-button"
      data-size="sm"
      aria-label={done ? t("thread.copied") : t("thread.copy")}
      title={done ? t("thread.copied") : t("thread.copy")}
      onClick={() => {
        void navigator.clipboard.writeText(text).then(() => {
          setDone(true);
          setTimeout(() => setDone(false), 1200);
        });
      }}
    >
      {done ? <IconCheck key="check" className="icon-in" /> : <IconCopy key="copy" className="icon-in" />}
    </button>
  );
}

function CodeBlock({ children }: ComponentProps<"pre">) {
  const code = textOf(children).replace(/\n$/, "");
  const child = Array.isArray(children) ? children[0] : children;
  const className = child && typeof child === "object" && "props" in child ? (child.props as { className?: string }).className : "";
  const language = /language-([\w+-]+)/.exec(className ?? "")?.[1] ?? "";
  return (
    <div className="code-block">
      <div className="code-head">
        <span>{language}</span>
        <CopyButton text={code} />
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
  const blocks = useMemo(() => splitBlocks(text), [text]);
  return (
    <div className="markdown">
      {blocks.map((raw, i) => (
        <Block key={i} raw={streaming && i === blocks.length - 1 ? remend(raw) : raw} live={streaming} />
      ))}
    </div>
  );
}
