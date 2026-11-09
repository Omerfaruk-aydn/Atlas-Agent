// Two more views of the browser's own data: the saved addresses and the
// browser's settings. Like the others, they list what the main process keeps
// and change it there.

import { useCallback, useEffect, useState } from "react";
import type { AddressFields, AddressRecord, BrowserSettings } from "../../api/types";
import { t, type Key } from "../../lib/i18n";
import { hostOf } from "../../lib/panel";
import { IconDownload, IconGear, IconKey2, IconMapPin, IconPencil, IconPlus, IconTrash } from "../../primitives/icons";
import { Switch } from "../../primitives/ui";
import { useApp } from "../../stores/app";
import { useBrowserUI } from "../../stores/browserui";
import { usePanel } from "../../stores/panel";
import { SiteFavicon } from "./SiteIcon";

export const ADDRESS_FIELDS = ["name", "organization", "street", "street2", "city", "region", "postal", "country", "phone", "email"] as const;
const BLANK_ADDRESS: AddressFields = { name: "", organization: "", street: "", street2: "", city: "", region: "", postal: "", country: "", phone: "", email: "" };

export function addressLabel(a: AddressFields): string {
  return [a.name || a.organization, a.street, a.city].filter(Boolean).join(", ") || a.email || a.phone;
}

const fieldsOf = (a: AddressRecord): AddressFields => ({ name: a.name, organization: a.organization, street: a.street, street2: a.street2, city: a.city, region: a.region, postal: a.postal, country: a.country, phone: a.phone, email: a.email });

