import { useEffect, useState, type CSSProperties, type ReactNode } from "react";

/** Text with a light sweeping through it, one character at a time. */
export function Shimmer({ text }: { text: string }) {
  return (
    <span className="shimmer" aria-label={text}>
      {[...text].map((ch, i) => (
        <span key={i} aria-hidden="true" style={{ "--i": i } as CSSProperties}>
          {ch === " " ? " " : ch}
        </span>
      ))}
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
