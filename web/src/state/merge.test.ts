import { describe, expect, test } from "vitest"
import type { Message, RunComplete } from "../api/types"
import { addPermission, lastFinish, runState, settlePermission, sortMessages, upsertMessage } from "./merge"

const msg = (id: string, created: number, role: Message["role"] = "assistant", text = id): Message => ({
  id,
  role,
  session_id: "s",
  parts: [{ type: "text", data: { text } }],
  model: "",
  provider: "",
  created_at: created,
  updated_at: created,
})

describe("upsertMessage", () => {
  test("keeps server order among messages created in the same second", () => {
    // IDs sort the other way round; order must follow arrival, as the
    // server lists them (created_at, rowid).
    let list: Message[] = []
    list = upsertMessage(list, msg("zzz-user", 100, "user"))
    list = upsertMessage(list, msg("aaa-assistant", 100))
    expect(list.map((m) => m.id)).toEqual(["zzz-user", "aaa-assistant"])
  })

  test("replacing a message keeps its place and applies whole state", () => {
    let list = [msg("a", 1), msg("b", 2)]
    list = upsertMessage(list, { ...msg("a", 1), parts: [{ type: "text", data: { text: "streamed more" } }] })
    expect(list.map((m) => m.id)).toEqual(["a", "b"])
    expect(list[0].parts[0]).toEqual({ type: "text", data: { text: "streamed more" } })
  })

  test("replaying buffered events over a newer snapshot ends at the newest state", () => {
    // The snapshot already contains v2; the buffer holds v1 then v2.
    const v1 = { ...msg("a", 1), parts: [{ type: "text" as const, data: { text: "par" } }], updated_at: 2 }
    const v2 = { ...msg("a", 1), parts: [{ type: "text" as const, data: { text: "partial answer" } }], updated_at: 3 }
    let list: Message[] = [v2]
    for (const ev of [v1, v2]) list = upsertMessage(list, ev)
    expect(list).toEqual([v2])
  })

  test("a late message from an earlier second is placed before newer ones", () => {
    let list = [msg("a", 1), msg("c", 3)]
    list = upsertMessage(list, msg("b", 2))
    expect(list.map((m) => m.id)).toEqual(["a", "b", "c"])
  })

  test("snapshot sort is stable for ties", () => {
    const list = sortMessages([msg("z", 5), msg("a", 5), msg("m", 1)])
    expect(list.map((m) => m.id)).toEqual(["m", "z", "a"])
  })
})

describe("permissions", () => {
  const p = { id: "p1", session_id: "s", tool_call_id: "tc", tool_name: "write", description: "", action: "write", params: {}, path: "" }

  test("a duplicate request event is ignored", () => {
    expect(addPermission(addPermission([], p), p)).toHaveLength(1)
  })

  test("only a decision closes a request, whichever client made it", () => {
    expect(settlePermission([p], { tool_call_id: "tc" })).toHaveLength(1)
    expect(settlePermission([p], { tool_call_id: "tc", granted: true })).toHaveLength(0)
    expect(settlePermission([p], { tool_call_id: "other", denied: true })).toHaveLength(1)
  })
})

describe("runState", () => {
  const done: RunComplete = { session_id: "s", message_id: "m" }

  test("busy comes from the server and wins over a finished run", () => {
    expect(runState({ busy: true, starting: false, hasQuestion: false, hasPermission: false, lastRun: done })).toBe("running")
  })

  test("a late event cannot make a finished run look active", () => {
    // After run_complete the server reports idle; message events do not
    // touch busy, so the state stays finished.
    expect(runState({ busy: false, starting: false, hasQuestion: false, hasPermission: false, lastRun: done })).toBe("done")
  })

  test("waiting states take precedence", () => {
    const base = { busy: true, starting: false, lastRun: undefined }
    expect(runState({ ...base, hasQuestion: true, hasPermission: false })).toBe("question")
    expect(runState({ ...base, hasQuestion: true, hasPermission: true })).toBe("permission")
  })

  test("cancelled and failed are told apart", () => {
    const idle = { busy: false, starting: false, hasQuestion: false, hasPermission: false }
    expect(runState({ ...idle, lastRun: { ...done, cancelled: true } })).toBe("cancelled")
    expect(runState({ ...idle, lastRun: { ...done, error: "boom" } })).toBe("failed")
    expect(runState({ ...idle, lastFinish: "canceled" })).toBe("cancelled")
    expect(runState(idle)).toBe("idle")
  })

  test("lastFinish reads the newest assistant message only", () => {
    const user: Message = { ...msg("u", 2, "user"), parts: [{ type: "finish", data: { reason: "stop", time: 0 } }] }
    const assistant: Message = { ...msg("a", 1), parts: [{ type: "finish", data: { reason: "canceled", time: 0 } }] }
    expect(lastFinish([assistant, user])).toBe("canceled")
  })
})
