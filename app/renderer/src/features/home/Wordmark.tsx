// The ATLAS-AGENT wordmark: the terminal banner's ANSI Shadow lettering with
// the colour sweep running across it, one canvas cell per terminal column.

import { useEffect, useRef } from "react";

const ROWS = 6;
const GLYPHS: Record<string, string[]> = {
  A: [" █████╗ ", "██╔══██╗", "███████║", "██╔══██║", "██║  ██║", "╚═╝  ╚═╝"],
  T: ["████████╗", "╚══██╔══╝", "   ██║   ", "   ██║   ", "   ██║   ", "   ╚═╝   "],
  L: ["██╗     ", "██║     ", "██║     ", "██║     ", "███████╗", "╚══════╝"],
  S: ["███████╗", "██╔════╝", "███████╗", "╚════██║", "███████║", "╚══════╝"],
  G: [" ██████╗ ", "██╔════╝ ", "██║  ███╗", "██║   ██║", "╚██████╔╝", " ╚═════╝ "],
  E: ["███████╗", "██╔════╝", "█████╗  ", "██╔══╝  ", "███████╗", "╚══════╝"],
  N: ["███╗   ██╗", "████╗  ██║", "██╔██╗ ██║", "██║╚██╗██║", "██║ ╚████║", "╚═╝  ╚═══╝"],
  "-": ["      ", "      ", "█████╗", "╚════╝", "      ", "      "],
};
const CELLS = Array.from({ length: ROWS }, (_, row) => [...[..."ATLAS-AGENT"].map((ch) => GLYPHS[ch][row]).join("")]);
const COLS = Math.max(...CELLS.map((l) => l.length));
const SKEW = 2;
const SPEED = 14;
const FONT = "'Cascadia Mono', Consolas, monospace";

const hue = (x: number, y: number, frame: number) => ((((x + y * SKEW + frame) % COLS) + COLS) % COLS / COLS) * 360;

export function Wordmark() {
  const ref = useRef<HTMLCanvasElement>(null);

  useEffect(() => {
    const el = ref.current;
    const ctx = el?.getContext("2d");
    if (!el || !ctx) return;
    const still = document.documentElement.dataset.motion === "reduce";
    let cw = 0;
    let ch = 0;
    let raf = 0;
    let lastFrame = -1;
    const started = performance.now();

    const resize = () => {
      const dpr = devicePixelRatio || 1;
      const width = el.clientWidth;
      cw = width / COLS;
      ch = cw * 2;
      el.style.height = `${ch * ROWS}px`;
      el.width = Math.round(width * dpr);
      el.height = Math.round(ch * ROWS * dpr);
      ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
      lastFrame = -1;
    };

    const paint = (frame: number) => {
      ctx.clearRect(0, 0, el.clientWidth, ch * ROWS);
      ctx.font = `${cw / 0.6}px ${FONT}`;
      ctx.textAlign = "center";
      ctx.textBaseline = "middle";
      for (let y = 0; y < ROWS; y++) {
        for (let x = 0; x < CELLS[y].length; x++) {
          const glyph = CELLS[y][x];
          if (glyph === " ") continue;
          ctx.fillStyle = `hsl(${hue(x, y, frame).toFixed(1)} 100% 57.5%)`;
          if (glyph === "█") ctx.fillRect(x * cw, y * ch, Math.ceil(cw) + 0.5, Math.ceil(ch) + 0.5);
          else ctx.fillText(glyph, x * cw + cw / 2, y * ch + ch / 2);
        }
      }
    };

    // The sweep moves 14 columns a second, so most display frames paint nothing.
    const tick = (now: number) => {
      const frame = still ? 0 : Math.floor(((now - started) / 1000) * SPEED);
      if (frame !== lastFrame) {
        lastFrame = frame;
        paint(frame);
      }
      if (!still) raf = requestAnimationFrame(tick);
    };

    resize();
    raf = requestAnimationFrame(tick);
    const observer = new ResizeObserver(() => {
      resize();
      if (still) tick(started);
    });
    observer.observe(el);
    return () => {
      cancelAnimationFrame(raf);
      observer.disconnect();
    };
  }, []);

  return <canvas ref={ref} className="wordmark" aria-label="ATLAS-AGENT" role="img" />;
}
