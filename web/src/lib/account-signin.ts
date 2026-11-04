// Signing in to a subscription account from the browser. The gateway runs the
// flow and keeps the token; the page only shows the code and watches for the
// end. The web client and Dots draw their own controls on top of this.

import { createSignal, onCleanup } from "solid-js"
import { api, type AccountLogin } from "../api/client"

const POLL_MS = 2000

export function useAccountSignIn(wsId: () => string | undefined, provider: string, onDone: () => void) {
  const [login, setLogin] = createSignal<AccountLogin>()
  const [starting, setStarting] = createSignal(false)
  let timer: ReturnType<typeof setTimeout> | undefined

  const stopWatching = () => {
    clearTimeout(timer)
    timer = undefined
  }

  const watch = (id: string) => {
    timer = setTimeout(async () => {
      try {
        const next = await api.accountLogin(id, provider)
        if (timer === undefined) return
        if (next.state === "waiting") {
          setLogin(next)
          watch(id)
          return
        }
        timer = undefined
        setLogin(next.state === "done" ? undefined : next)
        if (next.state === "done") onDone()
      } catch (err) {
        timer = undefined
        setLogin({ state: "failed", error: (err as Error).message })
      }
    }, POLL_MS)
  }

  const start = async () => {
    const id = wsId()
    if (!id || starting()) return
    setStarting(true)
    stopWatching()
    try {
      setLogin(await api.startAccountLogin(id, provider))
      watch(id)
    } catch (err) {
      setLogin({ state: "failed", error: (err as Error).message })
    } finally {
      setStarting(false)
    }
  }

  const cancel = () => {
    const id = wsId()
    const waiting = login()?.state === "waiting"
    stopWatching()
    setLogin(undefined)
    if (id && waiting) void api.cancelAccountLogin(id, provider).catch(() => undefined)
  }

  onCleanup(cancel)
  return { login, starting, start, cancel }
}
