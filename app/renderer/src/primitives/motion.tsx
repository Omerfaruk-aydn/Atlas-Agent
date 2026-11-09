import { useEffect, useState, type CSSProperties, type ReactNode } from "react";

/** Text with a light sweeping through it: one element and one gradient, however long the text. */
export function Shimmer({ text }: { text: string }) {
  return <span className="shimmer">{text}</span>;
}

/** Three dots that rise in turn: the quiet "thinking" mark. */
export function ThinkingDots() {
  return (
    <span className="think-dots" aria-hidden="true">
      <i style={{ "--i": 0 } as CSSProperties} />
      <i style={{ "--i": 1 } as CSSProperties} />
      <i style={{ "--i": 2 } as CSSProperties} />
    </span>
  );
}

/** Height animation without measuring: the grid row goes from 0fr to 1fr. */
export function Collapse({ open, children }: { open: boolean; children: ReactNode }) {
  return (
    <div className="collapse" data-open={open || undefined}>
      <div className="collapse-inner">{children}</div>
    </div>
  );
}

/** Milliseconds since `from`, updated once a second while mounted. */
export function useElapsed(from: number): number {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(id);
  }, []);
  return Math.max(0, now - from);
}
