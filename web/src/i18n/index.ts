import { createMemo, createRoot } from "solid-js"
import { prefs } from "../state/prefs"
import { en, type MessageKey } from "./en"
import { tr } from "./tr"

const dictionaries: Record<string, Partial<Record<MessageKey, string>>> = { en, tr }

function browserLanguage(): "en" | "tr" {
  const langs = typeof navigator !== "undefined" ? navigator.languages ?? [navigator.language] : []
  for (const l of langs) {
    const base = l.toLowerCase().split("-")[0]
    if (base === "tr" || base === "en") return base
  }
  return "en"
}

const language = createRoot(() =>
  createMemo(() => {
    const p = prefs().language
    return p === "system" ? browserLanguage() : p
  }),
)

export function currentLocale(): string {
  return language()
}

/** t returns the string for key in the current language, falling back to
 * English, with {name} placeholders filled in. */
export function t(key: MessageKey, params?: Record<string, string | number>): string {
  const s = dictionaries[language()]?.[key] ?? en[key]
  if (!params) return s
  return s.replace(/\{(\w+)\}/g, (_, name) => (name in params ? String(params[name]) : `{${name}}`))
}

export type { MessageKey }
