import { createMediaQuery } from "@solid-primitives/media"
import { DialogProvider } from "@atlas/ui/context/dialog"
import { ThemeProvider } from "@atlas/ui/theme/context"
import { Button } from "@atlas/ui/button"
import { Spinner } from "@atlas/ui/spinner"
import { Match, onMount, Suspense, Switch } from "solid-js"
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query"
import { app, bootstrap, currentWorkspace } from "./state/app"
import { LanguageProvider, t } from "./oc/language"
import { Titlebar } from "./oc/titlebar"
import { Home } from "./oc/home"
import { ToastRegion } from "./oc/toast"
import { HelpDialog } from "./components/HelpDialog"
import { useCatalog } from "./components/ModelPicker"
import { ProjectDialog } from "./components/ProjectDialog"
import { SessionScreen } from "./oc/session"
import { SettingsPage } from "./oc/settings"
import { NewSession } from "./oc/new-session"

const queryClient = new QueryClient({
  defaultOptions: { queries: { refetchOnReconnect: false, refetchOnWindowFocus: false, refetchOnMount: false } },
})

export function App() {
  onMount(() => void bootstrap())
  return (
    <ThemeProvider>
      <LanguageProvider>
        <QueryClientProvider client={queryClient}>
        <DialogProvider>
          <Switch>
            <Match when={app.auth === "loading"}>
              <div class="flex flex-1 items-center justify-center" role="status">
                <Spinner class="size-5" />
              </div>
            </Match>
            <Match when={app.auth === "signed-out"}>
              <Notice title={t("atlas.auth.title")} body={t("atlas.auth.body")}>
                <pre class="rounded-[6px] bg-v2-background-bg-layer-01 px-3 py-2 font-mono text-[12px]">atlas-agent web</pre>
              </Notice>
            </Match>
            <Match when={app.auth === "unreachable"}>
              <Notice title={t("atlas.offline.title")} body={t("atlas.offline.body")}>
                <Button variant="neutral" onClick={() => location.reload()}>
                  {t("common.retry")}
                </Button>
              </Notice>
            </Match>
            <Match when={app.auth === "signed-in"}>
              <Layout />
            </Match>
          </Switch>
        </DialogProvider>
        </QueryClientProvider>
      </LanguageProvider>
    </ThemeProvider>
  )
}

function Notice(props: { title: string; body: string; children?: any }) {
  return (
    <div class="flex flex-1 items-center justify-center p-4">
      <div class="flex w-full max-w-[420px] flex-col gap-3 rounded-[10px] bg-v2-background-bg-base p-6 shadow-[var(--v2-elevation-raised)]">
        <div class="text-[15px] text-v2-text-text-base [font-weight:530]">{props.title}</div>
        <div class="text-v2-text-text-muted">{props.body}</div>
        {props.children}
      </div>
    </div>
  )
}

/** Layout is Atlas Agent's shell (shell/shell.tsx): titlebar over the routed screen. */
function Layout() {
  const mobile = createMediaQuery("(max-width: 767px)")
  const store = () => currentWorkspace()
  const { catalog, mutate } = useCatalog(store)

  return (
    <div
      class="relative bg-v2-background-bg-deep flex-1 min-h-0 min-w-0 flex flex-col select-none [&_input]:select-text [&_textarea]:select-text [&_[contenteditable]]:select-text"
      style={{
        "--shell-inline-inset": mobile() ? "1px" : "8px",
        "--shell-top-inset": "8px",
        "--shell-bottom-inset": "max(0px, calc(8px - var(--safe-area-inset-bottom, env(safe-area-inset-bottom, 0px))))",
      }}
    >
      <Titlebar />
      <div class="flex flex-1 min-h-0 min-w-0 flex-row">
        <main
          class="flex-1 min-h-0 min-w-0 overflow-x-hidden flex flex-col items-start contain-content"
          style={{ "padding-bottom": app.screen === "settings" ? "0px" : "var(--safe-area-inset-bottom, env(safe-area-inset-bottom, 0px))" }}
        >
          <Suspense>
            <Switch>
              <Match when={app.screen === "settings"}>
                <SettingsPage catalog={catalog()} onCatalog={(c) => mutate(c)} />
              </Match>
              <Match when={app.screen === "draft" && app.draft}>
                {(id) => <NewSession draftID={id()} catalog={catalog()} onCatalog={(c) => mutate(c)} />}
              </Match>
              <Match when={app.screen === "session" && app.session && store()}>
                <SessionScreen store={store()!} sessionID={app.session!} catalog={catalog()} onCatalog={(c) => mutate(c)} />
              </Match>
              <Match when={true}>
                <Home />
              </Match>
            </Switch>
          </Suspense>
        </main>
      </div>
      <ToastRegion />
      <ProjectDialog />
      <HelpDialog />
    </div>
  )
}
