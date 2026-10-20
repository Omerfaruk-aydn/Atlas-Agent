// Bridges Atlas state (projects = server workspaces, their sessions and run
// states) to the shapes the copied Atlas Agent views expect.

import type { Session } from "../api/types"
import type { WorkspaceStore } from "../state/workspace"
import { workspaces } from "../state/app"
import type { SessionInfo } from "./shims/client"
import type { LocalProject } from "@/shell/state/layout"

export function projectOf(store: WorkspaceStore): LocalProject {
  return { id: store.id, worktree: store.state.view.path, name: store.state.view.name }
}

export function sessionInfo(store: WorkspaceStore, s: Session): SessionInfo {
  return {
    id: s.id,
    title: s.title,
    parentID: s.parent_session_id || undefined,
    projectID: store.id,
    location: { directory: store.state.view.path },
    time: { created: s.created_at * 1000, updated: s.updated_at * 1000 },
    cost: s.cost,
    tokens: { input: s.prompt_tokens, output: s.completion_tokens, reasoning: 0, cache: { read: 0, write: 0 } },
  }
}

/** findSession locates a session among the open projects. */
export function findSession(id: string): { store: WorkspaceStore; session: Session } | undefined {
  for (const store of workspaces()) {
    const session = store.state.sessions.find((s) => s.id === id)
    if (session) return { store, session }
  }
  return undefined
}
