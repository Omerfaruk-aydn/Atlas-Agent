import { Tabs } from "radix-ui";
import { useEffect, useMemo, useState, type ReactNode } from "react";
import type { PermissionMode } from "../../api/types";
import { t, type Key } from "../../lib/i18n";
import { canBind, chord, holderOf, keysOf, rebind, SHORTCUTS, type Shortcut } from "../../lib/shortcuts";
import { IconReset, IconSearch } from "../../primitives/icons";
import { Picker } from "../../primitives/Picker";
import { Kbd, Modal, Segmented, Switch, Tip } from "../../primitives/ui";
import { reloadKeepingChat, useApp, type SettingsTab } from "../../stores/app";
import { ProviderLogo } from "../models/ProviderLogo";
import { Providers } from "../models/Providers";

const TABS: { id: SettingsTab; label: Key }[] = [
  { id: "general", label: "settings.general" },
  { id: "appearance", label: "settings.appearance" },
  { id: "chat", label: "settings.chat" },
  { id: "models", label: "settings.models" },
  { id: "permissions", label: "settings.permissions" },
  { id: "integrations", label: "settings.integrations" },
  { id: "shortcuts", label: "settings.shortcuts" },
  { id: "about", label: "settings.about" },
];

function Row({ title, hint, children }: { title: string; hint?: string; children: ReactNode }) {
  return (
    <div className="setting">
      <div className="setting-text">
        <div>{title}</div>
        {hint && <div className="setting-hint">{hint}</div>}
      </div>
      <div className="setting-control">{children}</div>
    </div>
  );
}

function General() {
  const ui = useApp((s) => s.ui);
  const setUI = useApp((s) => s.setUI);
  return (
    <>
      <Row title={t("settings.language")} hint={t("settings.languageHint")}>
        <Segmented
          label={t("settings.language")}
          value={ui.lang}
          options={[
            { value: "system", label: t("common.system") },
            { value: "tr", label: "Türkçe" },
            { value: "en", label: "English" },
          ]}
          onChange={(v) => {
            setUI("lang", v);
            setTimeout(reloadKeepingChat, 150);
          }}
        />
      </Row>
      <Row title={t("settings.notifyDone")} hint={t("settings.notifyDoneHint")}>
        <Switch label={t("settings.notifyDone")} checked={ui.notifyDone} onChange={(v) => setUI("notifyDone", v)} />
      </Row>
      <Row title={t("settings.notifyWait")} hint={t("settings.notifyWaitHint")}>
        <Switch label={t("settings.notifyWait")} checked={ui.notifyWait} onChange={(v) => setUI("notifyWait", v)} />
      </Row>
    </>
  );
}

function Appearance() {
  const theme = useApp((s) => s.theme);
  const motion = useApp((s) => s.motion);
  const setTheme = useApp((s) => s.setTheme);
  const setMotion = useApp((s) => s.setMotion);
  return (
    <>
      <Row title={t("settings.theme")}>
        <Segmented
          label={t("settings.theme")}
          value={theme}
          options={[
            { value: "system", label: t("common.system") },
            { value: "light", label: t("settings.light") },
            { value: "dark", label: t("settings.dark") },
          ]}
          onChange={setTheme}
        />
      </Row>
      <Row title={t("settings.motion")} hint={t("settings.motionHint")}>
        <Segmented
          label={t("settings.motion")}
          value={motion}
          options={[
            { value: "system", label: t("common.system") },
            { value: "full", label: t("settings.motionFull") },
            { value: "reduce", label: t("settings.motionReduce") },
          ]}
          onChange={setMotion}
        />
      </Row>
    </>
  );
}

