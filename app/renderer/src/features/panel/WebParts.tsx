// The parts of the browser toolbar that open something: find in page, the
// downloads list, extensions, notes, saved logins, and the `...` menu.

import { DropdownMenu, Popover } from "radix-ui";
import { useEffect, useRef, useState } from "react";
import type { DevicePreset, ExtensionInfo, PageNote, VaultEntry } from "../../api/types";
import { t } from "../../lib/i18n";
import { isWebUrl, type PanelTab } from "../../lib/panel";
import { IconChevron, IconChevronDown, IconChevronUp, IconClose, IconDownload, IconExternal, IconEye, IconKey2, IconMapPin, IconMinus, IconMore, IconNote, IconPause, IconPencil, IconPlay, IconPlus, IconPuzzle, IconReset, IconTrash } from "../../primitives/icons";
import { Tip } from "../../primitives/ui";
import { useApp } from "../../stores/app";
import { useBrowserUI } from "../../stores/browserui";
import { usePanel, type LiveTab } from "../../stores/panel";
import { SiteFavicon } from "./SiteIcon";

const STORE_URL = "https://chromewebstore.google.com/";

export const activeThread = (): string | undefined => usePanel.getState().activeThread();

const size = (bytes: number) => (bytes >= 1 << 20 ? `${(bytes / (1 << 20)).toFixed(1)} MB` : bytes >= 1024 ? `${Math.round(bytes / 1024)} KB` : `${bytes} B`);

/** Find in page: the match count, next and previous, Esc to close. */
export function FindBar({ tab, onClose }: { tab: string; onClose(): void }) {
  const browser = window.atlas?.browser;
  const [text, setText] = useState("");
  const [found, setFound] = useState({ active: 0, matches: 0 });
  const input = useRef<HTMLInputElement>(null);

  useEffect(() => {
    input.current?.focus();
    input.current?.select();
    const off = browser?.onFind((f) => f.id === tab && setFound({ active: f.active, matches: f.matches }));
    return () => {
      off?.();
      void browser?.stopFind(tab);
    };
  }, [browser, tab]);

  const step = (forward: boolean) => text && void browser?.find(tab, text, { forward, findNext: true });
  return (
    <div className="find-bar" role="search">
      <input
        ref={input}
        value={text}
        placeholder={t("web.find")}
        aria-label={t("web.find")}
        onChange={(e) => {
          setText(e.target.value);
          setFound({ active: 0, matches: 0 });
          void browser?.find(tab, e.target.value);
        }}
        onKeyDown={(e) => {
          if (e.key === "Escape") {
            e.preventDefault();
            e.stopPropagation();
            onClose();
          } else if (e.key === "Enter") {
            e.preventDefault();
            step(!e.shiftKey);
          }
        }}
      />
      <span className="find-count" aria-live="polite">
        {text ? (found.matches > 0 ? `${found.active}/${found.matches}` : t("web.noMatches")) : ""}
      </span>
      <button type="button" className="icon-button" data-size="sm" aria-label={t("web.findPrev")} disabled={!found.matches} onClick={() => step(false)}>
        <IconChevronUp />
      </button>
      <button type="button" className="icon-button" data-size="sm" aria-label={t("web.findNext")} disabled={!found.matches} onClick={() => step(true)}>
        <IconChevronDown />
      </button>
      <button type="button" className="icon-button" data-size="sm" aria-label={t("common.close")} onClick={onClose}>
        <IconClose />
      </button>
    </div>
  );
}

/** Offers to keep a login after the person submitted one. The password never leaves the main process. */
export function SaveLoginBar({ tab }: { tab: string }) {
  const offer = useBrowserUI((s) => s.offers.find((o) => o.tab === tab));
  const drop = useBrowserUI((s) => s.dropOffer);
  const toast = useApp((s) => s.toast);
  if (!offer) return null;
  const answer = async (choice: "save" | "never" | "dismiss") => {
    drop(offer.id);
    const result = await window.atlas?.data.vault.answer(offer.id, choice);
    if (choice === "save") toast(result?.ok ? t(offer.update ? "vault.updated" : "vault.saved") : t("vault.saveFailed"), result?.ok ? undefined : "danger");
  };
  return (
    <div className="login-bar" role="alert" data-unavailable={offer.unavailable || undefined}>
      <IconKey2 />
      {offer.unavailable ? (
        <>
          <span className="login-text">{t("vault.unavailable")}</span>
          <button type="button" className="button" onClick={() => void answer("dismiss")}>
            {t("common.close")}
          </button>
        </>
      ) : (
        <>
          <span className="login-text">
            {t(offer.update ? "vault.offerUpdate" : "vault.offerSave")} <strong>{offer.host}</strong>
            {offer.username ? ` (${offer.username})` : ""}
          </span>
          <button type="button" className="button" data-variant="primary" onClick={() => void answer("save")}>
            {t(offer.update ? "vault.update" : "vault.save")}
          </button>
          <button type="button" className="button" onClick={() => void answer("never")}>
            {t("vault.never")}
          </button>
          <button type="button" className="button" onClick={() => void answer("dismiss")}>
            {t("vault.notNow")}
          </button>
        </>
      )}
    </div>
  );
}

