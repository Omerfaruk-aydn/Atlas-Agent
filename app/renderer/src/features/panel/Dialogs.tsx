// The browser's dialogs: clearing data, importing from another browser, and a
// custom device size.

import { useEffect, useState } from "react";
import { t } from "../../lib/i18n";
import { Modal } from "../../primitives/ui";
import { useApp } from "../../stores/app";
import { useBrowserUI } from "../../stores/browserui";
import { usePanelState } from "../../stores/panel";

const RANGES: { key: "web.rangeHour" | "web.rangeDay" | "web.rangeWeek" | "web.rangeMonth" | "web.rangeAll"; ms: number }[] = [
  { key: "web.rangeHour", ms: 3_600_000 },
  { key: "web.rangeDay", ms: 86_400_000 },
  { key: "web.rangeWeek", ms: 7 * 86_400_000 },
  { key: "web.rangeMonth", ms: 28 * 86_400_000 },
  { key: "web.rangeAll", ms: 0 },
];

export function ClearDataDialog() {
  const open = useBrowserUI((s) => s.clearOpen);
  const set = useBrowserUI((s) => s.set);
  const toast = useApp((s) => s.toast);
  const [what, setWhat] = useState({ history: true, cookies: false, cache: true, siteData: false, passwords: false });
  const [range, setRange] = useState(0);
  const any = Object.values(what).some(Boolean);
  // Only history keeps a time; everything else goes whole, whatever range is chosen. Said where it is chosen.
  const wholeOnly = range > 0 && (what.cookies || what.cache || what.siteData || what.passwords);
  const close = () => set({ clearOpen: false });
  const box = (key: keyof typeof what, label: Parameters<typeof t>[0]) => (
    <label className="check-row">
      <input type="checkbox" checked={what[key]} onChange={(e) => setWhat({ ...what, [key]: e.target.checked })} />
      <span>{t(label)}</span>
    </label>
  );
  return (
    <Modal open={open} onClose={close} title={t("web.clear")} description={t("web.clearBody")} className="modal-sm">
      <div className="check-list">
        {box("history", "web.clearHistory")}
        {box("cookies", "web.clearCookies")}
        {box("cache", "web.clearCache")}
        {box("siteData", "web.clearSiteData")}
        {box("passwords", "web.clearPasswords")}
      </div>
      <label className="field-row">
        <span>{t("web.range")}</span>
        <select value={range} onChange={(e) => setRange(Number(e.target.value))} aria-describedby="range-note">
          {RANGES.map((r) => (
            <option key={r.ms} value={r.ms}>
              {t(r.key)}
            </option>
          ))}
        </select>
      </label>
      <p className="modal-text" id="range-note">
        {t("web.rangeNote")}
      </p>
      <div className="reveal" data-open={wholeOnly || undefined}>
        <div className="reveal-inner">
          <p className="data-banner" role={wholeOnly ? "alert" : undefined} data-testid="clear-whole-warning">
            {t("web.clearCookiesAll")}
          </p>
        </div>
      </div>
      <div className="modal-actions">
        <button type="button" className="button" onClick={close}>
          {t("common.cancel")}
        </button>
        <button
          type="button"
          className="button"
          data-variant="primary"
          disabled={!any}
          onClick={async () => {
            close();
            const result = await window.atlas?.browser.clear({ ...what, since: range ? Date.now() - range : 0 });
            toast(`${t("web.cleared")}${what.history ? ` (${result?.history ?? 0})` : ""}`);
          }}
        >
          {t("web.clearAction")}
        </button>
      </div>
    </Modal>
  );
}

type Imported = { kind: "cookies" | "passwords"; lines: string[]; tone?: "danger" };

