// Atlas Agent's current-server hook for the copied session header pieces.
import type { SessionInfo } from "@atlas/client/promise"
import { atlasServer, ServerConnection } from "@/runtime/server/registry"
import { findSession, sessionInfo } from "../../../adapters"

export function useServer() {
  return {
    key: ServerConnection.key(atlasServer),
    conn: atlasServer,
    isLocal: false,
    ctx: {
      data: {
        session: {
          get: (id: string): SessionInfo | undefined => {
            const hit = findSession(id)
            return hit ? sessionInfo(hit.store, hit.session) : undefined
          },
          status: (id: string) => {
            const hit = findSession(id)
            return hit && ["running", "starting"].includes(hit.store.runState(id)) ? "running" : "idle"
          },
        },
      },
    },
  }
}
