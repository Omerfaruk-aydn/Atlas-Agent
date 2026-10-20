// The types and status helper the copied home sessions view imports from
// Atlas Agent's controller; the controller itself is ../../../home.tsx.
import type { Accessor, JSX } from "solid-js"
import type { SessionInfo } from "@atlas/client/promise"
import type { LocalProject } from "@/shell/state/layout"
import type { ServerConnection } from "@/runtime/server/registry"
import { useSessionTabAvatarState } from "@/shell/layout/project-avatar-state"

export type HomeSessionRecord = {
  session: SessionInfo
  project: LocalProject
  projectName: string
}

export type HomeSessionGroup = {
  id: "today" | "yesterday" | "older"
  title: string
  sessions: HomeSessionRecord[]
}

export type OpenSessionOptions = { background?: boolean }

export function homeSessionSearchKey(record: HomeSessionRecord) {
  return `${record.session.location.directory}:${record.session.id}`
}

export function HomeSessionStatusController(props: {
  server: ServerConnection.Key
  record: HomeSessionRecord
  isOpenTab: (record: HomeSessionRecord) => boolean
  render: (state: { unread: Accessor<boolean>; loading: Accessor<boolean>; open: Accessor<boolean> }) => JSX.Element
}) {
  const avatar = useSessionTabAvatarState(
    () => props.server,
    () => props.record.session.id,
  )
  return props.render({ unread: avatar.unread, loading: avatar.loading, open: () => props.isOpenTab(props.record) })
}
