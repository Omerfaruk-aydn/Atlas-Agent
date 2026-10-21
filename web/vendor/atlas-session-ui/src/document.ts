import type { FileDiffInfo, SessionMessageInfo, SessionStatus } from "@atlas/client/promise"

export type SessionDocument = {
  sessionID: string
  messages: SessionMessageInfo[]
  status: SessionStatus
  diffs: FileDiffInfo[]
}
