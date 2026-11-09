// A site's icon for the new tab page: its favicon when the main process can
// fetch one, a globe when it cannot. Never a letter.

import { useEffect, useState } from "react";
import { IconGlobe } from "../../primitives/icons";
import { useApp } from "../../stores/app";

const icons = new Map<string, Promise<string>>();

function iconOf(host: string): Promise<string> {
  let icon = icons.get(host);
  if (!icon) {
    icon = window.atlas?.favicon(host).catch(() => "") ?? Promise.resolve("");
    icons.set(host, icon);
  }
  return icon;
}

/**
 * Whether a logo is a plain dark (or light) mark that would vanish on this
 * theme's surface. Only its brightness is read; the logo itself is not changed
 * beyond being inverted on screen, as a site shows it on a dark page.
 */
function plainMark(src: string): Promise<"dark" | "light" | ""> {
  return new Promise((resolve) => {
    const image = new Image();
    image.onload = () => {
      try {
        const canvas = document.createElement("canvas");
        canvas.width = canvas.height = 24;
        const ctx = canvas.getContext("2d", { willReadFrequently: true });
        if (!ctx) return resolve("");
        ctx.drawImage(image, 0, 0, 24, 24);
        const { data } = ctx.getImageData(0, 0, 24, 24);
        let sum = 0;
        let spread = 0;
        let n = 0;
        for (let i = 0; i < data.length; i += 4) {
          if (data[i + 3] < 128) continue;
          const [r, g, b] = [data[i], data[i + 1], data[i + 2]];
          sum += (r + g + b) / 3;
          spread += Math.max(r, g, b) - Math.min(r, g, b);
          n++;
        }
        if (n < 12) return resolve("");
        const light = sum / n / 255;
        // Only greys: a coloured logo is left as it is.
        if (spread / n > 40) return resolve("");
        resolve(light < 0.22 ? "dark" : light > 0.85 ? "light" : "");
      } catch {
        resolve("");
      }
    };
    image.onerror = () => resolve("");
    image.src = src;
  });
}

export function SiteFavicon({ host, src: known }: { host?: string; src?: string }) {
  const [src, setSrc] = useState(known ?? "");
  const [mark, setMark] = useState<"dark" | "light" | "">("");
  const scheme = useApp((s) => s.scheme);
  useEffect(() => {
    let stale = false;
    setMark("");
    if (src) void plainMark(src).then((m) => !stale && setMark(m));
    return () => {
      stale = true;
    };
  }, [src]);
  useEffect(() => {
    setSrc(known ?? "");
    if (known || !host) return;
    let stale = false;
    void iconOf(host).then((value) => {
      if (!stale) setSrc(value);
    });
    return () => {
      stale = true;
    };
  }, [host, known]);
  const flip = (mark === "dark" && scheme === "dark") || (mark === "light" && scheme === "light");
  return src ? <img className="favicon" data-invert={flip || undefined} src={src} alt="" draggable={false} /> : <IconGlobe className="favicon-fallback" />;
}