function Chat() {
  const ui = useApp((s) => s.ui);
  const setUI = useApp((s) => s.setUI);
  const settings = useApp((s) => s.settings);
  const setCompaction = useApp((s) => s.setCompaction);
  const [percent, setPercent] = useState(String(settings?.auto_compact_at || ""));
  useEffect(() => setPercent(String(settings?.auto_compact_at || "")), [settings?.auto_compact_at]);
  return (
    <>
      <Row title={t("settings.foldTurns")} hint={t("settings.foldTurnsHint")}>
        <Switch label={t("settings.foldTurns")} checked={ui.foldTurns} onChange={(v) => setUI("foldTurns", v)} />
      </Row>
      <Row title={t("settings.showThinking")}>
        <Switch label={t("settings.showThinking")} checked={ui.showThinking} onChange={(v) => setUI("showThinking", v)} />
      </Row>
      <Row title={t("settings.autoCompact")} hint={t("settings.autoCompactHint")}>
        <Switch label={t("settings.autoCompact")} checked={settings?.auto_compact ?? true} onChange={(enabled) => void setCompaction({ enabled })} />
      </Row>
      <Row title={t("settings.compactAt")} hint={t("settings.compactAtHint")}>
        <input
          className="number-input"
          type="number"
          min={0}
          max={99}
          value={percent}
          placeholder="auto"
          aria-label={t("settings.compactAt")}
          onChange={(e) => setPercent(e.target.value)}
          onBlur={() => {
            const value = Number(percent) || 0;
            if (value !== (settings?.auto_compact_at ?? 0) && value >= 0 && value <= 99) void setCompaction({ percent: value });
          }}
        />
      </Row>
    </>
  );
}

function ModelRole({ type, label, hint }: { type: "large" | "small"; label: Key; hint: Key }) {
  const catalog = useApp((s) => s.catalog);
  const selectModel = useApp((s) => s.selectModel);
  const current = catalog?.selected?.[type];
  const value = current ? `${current.provider}\0${current.model}` : "";
  const options = useMemo(
    () => (catalog?.providers ?? []).flatMap((p) => p.models.map((m) => ({ value: `${p.id}\0${m.id}`, label: m.name, group: p.name, icon: <ProviderLogo id={p.id} size={16} /> }))),
    [catalog],
  );
  return (
    <Row title={t(label)} hint={t(hint)}>
      <Picker
        label={t(label)}
        value={value}
        options={options}
        placeholder={t("composer.noModel")}
        searchLabel={t("models.search")}
        emptyLabel={t("sidebar.noResults")}
        onChange={(next) => {
          const [provider, model] = next.split("\0");
          void selectModel(provider, model, { type });
        }}
      />
    </Row>
  );
}

function Models() {
  return (
    <>
      <ModelRole type="large" label="settings.largeModel" hint="settings.largeModelHint" />
      <ModelRole type="small" label="settings.smallModel" hint="settings.smallModelHint" />
      <h3 className="settings-heading">{t("settings.providers")}</h3>
      <Providers />
    </>
  );
}

const MODES: { id: PermissionMode; label: Key; hint: Key }[] = [
  { id: "manual", label: "mode.manual", hint: "mode.manual.hint" },
  { id: "auto_accept_edits", label: "mode.autoEdits", hint: "mode.autoEdits.hint" },
  { id: "plan", label: "mode.plan", hint: "mode.plan.hint" },
  { id: "bypass", label: "mode.bypass", hint: "mode.bypass.hint" },
];

function Permissions() {
  const mode = useApp((s) => s.settings?.permission_mode ?? "manual");
  const setMode = useApp((s) => s.setPermissionMode);
  return (
    <div className="radio-list" role="radiogroup" aria-label={t("settings.permissions")}>
      {MODES.map((m) => (
        <button key={m.id} type="button" role="radio" aria-checked={m.id === mode} className="radio-row" data-danger={m.id === "bypass" || undefined} onClick={() => void setMode(m.id)}>
          <span className="radio-mark" />
          <span>
            <span className="radio-title">{t(m.label)}</span>
            <span className="setting-hint">{t(m.hint)}</span>
          </span>
        </button>
      ))}
    </div>
  );
}

function State({ value, error }: { value: string; error?: string }) {
  const good = /ready|connected|loaded|enabled/i.test(value);
  const bad = /error|failed/i.test(value) || !!error;
  return (
    <span className="chip" data-tone={bad ? "danger" : good ? "success" : undefined} title={error}>
      {value}
    </span>
  );
}