/** The key button of a page with a login form: fills one saved login, only when pressed. */
export function KeyButton({ tab }: { tab: string }) {
  const [entries, setEntries] = useState<VaultEntry[] | null>(null);
  const [open, setOpen] = useState(false);
  const [available, setAvailable] = useState(true);
  const toast = useApp((s) => s.toast);
  useEffect(() => {
    if (!open) return;
    void window.atlas?.data.vault.status().then((s) => setAvailable(s.available));
    void window.atlas?.data.vault.forTab(tab).then(setEntries);
  }, [open, tab]);
  return (
    <Popover.Root open={open} onOpenChange={setOpen}>
      <Tip label={t("vault.fill")} hidden={open}>
        <Popover.Trigger asChild>
          <button type="button" className="icon-button address-key" aria-label={t("vault.fill")}>
            <IconKey2 />
          </button>
        </Popover.Trigger>
      </Tip>
      <Popover.Portal>
        <Popover.Content className="pop web-pop" side="bottom" align="end" sideOffset={6} collisionPadding={8} aria-label={t("vault.fill")}>
          <h3 className="web-pop-title">{t("vault.fill")}</h3>
          {!available && <p className="summary-empty">{t("vault.unavailable")}</p>}
          {available && entries?.length === 0 && <p className="summary-empty">{t("vault.noneForSite")}</p>}
          {entries?.map((entry) => (
            <button
              key={entry.id}
              type="button"
              className="summary-row"
              onClick={async () => {
                setOpen(false);
                const result = await window.atlas?.data.vault.fill(tab, entry.id);
                if (result && !result.ok) toast(result.error ?? t("vault.fillFailed"), "danger");
              }}
            >
              <IconKey2 />
              <span className="summary-name">{entry.username || t("vault.noUser")}</span>
            </button>
          ))}
          <button type="button" className="summary-row summary-all" onClick={() => { setOpen(false); usePanel.getState().openView("passwords"); }}>
            <IconKey2 />
            <span className="summary-name">{t("vault.manage")}</span>
          </button>
        </Popover.Content>
      </Popover.Portal>
    </Popover.Root>
  );
}

/** The address button of a page with an address form: fills one saved address, only when pressed. */
export function AddressButton({ tab }: { tab: string }) {
  const [items, setItems] = useState<{ id: string; label: string }[] | null>(null);
  const [open, setOpen] = useState(false);
  const toast = useApp((s) => s.toast);
  useEffect(() => {
    if (open) void window.atlas?.data.addresses.labels().then(setItems);
  }, [open]);
  return (
    <Popover.Root open={open} onOpenChange={setOpen}>
      <Tip label={t("addr.fill")} hidden={open}>
        <Popover.Trigger asChild>
          <button type="button" className="icon-button address-pin" aria-label={t("addr.fill")}>
            <IconMapPin />
          </button>
        </Popover.Trigger>
      </Tip>
      <Popover.Portal>
        <Popover.Content className="pop web-pop" side="bottom" align="end" sideOffset={6} collisionPadding={8} aria-label={t("addr.fill")}>
          <h3 className="web-pop-title">{t("addr.fill")}</h3>
          {items?.length === 0 && <p className="summary-empty">{t("addr.noneYet")}</p>}
          {items?.map((entry) => (
            <button
              key={entry.id}
              type="button"
              className="summary-row"
              onClick={async () => {
                setOpen(false);
                const result = await window.atlas?.data.addresses.fill(tab, entry.id);
                if (result && !result.ok) toast(result.error ?? t("addr.fillFailed"), "danger");
              }}
            >
              <IconMapPin />
              <span className="summary-name">{entry.label}</span>
            </button>
          ))}
          <button type="button" className="summary-row summary-all" onClick={() => { setOpen(false); usePanel.getState().openView("addresses"); }}>
            <IconPencil />
            <span className="summary-name">{t("addr.manage")}</span>
          </button>
        </Popover.Content>
      </Popover.Portal>
    </Popover.Root>
  );
}

