// Event stream of one workspace, as relayed by the gateway.
//
// The gateway numbers events and replays from Last-Event-ID, which
// EventSource sends on its own when it reconnects. This module adds what
// EventSource does not do: it reconnects after the stream was closed for
// good (a 401 or a restarted gateway), notices a gap the gateway reports
// with "resync" or a changed instance, and drops events it already
// delivered.

import { api } from "./client"
import type { HubEvent } from "./types"

export type StreamState = "connecting" | "open" | "offline"

export interface Hello {
  instance: string
  seq: number
  workspace_id: string
  resync: boolean
}

export interface StreamHandlers {
  /** A (re)connection is live. needsSnapshot is true when the client must
   * reload its state because events may have been missed. */
  onHello(hello: Hello, needsSnapshot: boolean): void
  onEvent(ev: HubEvent): void
  onState(state: StreamState): void
  /** Called when the browser gave up on the stream. EventSource hides the
   * status code, so the handler asks the API why: it resolves false when
   * reconnecting is pointless (signed out). */
  onClosed?(): Promise<boolean>
}

export interface StreamControl {
  close(): void
  /** Forces a reconnect, e.g. after the page becomes visible again. */
  reconnect(): void
}

export function openEventStream(workspaceID: () => string, handlers: StreamHandlers): StreamControl {
  let source: EventSource | undefined
  let closed = false
  let lastSeq = 0
  let instance = ""
  let retry = 0
  let timer: ReturnType<typeof setTimeout> | undefined
  let firstHello = true

  const connect = () => {
    if (closed) return
    handlers.onState("connecting")
    source?.close()
    const es = new EventSource(api.eventsURL(workspaceID(), lastSeq, instance))
    source = es
    es.onmessage = (msg) => {
      let ev: HubEvent
      try {
        ev = JSON.parse(msg.data)
      } catch {
        return
      }
      if (ev.type === "hello") {
        const hello = ev.payload as Hello
        const restarted = instance !== "" && hello.instance !== instance
        const needsSnapshot = firstHello || hello.resync || restarted
        firstHello = false
        instance = hello.instance
        if (needsSnapshot || hello.seq < lastSeq) lastSeq = hello.seq
        retry = 0
        handlers.onState("open")
        handlers.onHello(hello, needsSnapshot)
        return
      }
      if (ev.seq <= lastSeq) return
      lastSeq = ev.seq
      if (ev.type === "resync") {
        handlers.onHello({ instance, seq: ev.seq, workspace_id: workspaceID(), resync: true }, true)
        return
      }
      handlers.onEvent(ev)
    }
    es.onerror = () => {
      if (closed) return
      if (es.readyState === EventSource.CLOSED) {
        // The browser gave up (e.g. the gateway answered 401 or 404).
        handlers.onState("offline")
        if (!handlers.onClosed) {
          schedule()
          return
        }
        void handlers.onClosed().then(
          (retry) => retry && schedule(),
          () => schedule(),
        )
      } else {
        handlers.onState("connecting")
      }
    }
  }

  const schedule = () => {
    if (closed || timer) return
    const delay = Math.min(10_000, 500 * 2 ** retry++)
    timer = setTimeout(() => {
      timer = undefined
      connect()
    }, delay)
  }

  connect()
  return {
    close() {
      closed = true
      if (timer) clearTimeout(timer)
      source?.close()
    },
    reconnect() {
      if (closed) return
      if (timer) {
        clearTimeout(timer)
        timer = undefined
      }
      connect()
    },
  }
}
