// Syntax highlighting, loaded only when the first code block needs it. One
// theme made of CSS variables serves light and dark, and the JavaScript regex
// engine avoids shipping a WebAssembly file. Each language is its own chunk.

import { useEffect, useState } from "react";
import type { HighlighterCore, ThemedToken } from "shiki/core";

const LANGS: Record<string, () => Promise<unknown>> = {
  bash: () => import("shiki/langs/bash.mjs"),
  c: () => import("shiki/langs/c.mjs"),
  cpp: () => import("shiki/langs/cpp.mjs"),
  csharp: () => import("shiki/langs/csharp.mjs"),
  css: () => import("shiki/langs/css.mjs"),
  diff: () => import("shiki/langs/diff.mjs"),
  docker: () => import("shiki/langs/docker.mjs"),
  go: () => import("shiki/langs/go.mjs"),
  html: () => import("shiki/langs/html.mjs"),
  java: () => import("shiki/langs/java.mjs"),
  javascript: () => import("shiki/langs/javascript.mjs"),
  json: () => import("shiki/langs/json.mjs"),
  jsx: () => import("shiki/langs/jsx.mjs"),
  kotlin: () => import("shiki/langs/kotlin.mjs"),
  lua: () => import("shiki/langs/lua.mjs"),
  markdown: () => import("shiki/langs/markdown.mjs"),
  php: () => import("shiki/langs/php.mjs"),
  powershell: () => import("shiki/langs/powershell.mjs"),
  python: () => import("shiki/langs/python.mjs"),
  ruby: () => import("shiki/langs/ruby.mjs"),
  rust: () => import("shiki/langs/rust.mjs"),
  sql: () => import("shiki/langs/sql.mjs"),
  swift: () => import("shiki/langs/swift.mjs"),
  toml: () => import("shiki/langs/toml.mjs"),
  tsx: () => import("shiki/langs/tsx.mjs"),
  typescript: () => import("shiki/langs/typescript.mjs"),
  xml: () => import("shiki/langs/xml.mjs"),
  yaml: () => import("shiki/langs/yaml.mjs"),
};

const ALIASES: Record<string, string> = {
  sh: "bash", shell: "bash", zsh: "bash", console: "bash", ps1: "powershell", pwsh: "powershell",
  js: "javascript", mjs: "javascript", cjs: "javascript", ts: "typescript", mts: "typescript", cts: "typescript",
  py: "python", rb: "ruby", rs: "rust", yml: "yaml", md: "markdown", cs: "csharp", "c++": "cpp", cc: "cpp", h: "c", hpp: "cpp",
  dockerfile: "docker", kt: "kotlin", golang: "go", htm: "html", svg: "xml",
};

export function languageOf(tag: string): string {
  const name = tag.toLowerCase();
  const resolved = ALIASES[name] ?? name;
  return resolved in LANGS ? resolved : "";
}

/** The language for a file name, or "" when there is none to highlight. */
export function languageOfFile(path: string): string {
  const base = path.split(/[\\/]/).pop()?.toLowerCase() ?? "";
  if (base === "dockerfile") return "docker";
  return languageOf(base.split(".").pop() ?? "");
}

const MAX_CODE = 200_000;
const CACHE_SIZE = 200;

let core: Promise<HighlighterCore> | null = null;
const loaded = new Map<string, Promise<void>>();
const cache = new Map<string, ThemedToken[][]>();

function highlighter() {
  core ??= (async () => {
    const [{ createHighlighterCore, createCssVariablesTheme }, { createJavaScriptRegexEngine }] = await Promise.all([import("shiki/core"), import("shiki/engine/javascript")]);
    const theme = createCssVariablesTheme({ name: "atlas", variablePrefix: "--atlas-code-", fontStyle: true });
    return createHighlighterCore({ themes: [theme], langs: [], engine: createJavaScriptRegexEngine({ forgiving: true }) });
  })();
  return core;
}

async function ensureLanguage(h: HighlighterCore, lang: string) {
  let done = loaded.get(lang);
  if (!done) {
    done = LANGS[lang]().then((mod) => h.loadLanguage((mod as { default: Parameters<HighlighterCore["loadLanguage"]>[0] }).default));
    loaded.set(lang, done);
  }
  await done;
}

async function tokenize(code: string, lang: string): Promise<ThemedToken[][]> {
  const h = await highlighter();
  await ensureLanguage(h, lang);
  return h.codeToTokens(code, { lang, theme: "atlas" }).tokens;
}

function remember(key: string, tokens: ThemedToken[][]) {
  if (cache.size >= CACHE_SIZE) {
    const oldest = cache.keys().next().value;
    if (oldest !== undefined) cache.delete(oldest);
  }
  cache.set(key, tokens);
}

/**
 * Tokens for a piece of code, or null until they are ready (and for languages
 * Atlas does not highlight). Fast-changing code, like a block still streaming,
 * is highlighted after a short pause rather than on every token.
 */
export function useTokens(code: string, tag: string, delay = 120): ThemedToken[][] | null {
  const lang = languageOf(tag);
  const key = `${lang}\0${code}`;
  const [tokens, setTokens] = useState<ThemedToken[][] | null>(() => cache.get(key) ?? null);

  useEffect(() => {
    if (!lang || code.length > MAX_CODE) {
      setTokens(null);
      return;
    }
    const hit = cache.get(key);
    if (hit) {
      setTokens(hit);
      return;
    }
    let stale = false;
    const timer = setTimeout(() => {
      tokenize(code, lang)
        .then((result) => {
          remember(key, result);
          if (!stale) setTokens(result);
        })
        .catch((err: unknown) => console.warn("Highlighting failed", err));
    }, delay);
    return () => {
      stale = true;
      clearTimeout(timer);
    };
  }, [key, code, lang, delay]);

  return tokens;
}
