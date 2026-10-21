// The settings page: Atlas Agent's settings screen (settings/shell.tsx,
// navigation.tsx, general, appearance) with its markup, classes and
// components, listing Atlas's own preferences, models, agent modes,
// extensions and server rows.

import { Button } from "@atlas/ui/button"
import { Icon, type IconProps } from "@atlas/ui/icon"
import { Menu } from "@atlas/ui/menu"
import { Select } from "@atlas/ui/select"
import { Tabs } from "@atlas/ui/tabs"
import { useTheme } from "@atlas/ui/theme/context"
import { createEffect, createMemo, createResource, createSignal, ErrorBoundary, For, on, onMount, Show, type JSX } from "solid-js"
import { SettingsSearchField } from "@/settings/search-field"
import { SettingsList } from "@/settings/list"
import { SettingsRow } from "@/settings/row"
import "@/settings/settings.css"
import { api } from "../api/client"
import type { Catalog, PermissionMode } from "../api/types"
import { t as at, type MessageKey } from "../i18n"
import { app, currentWorkspace, goHome, openSettings, workspaces } from "../state/app"
import { prefs, setPref, type LanguagePref } from "../state/prefs"
import { findModel } from "../components/ModelPicker"
import { useLanguage } from "./language"
import { showToast } from "./toast"

type Section = "preferences" | "appearance" | "models" | "agent" | "extensions" | "server" | "about"

const nav: { group: Section[] }[] = [
  { group: ["preferences", "appearance"] },
  { group: ["models", "agent", "extensions", "server"] },
  { group: ["about"] },
]

const icons: Record<Section, IconProps["name"]> = {
  preferences: "sliders",
  appearance: "appearance",
  models: "models",
  agent: "outline-worktree",
  extensions: "extensions",
  server: "server",
  about: "info",
}