export function ImportDialog() {
  const open = useBrowserUI((s) => s.importOpen);
  const set = useBrowserUI((s) => s.set);
  const [profiles, setProfiles] = useState<{ id: string; browser: string; name: string }[]>([]);
  const [profile, setProfile] = useState("");
  const [busy, setBusy] = useState(false);
  const [done, setDone] = useState<Imported | null>(null);
  useEffect(() => {
    if (!open) return;
    setDone(null);
    void window.atlas?.data.imports.profiles().then((list) => {
      setProfiles(list);
      setProfile(list[0]?.id ?? "");
    });
  }, [open]);
  const close = () => set({ importOpen: false });
  const browserName = (b: string) => (b === "firefox" ? "Firefox" : b === "edge" ? "Microsoft Edge" : "Google Chrome");

  const importCookies = async () => {
    setBusy(true);
    const r = await window.atlas?.data.imports.cookies(profile);
    setBusy(false);
    if (!r?.ok) {
      setDone({ kind: "cookies", tone: "danger", lines: [r?.error === "locked" ? t("import.locked") : t("import.failed")] });
      return;
    }
    const lines = [`${t("import.cookiesDone")}: ${r.imported ?? 0}`];
    if ((r.refused ?? 0) + (r.failed ?? 0) > 0) lines.push(`${t("import.cookiesSkipped")}: ${(r.refused ?? 0) + (r.failed ?? 0)}`);
    if ((r.appBound ?? 0) > 0) lines.push(`${t("import.appBound")}: ${r.appBound}`, t("import.appBoundHelp"));
    setDone({ kind: "cookies", lines });
  };
  const importPasswords = async () => {
    setBusy(true);
    const r = await window.atlas?.data.imports.passwords();
    setBusy(false);
    if (!r) return;
    if (!r.ok) {
      setDone({ kind: "passwords", tone: "danger", lines: [r.unavailable ? t("vault.unavailable") : r.error === "format" ? t("import.csvFormat") : t("import.failed")] });
      return;
    }
    setDone({ kind: "passwords", lines: [`${t("import.passwordsDone")}: ${(r.saved ?? 0) + (r.replaced ?? 0)}`, ...((r.skipped ?? 0) + (r.refused ?? 0) > 0 ? [`${t("import.passwordsSkipped")}: ${(r.skipped ?? 0) + (r.refused ?? 0)}`] : []), t("import.deleteCsv")] });
  };

  return (
    <Modal open={open} onClose={close} title={t("web.import")} description={t("import.body")}>
      <section className="import-section">
        <h3>{t("import.cookies")}</h3>
        {profiles.length === 0 ? (
          <p className="modal-text">{t("import.noProfiles")}</p>
        ) : (
          <label className="field-row">
            <span>{t("import.profile")}</span>
            <select value={profile} onChange={(e) => setProfile(e.target.value)}>
              {profiles.map((p) => (
                <option key={p.id} value={p.id}>
                  {browserName(p.browser)}: {p.name}
                </option>
              ))}
            </select>
          </label>
        )}
        <p className="modal-text">{t("import.cookiesNote")}</p>
        <button type="button" className="button" disabled={busy || !profile} onClick={() => void importCookies()}>
          {t("import.cookiesAction")}
        </button>
      </section>
      <section className="import-section">
        <h3>{t("import.passwords")}</h3>
        <p className="modal-text">{t("import.passwordsNote")}</p>
        <p className="modal-text import-warning">{t("import.deleteCsv")}</p>
        <button type="button" className="button" disabled={busy} onClick={() => void importPasswords()}>
          {t("import.passwordsAction")}
        </button>
      </section>
      {done && (
        <div className="import-result" role="status" data-tone={done.tone}>
          {done.lines.map((line) => (
            <p key={line}>{line}</p>
          ))}
        </div>
      )}
      <div className="modal-actions">
        <button type="button" className="button" onClick={close}>
          {t("common.close")}
        </button>
      </div>
    </Modal>
  );
}

export function CustomDeviceDialog() {
  const open = useBrowserUI((s) => s.customDeviceOpen);
  const set = useBrowserUI((s) => s.set);
  const panel = usePanelState();
  const [size, setSize] = useState({ width: "390", height: "844", ratio: "2" });
  const close = () => set({ customDeviceOpen: false });
  const valid = Number(size.width) >= 200 && Number(size.height) >= 200 && Number(size.ratio) >= 1;
  const field = (key: keyof typeof size, label: Parameters<typeof t>[0]) => (
    <label className="field-row">
      <span>{t(label)}</span>
      <input type="number" value={size[key]} onChange={(e) => setSize({ ...size, [key]: e.target.value })} />
    </label>
  );
  return (
    <Modal open={open} onClose={close} title={t("web.deviceCustom")} className="modal-sm">
      {field("width", "web.deviceWidth")}
      {field("height", "web.deviceHeight")}
      {field("ratio", "web.deviceRatio")}
      <div className="modal-actions">
        <button type="button" className="button" onClick={close}>
          {t("common.cancel")}
        </button>
        <button
          type="button"
          className="button"
          data-variant="primary"
          disabled={!valid}
          onClick={() => {
            close();
            void window.atlas?.browser.device(panel.active, { width: Number(size.width), height: Number(size.height), ratio: Number(size.ratio) });
          }}
        >
          {t("web.deviceApply")}
        </button>
      </div>
    </Modal>
  );
}
