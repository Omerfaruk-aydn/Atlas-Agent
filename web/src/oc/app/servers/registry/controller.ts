// Atlas has one fixed server, so server actions are no-ops.
import type { ServerConnection } from "@/runtime/server/registry"

export type ServerActionsController = {
  connection: {
    canRemove: (key: ServerConnection.Key) => boolean
    canHide: (key: ServerConnection.Key) => boolean
    isHidden: (key: ServerConnection.Key) => boolean
    remove: (key: ServerConnection.Key) => void
    setHidden: (key: ServerConnection.Key, hidden: boolean) => void
  }
}
