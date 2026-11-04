// Commands for the open thread, which keeps its own scroll position.

export type ThreadCommand = "prev" | "next" | "fold";

const listeners = new Set<(command: ThreadCommand) => void>();

export function emitThread(command: ThreadCommand) {
  for (const listener of listeners) listener(command);
}

export function onThread(listener: (command: ThreadCommand) => void) {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}