/** "Not ekle": a note tied to the page and to the selected text, sent on to the thread's composer. */
export function NotesButton({ tab, url }: { tab: string; url: string }) {
  const [open, setOpen] = useState(false);
  const [notes, setNotes] = useState<PageNote[]>([]);
  const [quote, setQuote] = useState("");
  const [draft, setDraft] = useState("");
  const [editing, setEditing] = useState("");
  const [edit, setEdit] = useState("");
  const toast = useApp((s) => s.toast);

  const reload = () => void window.atlas?.data.notes.list(url).then(setNotes);
  useEffect(() => {
    if (!open) return;
    setDraft("");
    setEditing("");
    reload();
    void window.atlas?.browser.selection(tab).then(setQuote);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, tab, url]);

  const send = (text: string, selected: string) => {
    const body = selected ? `> ${selected.replace(/\s*\n\s*/g, "\n> ")}\n\n${text}` : text;
    useApp.getState().insertInComposer(`${body}\n\n(${url})`, { append: true, target: activeThread() });
  };

  return (
    <Popover.Root open={open} onOpenChange={setOpen}>
      <Tip label={t("web.addNote")} hidden={open}>
        <Popover.Trigger asChild>
          <button type="button" className="bar-pill pill-button" aria-label={t("web.addNote")}>
            <IconNote />
            <span>{t("web.addNote")}</span>
          </button>
        </Popover.Trigger>
      </Tip>
      <Popover.Portal>
        <Popover.Content className="pop web-pop note-pop" side="bottom" align="start" sideOffset={6} collisionPadding={8} aria-label={t("web.addNote")}>
          <h3 className="web-pop-title">{t("web.addNote")}</h3>
          <p className="note-url" title={url}>
            {url}
          </p>
          {quote && (
            <blockquote className="note-quote" aria-label={t("web.selection")}>
              {quote}
            </blockquote>
          )}
          <textarea className="note-input" value={draft} rows={3} placeholder={t("web.notePlaceholder")} aria-label={t("web.noteText")} onChange={(e) => setDraft(e.target.value)} />
          <div className="note-actions">
            <button
              type="button"
              className="button"
              data-variant="primary"
              disabled={!draft.trim()}
              onClick={async () => {
                const note = await window.atlas?.data.notes.add(url, draft, quote);
                if (!note) return toast(t("web.noteFailed"), "danger");
                send(note.text, note.quote);
                setDraft("");
                reload();
                toast(t("web.noteAdded"));
              }}
            >
              {t("web.noteSave")}
            </button>
          </div>
          {notes.length > 0 && <h3 className="web-pop-title">{t("web.notesOfPage")}</h3>}
          <div className="note-list">
            {notes.map((note) => (
              <div key={note.id} className="note-item">
                {editing === note.id ? (
                  <>
                    <textarea className="note-input" value={edit} rows={3} aria-label={t("web.noteText")} onChange={(e) => setEdit(e.target.value)} />
                    <div className="note-actions">
                      <button
                        type="button"
                        className="button"
                        disabled={!edit.trim()}
                        onClick={async () => {
                          await window.atlas?.data.notes.update(url, note.id, edit);
                          setEditing("");
                          reload();
                        }}
                      >
                        {t("common.save")}
                      </button>
                      <button type="button" className="button" onClick={() => setEditing("")}>
                        {t("common.cancel")}
                      </button>
                    </div>
                  </>
                ) : (
                  <>
                    {note.quote && <blockquote className="note-quote">{note.quote}</blockquote>}
                    <p className="note-text">{note.text}</p>
                    <div className="note-actions">
                      <button type="button" className="icon-button" data-size="sm" aria-label={t("web.noteToComposer")} onClick={() => send(note.text, note.quote)}>
                        <IconExternal />
                      </button>
                      <button
                        type="button"
                        className="icon-button"
                        data-size="sm"
                        aria-label={t("web.noteEdit")}
                        onClick={() => {
                          setEditing(note.id);
                          setEdit(note.text);
                        }}
                      >
                        <IconPencil />
                      </button>
                      <button
                        type="button"
                        className="icon-button"
                        data-size="sm"
                        aria-label={t("web.noteDelete")}
                        onClick={async () => {
                          await window.atlas?.data.notes.remove(url, note.id);
                          reload();
                        }}
                      >
                        <IconTrash />
                      </button>
                    </div>
                  </>
                )}
              </div>
            ))}
          </div>
        </Popover.Content>
      </Popover.Portal>
    </Popover.Root>
  );
}

