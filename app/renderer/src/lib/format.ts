import { lang } from "./i18n";

/** Server timestamps are Unix seconds in some places and milliseconds in others. */
export function toMs(ts: number | undefined): number {
  if (!ts) return 0;
  return ts < 1e11 ? ts * 1000 : ts;
}

const compactNumber = new Intl.NumberFormat(lang, { notation: "compact", maximumFractionDigits: 1 });

/** "116.3K" / "116,3 B". */
export function compact(n: number): string {
  return compactNumber.format(n);
}

/** A small amount keeps the digits that say anything. */
export function money(amount: number): string {
  return `$${amount.toFixed(amount < 0.1 ? 3 : 2)}`;
}

/** "2 dk 34 sn" / "2m 34s". */
export function duration(ms: number): string {
  const total = Math.max(0, Math.round(ms / 1000));
  const m = Math.floor(total / 60);
  const s = total % 60;
  const tr = lang === "tr";
  if (m === 0) return tr ? `${s} sn` : `${s}s`;
  if (m < 60) return tr ? `${m} dk ${s} sn` : `${m}m ${s}s`;
  const h = Math.floor(m / 60);
  return tr ? `${h} sa ${m % 60} dk` : `${h}h ${m % 60}m`;
}

/** "0:42", for the live counter. */
export function clock(ms: number): string {
  const total = Math.max(0, Math.floor(ms / 1000));
  return `${Math.floor(total / 60)}:${String(total % 60).padStart(2, "0")}`;
}

export function relative(ts: number): string {
  const diff = Date.now() - toMs(ts);
  const tr = lang === "tr";
  const min = Math.floor(diff / 60000);
  if (min < 1) return tr ? "şimdi" : "now";
  if (min < 60) return tr ? `${min} dk` : `${min}m`;
  const hours = Math.floor(min / 60);
  if (hours < 24) return tr ? `${hours} sa` : `${hours}h`;
  const days = Math.floor(hours / 24);
  if (days < 30) return tr ? `${days} g` : `${days}d`;
  return new Date(toMs(ts)).toLocaleDateString(lang);
}

export function baseName(p: string): string {
  return p.split(/[\\/]/).filter(Boolean).pop() ?? p;
}
