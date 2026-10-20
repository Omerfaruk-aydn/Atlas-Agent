import { afterEach, beforeEach, describe, expect, test, vi } from "vitest"
import { openEventStream, type Hello } from "./events"
import type { HubEvent } from "./types"

class FakeEventSource {
  static instances: FakeEventSource[] = []
  static CONNECTING = 0
  static OPEN = 1
  static CLOSED = 2
  readyState = FakeEventSource.CONNECTING
  onmessage: ((m: { data: string }) => void) | null = null
  onerror: (() => void) | null = null
  constructor(readonly url: string) {
    FakeEventSource.instances.push(this)
  }
  close() {
    this.readyState = FakeEventSource.CLOSED
  }
  emit(ev: Partial<HubEvent> & { type: string }) {
    this.onmessage?.({ data: JSON.stringify({ seq: 0, payload: {}, ...ev }) })
  }
  fail() {
    this.readyState = FakeEventSource.CLOSED
    this.onerror?.()
  }
}

beforeEach(() => {
  FakeEventSource.instances = []
  vi.stubGlobal("EventSource", FakeEventSource)
  vi.useFakeTimers()
})
afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

function hello(instance: string, seq: number, resync = false) {
  return { type: "hello", seq, payload: { instance, seq, workspace_id: "w", resync } satisfies Hello }
}

describe("openEventStream", () => {
  test("drops events at or below the last delivered sequence", () => {
    const events: number[] = []
    openEventStream(() => "w", { onHello: () => {}, onEvent: (e) => events.push(e.seq), onState: () => {} })
    const es = FakeEventSource.instances[0]
    es.emit(hello("i1", 0))
    es.emit({ type: "message", seq: 1 })
    es.emit({ type: "message", seq: 2 })
    // A replay after an automatic reconnect repeats the last event.
    es.emit({ type: "message", seq: 2 })
    es.emit({ type: "message", seq: 3 })
    expect(events).toEqual([1, 2, 3])
  })

  test("asks for snapshots on first connect, on resync and after a gateway restart only", () => {
    const snapshots: boolean[] = []
    openEventStream(() => "w", {
      onHello: (_, needs) => snapshots.push(needs),
      onEvent: () => {},
      onState: () => {},
      onClosed: async () => true,
    })
    let es = FakeEventSource.instances[0]
    es.emit(hello("i1", 5))
    es.emit({ type: "message", seq: 6 })
    // An ordinary reconnect to the same gateway resumes without reload.
    es.fail()
    return vi.runAllTimersAsync().then(() => {
      es = FakeEventSource.instances.at(-1)!
      expect(es.url).toContain("after=6")
      expect(es.url).toContain("instance=i1")
      es.emit(hello("i1", 6))
      es.emit(hello("i1", 6, true))
      es.emit(hello("i2", 1))
      expect(snapshots).toEqual([true, false, true, true])
    })
  })

  test("a resync event inside the stream triggers a snapshot", () => {
    const snapshots: boolean[] = []
    openEventStream(() => "w", { onHello: (_, n) => snapshots.push(n), onEvent: () => {}, onState: () => {} })
    const es = FakeEventSource.instances[0]
    es.emit(hello("i1", 0))
    es.emit({ type: "resync", seq: 1 })
    expect(snapshots).toEqual([true, true])
  })

  test("stops reconnecting when the closed-stream probe says signed out", async () => {
    const onClosed = vi.fn(async () => false)
    openEventStream(() => "w", { onHello: () => {}, onEvent: () => {}, onState: () => {}, onClosed })
    FakeEventSource.instances[0].fail()
    await vi.runAllTimersAsync()
    expect(onClosed).toHaveBeenCalledOnce()
    expect(FakeEventSource.instances).toHaveLength(1)
  })
})
