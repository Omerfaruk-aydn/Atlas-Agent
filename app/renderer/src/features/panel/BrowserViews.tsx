// The browser's own data as tabs of the panel: history, saved passwords and
// extensions. Each lists what the main process keeps and edits it there.

import { useCallback, useEffect, useState } from "react";
import type { ExtensionInfo, ExtensionPreview, HistoryEntry, VaultEntry } from "../../api/types";
import { t, type Key } from "../../lib/i18n";
import { hostOf } from "../../lib/panel";
import { IconClose, IconCopy, IconDownload, IconExternal, IconEye, IconHistory, IconKey2, IconPencil, IconPlus, IconPuzzle, IconSearch, IconTrash } from "../../primitives/icons";
import { Modal, useDebounced } from "../../primitives/ui";
import { useApp } from "../../stores/app";
import { useBrowserUI } from "../../stores/browserui";
import { usePanel } from "../../stores/panel";
import { SiteFavicon } from "./SiteIcon";

const when = (at: number) => new Date(at).toLocaleString(undefined, { dateStyle: "medium", timeStyle: "short" });

/** A permission as people read it; an unknown one is shown as the extension wrote it. */
export function permLabel(name: string): string {
  const label = t(`perm.${name}` as Key) as string | undefined;
  return label ? `${label} (${name})` : name;
}

export function HistoryView() {
  const [query, setQuery] = useState("");
  const [entries, setEntries] = useState<HistoryEntry[]>([]);
  const [confirm, setConfirm] = useState(false);
  const openUrl = usePanel((s) => s.openUrl);
  const load = useCallback((q: string) => void window.atlas?.data.history.search(q, 500).then(setEntries), []);
  useEffect(() => load(""), [load]);
  useDebounced(query, 150, load);
  return (
    <div className="data-view">
      <div className="data-head">
        <IconHistory />
        <h2>{t("web.history")}</h2>
        <span className="data-spacer" />
        {confirm ? (
          <>
            <span className="data-note">{t("web.historyClearAsk")}</span>
            <button
              type="button"
              className="button"
              data-variant="primary"
              onClick={async () => {
                await window.atlas?.data.history.clear(0);
                setConfirm(false);
                load(query);
              }}
            >
              {t("web.clearAction")}
            </button>
            <button type="button" className="button" onClick={() => setConfirm(false)}>
              {t("common.cancel")}
            </button>
          </>
        ) : (
          <button type="button" className="button" disabled={entries.length === 0} onClick={() => setConfirm(true)}>
            {t("web.historyClearAll")}
          </button>
        )}
      </div>
      <label className="search search-boxed data-search">
        <IconSearch />
        <input value={query} placeholder={t("web.historySearch")} aria-label={t("web.historySearch")} onChange={(e) => setQuery(e.target.value)} />
      </label>
      <div className="data-list">
        {entries.length === 0 && <div className="rail-empty">{t("web.historyEmpty")}</div>}
        {entries.map((entry) => (
          <div key={entry.id} className="data-row">
            <button type="button" className="data-main" title={entry.url} onClick={() => openUrl(entry.url)}>
              <SiteFavicon host={hostOf(entry.url)} />
              <span className="data-title">{entry.title || entry.url}</span>
              <span className="data-sub">{hostOf(entry.url)}</span>
              <span className="data-time">{when(entry.at)}</span>
            </button>
            <button
              type="button"
              className="icon-button"
              data-size="sm"
              aria-label={`${t("web.historyRemove")}: ${entry.title || entry.url}`}
              onClick={async () => {
                await window.atlas?.data.history.remove(entry.id);
                load(query);
              }}
            >
              <IconTrash />
            </button>
          </div>
        ))}
      </div>
    </div>
  );
}