/** The saved addresses: add, edit, delete. They are kept encrypted, like passwords. */
export function AddressesView() {
  const [items, setItems] = useState<AddressRecord[]>([]);
  const [available, setAvailable] = useState(true);
  const [editing, setEditing] = useState<{ id: string; draft: AddressFields } | null>(null);
  const toast = useApp((s) => s.toast);
  const load = useCallback(() => {
    void window.atlas?.data.addresses.status().then((s) => setAvailable(s.available));
    void window.atlas?.data.addresses.list().then(setItems);
  }, []);
  useEffect(load, [load]);
  const save = async () => {
    if (!editing) return;
    const r = await window.atlas?.data.addresses.save(editing.id, editing.draft);
    if (!r?.ok) return toast(r?.error ?? t("addr.saveFailed"), "danger");
    setEditing(null);
    toast(t("addr.saved"));
    load();
  };
  return (
    <div className="data-view">
      <div className="data-head">
        <IconMapPin />
        <h2>{t("addr.title")}</h2>
        <span className="data-spacer" />
        <button type="button" className="button" disabled={!available} onClick={() => setEditing({ id: "", draft: { ...BLANK_ADDRESS } })}>
          <IconPlus />
          {t("addr.add")}
        </button>
      </div>
      {!available && (
        <div className="data-banner" role="alert">
          {t("addr.unavailable")}
        </div>
      )}
      {editing && (
        <form
          className="address-form"
          onSubmit={(e) => {
            e.preventDefault();
            void save();
          }}
        >
          {ADDRESS_FIELDS.map((field) => (
            <label key={field} className="field-row">
              <span>{t(`addr.${field}` as Key)}</span>
              <input value={editing.draft[field]} aria-label={t(`addr.${field}` as Key)} onChange={(e) => setEditing({ ...editing, draft: { ...editing.draft, [field]: e.target.value } })} />
            </label>
          ))}
          <div className="address-form-actions">
            <button type="submit" className="button" data-variant="primary" disabled={!ADDRESS_FIELDS.some((f) => editing.draft[f].trim())}>
              {t("addr.save")}
            </button>
            <button type="button" className="button" onClick={() => setEditing(null)}>
              {t("addr.cancel")}
            </button>
          </div>
        </form>
      )}
      <div className="data-list">
        {items.length === 0 && <div className="rail-empty">{t("addr.empty")}</div>}
        {items.map((a) => (
          <div key={a.id} className="data-row address-row">
            <IconMapPin />
            <div className="password-info">
              <span className="data-title">{addressLabel(a)}</span>
              <span className="data-sub">{[a.postal, a.country, a.phone, a.email].filter(Boolean).join(" · ")}</span>
            </div>
            <div className="data-actions">
              <button type="button" className="icon-button" data-size="sm" aria-label={`${t("addr.edit")}: ${addressLabel(a)}`} onClick={() => setEditing({ id: a.id, draft: fieldsOf(a) })}>
                <IconPencil />
              </button>
              <button
                type="button"
                className="icon-button"
                data-size="sm"
                aria-label={`${t("addr.delete")}: ${addressLabel(a)}`}
                onClick={async () => {
                  await window.atlas?.data.addresses.remove(a.id);
                  load();
                }}
              >
                <IconTrash />
              </button>
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}

/** Browser settings: what the browser does by itself, each one working at once. */
export function BrowserSettingsView() {
  const settings = useBrowserUI((s) => s.settings);
  const set = useBrowserUI((s) => s.set);
  const [never, setNever] = useState<string[]>([]);
  useEffect(() => {
    void window.atlas?.data.vault.neverList().then(setNever);
  }, []);
  const change = async <K extends keyof BrowserSettings>(key: K, value: BrowserSettings[K]) => {
    const next = await window.atlas?.data.settings.set(key, value);
    if (next) useBrowserUI.getState().set({ settings: next });
  };
  return (
    <div className="data-view settings-view">
      <div className="data-head">
        <IconGear />
        <h2>{t("bset.title")}</h2>
      </div>
      <div className="data-list">
        <section className="bset-section">
          <label className="bset-row">
            <span>{t("bset.engine")}</span>
            <select value={settings.searchEngine} aria-label={t("bset.engine")} onChange={(e) => void change("searchEngine", e.target.value as BrowserSettings["searchEngine"])}>
              {(["duckduckgo", "google", "bing"] as const).map((id) => (
                <option key={id} value={id}>
                  {t(`bset.engine${id[0]!.toUpperCase()}${id.slice(1)}` as Key)}
                </option>
              ))}
            </select>
          </label>
        </section>
        <section className="bset-section">
          <h3>{t("bset.passwordsHead")}</h3>
          <div className="bset-row">
            <span>{t("bset.offerSave")}</span>
            <Switch checked={settings.offerSave} label={t("bset.offerSave")} onChange={(v) => void change("offerSave", v)} />
          </div>
          <div className="bset-row">
            <span>{t("bset.addressAutofill")}</span>
            <Switch checked={settings.addressAutofill} label={t("bset.addressAutofill")} onChange={(v) => void change("addressAutofill", v)} />
          </div>
          <div className="bset-row">
            <button type="button" className="button" onClick={() => usePanel.getState().openView("passwords")}>
              <IconKey2 />
              {t("vault.title")}
            </button>
            <button type="button" className="button" onClick={() => usePanel.getState().openView("addresses")}>
              <IconMapPin />
              {t("addr.title")}
            </button>
          </div>
        </section>
        <section className="bset-section">
          <h3>{t("bset.neverHead")}</h3>
          {never.length === 0 && <div className="rail-empty">{t("bset.neverNone")}</div>}
          {never.map((origin) => (
            <div key={origin} className="data-row never-row">
              <SiteFavicon host={hostOf(origin)} />
              <span className="data-title">{hostOf(origin)}</span>
              <span className="data-spacer" />
              <button type="button" className="button" aria-label={`${t("bset.neverRemove")}: ${hostOf(origin)}`} onClick={async () => setNever(await (window.atlas?.data.vault.neverRemove(origin) ?? Promise.resolve([])))}>
                {t("bset.neverRemove")}
              </button>
            </div>
          ))}
        </section>
        <section className="bset-section">
          <h3>{t("bset.dataHead")}</h3>
          <div className="bset-row">
            <button type="button" className="button" onClick={() => set({ clearOpen: true })}>
              <IconTrash />
              {t("web.clear")}
            </button>
            <button type="button" className="button" onClick={() => set({ importOpen: true })}>
              <IconDownload />
              {t("web.import")}
            </button>
          </div>
        </section>
      </div>
    </div>
  );
}