function Integrations() {
  const settings = useApp((s) => s.settings);
  const load = useApp((s) => s.loadSettings);
  useEffect(() => {
    void load().catch(() => {});
  }, [load]);
  const section = (title: Key, rows: { name: string; state: string; error?: string; note?: string }[]) => (
    <>
      <h3 className="settings-heading">{t(title)}</h3>
      {rows.length === 0 && <p className="settings-note">{t("settings.none")}</p>}
      {rows.map((r) => (
        <div className="status-row" key={r.name}>
          <span>{r.name}</span>
          {r.note && <span className="setting-hint">{r.note}</span>}
          <State value={r.state} error={r.error} />
        </div>
      ))}
    </>
  );
  return (
    <>
      {section("settings.mcp", (settings?.mcp ?? []).map((m) => ({ ...m, note: m.tools ? `${m.tools} ${t("settings.tools")}` : undefined })))}
      {section("settings.lsp", (settings?.lsp ?? []).map((l) => ({ ...l, note: l.diagnostics ? `${l.diagnostics} ${t("settings.diagnostics")}` : undefined })))}
      {section("settings.skills", settings?.skills ?? [])}
    </>
  );
}

function ShortcutRow({ shortcut }: { shortcut: Shortcut }) {
  const overrides = useApp((s) => s.ui.shortcuts);
  const setUI = useApp((s) => s.setUI);
  const [recording, setRecording] = useState(false);
  const [clash, setClash] = useState<{ keys: string; holder: Shortcut } | null>(null);
  const keys = keysOf(shortcut, overrides);
  const changed = shortcut.id in overrides;

  useEffect(() => {
    if (!recording) return;
    // Ahead of everything else, so the keys only land here: Esc must not
    // close the dialog and a chord in use must not run its shortcut.
    const onKey = (e: KeyboardEvent) => {
      e.preventDefault();
      e.stopPropagation();
      if (e.key === "Escape") {
        setRecording(false);
        return;
      }
      if (!canBind(e)) return;
      const next = chord(e);
      const holder = holderOf(next, overrides, shortcut.id);
      setRecording(false);
      if (holder) setClash({ keys: next, holder });
      else setUI("shortcuts", rebind(shortcut.id, next, overrides));
    };
    window.addEventListener("keydown", onKey, true);
    return () => window.removeEventListener("keydown", onKey, true);
  }, [recording, overrides, shortcut.id, setUI]);

  const reset = () => {
    const { [shortcut.id]: _, ...rest } = overrides;
    setUI("shortcuts", rest);
    setClash(null);
  };

  return (
    <div className="shortcut-row" data-recording={recording || undefined}>
      <div className="shortcut-main">
        <span className="shortcut-name">{t(shortcut.label)}</span>
        {changed && (
          <Tip label={t("shortcuts.reset")}>
            <button type="button" className="icon-button" data-size="xs" aria-label={t("shortcuts.reset")} onClick={reset}>
              <IconReset />
            </button>
          </Tip>
        )}
        <button
          type="button"
          className="shortcut-keys"
          aria-label={`${t(shortcut.label)}: ${keys.join(", ") || t("shortcuts.none")}`}
          onClick={() => {
            setClash(null);
            setRecording(!recording);
          }}
          onBlur={() => setRecording(false)}
        >
          {recording ? <span className="shortcut-prompt">{t("shortcuts.record")}</span> : keys.length > 0 ? keys.map((k) => <Kbd key={k} keys={k} />) : <span className="shortcut-prompt">{t("shortcuts.none")}</span>}
        </button>
      </div>
      {clash && (
        <div className="shortcut-clash">
          <Kbd keys={clash.keys} />
          <span>
            {t("shortcuts.taken")} {t(clash.holder.label)}
          </span>
          {!clash.holder.info && (
            <button
              type="button"
              className="link-button"
              onClick={() => {
                setUI("shortcuts", rebind(shortcut.id, clash.keys, overrides));
                setClash(null);
              }}
            >
              {t("shortcuts.replace")}
            </button>
          )}
          <button type="button" className="link-button" onClick={() => setClash(null)}>
            {t("common.cancel")}
          </button>
        </div>
      )}
    </div>
  );
}

