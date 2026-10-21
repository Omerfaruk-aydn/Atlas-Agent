// Tab and row status, from the Atlas run state: a session waiting on the
// person is "unread" (the attention dot), a working one is "loading".
import { createMemo, type Accessor } from "solid-js"
import type { SessionInfo } from "@atlas/client/promise"
import type { ServerConnection } from "@/runtime/server/registry"
import { findSession } from "../../../adapters"

export function useSessionTabAvatarState(
  _server: Accessor<ServerConnection.Key>,
  sessionId: Accessor<string>,
  _root?: Accessor<boolean>,
  _snapshot?: Accessor<SessionInfo | undefined>,
) {
  const state = createMemo(() => {
    const hit = findSession(sessionId())
    return hit ? hit.store.runState(hit.session.id) : "idle"
  })
  const unread = createMemo(() => state() === "question" || state() === "permission")
  const loading = createMemo(() => state() === "running" || state() === "starting")
  return { unread, loading }
}
