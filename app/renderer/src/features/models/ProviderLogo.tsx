import { logoOf } from "../../lib/logos";

/** A provider's mark, or nothing. In a list, `reserve` keeps the names lined up. */
export function ProviderLogo({ id, size = 18, reserve }: { id: string; size?: number; reserve?: boolean }) {
  const svg = logoOf(id);
  if (!svg && !reserve) return null;
  // The source is a file bundled at build time, never data from outside.
  return <span className="provider-logo" style={{ inlineSize: size, blockSize: size }} aria-hidden="true" dangerouslySetInnerHTML={svg ? { __html: svg } : undefined} />;
}