function PasswordRow({ entry, reload }: { entry: VaultEntry; reload(): void }) {
  const toast = useApp((s) => s.toast);
  const [secret, setSecret] = useState("");
  const [editing, setEditing] = useState(false);
  const [user, setUser] = useState(entry.username);
  const [pass, setPass] = useState("");

  // A revealed password is shown for a short while, then forgotten.
  useEffect(() => {
    if (!secret) return;
    const timer = setTimeout(() => setSecret(""), 15_000);
    return () => clearTimeout(timer);
  }, [secret]);

  const reveal = async () => {
    const r = await window.atlas?.data.vault.reveal(entry.id);
    if (r?.ok && r.password !== undefined) setSecret(r.password);
    else if (r?.error !== "Cancelled.") toast(r?.error ?? t("vault.fillFailed"), "danger");
  };

  return (
    <div className="data-row password-row">
      <SiteFavicon host={hostOf(entry.origin)} />
      <div className="password-info">
        <span className="data-title">{hostOf(entry.origin)}</span>
        {editing ? (
          <div className="password-edit">
            <input value={user} aria-label={t("vault.username")} placeholder={t("vault.username")} onChange={(e) => setUser(e.target.value)} />
            <input value={pass} type="password" aria-label={t("vault.newPassword")} placeholder={t("vault.newPassword")} autoComplete="new-password" onChange={(e) => setPass(e.target.value)} />
            <button
              type="button"
              className="button"
              onClick={async () => {
                const r = await window.atlas?.data.vault.update(entry.id, { username: user, password: pass || undefined });
                if (!r?.ok) toast(r?.error ?? t("vault.saveFailed"), "danger");
                setEditing(false);
                setPass("");
                reload();
              }}
            >
              {t("common.save")}
            </button>
            <button type="button" className="button" onClick={() => { setEditing(false); setUser(entry.username); setPass(""); }}>
              {t("common.cancel")}
            </button>
          </div>
        ) : (
          <span className="data-sub">
            {entry.username || t("vault.noUser")}
            {secret && (
              <>
                {" · "}
                <code className="password-secret">{secret}</code>
              </>
            )}
          </span>
        )}
      </div>
      {!editing && (
        <div className="data-actions">
          <button type="button" className="icon-button" data-size="sm" aria-label={`${t(secret ? "vault.hide" : "vault.reveal")}: ${hostOf(entry.origin)}`} aria-pressed={!!secret} onClick={() => (secret ? setSecret("") : void reveal())}>
            <IconEye />
          </button>
          <button
            type="button"
            className="icon-button"
            data-size="sm"
            aria-label={`${t("vault.copy")}: ${hostOf(entry.origin)}`}
            onClick={async () => {
              const r = await window.atlas?.data.vault.copy(entry.id);
              if (r?.ok) toast(t("panel.copied"));
              else if (r?.error && r.error !== "Cancelled.") toast(r.error, "danger");
            }}
          >
            <IconCopy />
          </button>
          <button type="button" className="icon-button" data-size="sm" aria-label={`${t("vault.edit")}: ${hostOf(entry.origin)}`} onClick={() => setEditing(true)}>
            <IconPencil />
          </button>
          <button
            type="button"
            className="icon-button"
            data-size="sm"
            aria-label={`${t("vault.delete")}: ${hostOf(entry.origin)}`}
            onClick={async () => {
              await window.atlas?.data.vault.remove(entry.id);
              reload();
            }}
          >
            <IconTrash />
          </button>
        </div>
      )}
    </div>
  );
}

