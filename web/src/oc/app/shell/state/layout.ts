// Types the copied home views share with Atlas Agent's layout state.
import type { ServerConnection } from "@/runtime/server/registry"

export type LocalProject = {
  id: string
  worktree: string
  name?: string
  icon?: { url?: string; override?: string; color?: string }
}

export type HomeProjectSelection = { server: ServerConnection.Key; directory?: string }