export function SettingsPage(props: { catalog: Catalog | undefined; onCatalog: (c: Catalog) => void }) {
  const language = useLanguage()
  const [query, setQuery] = createSignal("")
  let root: HTMLDivElement | undefined
  onMount(() => root?.focus({ preventScroll: true }))

  const label = (id: Section) => (id === "appearance" ? language.t("settings.general.section.appearance") : id === "preferences" ? language.t("settings.tab.preferences") : id === "about" ? language.t("settings.tab.about") : at(`settingsNav.${id}` as MessageKey))
  const groups = createMemo(() =>
    nav
      .map((g) => ({ items: g.group.filter((id) => !query().trim() || label(id).toLowerCase().includes(query().trim().toLowerCase())) }))
      .filter((g) => g.items.length > 0),
  )
  const current = (): Section => (app.settingsSection === "general" ? "preferences" : (app.settingsSection as Section))
  const change = (value: string) => {
    setQuery("")
    openSettings(value)
  }

  return (
    <div
      ref={root}
      data-testid="settings-screen"
      class="settings-screen"
      tabIndex={-1}
      onKeyDown={(event) => {
        if (event.key !== "Escape" || event.defaultPrevented) return
        event.preventDefault()
        if (query().trim()) return setQuery("")
        goHome()
      }}
    >
      <Tabs orientation="vertical" variant="settings" value={current()} onChange={change} class="settings">
        <div class="settings-mobile-nav">
          <button type="button" class="settings-back" onClick={goHome}>
            <Icon name="arrow-left" size="small" class="settings-back-icon" />
            <span>{language.t("settings.backToApp")}</span>
          </button>
          <div class="settings-mobile-actions">
            <Menu placement="bottom-end" gutter={8}>
              <Menu.Trigger as={Button} size="normal" variant="outline" class="settings-mobile-menu-trigger">
                <span>{label(current())}</span>
                <Icon name="chevron-down" size="small" />
              </Menu.Trigger>
              <Menu.Portal>
                <Menu.Content class="settings-mobile-menu" onEscapeKeyDown={(event) => event.stopPropagation()}>
                  <Menu.RadioGroup value={current()} onChange={change}>
                    <For each={nav}>
                      {(group, index) => (
                        <>
                          <Show when={index() > 0}>
                            <Menu.Separator />
                          </Show>
                          <For each={group.group}>
                            {(id) => (
                              <Menu.RadioItem value={id} closeOnSelect>
                                <Icon name={icons[id]} />
                                {label(id)}
                              </Menu.RadioItem>
                            )}
                          </For>
                        </>
                      )}
                    </For>
                  </Menu.RadioGroup>
                </Menu.Content>
              </Menu.Portal>
            </Menu>
          </div>
        </div>
        <aside class="settings-sidebar" data-searchable="true">
          <div class="settings-nav">
            <button type="button" class="settings-back" onClick={goHome}>
              <Icon name="arrow-left" size="small" class="settings-back-icon" />
              <span>{language.t("settings.backToApp")}</span>
            </button>
            <SettingsSearchField value={query()} placeholder={language.t("settings.search.placeholder")} active onInput={setQuery} />
            <Tabs.List class="settings-nav-groups">
              <For each={groups()}>
                {(group) => (
                  <div class="settings-nav-group">
                    <For each={group.items}>
                      {(id) => (
                        <Tabs.Trigger value={id}>
                          <Icon name={icons[id]} />
                          {label(id)}
                        </Tabs.Trigger>
                      )}
                    </For>
                  </div>
                )}
              </For>
            </Tabs.List>
          </div>
        </aside>
        <div class="settings-content">
          <ErrorBoundary
            fallback={(error, reset) => {
              createEffect(on(current, reset, { defer: true }))
              return <div class="settings-tab-body">{String(error)}</div>
            }}
          >
            <Tabs.Content value="preferences" class="settings-panel">
              <Preferences />
            </Tabs.Content>
            <Tabs.Content value="appearance" class="settings-panel">
              <Appearance />
            </Tabs.Content>
            <Tabs.Content value="models" class="settings-panel">
              <Models catalog={props.catalog} onCatalog={props.onCatalog} />
            </Tabs.Content>
            <Tabs.Content value="agent" class="settings-panel">
              <Agent />
            </Tabs.Content>
            <Tabs.Content value="extensions" class="settings-panel">
              <Extensions />
            </Tabs.Content>
            <Tabs.Content value="server" class="settings-panel">
              <Server />
            </Tabs.Content>
            <Tabs.Content value="about" class="settings-panel settings-about">
              <About />
            </Tabs.Content>
          </ErrorBoundary>
        </div>
      </Tabs>
    </div>
  )
}

function Page(props: { title: string; description: string; children: JSX.Element }) {
  return (
    <>
      <div class="settings-tab-header">
        <div class="settings-tab-header-row">
          <div class="flex flex-col gap-1">
            <h2 class="settings-tab-title">{props.title}</h2>
            <span class="text-11-regular text-v2-text-text-muted">{props.description}</span>
          </div>
        </div>
      </div>
      <div class="settings-tab-body">{props.children}</div>
    </>
  )
}

function Section(props: { title?: string; children: JSX.Element }) {
  return (
    <div class="settings-section">
      <Show when={props.title}>
        <h3 class="settings-section-title">{props.title}</h3>
      </Show>
      <SettingsList>{props.children}</SettingsList>
    </div>
  )
}

function NeedsProject() {
  return <p class="text-14-regular text-v2-text-text-muted">{at("settings.needsProject")}</p>
}