export function PasswordsView() {
  const [query, setQuery] = useState("");
  const [entries, setEntries] = useState<VaultEntry[]>([]);
  const [available, setAvailable] = useState(true);
  const [adding, setAdding] = useState(false);
  const [draft, setDraft] = useState({ site: "", user: "", pass: "" });
  const toast = useApp((s) => s.toast);
  const set = useBrowserUI((s) => s.set);
  const load = useCallback((q: string) => {
    void window.atlas?.data.vault.status().then((s) => setAvailable(s.available));
    void window.atlas?.data.vault.list(q).then(setEntries);
  }, []);
  useEffect(() => load(""), [load]);
  useDebounced(query, 150, load);
  const reload = () => load(query);
  return (
    <div className="data-view">
      <div className="data-head">
        <IconKey2 />
        <h2>{t("vault.title")}</h2>
        <span className="data-spacer" />
        <button type="button" className="button" onClick={() => set({ importOpen: true })}>
          <IconDownload />
          {t("web.import")}
        </button>
        <button type="button" className="button" disabled={!available} onClick={() => setAdding(!adding)}>
          <IconPlus />
          {t("vault.add")}
        </button>
      </div>
      {!available && (
        <div className="data-banner" role="alert">
          {t("vault.unavailable")}
        </div>
      )}
      {adding && (
        <form
          className="password-edit data-add"
          onSubmit={async (e) => {
            e.preventDefault();
            const r = await window.atlas?.data.vault.add(draft.site.includes("://") ? draft.site : `https://${draft.site}`, draft.user, draft.pass);
            if (!r?.ok) return toast(r?.error ?? t("vault.saveFailed"), "danger");
            setDraft({ site: "", user: "", pass: "" });
            setAdding(false);
            reload();
          }}
        >
          <input value={draft.site} aria-label={t("vault.site")} placeholder={t("vault.site")} onChange={(e) => setDraft({ ...draft, site: e.target.value })} />
          <input value={draft.user} aria-label={t("vault.username")} placeholder={t("vault.username")} onChange={(e) => setDraft({ ...draft, user: e.target.value })} />
          <input value={draft.pass} type="password" aria-label={t("vault.password")} placeholder={t("vault.password")} autoComplete="new-password" onChange={(e) => setDraft({ ...draft, pass: e.target.value })} />
          <button type="submit" className="button" data-variant="primary" disabled={!draft.site || !draft.pass}>
            {t("common.save")}
          </button>
        </form>
      )}
      <label className="search search-boxed data-search">
        <IconSearch />
        <input value={query} placeholder={t("vault.search")} aria-label={t("vault.search")} onChange={(e) => setQuery(e.target.value)} />
      </label>
      <div className="data-list">
        {entries.length === 0 && <div className="rail-empty">{t("vault.empty")}</div>}
        {entries.map((entry) => (
          <PasswordRow key={entry.id} entry={entry} reload={reload} />
        ))}
      </div>
    </div>
  );
}

/** What an extension asks for, shown before it is allowed to run. */
function PermissionList({ preview }: { preview: Pick<ExtensionPreview, "permissions" | "hostPermissions"> }) {
  const permissions = preview.permissions ?? [];
  const hosts = preview.hostPermissions ?? [];
  if (permissions.length + hosts.length === 0) return <p className="modal-text">{t("ext.noPermissions")}</p>;
  return (
    <ul className="permission-list" aria-label={t("ext.permissions")}>
      {permissions.map((p) => (
        <li key={p}>{permLabel(p)}</li>
      ))}
      {hosts.map((h) => (
        <li key={h}>
          {t("ext.readSites")}: <code>{h}</code>
        </li>
      ))}
    </ul>
  );
}