/** Downloads: progress, pause, resume, cancel, open, show in folder. */
export function DownloadsButton() {
  const open = useBrowserUI((s) => s.downloadsOpen);
  const downloads = useBrowserUI((s) => s.downloads);
  const set = useBrowserUI((s) => s.set);
  const active = downloads.some((d) => d.state === "progressing" || d.state === "paused");
  const act = (id: number, action: "cancel" | "pause" | "resume" | "open" | "show" | "remove") => void window.atlas?.data.downloads.action(id, action).then(() => window.atlas?.data.downloads.list().then((list) => useBrowserUI.getState().setDownloads(list)));
  return (
    <Popover.Root open={open} onOpenChange={(next) => set({ downloadsOpen: next })}>
      <Tip label={t("web.downloads")} hidden={open}>
        <Popover.Trigger asChild>
          <button type="button" className="icon-button" data-on={open || undefined} aria-label={t("web.downloads")}>
            <IconDownload />
            {active && <span className="badge-dot" aria-hidden="true" />}
          </button>
        </Popover.Trigger>
      </Tip>
      <Popover.Portal>
        <Popover.Content className="pop web-pop downloads-pop" side="bottom" align="end" sideOffset={6} collisionPadding={8} aria-label={t("web.downloads")}>
          <h3 className="web-pop-title">{t("web.downloads")}</h3>
          {downloads.length === 0 && <p className="summary-empty">{t("web.noDownloads")}</p>}
          {downloads.map((d) => {
            const done = d.state === "completed";
            const percent = d.total > 0 ? Math.min(100, Math.round((d.received / d.total) * 100)) : 0;
            return (
              <div key={d.id} className="download-row" data-state={d.state}>
                <div className="download-info">
                  <span className="download-name" title={d.path}>
                    {d.name}
                  </span>
                  <span className="download-meta">
                    {done ? `${size(d.received)} · ${d.host || t("web.fromBrowser")}` : d.state === "progressing" || d.state === "paused" ? `${d.state === "paused" ? t("web.paused") : `${percent}%`} · ${size(d.received)}${d.total ? ` / ${size(d.total)}` : ""}` : d.state === "cancelled" ? t("web.cancelled") : t("web.failed")}
                  </span>
                  {(d.state === "progressing" || d.state === "paused") && <progress className="download-bar" value={d.total ? d.received : undefined} max={d.total || undefined} aria-label={d.name} />}
                </div>
                <div className="download-actions">
                  {d.state === "progressing" && (
                    <button type="button" className="icon-button" data-size="sm" aria-label={t("web.pause")} onClick={() => act(d.id, "pause")}>
                      <IconPause />
                    </button>
                  )}
                  {d.state === "paused" && (
                    <button type="button" className="icon-button" data-size="sm" aria-label={t("web.resume")} onClick={() => act(d.id, "resume")}>
                      <IconPlay />
                    </button>
                  )}
                  {(d.state === "progressing" || d.state === "paused") && (
                    <button type="button" className="icon-button" data-size="sm" aria-label={t("web.cancel")} onClick={() => act(d.id, "cancel")}>
                      <IconClose />
                    </button>
                  )}
                  {done && (
                    <>
                      <button type="button" className="icon-button" data-size="sm" aria-label={t("web.openFile")} onClick={() => act(d.id, "open")}>
                        <IconExternal />
                      </button>
                      <button type="button" className="icon-button" data-size="sm" aria-label={t("web.showInFolder")} onClick={() => act(d.id, "show")}>
                        <IconEye />
                      </button>
                    </>
                  )}
                  {d.state !== "progressing" && d.state !== "paused" && (
                    <button type="button" className="icon-button" data-size="sm" aria-label={t("web.removeFromList")} onClick={() => act(d.id, "remove")}>
                      <IconTrash />
                    </button>
                  )}
                </div>
              </div>
            );
          })}
        </Popover.Content>
      </Popover.Portal>
    </Popover.Root>
  );
}

