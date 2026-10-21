// Language for the Atlas Agent-derived screens. The dictionaries are Atlas Agent's
// own (app + UI package), so labels match Atlas Agent word for word; Atlas-only
// strings are layered on top in ./i18n/atlas.ts.

import { flatten, resolveTemplate, translator } from "@solid-primitives/i18n"
import { createEffect, createMemo, type JSX, type ParentProps } from "solid-js"
import { I18nProvider, pluralCategory, type UiI18n, type UiPluralCategory } from "@atlas/ui/context/i18n"
import { dict as uiEn } from "@atlas/ui/i18n/en"
import { dict as uiTr } from "@atlas/ui/i18n/tr"
import { dict as appEn } from "./i18n/en"
import { dict as appTr } from "./i18n/tr"
import { atlasEn, atlasTr } from "./i18n/atlas"
import { prefs } from "../state/prefs"

export type Locale = "en" | "tr"

type Dictionary = Record<string, string>

const en = flatten({ ...appEn, ...uiEn, ...atlasEn }) as Dictionary
const tr = { ...en, ...(flatten({ ...appTr, ...uiTr, ...atlasTr }) as Dictionary) }

function detect(): Locale {
  const langs = typeof navigator === "object" ? (navigator.languages?.length ? navigator.languages : [navigator.language]) : []
  return langs.some((l) => l?.toLowerCase().startsWith("tr")) ? "tr" : "en"
}

export const locale = createMemo(() => {
  const pref = prefs().language
  return pref === "system" ? detect() : pref
})

const dictionary = () => (locale() === "tr" ? tr : en)

type Params = Record<string, string | number | boolean>

const translate = translator(dictionary, resolveTemplate) as (key: string, params?: Params) => string | undefined

/** t translates an Atlas Agent or Atlas key; unknown keys fall back to the key. */
export function t(key: string, params?: Params): string {
  return translate(key, params) ?? key
}

const intl = () => (locale() === "tr" ? "tr-TR" : "en-US")

function pluralForm(key: string, category: UiPluralCategory, params?: Params) {
  const d = dictionary()
  return resolveTemplate(d[`${key}.${category}`] ?? d[`${key}.other`] ?? `${key}.other`, params)
}

function plural(key: string, count: number, params?: Params) {
  return pluralForm(key, pluralCategory(intl(), count), { ...params, count })
}

/** rich splits a template around {{name}} slots filled with elements. */
export function rich(key: string, params: Record<string, JSX.Element>): JSX.Element[] {
  return (dictionary()[key] ?? key)
    .split(/({{\s*[^}]+?\s*}})/g)
    .filter(Boolean)
    .map((part) => {
      const match = part.match(/^{{\s*([^}]+?)\s*}}$/)
      return match ? (params[match[1]] ?? "") : part
    })
}

export function useLanguage() {
  return {
    locale,
    intl,
    t,
    plural,
    pluralForm,
    rich,
    tDynamic: (key: string, source: string, params?: Params) => (locale() === "en" ? resolveTemplate(source, params) : t(key, params)),
    list: (items: readonly JSX.Element[]) =>
      new Intl.ListFormat(intl(), { style: "long", type: "conjunction" })
        .formatToParts(items.map((_, i) => String(i)))
        .map((p) => (p.type === "element" ? items[Number(p.value)] : p.value)),
  }
}

/** LanguageProvider hands the dictionaries to the vendored UI package and
 * keeps <html lang> current. */
export function LanguageProvider(props: ParentProps) {
  createEffect(() => {
    document.documentElement.lang = intl()
    document.documentElement.dir = "ltr"
  })
  return (
    <I18nProvider
      value={{
        locale: intl,
        layoutLocale: intl,
        t: ((key: string, params?: Params) => t(key, params)) as UiI18n["t"],
        plural: plural as UiI18n["plural"],
        pluralForm: pluralForm as UiI18n["pluralForm"],
      }}
    >
      {props.children}
    </I18nProvider>
  )
}
