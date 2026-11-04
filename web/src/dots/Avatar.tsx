// A dot's living face. The body is a polar shape redrawn every frame, so it
// can breathe and ripple while the dot speaks; the eyes follow the pointer
// on a spring and blink on their own. Each state reads at a glance without
// any text: listening widens the eyes and pulses a ring with the mic level,
// thinking looks up, working looks toward the activity panel, waiting for
// approval bobs gently, sleeping closes the eyes. On top of that a dot
// can feel something for a moment: happy after a finished task (a squint
// and a little hop), worried after a failure (a lowered, uneasy look). While
// the owner types it looks down at the message being written.

import { createEffect, onCleanup, onMount } from "solid-js"
import type { Shape } from "./api"

export type AvatarState = "idle" | "listening" | "thinking" | "speaking" | "working" | "waiting" | "sleeping"
export type AvatarMood = "happy" | "worried" | undefined

const TAU = Math.PI * 2
const POINTS = 96

/** radius returns the shape's outline radius at angle a (0 = right, y down). */
function radius(shape: Shape, a: number): number {
  switch (shape) {
    case "orb":
      return 44
    case "pebble": {
      const n = 3.2
      const c = Math.abs(Math.cos(a)) ** n + Math.abs(Math.sin(a)) ** n
      return 41 / c ** (1 / n)
    }
    case "bloom":
      return 38.5 + 5.5 * Math.cos(6 * a)
    case "hex": {
      const seg = Math.PI / 3
      const local = ((a % seg) + seg) % seg - seg / 2
      const hex = (43 * Math.cos(seg / 2)) / Math.cos(local)
      return hex * 0.8 + 40 * 0.2
    }
    case "drop": {
      const up = Math.max(0, -Math.sin(a))
      return 37 + 11 * up ** 4 + 2 * Math.sin(a)
    }
    case "cloud":
      return 37 + 6 * Math.abs(Math.cos(2.5 * a + Math.PI / 2)) ** 0.8
    case "star":
      return 37 + 7.5 * Math.cos(5 * a - Math.PI / 2)
    case "shield": {
      const down = Math.max(0, Math.sin(a))
      const top = Math.max(0, -Math.sin(a))
      return 41 + 6 * down ** 6 - 3 * top ** 2 + 2 * Math.abs(Math.cos(a)) ** 6
    }
  }
}

/** outline builds a smooth closed path (Catmull-Rom through samples). */
function outline(shape: Shape, wobble: number, t: number, sx = 1, sy = 1): string {
  const pts: [number, number][] = []
  for (let i = 0; i < POINTS; i++) {
    const a = (i / POINTS) * TAU
    let r = radius(shape, a)
    if (wobble > 0) r += wobble * (Math.sin(3 * a + t * 7) * 0.6 + Math.sin(5 * a - t * 5) * 0.4)
    pts.push([50 + Math.cos(a) * r * sx, 50 + Math.sin(a) * r * sy])
  }
  let d = `M${pts[0][0].toFixed(2)},${pts[0][1].toFixed(2)}`
  for (let i = 0; i < POINTS; i++) {
    const p0 = pts[(i - 1 + POINTS) % POINTS]
    const p1 = pts[i]
    const p2 = pts[(i + 1) % POINTS]
    const p3 = pts[(i + 2) % POINTS]
    const c1x = p1[0] + (p2[0] - p0[0]) / 6
    const c1y = p1[1] + (p2[1] - p0[1]) / 6
    const c2x = p2[0] - (p3[0] - p1[0]) / 6
    const c2y = p2[1] - (p3[1] - p1[1]) / 6
    d += `C${c1x.toFixed(2)},${c1y.toFixed(2)} ${c2x.toFixed(2)},${c2y.toFixed(2)} ${p2[0].toFixed(2)},${p2[1].toFixed(2)}`
  }
  return d + "Z"
}