function Preferences() {
  const language = useLanguage()
  const options: { value: LanguagePref; label: string }[] = [
    { value: "system", label: at("settings.languageSystem") },
    { value: "en", label: "English" },
    { value: "tr", label: "Türkçe" },
  ]
  const styles: { value: "unified" | "split"; label: string }[] = [
    { value: "unified", label: at("review.unified") },
    { value: "split", label: at("review.split") },
  ]
  return (
    <Page title={language.t("settings.tab.preferences")} description={at("settings.preferencesDescription")}>
      <Section title={language.t("settings.general.section.general")}>
        <SettingsRow title={language.t("settings.general.row.language.title")} description={`${at("settings.languageDescription")} ${at("settings.browserOnly")}`}>
          <Select
            data-action="settings-language"
            options={options}
            current={options.find((o) => o.value === prefs().language)}
            value={(o) => o.value}
            label={(o) => o.label}
            placement="bottom-end"
            gutter={6}
            onSelect={(o) => o && setPref("language", o.value)}
          />
        </SettingsRow>
        <SettingsRow title={at("settings.diffStyle")} description={`${at("settings.diffStyleDescription")} ${at("settings.browserOnly")}`}>
          <Select
            data-action="settings-diff-style"
            options={styles}
            current={styles.find((o) => o.value === prefs().diffStyle)}
            value={(o) => o.value}
            label={(o) => o.label}
            placement="bottom-end"
            gutter={6}
            onSelect={(o) => o && setPref("diffStyle", o.value)}
          />
        </SettingsRow>
      </Section>
    </Page>
  )
}

const schemeOptions: ("system" | "light" | "dark")[] = ["system", "light", "dark"]

function Appearance() {
  const language = useLanguage()
  const theme = useTheme()
  const themes = createMemo(() => theme.ids().map((id) => ({ id, name: theme.name(id) })))
  return (
    <Page title={language.t("settings.general.section.appearance")} description={language.t("settings.appearance.description")}>
      <Section>
        <SettingsRow title={language.t("settings.general.row.colorScheme.title")} description={language.t("settings.general.row.colorScheme.description")}>
          <Select
            data-action="settings-color-scheme"
            options={schemeOptions}
            current={schemeOptions.find((o) => o === theme.colorScheme())}
            placement="bottom-end"
            gutter={6}
            label={(o) => language.t(`theme.scheme.${o}`)}
            onSelect={(o) => o && theme.setColorScheme(o)}
          />
        </SettingsRow>
        <SettingsRow title={language.t("settings.general.row.theme.title")} description={language.t("settings.general.row.theme.description")}>
          <Select
            data-action="settings-theme"
            options={themes()}
            current={themes().find((o) => o.id === theme.themeId())}
            placement="bottom-end"
            gutter={6}
            value={(o) => o.id}
            label={(o) => o.name}
            onSelect={(o) => o && theme.setTheme(o.id)}
          />
        </SettingsRow>
      </Section>
    </Page>
  )
}

