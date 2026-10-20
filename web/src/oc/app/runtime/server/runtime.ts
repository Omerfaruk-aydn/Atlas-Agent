// A small stand-in for Atlas Agent's per-server context: the copied views ask
// it for a session's project, the home directory and live events.
import type { Accessor } from "solid-js"
import type { SessionInfo } from "@atlas/client/promise"
import type { LocalProject } from "@/shell/state/layout"
import type { ServerConnection } from "@/runtime/server/registry"
import { findSession, projectOf } from "../../../adapters"

export type ServerCtx = ReturnType<typeof createServerCtx>

function createServerCtx() {
  return {
    projects: {
      forSession: (session: SessionInfo): LocalProject | undefined => {
        const hit = findSession(session.id)
        return hit ? projectOf(hit.store) : undefined
      },
    },
    sync: { data: { path: { home: "" } } },
    data: {
      on: (_event: string, _cb: (event: any) => void) => () => {},
      session: { root: (id: string) => findSession(id)?.session.parent_session_id || id },
    },
  }
}

const ctx = createServerCtx()

export function useServerCtx(_conn: Accessor<ServerConnection.Any | undefined>): Accessor<ServerCtx | undefined> {
  return () => ctx
}