/** inkFor picks eye color with enough contrast against the body. */
export function inkFor(hex: string): string {
  const n = parseInt(hex.slice(1), 16)
  const r = (n >> 16) & 255
  const g = (n >> 8) & 255
  const b = n & 255
  const l = (0.2126 * r + 0.7152 * g + 0.0722 * b) / 255
  return l > 0.62 ? "#1b1a18" : "#ffffff"
}

const staticPaths = new Map<string, string>()
export function shapePath(shape: Shape): string {
  let d = staticPaths.get(shape)
  if (!d) staticPaths.set(shape, (d = outline(shape, 0, 0)))
  return d
}

/** MiniAvatar is the still version used in lists and pickers. */
export function MiniAvatar(props: { shape: Shape; color: string; size?: number; sleeping?: boolean }) {
  const size = () => props.size ?? 32
  return (
    <svg width={size()} height={size()} viewBox="0 0 100 100" aria-hidden="true" style={{ display: "block", overflow: "visible" }}>
      <path d={shapePath(props.shape)} fill={props.color} />
      {props.sleeping ? (
        <g stroke={inkFor(props.color)} stroke-width="5" stroke-linecap="round">
          <line x1="34" y1="50" x2="42" y2="50" />
          <line x1="58" y1="50" x2="66" y2="50" />
        </g>
      ) : (
        <g fill={inkFor(props.color)}>
          <rect x="33" y="38" width="9" height="20" rx="4.5" />
          <rect x="58" y="38" width="9" height="20" rx="4.5" />
        </g>
      )}
    </svg>
  )
}

interface Spring {
  x: number
  v: number
}
function step(s: Spring, target: number, k: number, d: number) {
  s.v += (target - s.x) * k
  s.v *= d
  s.x += s.v
}

/**
 * LiveAvatar animates a dot. level() is the current voice level (0-1) of
 * whatever is playing or being heard, read once per frame.
 */