function Models(props: { catalog: Catalog | undefined; onCatalog: (c: Catalog) => void }) {
  const language = useLanguage()
  const store = () => currentWorkspace() ?? workspaces()[0]
  const [scope, setScope] = createSignal<"global" | "workspace">("global")
  const choose = async (type: "large" | "small", value: string, effort?: string) => {
    const s = store()
    if (!s || !value) return
    const [provider, model] = value.split("\u0000")
    try {
      props.onCatalog(await api.selectModel(s.id, { type, provider, model, reasoning_effort: effort, scope: scope() }))
      showToast({ title: at("model.saved") })
    } catch (err) {
      showToast({ title: language.t("common.requestFailed"), description: (err as Error).message })
    }
  }
  const models = createMemo(() => (props.catalog?.providers ?? []).flatMap((p) => p.models.map((m) => ({ value: `${p.id}\u0000${m.id}`, label: `${p.name} · ${m.name}` }))))
  const large = () => props.catalog?.selected?.large
  const small = () => props.catalog?.selected?.small
  const largeModel = () => findModel(props.catalog, large()?.provider, large()?.model)
  const scopes: { value: "global" | "workspace"; label: string }[] = [
    { value: "global", label: at("model.scopeGlobal") },
    { value: "workspace", label: at("model.scopeWorkspace") },
  ]
  const levels = () => [{ value: "", label: at("model.effortDefault", { level: largeModel()?.default_reasoning_effort ?? "—" }) }, ...(largeModel()?.reasoning_levels ?? []).map((l) => ({ value: l, label: l }))]
  return (
    <Page title={at("settingsNav.models")} description={at("settings.modelsDescription")}>
      <Show when={store()} fallback={<NeedsProject />}>
        <Section title={at("settings.models")}>
          <SettingsRow title={at("model.scope")} description={at("settings.scopeDescription", { project: store()!.state.view.name })}>
            <Select options={scopes} current={scopes.find((o) => o.value === scope())} value={(o) => o.value} label={(o) => o.label} placement="bottom-end" gutter={6} onSelect={(o) => o && setScope(o.value)} />
          </SettingsRow>
          <SettingsRow title={at("settings.large")} description={props.catalog?.active ? `${props.catalog.active.provider}/${props.catalog.active.model}` : ""}>
            <Select
              options={models()}
              current={models().find((o) => o.value === `${large()?.provider ?? ""}\u0000${large()?.model ?? ""}`)}
              value={(o) => o.value}
              label={(o) => o.label}
              placement="bottom-end"
              gutter={6}
              onSelect={(o) => o && choose("large", o.value, large()?.reasoning_effort)}
            />
          </SettingsRow>
          <Show when={largeModel()?.can_reason && (largeModel()?.reasoning_levels?.length ?? 0) > 0}>
            <SettingsRow title={at("model.effort")} description={at("settings.effortDescription")}>
              <Select
                options={levels()}
                current={levels().find((o) => o.value === (large()?.reasoning_effort ?? ""))}
                value={(o) => o.value}
                label={(o) => o.label}
                placement="bottom-end"
                gutter={6}
                onSelect={(o) => o && choose("large", `${large()!.provider}\u0000${large()!.model}`, o.value || undefined)}
              />
            </SettingsRow>
          </Show>
          <SettingsRow title={at("settings.small")} description="">
            <Select
              options={models()}
              current={models().find((o) => o.value === `${small()?.provider ?? ""}\u0000${small()?.model ?? ""}`)}
              value={(o) => o.value}
              label={(o) => o.label}
              placement="bottom-end"
              gutter={6}
              onSelect={(o) => o && choose("small", o.value)}
            />
          </SettingsRow>
        </Section>
      </Show>
    </Page>
  )
}

function useWorkspaceSettings() {
  const [settings, { mutate }] = createResource(
    () => {
      const s = currentWorkspace() ?? workspaces()[0]
      return s ? { id: s.id, v: s.state.versions.settings } : undefined
    },
    ({ id }) => api.settings(id),
  )
  // The last loaded value, so a refresh never suspends the page.
  return { settings: () => settings.latest, mutate }
}

function Agent() {
  const language = useLanguage()
  const { settings, mutate } = useWorkspaceSettings()
  const store = () => currentWorkspace() ?? workspaces()[0]
  const modes: PermissionMode[] = ["manual", "auto_accept_edits", "plan", "bypass"]
  const fail = (err: unknown) => showToast({ title: language.t("common.requestFailed"), description: err instanceof Error ? err.message : String(err) })
  return (
    <Page title={at("settingsNav.agent")} description={at("settings.agentDescription")}>
      <Show when={store()} fallback={<NeedsProject />}>
        <Show when={settings()} fallback={<p class="text-14-regular text-v2-text-text-muted">{language.t("common.loading")}</p>}>
          {(s) => {
            const sessionModes = () => [{ value: "", label: at("mode.none") }, ...s().modes.map((m) => ({ value: m.name, label: m.name }))]
            return (
              <Section title={store()!.state.view.name}>
                <SettingsRow title={at("mode.permission")} description={`${at(`mode.${s().permission_mode}Hint` as MessageKey)} ${at("mode.workspaceNote")}`}>
                  <Select
                    options={modes}
                    current={s().permission_mode}
                    placement="bottom-end"
                    gutter={6}
                    label={(m) => at(`mode.${m}` as MessageKey)}
                    onSelect={async (m) => {
                      if (!m) return
                      try {
                        const res = await api.setPermissionMode(store()!.id, m)
                        mutate((v) => (v ? { ...v, permission_mode: res.mode } : v))
                        store()!.settingsChanged()
                      } catch (err) {
                        fail(err)
                      }
                    }}
                  />
                </SettingsRow>
                <SettingsRow title={at("mode.session")} description={`${s().modes.find((m) => m.name === s().session_mode)?.description ?? at("mode.noneHint")} ${at("mode.globalNote")}`}>
                  <Select
                    options={sessionModes()}
                    current={sessionModes().find((o) => o.value === s().session_mode)}
                    value={(o) => o.value}
                    label={(o) => o.label}
                    placement="bottom-end"
                    gutter={6}
                    onSelect={async (o) => {
                      if (!o) return
                      try {
                        const res = await api.setSessionMode(store()!.id, o.value)
                        mutate((v) => (v ? { ...v, session_mode: res.session_mode } : v))
                      } catch (err) {
                        fail(err)
                      }
                    }}
                  />
                </SettingsRow>
              </Section>
            )
          }}
        </Show>
      </Show>
    </Page>
  )
}