function Shortcuts() {
  const overrides = useApp((s) => s.ui.shortcuts);
  const setUI = useApp((s) => s.setUI);
  const [query, setQuery] = useState("");
  const q = query.trim().toLocaleLowerCase();
  const matches = (s: Shortcut) => !q || t(s.label).toLocaleLowerCase().includes(q) || keysOf(s, overrides).some((k) => k.toLowerCase().includes(q));
  const shown = SHORTCUTS.filter((s) => !s.info && matches(s));
  const fixed = SHORTCUTS.filter((s) => s.info && matches(s));
  return (
    <>
      <p className="settings-note shortcut-intro">{t("shortcuts.hint")}</p>
      <div className="shortcut-tools">
        <label className="search search-boxed">
          <IconSearch />
          <input value={query} placeholder={t("shortcuts.search")} aria-label={t("shortcuts.search")} onChange={(e) => setQuery(e.target.value)} />
        </label>
        <button type="button" className="button" disabled={Object.keys(overrides).length === 0} onClick={() => setUI("shortcuts", {})}>
          {t("shortcuts.resetAll")}
        </button>
      </div>
      <div className="shortcut-list">
        {shown.map((s) => (
          <ShortcutRow key={s.id} shortcut={s} />
        ))}
      </div>
      {fixed.length > 0 && <h3 className="settings-heading">{t("shortcuts.fixed")}</h3>}
      <div className="shortcut-list">
        {fixed.map((s) => (
          <div className="shortcut-row" key={s.id}>
            <div className="shortcut-main">
              <span className="shortcut-name">{t(s.label)}</span>
              <span className="shortcut-fixed">
                {s.keys.map((k) => (
                  <Kbd key={k} keys={k} />
                ))}
              </span>
            </div>
          </div>
        ))}
      </div>
    </>
  );
}

function About() {
  const [info, setInfo] = useState<{ version: string; core: string; electron: string; packaged: boolean } | null>(null);
  const project = useApp((s) => s.workspace?.path ?? "");
  useEffect(() => {
    void window.atlas?.appInfo().then(setInfo);
  }, []);
  return (
    <>
      <Row title={t("settings.appVersion")}>{info?.version}</Row>
      <Row title={t("settings.coreVersion")}>{info?.core}</Row>
      <Row title="Electron">{info?.electron}</Row>
      <Row title={t("settings.project")}>
        <button type="button" className="link-button" onClick={() => void window.atlas?.reveal(project)}>
          {project}
        </button>
      </Row>
    </>
  );
}

const PAGES: Record<SettingsTab, () => ReactNode> = {
  general: General,
  appearance: Appearance,
  chat: Chat,
  models: Models,
  permissions: Permissions,
  integrations: Integrations,
  shortcuts: Shortcuts,
  about: About,
};

export function SettingsDialog() {
  const open = useApp((s) => s.dialog === "settings");
  const close = useApp((s) => s.openDialog);
  const tab = useApp((s) => s.settingsTab);
  return (
    <Modal open={open} onClose={() => close(null)} title={t("sidebar.settings")} hideTitle className="settings">
      <Tabs.Root className="settings-body" orientation="vertical" value={tab} onValueChange={(value) => useApp.setState({ settingsTab: value as SettingsTab })}>
        <Tabs.List className="settings-nav" aria-label={t("sidebar.settings")}>
          <div className="settings-title">{t("sidebar.settings")}</div>
          {TABS.map((item) => (
            <Tabs.Trigger key={item.id} value={item.id} className="settings-tab">
              {t(item.label)}
            </Tabs.Trigger>
          ))}
        </Tabs.List>
        {TABS.map((item) => {
          const Page = PAGES[item.id];
          return (
            <Tabs.Content key={item.id} value={item.id} className="settings-page">
              <h2>{t(item.label)}</h2>
              <Page />
            </Tabs.Content>
          );
        })}
      </Tabs.Root>
    </Modal>
  );
}