export function ExtensionsView() {
  const [items, setItems] = useState<ExtensionInfo[]>([]);
  const [store, setStore] = useState({ ready: false, error: "" });
  const [preview, setPreview] = useState<ExtensionPreview | null>(null);
  const [error, setError] = useState("");
  const toast = useApp((s) => s.toast);
  const load = useCallback(() => void window.atlas?.data.extensions.list().then((r) => { setItems(r.items); setStore({ ready: r.store, error: r.storeError }); }), []);
  useEffect(() => {
    load();
    return window.atlas?.data.extensions.onChange(load);
  }, [load]);

  const choose = async () => {
    setError("");
    const picked = await window.atlas?.data.extensions.pick();
    if (!picked) return;
    if (!picked.ok) return setError(picked.error ?? t("ext.invalid"));
    setPreview(picked);
  };
  const enable = async (item: ExtensionInfo) => {
    setError("");
    const r = await window.atlas?.data.extensions.inspect(item.path);
    if (!r?.ok) return setError(r?.error ?? t("ext.invalid"));
    setPreview(r);
  };
  const confirm = async () => {
    if (!preview?.path || !preview.sig) return;
    const r = await window.atlas?.data.extensions.load(preview.path, preview.sig);
    setPreview(null);
    if (!r?.ok) setError(r?.error ?? t("ext.invalid"));
    else toast(t("ext.added"));
    load();
  };

  return (
    <div className="data-view">
      <div className="data-head">
        <IconPuzzle />
        <h2>{t("web.extensions")}</h2>
        <span className="data-spacer" />
        <button type="button" className="button" onClick={() => usePanel.getState().openUrl("https://chromewebstore.google.com/")}>
          <IconExternal />
          {t("web.visitStore")}
        </button>
        <button type="button" className="button" data-variant="primary" onClick={() => void choose()}>
          <IconPlus />
          {t("ext.loadUnpacked")}
        </button>
      </div>
      {!store.ready && (
        <div className="data-banner" role="status">
          {t("ext.storeOff")}
          {store.error ? `: ${store.error}` : ""}
        </div>
      )}
      {error && (
        <div className="data-banner" role="alert" data-tone="danger">
          {error}
        </div>
      )}
      <div className="data-list">
        {items.length === 0 && <div className="rail-empty">{t("web.noExtensions")}</div>}
        {items.map((item) => (
          <div key={item.id} className="data-row ext-card" data-off={!item.enabled || undefined}>
            {item.icon ? <img className="ext-icon" src={item.icon} alt="" draggable={false} /> : <IconPuzzle />}
            <div className="password-info">
              <span className="data-title">
                {item.name} <span className="data-sub">{item.version}</span>
              </span>
              <span className="data-sub">{item.description || (item.missing ? t("ext.missing") : "")}</span>
              <span className="data-sub">
                {t(item.source === "store" ? "ext.fromStore" : "ext.fromFolder")}
                {item.path ? ` · ${item.path}` : ""}
              </span>
              <details className="ext-perms">
                <summary>{t("ext.permissions")}</summary>
                <PermissionList preview={item} />
              </details>
            </div>
            <div className="data-actions">
              {item.source === "folder" && !item.missing && (
                <button
                  type="button"
                  className="button"
                  onClick={async () => {
                    if (item.enabled) {
                      const r = await window.atlas?.data.extensions.disable(item.id);
                      if (!r?.ok) setError(r?.error ?? t("ext.invalid"));
                      load();
                    } else await enable(item);
                  }}
                >
                  {t(item.enabled ? "ext.disable" : "ext.enable")}
                </button>
              )}
              <button
                type="button"
                className="icon-button"
                data-size="sm"
                aria-label={`${t("ext.remove")}: ${item.name}`}
                onClick={async () => {
                  await window.atlas?.data.extensions.remove(item.id);
                  load();
                }}
              >
                <IconTrash />
              </button>
            </div>
          </div>
        ))}
      </div>
      <Modal open={!!preview} onClose={() => setPreview(null)} title={`${t("ext.confirmTitle")}: ${preview?.name ?? ""}`} description={t("ext.confirmBody")}>
        {preview && (
          <>
            <p className="modal-text">
              {preview.description} <span className="data-sub">{preview.version}</span>
            </p>
            <PermissionList preview={preview} />
            <div className="modal-actions">
              <button type="button" className="button" onClick={() => setPreview(null)}>
                {t("common.cancel")}
              </button>
              <button type="button" className="button" data-variant="primary" onClick={() => void confirm()}>
                {t("ext.allow")}
              </button>
            </div>
          </>
        )}
      </Modal>
    </div>
  );
}

export { IconClose };
