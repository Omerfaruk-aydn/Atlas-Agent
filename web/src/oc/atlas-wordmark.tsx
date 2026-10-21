// The Atlas Agent wordmark of the terminal UI's main screen
// (internal/ui/logo/pixelbanner.go): "ATLAS-AGENT" in ANSI Shadow lettering,
// with the rainbow sweep that runs diagonally across it. Drawn on a canvas,
// one cell per terminal column and row, so every cell gets its own hue.

import { onCleanup, onMount } from "solid-js"

const ROWS = 6

const GLYPHS: Record<string, string[]> = {
  A: [" █████╗ ", "██╔══██╗", "███████║", "██╔══██║", "██║  ██║", "╚═╝  ╚═╝"],
  T: ["████████╗", "╚══██╔══╝", "   ██║   ", "   ██║   ", "   ██║   ", "   ╚═╝   "],
  L: ["██╗     ", "██║     ", "██║     ", "██║     ", "███████╗", "╚══════╝"],
  S: ["███████╗", "██╔════╝", "███████╗", "╚════██║", "███████║", "╚══════╝"],
  G: [" ██████╗ ", "██╔════╝ ", "██║  ███╗", "██║   ██║", "╚██████╔╝", " ╚═════╝ "],
  E: ["███████╗", "██╔════╝", "█████╗  ", "██╔══╝  ", "███████╗", "╚══════╝"],
  N: ["███╗   ██╗", "████╗  ██║", "██╔██╗ ██║", "██║╚██╗██║", "██║ ╚████║", "╚═╝  ╚═══╝"],
  "-": ["      ", "      ", "█████╗", "╚════╝", "      ", "      "],
}

const lines = Array.from({ length: ROWS }, (_, row) => [..."ATLAS-AGENT"].map((ch) => GLYPHS[ch][row]).join(""))
const COLS = Math.max(...lines.map((l) => [...l].length))
const cells = lines.map((l) => [...l])

// The terminal's sweep: saturation .85 and value 1 around a full hue circle as
// wide as the wordmark, skewed two columns per row.
const ROW_SKEW = 2
const hue = (x: number, y: number, frame: number) => (((x + y * ROW_SKEW + frame) % COLS) + COLS) % COLS / COLS * 360
// HSV(h, .85, 1) as HSL: lightness 57.5%, full saturation.
const color = (h: number) => `hsl(${h.toFixed(1)} 100% 57.5%)`
const SPEED = 14 // columns per second

export function AtlasWordmark(props: { class?: string }) {
  let canvas: HTMLCanvasElement | undefined
  let raf = 0

  onMount(() => {
    const el = canvas
    const ctx = el?.getContext("2d")
    if (!el || !ctx) return
    const reduced = matchMedia("(prefers-reduced-motion: reduce)").matches
    const font = "'Cascadia Mono', Consolas, 'SF Mono', Menlo, 'DejaVu Sans Mono', monospace"
    let cw = 0
    let ch = 0
    let started = performance.now()

    const resize = () => {
      const dpr = devicePixelRatio || 1
      const width = el.clientWidth
      cw = width / COLS
      ch = cw * 2
      el.style.height = `${ch * ROWS}px`
      el.width = Math.round(width * dpr)
      el.height = Math.round(ch * ROWS * dpr)
      ctx.setTransform(dpr, 0, 0, dpr, 0, 0)
    }

    const draw = (now: number) => {
      const frame = reduced ? 0 : Math.floor(((now - started) / 1000) * SPEED)
      ctx.clearRect(0, 0, el.clientWidth, ch * ROWS)
      ctx.font = `${cw / 0.6}px ${font}`
      ctx.textAlign = "center"
      ctx.textBaseline = "middle"
      for (let y = 0; y < ROWS; y++) {
        for (let x = 0; x < cells[y].length; x++) {
          const glyph = cells[y][x]
          if (glyph === " ") continue
          ctx.fillStyle = color(hue(x, y, frame))
          // Face blocks fill their cell exactly; the bevel is lettering.
          if (glyph === "█") ctx.fillRect(x * cw, y * ch, Math.ceil(cw) + 0.5, Math.ceil(ch) + 0.5)
          else ctx.fillText(glyph, x * cw + cw / 2, y * ch + ch / 2)
        }
      }
      if (!reduced) raf = requestAnimationFrame(draw)
    }

    resize()
    started = performance.now()
    raf = requestAnimationFrame(draw)
    const observer = new ResizeObserver(() => {
      resize()
      if (reduced) draw(started)
    })
    observer.observe(el)
    onCleanup(() => {
      cancelAnimationFrame(raf)
      observer.disconnect()
    })
  })

  return <canvas ref={canvas} class={props.class} aria-hidden="true" role="presentation" style={{ width: "100%", display: "block" }} />
}