function StateChip(props: { state: string }) {
  return (
    <span class="inline-flex items-center gap-1.5 text-12-regular text-v2-text-text-muted">
      <span class="size-1.5 rounded-full" classList={{ "bg-v2-icon-success": props.state === "connected" || props.state === "ready" || props.state === "started", "bg-v2-icon-critical": props.state === "error", "bg-v2-icon-muted": !["connected", "ready", "started", "error"].includes(props.state) }} aria-hidden="true" />
      {props.state}
    </span>
  )
}

function Extensions() {
  const { settings } = useWorkspaceSettings()
  const list = (items: { name: string; state: string; detail?: string }[]) => (
    <For each={items} fallback={<SettingsRow title={at("settings.none")} description="">{null}</SettingsRow>}>
      {(i) => (
        <SettingsRow title={i.name} description={i.detail || ""}>
          <StateChip state={i.state} />
        </SettingsRow>
      )}
    </For>
  )
  return (
    <Page title={at("settingsNav.extensions")} description={at("settings.extensionsDescription")}>
      <Show when={settings()} fallback={<NeedsProject />}>
        {(s) => (
          <>
            <Section title={at("settings.mcp")}>{list(s().mcp.map((m) => ({ name: m.name, state: m.state, detail: m.error || (m.tools ? at("settings.tools", { n: m.tools }) : "") })))}</Section>
            <Section title={at("settings.skills")}>{list(s().skills.map((k) => ({ name: k.name, state: k.error ? "error" : k.state, detail: k.error })))}</Section>
            <Section title={at("settings.lsp")}>{list(s().lsp.map((l) => ({ name: l.name, state: l.state })))}</Section>
          </>
        )}
      </Show>
    </Page>
  )
}

function Server() {
  return (
    <Page title={at("settingsNav.server")} description={at("settings.serverDescription")}>
      <Section title={at("settings.server")}>
        <SettingsRow title={at("settings.serverAddress")} description={app.boot?.server.address ?? ""}>
          <StateChip state={app.boot?.server.reachable ? "connected" : "error"} />
        </SettingsRow>
        <SettingsRow title={at("settings.versions")} description={at("settings.version", { web: app.boot?.version ?? "?", server: app.boot?.server.version ?? "?" })}>
          {null}
        </SettingsRow>
        <SettingsRow title={at("settings.signOut")} description={at("settings.signOutDescription")}>
          <Button
            variant="outline"
            size="normal"
            onClick={async () => {
              await api.signOut().catch(() => undefined)
              location.reload()
            }}
          >
            {at("settings.signOut")}
          </Button>
        </SettingsRow>
      </Section>
    </Page>
  )
}

function About() {
  const language = useLanguage()
  return (
    <Page title={language.t("settings.tab.about")} description={at("settings.aboutDescription")}>
      <Section>
        <SettingsRow title="Atlas" description={at("settings.version", { web: app.boot?.version ?? "?", server: app.boot?.server.version ?? "?" })}>
          {null}
        </SettingsRow>
        <SettingsRow title={at("settings.thirdParty")} description={at("settings.thirdPartyDescription")}>
          {null}
        </SettingsRow>
      </Section>
    </Page>
  )
}
