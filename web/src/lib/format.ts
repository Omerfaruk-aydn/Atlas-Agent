import { currentLocale } from "../i18n"

/** Server timestamps are Unix seconds for sessions and messages. */
export function fromUnix(seconds: number): Date {
  return new Date(seconds > 1e12 ? seconds : seconds * 1000)
}

export function relativeTime(seconds: number): string {
  const d = fromUnix(seconds)
  const diff = (Date.now() - d.getTime()) / 1000
  const rtf = new Intl.RelativeTimeFormat(currentLocale(), { numeric: "auto" })
  if (Math.abs(diff) < 60) return rtf.format(-Math.round(diff), "second")
  if (Math.abs(diff) < 3600) return rtf.format(-Math.round(diff / 60), "minute")
  if (Math.abs(diff) < 86400) return rtf.format(-Math.round(diff / 3600), "hour")
  if (Math.abs(diff) < 86400 * 30) return rtf.format(-Math.round(diff / 86400), "day")
  return d.toLocaleDateString(currentLocale())
}

export function formatTokens(n: number): string {
  return new Intl.NumberFormat(currentLocale(), { notation: n >= 10_000 ? "compact" : "standard", maximumFractionDigits: 1 }).format(n)
}

export function formatCost(usd: number): string {
  return new Intl.NumberFormat(currentLocale(), {
    style: "currency",
    currency: "USD",
    minimumFractionDigits: usd < 1 ? 3 : 2,
    maximumFractionDigits: usd < 1 ? 4 : 2,
  }).format(usd)
}

export function formatBytes(n: number): string {
  if (n < 1024) return `${n} B`
  const units = ["KB", "MB", "GB"]
  let v = n / 1024
  let i = 0
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024
    i++
  }
  return `${v.toFixed(v < 10 ? 1 : 0)} ${units[i]}`
}

export function formatDuration(ms: number): string {
  if (ms < 1000) return `${Math.max(0, Math.round(ms))} ms`
  const s = ms / 1000
  if (s < 60) return `${s.toFixed(s < 10 ? 1 : 0)} s`
  const m = Math.floor(s / 60)
  return `${m} min ${Math.round(s % 60)} s`
}

export function truncate(s: string, n: number): string {
  return s.length > n ? s.slice(0, n - 1) + "…" : s
}

/** parseJSON never throws; tool input arrives as model-written text. */
export function parseJSON<T = any>(s: string | undefined): T | undefined {
  if (!s) return undefined
  try {
    return JSON.parse(s) as T
  } catch {
    return undefined
  }
}