/** Extensions: the popup of the toolbar, as in the reference. */
export function ExtensionsButton({ tab, anchorRef }: { tab?: string; anchorRef?: React.RefObject<HTMLButtonElement | null> }) {
  const open = useBrowserUI((s) => s.extensionsOpen);
  const set = useBrowserUI((s) => s.set);
  const [items, setItems] = useState<ExtensionInfo[]>([]);
  const load = () => void window.atlas?.data.extensions.list().then((r) => setItems(r.items));
  useEffect(() => {
    load();
    return window.atlas?.data.extensions.onChange(load);
  }, []);
  const enabled = items.filter((i) => i.enabled);
  return (
    <>
      {enabled
        .filter((e) => e.action && !e.hidden)
        .map((e) => (
          <Tip key={e.id} label={e.action?.title ?? e.name}>
            <button
              type="button"
              className="icon-button ext-action"
              aria-label={e.action?.title ?? e.name}
              data-popup={e.action?.popup || undefined}
              onClick={async (ev) => {
                if (e.action?.popup) {
                  const box = ev.currentTarget.getBoundingClientRect();
                  void window.atlas?.data.extensions.popup(e.id, { x: box.left, y: box.bottom + 4 });
                  return;
                }
                // No popup: the extension wants to be told of the click, in the page in front.
                const target = tab ?? "";
                const done = target ? await window.atlas?.data.extensions.click(e.id, target) : false;
                if (!done) useApp.getState().toast(t("ext.clickNoPage"), "danger");
              }}
            >
              {e.icon ? <img src={e.icon} alt="" className="ext-icon" draggable={false} /> : <IconPuzzle />}
            </button>
          </Tip>
        ))}
      <Popover.Root open={open} onOpenChange={(next) => set({ extensionsOpen: next })}>
        <Tip label={t("web.extensions")} hidden={open}>
          <Popover.Trigger asChild>
            <button ref={anchorRef} type="button" className="icon-button" data-on={open || undefined} aria-label={t("web.extensions")} onClick={load}>
              <IconPuzzle />
            </button>
          </Popover.Trigger>
        </Tip>
        <Popover.Portal>
          <Popover.Content className="pop web-pop ext-pop" side="bottom" align="end" sideOffset={6} collisionPadding={8} aria-label={t("web.extensions")}>
            <h3 className="web-pop-title">
              {t("web.extensions")} ({enabled.length})
            </h3>
            {enabled.length === 0 && (
              <div className="ext-empty">
                <IconPuzzle />
                <strong>{t("web.noExtensions")}</strong>
              </div>
            )}
            {enabled.map((e) => (
              <div key={e.id} className="ext-row">
                {e.icon ? <img src={e.icon} alt="" className="ext-icon" draggable={false} /> : <IconPuzzle />}
                <span className="summary-name" title={e.description}>
                  {e.name}
                  {!e.action && <span className="ext-reason">{t("ext.noAction")}</span>}
                </span>
                <button type="button" className="button" onClick={() => void window.atlas?.data.extensions.hide(e.id, !e.hidden)}>
                  {t(e.hidden ? "web.showInToolbar" : "web.hideFromToolbar")}
                </button>
              </div>
            ))}
            <button type="button" className="summary-row" onClick={() => { set({ extensionsOpen: false }); usePanel.getState().openView("extensions"); }}>
              <IconPencil />
              <span className="summary-name">{t("web.manageExtensions")}</span>
            </button>
            <button type="button" className="summary-row" onClick={() => { set({ extensionsOpen: false }); usePanel.getState().openUrl(STORE_URL); }}>
              <IconExternal />
              <span className="summary-name">{t("web.visitStore")}</span>
            </button>
          </Popover.Content>
        </Popover.Portal>
      </Popover.Root>
    </>
  );
}

const item = "menu-item";

/**
 * The `...` menu of the browser, in the order and with the items of the
 * reference: find, print, zoom; device toolbar, screenshot; import;
 * passwords and autofill; downloads, history, clear; settings. Open in your
 * browser and copy address have no other entry point, so they stay, last.
 */
