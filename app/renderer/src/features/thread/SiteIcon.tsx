// A site's favicon. The page cannot reach the web itself, so the main process
// fetches it; until then, and when there is none, the site's first letter shows.

import { useEffect, useState } from "react";

const icons = new Map<string, Promise<string>>();

function iconOf(host: string): Promise<string> {
  let icon = icons.get(host);
  if (!icon) {
    icon = window.atlas?.favicon(host).catch(() => "") ?? Promise.resolve("");
    icons.set(host, icon);
  }
  return icon;
}

export function SiteIcon({ host }: { host: string }) {
  const [src, setSrc] = useState("");
  useEffect(() => {
    let stale = false;
    void iconOf(host).then((value) => {
      if (!stale) setSrc(value);
    });
    return () => {
      stale = true;
    };
  }, [host]);
  return (
    <span className="site-icon" aria-hidden="true">
      {src ? <img src={src} alt="" /> : host.charAt(0).toUpperCase()}
    </span>
  );
}
