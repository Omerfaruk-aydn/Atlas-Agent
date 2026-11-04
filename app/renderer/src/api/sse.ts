// The workspace's one live channel. Built on fetch rather than EventSource so
// the sequence number and gateway instance stay in our hands across reconnects.

import type { HubEvent } from "./types";

export interface StreamHandlers {
  onEvent(ev: HubEvent): void;
  onOpen(): void;
  onClose(): void;
}

const MAX_BACKOFF_MS = 5000;

export function openStream(workspaceId: () => string, instance: () => string, handlers: StreamHandlers) {
  let stopped = false;
  let after = 0;
  let controller: AbortController | null = null;

  const readBlock = (block: string) => {
    let data = "";
    for (const line of block.split("\n")) {
      if (line.startsWith("data: ")) data += line.slice(6);
      else if (line.startsWith("id: ")) after = Number(line.slice(4)) || after;
    }
    if (!data) return;
    try {
      handlers.onEvent(JSON.parse(data) as HubEvent);
    } catch (err) {
      console.error("Bad event", err);
    }
  };

  const run = async () => {
    let backoff = 250;
    while (!stopped) {
      controller = new AbortController();
      try {
        const q = new URLSearchParams({ after: String(after), instance: instance() });
        const res = await fetch(`/api/workspaces/${encodeURIComponent(workspaceId())}/events?${q}`, {
          signal: controller.signal,
          headers: { Accept: "text/event-stream" },
        });
        if (!res.ok || !res.body) throw new Error(`events: ${res.status}`);
        backoff = 250;
        handlers.onOpen();
        const reader = res.body.pipeThrough(new TextDecoderStream()).getReader();
        let buf = "";
        for (;;) {
          const { value, done } = await reader.read();
          if (done) break;
          buf += value;
          let end = buf.indexOf("\n\n");
          while (end >= 0) {
            readBlock(buf.slice(0, end));
            buf = buf.slice(end + 2);
            end = buf.indexOf("\n\n");
          }
        }
      } catch (err) {
        if (stopped) return;
        console.warn("Event stream dropped", err);
      }
      if (stopped) return;
      handlers.onClose();
      await new Promise((r) => setTimeout(r, backoff));
      backoff = Math.min(backoff * 2, MAX_BACKOFF_MS);
    }
  };
  void run();

  return {
    stop() {
      stopped = true;
      controller?.abort();
    },
    restart() {
      after = 0;
      controller?.abort();
    },
  };
}