export function BrowserMenu({ tab, live, url, onFind }: { tab: PanelTab; live?: LiveTab; url: string; onFind(): void }) {
  const browser = window.atlas?.browser;
  const toast = useApp((s) => s.toast);
  const set = useBrowserUI((s) => s.set);
  const settings = useBrowserUI((s) => s.settings);
  const [open, setOpen] = useState(false);
  const [devices, setDevices] = useState<DevicePreset[]>([]);
  const page = isWebUrl(url);
  const zoom = Math.round((live?.zoom ?? 1) * 100);
  return (
    <DropdownMenu.Root
      onOpenChange={(next) => {
        setOpen(next);
        if (!next) return;
        void browser?.devices().then(setDevices);
      }}
    >
      <Tip label={t("panel.more")} hidden={open}>
        <DropdownMenu.Trigger asChild>
          <button type="button" className="icon-button round" aria-label={t("panel.more")}>
            <IconMore />
          </button>
        </DropdownMenu.Trigger>
      </Tip>
      <DropdownMenu.Portal>
        <DropdownMenu.Content className="menu web-menu" side="bottom" align="end" sideOffset={6} collisionPadding={{ top: 48, bottom: 12, left: 8, right: 8 }}>
          <DropdownMenu.Item className={item} disabled={!page} onSelect={onFind}>
            <span className="menu-text">{t("web.find")}</span>
            <span className="menu-keys">Ctrl+F</span>
          </DropdownMenu.Item>
          <DropdownMenu.Item
            className={item}
            disabled={!page}
            onSelect={async () => {
              const r = await browser?.print(tab.id);
              if (r && r.ok === false) toast(r.error === "none" ? t("web.printNone") : t("web.printFailed"), "danger");
            }}
          >
            <span className="menu-text">{t("web.print")}</span>
          </DropdownMenu.Item>
          <div className="menu-zoom" role="group" aria-label={t("web.zoom")}>
            <span className="menu-text">{t("web.zoom")}</span>
            <DropdownMenu.Item className="zoom-step" disabled={!page} aria-label={t("web.zoomOut")} onSelect={(e) => { e.preventDefault(); void browser?.zoom(tab.id, "out"); }}>
              <IconMinus />
            </DropdownMenu.Item>
            <span className="zoom-value" aria-live="polite">
              {zoom}%
            </span>
            <DropdownMenu.Item className="zoom-step" disabled={!page} aria-label={t("web.zoomIn")} onSelect={(e) => { e.preventDefault(); void browser?.zoom(tab.id, "in"); }}>
              <IconPlus />
            </DropdownMenu.Item>
            <DropdownMenu.Item className="zoom-step" disabled={!page || zoom === 100} aria-label={t("web.zoomReset")} onSelect={(e) => { e.preventDefault(); void browser?.zoom(tab.id, "reset"); }}>
              <IconReset />
            </DropdownMenu.Item>
          </div>
          <DropdownMenu.Separator className="menu-sep" />
          <DropdownMenu.Sub>
            <DropdownMenu.SubTrigger className={item} disabled={!page}>
              <span className="menu-text">{t("web.device")}</span>
              <IconChevron />
            </DropdownMenu.SubTrigger>
            <DropdownMenu.Portal>
              <DropdownMenu.SubContent className="menu web-menu" sideOffset={4} collisionPadding={8}>
                {devices.map((d) => (
                  <DropdownMenu.CheckboxItem key={d.id} className={item} checked={live?.device?.id === d.id} onSelect={() => void browser?.device(tab.id, d.id)}>
                    <span className="menu-text">{d.name}</span>
                    <span className="menu-keys">
                      {d.width}×{d.height}
                    </span>
                  </DropdownMenu.CheckboxItem>
                ))}
                <DropdownMenu.Item className={item} onSelect={() => set({ customDeviceOpen: true })}>
                  <span className="menu-text">{t("web.deviceCustom")}</span>
                </DropdownMenu.Item>
                <DropdownMenu.Item className={item} disabled={!live?.device} onSelect={() => void browser?.device(tab.id, null)}>
                  <span className="menu-text">{t("web.deviceOff")}</span>
                </DropdownMenu.Item>
              </DropdownMenu.SubContent>
            </DropdownMenu.Portal>
          </DropdownMenu.Sub>
          <DropdownMenu.Sub>
            <DropdownMenu.SubTrigger className={item} disabled={!page}>
              <span className="menu-text">{t("web.screenshot")}</span>
              <IconChevron />
            </DropdownMenu.SubTrigger>
            <DropdownMenu.Portal>
              <DropdownMenu.SubContent className="menu web-menu" sideOffset={4} collisionPadding={8}>
                <DropdownMenu.Item
                  className={item}
                  onSelect={async () => {
                    const saved = await browser?.screenshotSave(tab.id);
                    if (saved) toast(`${t("web.screenshotSaved")}: ${saved}`);
                  }}
                >
                  <span className="menu-text">{t("web.screenshotSave")}</span>
                </DropdownMenu.Item>
                <DropdownMenu.Item
                  className={item}
                  onSelect={async () => {
                    const shot = await browser?.screenshotData(tab.id);
                    if (!shot) return toast(t("web.screenshotFailed"), "danger");
                    useApp.getState().attachToComposer(shot, activeThread());
                    toast(t("web.screenshotAttached"));
                  }}
                >
                  <span className="menu-text">{t("web.screenshotAttach")}</span>
                </DropdownMenu.Item>
              </DropdownMenu.SubContent>
            </DropdownMenu.Portal>
          </DropdownMenu.Sub>
          <DropdownMenu.Separator className="menu-sep" />
          <DropdownMenu.Item className={item} onSelect={() => set({ importOpen: true })}>
            <span className="menu-text">{t("web.import")}</span>
          </DropdownMenu.Item>
          <DropdownMenu.Sub>
            <DropdownMenu.SubTrigger className={item}>
              <span className="menu-text">{t("web.passwordsAutofill")}</span>
              <IconChevron />
            </DropdownMenu.SubTrigger>
            <DropdownMenu.Portal>
              <DropdownMenu.SubContent className="menu web-menu" sideOffset={4} collisionPadding={8}>
                <DropdownMenu.Item className={item} onSelect={() => usePanel.getState().openView("passwords")}>
                  <span className="menu-text">{t("vault.title")}</span>
                </DropdownMenu.Item>
                <DropdownMenu.Item className={item} onSelect={() => usePanel.getState().openView("addresses")}>
                  <span className="menu-text">{t("addr.title")}</span>
                </DropdownMenu.Item>
                <DropdownMenu.Separator className="menu-sep" />
                <DropdownMenu.CheckboxItem
                  className={item}
                  checked={settings.offerSave}
                  onSelect={async (e) => {
                    e.preventDefault();
                    const next = await window.atlas?.data.settings.set("offerSave", !settings.offerSave);
                    if (next) set({ settings: next });
                  }}
                >
                  <span className="menu-text">{t("bset.offerSave")}</span>
                </DropdownMenu.CheckboxItem>
                <DropdownMenu.CheckboxItem
                  className={item}
                  checked={settings.addressAutofill}
                  onSelect={async (e) => {
                    e.preventDefault();
                    const next = await window.atlas?.data.settings.set("addressAutofill", !settings.addressAutofill);
                    if (next) set({ settings: next });
                  }}
                >
                  <span className="menu-text">{t("bset.addressAutofill")}</span>
                </DropdownMenu.CheckboxItem>
              </DropdownMenu.SubContent>
            </DropdownMenu.Portal>
          </DropdownMenu.Sub>
          <DropdownMenu.Item className={item} onSelect={() => set({ downloadsOpen: true })}>
            <span className="menu-text">{t("web.downloads")}</span>
          </DropdownMenu.Item>
          <DropdownMenu.Item className={item} onSelect={() => usePanel.getState().openView("history")}>
            <span className="menu-text">{t("web.history")}</span>
          </DropdownMenu.Item>
          <DropdownMenu.Item className={item} onSelect={() => set({ clearOpen: true })}>
            <span className="menu-text">{t("web.clear")}</span>
          </DropdownMenu.Item>
          <DropdownMenu.Separator className="menu-sep" />
          <DropdownMenu.Item className={item} onSelect={() => usePanel.getState().openView("settings")}>
            <span className="menu-text">{t("web.settings")}</span>
          </DropdownMenu.Item>
          <DropdownMenu.Separator className="menu-sep" />
          <DropdownMenu.Item className={item} disabled={!page} onSelect={() => page && void window.atlas?.openExternal(url)}>
            <span className="menu-text">{t("panel.openBrowser")}</span>
          </DropdownMenu.Item>
          <DropdownMenu.Item className={item} disabled={!page} onSelect={() => void navigator.clipboard.writeText(url).then(() => toast(t("panel.copied")))}>
            <span className="menu-text">{t("panel.copyAddress")}</span>
          </DropdownMenu.Item>
        </DropdownMenu.Content>
      </DropdownMenu.Portal>
    </DropdownMenu.Root>
  );
}

export { SiteFavicon };