export function LiveAvatar(props: {
  shape: Shape
  color: string
  state: AvatarState
  size?: number
  level?: () => number
  mood?: () => AvatarMood
  typing?: () => boolean
  onClick?: () => void
}) {
  let svg!: SVGSVGElement
  let body!: SVGPathElement
  let shadow!: SVGEllipseElement
  let eyeL!: SVGRectElement
  let eyeR!: SVGRectElement
  let mouth!: SVGRectElement
  let ring!: SVGCircleElement
  let arc!: SVGCircleElement
  let face!: SVGGElement

  const ex: Spring = { x: 0, v: 0 }
  const ey: Spring = { x: 0, v: 0 }
  const open: Spring = { x: 1, v: 0 }
  const mouthS: Spring = { x: 0, v: 0 }
  const widen: Spring = { x: 1, v: 0 }
  const narrow: Spring = { x: 1, v: 0 }
  const hop: Spring = { x: 0, v: 0 }
  const wob: Spring = { x: 0, v: 0 }
  let pointer = { x: 0, y: 0, at: 0 }
  let nextBlink = performance.now() + 1800
  let blinkAt = -1
  let glance = { x: 0, y: 0, until: 0 }
  let raf = 0
  let lastShape: Shape | undefined
  let lastPath = ""

  const onMove = (e: PointerEvent) => {
    const r = svg.getBoundingClientRect()
    const cx = r.left + r.width / 2
    const cy = r.top + r.height / 2
    pointer = { x: (e.clientX - cx) / Math.max(240, r.width * 2), y: (e.clientY - cy) / Math.max(240, r.height * 2), at: performance.now() }
  }

  const frame = (now: number) => {
    const s = props.state
    const t = now / 1000
    const level = Math.min(1, Math.max(0, props.level?.() ?? 0))
    const mood = s === "sleeping" || s === "listening" ? undefined : props.mood?.()
    const typing = s === "idle" && !!props.typing?.()

    // Gaze.
    let tx = 0
    let ty = 0
    const pointerFresh = now - pointer.at < 4000
    if (s === "thinking") {
      tx = 0.55 + Math.sin(t * 0.9) * 0.15
      ty = -0.75
    } else if (s === "working") {
      tx = 0.85
      ty = 0.35
      if (now < glance.until) {
        tx = glance.x
        ty = glance.y
      } else if (Math.random() < 0.003 && pointerFresh) glance = { x: pointer.x * 3, y: pointer.y * 3, until: now + 1200 }
    } else if (s === "sleeping") {
      ty = 0.25
    } else if (mood === "worried") {
      tx = Math.sin(t * 1.3) * 0.35
      ty = 0.55
    } else if (typing) {
      tx = Math.sin(t * 0.6) * 0.12
      ty = 0.85
    } else if (pointerFresh) {
      tx = pointer.x * 3
      ty = pointer.y * 3
    } else {
      // Idle drift: a slow look around so the dot never freezes.
      tx = Math.sin(t * 0.37) * 0.35
      ty = Math.sin(t * 0.23) * 0.2
    }
    tx = Math.max(-1, Math.min(1, tx))
    ty = Math.max(-1, Math.min(1, ty))
    step(ex, tx * 8, 0.08, 0.74)
    step(ey, ty * 6, 0.08, 0.74)

    // Blink.
    if (s !== "sleeping" && now > nextBlink) {
      blinkAt = now
      nextBlink = now + 2400 + Math.random() * 3800
      if (Math.random() < 0.18) nextBlink = now + 260 // double blink
    }
    let lid = 1
    if (blinkAt > 0) {
      const b = (now - blinkAt) / 140
      if (b >= 0 && b <= 1) lid = Math.abs(1 - 2 * b)
    }
    if (s === "sleeping") lid = 0.08
    if (s === "working") lid = Math.min(lid, 0.78)
    if (mood === "happy") lid = Math.min(lid, 0.34)
    if (mood === "worried") lid = Math.min(lid, 0.82)
    step(open, lid, 0.5, 0.45)
    step(widen, s === "listening" ? 1.12 : s === "thinking" ? 0.92 : 1, 0.12, 0.7)
    step(narrow, mood === "worried" ? 0.8 : mood === "happy" ? 1.15 : 1, 0.12, 0.7)
    // A happy dot hops twice, then settles.
    step(hop, mood === "happy" ? -Math.abs(Math.sin(t * 7.5)) * 4 : 0, 0.2, 0.62)

    // Mouth only shows while speaking.
    step(mouthS, s === "speaking" ? level : 0, 0.35, 0.5)
    step(wob, s === "speaking" ? 0.6 + level * 2.2 : s === "listening" ? level * 1.2 : 0, 0.15, 0.7)

    // Body: breathing, a bob while waiting, ripples while speaking.
    const breathe = Math.sin(t * (s === "sleeping" ? 0.9 : 1.4)) * (s === "sleeping" ? 0.016 : 0.011)
    const bob = (s === "waiting" ? Math.sin(t * 2.6) * 2.2 : 0) + hop.x
    const sx = 1 - breathe * 0.6
    const sy = 1 + breathe
    if (wob.x > 0.02 || breathe !== 0 || props.shape !== lastShape) {
      const d = outline(props.shape, wob.x, t, sx, sy)
      if (d !== lastPath) {
        body.setAttribute("d", d)
        lastPath = d
      }
      lastShape = props.shape
    }
    body.setAttribute("transform", `translate(0 ${(bob - breathe * 40).toFixed(2)})`)
    shadow.setAttribute("rx", (30 - breathe * 120 - bob * 0.8).toFixed(2))
    shadow.setAttribute("opacity", (0.16 - bob * 0.01).toFixed(3))

    // Face.
    const eh = Math.max(1.6, 20 * open.x * widen.x)
    const ew = 9 * (s === "listening" ? 1.08 : 1) * narrow.x
    const fx = ex.x
    const fy = ey.x + bob - breathe * 40
    face.setAttribute("transform", `translate(${fx.toFixed(2)} ${fy.toFixed(2)})`)
    for (const [el, x] of [
      [eyeL, 33],
      [eyeR, 58],
    ] as const) {
      el.setAttribute("x", (x + (9 - ew) / 2).toFixed(2))
      el.setAttribute("width", ew.toFixed(2))
      // Happy eyes sit a little higher, as a smile pushes them up.
      el.setAttribute("y", (48 - eh / 2 - 2 - (mood === "happy" ? 2 : 0)).toFixed(2))
      el.setAttribute("height", eh.toFixed(2))
      el.setAttribute("rx", Math.min(ew / 2, eh / 2).toFixed(2))
    }
    const mh = 2 + Math.max(0, mouthS.x) * 9
    const mw = 10 - Math.max(0, mouthS.x) * 2
    mouth.setAttribute("x", (50 - mw / 2).toFixed(2))
    mouth.setAttribute("y", (63 - mh / 2).toFixed(2))
    mouth.setAttribute("width", mw.toFixed(2))
    mouth.setAttribute("height", mh.toFixed(2))
    mouth.setAttribute("rx", Math.min(mw, mh / 2 + 1).toFixed(2))
    mouth.setAttribute("opacity", s === "speaking" ? "1" : "0")

    // Ring.
    let rr = 56
    let rop = 0
    if (s === "listening") {
      rr = 54 + level * 6
      rop = 0.35 + level * 0.5
    } else if (s === "waiting") {
      rop = 0.35 + Math.sin(t * 2.6) * 0.15
    } else if (s === "speaking") {
      rr = 55 + level * 2.5
      rop = 0.18
    } else if (mood === "worried") {
      rop = 0.28
    } else if (mood === "happy") {
      rr = 56 + Math.abs(Math.sin(t * 7.5)) * 2
      rop = 0.3
    }
    ring.setAttribute("r", rr.toFixed(2))
    ring.setAttribute("opacity", rop.toFixed(3))
    ring.setAttribute(
      "stroke",
      s === "waiting" || mood === "worried" ? "var(--warn)" : s === "listening" || mood === "happy" ? "var(--accent)" : "var(--line-2)",
    )
    arc.setAttribute("opacity", s === "working" || s === "thinking" ? "0.9" : "0")
    arc.setAttribute("transform", `rotate(${((t * (s === "thinking" ? 220 : 120)) % 360).toFixed(1)} 50 50)`)

    raf = requestAnimationFrame(frame)
  }

  onMount(() => {
    document.addEventListener("pointermove", onMove)
    raf = requestAnimationFrame(frame)
  })
  onCleanup(() => {
    document.removeEventListener("pointermove", onMove)
    cancelAnimationFrame(raf)
  })
  createEffect(() => {
    // A shape change redraws the body on the next frame.
    void props.shape
    lastShape = undefined
  })

  const size = () => props.size ?? 168
  const ink = () => inkFor(props.color)

  return (
    <svg
      ref={svg}
      width={size()}
      height={size()}
      viewBox="-10 -10 120 120"
      role="img"
      aria-label="Dot"
      style={{ overflow: "visible", cursor: props.onClick ? "pointer" : "default", display: "block" }}
      onClick={() => {
        blinkAt = performance.now()
        props.onClick?.()
      }}
    >
      <ellipse ref={shadow} cx="50" cy="104" rx="30" ry="3.2" fill="#000" opacity="0.16" />
      <circle ref={ring} cx="50" cy="50" r="56" fill="none" stroke-width="1.2" opacity="0" />
      <circle
        ref={arc}
        cx="50"
        cy="50"
        r="56"
        fill="none"
        stroke="var(--text-3)"
        stroke-width="1.4"
        stroke-linecap="round"
        stroke-dasharray="40 312"
        opacity="0"
      />
      <path ref={body} d={shapePath(props.shape)} fill={props.color} style={{ transition: "fill 240ms var(--ease)" }} />
      <g ref={face} fill={ink()}>
        <rect ref={eyeL} x="33" y="36" width="9" height="20" rx="4.5" />
        <rect ref={eyeR} x="58" y="36" width="9" height="20" rx="4.5" />
        <rect ref={mouth} x="45" y="62" width="10" height="2" rx="1" opacity="0" />
      </g>
    </svg>
  )
}
