// Speech out and speech in for dots.
//
// Speech out plays ElevenLabs audio through an AnalyserNode, so the dot's
// mouth moves with the sound actually coming out of the speakers. Without
// an ElevenLabs key it falls back to the browser's own voices, with the
// mouth driven by word boundaries.
//
// Speech in uses the browser's speech recognition and measures the mic
// level separately so the dot can visibly listen.

import { createSignal } from "solid-js"
import { dotsApi } from "./api"

let ctx: AudioContext | undefined
let analyser: AnalyserNode | undefined
let buf: Uint8Array<ArrayBuffer> | undefined
let current: HTMLAudioElement | undefined
let currentURL = ""
let abort: AbortController | undefined
let fallbackLevel = 0

const [speaking, setSpeaking] = createSignal(false)
export { speaking }

function audioGraph() {
  if (!ctx) {
    ctx = new AudioContext()
    analyser = ctx.createAnalyser()
    analyser.fftSize = 512
    analyser.smoothingTimeConstant = 0.55
    analyser.connect(ctx.destination)
    buf = new Uint8Array(new ArrayBuffer(analyser.fftSize))
  }
  return { ctx, analyser: analyser! }
}

/** outputLevel is the loudness of what the dot is saying, 0-1. */
export function outputLevel(): number {
  if (!speaking()) return 0
  if (analyser && buf && current) {
    analyser.getByteTimeDomainData(buf)
    let sum = 0
    for (let i = 0; i < buf.length; i++) {
      const v = (buf[i] - 128) / 128
      sum += v * v
    }
    return Math.min(1, Math.sqrt(sum / buf.length) * 4.2)
  }
  fallbackLevel *= 0.86
  return fallbackLevel
}

/** stop ends any speech in progress. */
export function stopSpeaking() {
  abort?.abort()
  abort = undefined
  if (current) {
    current.pause()
    current.src = ""
    current = undefined
  }
  if (currentURL) URL.revokeObjectURL(currentURL)
  currentURL = ""
  if ("speechSynthesis" in window) speechSynthesis.cancel()
  setSpeaking(false)
}

/** spokenText strips markdown so it reads naturally. */
export function spokenText(md: string): string {
  return md
    .replace(/```[\s\S]*?```/g, " ")
    .replace(/`([^`]+)`/g, "$1")
    .replace(/!\[[^\]]*\]\([^)]*\)/g, " ")
    .replace(/\[([^\]]+)\]\([^)]*\)/g, "$1")
    .replace(/^#+\s*/gm, "")
    .replace(/[*_~>|]/g, "")
    .replace(/\s+/g, " ")
    .trim()
}

/**
 * speak says text in voiceID. useCloud picks ElevenLabs; otherwise, or if
 * ElevenLabs fails, the browser voice speaks. Resolves when speech ends.
 */
export async function speak(text: string, voiceID: string, useCloud: boolean, lang = "tr-TR"): Promise<void> {
  stopSpeaking()
  const said = spokenText(text)
  if (!said) return
  if (useCloud && voiceID) {
    try {
      abort = new AbortController()
      const blob = await dotsApi.speak(voiceID, said, abort.signal)
      await playBlob(blob)
      return
    } catch (err) {
      if ((err as Error)?.name === "AbortError") return
      console.warn("Cloud speech failed, using the browser voice", err)
    }
  }
  await browserSpeak(said, lang)
}

/** playURL plays a voice preview. */
export async function playURL(url: string): Promise<void> {
  stopSpeaking()
  const { ctx, analyser } = audioGraph()
  if (ctx.state === "suspended") await ctx.resume()
  const el = new Audio()
  el.crossOrigin = "anonymous"
  el.src = url
  current = el
  try {
    ctx.createMediaElementSource(el).connect(analyser)
  } catch {
    // A cross-origin preview without CORS still plays, just unmeasured.
  }
  setSpeaking(true)
  await new Promise<void>((resolve) => {
    el.onended = el.onerror = () => resolve()
    void el.play().catch(() => resolve())
  })
  if (current === el) stopSpeaking()
}

async function playBlob(blob: Blob) {
  const { ctx, analyser } = audioGraph()
  if (ctx.state === "suspended") await ctx.resume()
  const el = new Audio()
  currentURL = URL.createObjectURL(blob)
  el.src = currentURL
  current = el
  ctx.createMediaElementSource(el).connect(analyser)
  setSpeaking(true)
  await new Promise<void>((resolve) => {
    el.onended = el.onerror = () => resolve()
    void el.play().catch(() => resolve())
  })
  if (current === el) stopSpeaking()
}

function browserSpeak(text: string, lang: string): Promise<void> {
  if (!("speechSynthesis" in window)) return Promise.resolve()
  return new Promise((resolve) => {
    const u = new SpeechSynthesisUtterance(text)
    u.lang = lang
    const voice = speechSynthesis.getVoices().find((v) => v.lang.toLowerCase().startsWith(lang.slice(0, 2).toLowerCase()))
    if (voice) u.voice = voice
    u.onboundary = () => {
      fallbackLevel = 0.55 + Math.random() * 0.45
    }
    u.onend = u.onerror = () => {
      setSpeaking(false)
      resolve()
    }
    setSpeaking(true)
    speechSynthesis.speak(u)
  })
}

// ---------- Listening ----------

type Recognition = {
  lang: string
  interimResults: boolean
  continuous: boolean
  onresult: ((e: any) => void) | null
  onend: (() => void) | null
  onerror: ((e: any) => void) | null
  start(): void
  stop(): void
  abort(): void
}

export function canListen(): boolean {
  return "webkitSpeechRecognition" in window || "SpeechRecognition" in window
}

let micStream: MediaStream | undefined
let micAnalyser: AnalyserNode | undefined
let micBuf: Uint8Array<ArrayBuffer> | undefined

/** inputLevel is the mic loudness while listening, 0-1. */
export function inputLevel(): number {
  if (!micAnalyser || !micBuf) return 0
  micAnalyser.getByteTimeDomainData(micBuf)
  let sum = 0
  for (let i = 0; i < micBuf.length; i++) {
    const v = (micBuf[i] - 128) / 128
    sum += v * v
  }
  return Math.min(1, Math.sqrt(sum / micBuf.length) * 5)
}

/**
 * listen starts recognition. onText gets the running transcript; the
 * returned function stops listening and resolves with the final text.
 */
export async function listen(lang: string, onText: (text: string) => void): Promise<() => Promise<string>> {
  const Ctor = (window as any).SpeechRecognition ?? (window as any).webkitSpeechRecognition
  const rec: Recognition = new Ctor()
  rec.lang = lang
  rec.interimResults = true
  rec.continuous = true
  let finalText = ""
  let interim = ""
  rec.onresult = (e: any) => {
    interim = ""
    for (let i = e.resultIndex; i < e.results.length; i++) {
      const r = e.results[i]
      if (r.isFinal) finalText += r[0].transcript
      else interim += r[0].transcript
    }
    onText((finalText + interim).trim())
  }
  let ended!: () => void
  const done = new Promise<void>((r) => (ended = r))
  rec.onend = () => ended()
  rec.onerror = () => ended()
  rec.start()

  try {
    micStream = await navigator.mediaDevices.getUserMedia({ audio: true })
    const { ctx } = audioGraph()
    if (ctx.state === "suspended") await ctx.resume()
    micAnalyser = ctx.createAnalyser()
    micAnalyser.fftSize = 512
    micBuf = new Uint8Array(new ArrayBuffer(micAnalyser.fftSize))
    ctx.createMediaStreamSource(micStream).connect(micAnalyser)
  } catch {
    // Recognition can still work without a level meter.
  }

  return async () => {
    rec.stop()
    await Promise.race([done, new Promise((r) => setTimeout(r, 1200))])
    micStream?.getTracks().forEach((t) => t.stop())
    micStream = undefined
    micAnalyser = undefined
    return (finalText + interim).trim()
  }
}

// ---------- Wake word ----------

/** fold lowercases and strips accents so "Selin" matches "selin" and
 * "Çınar" matches "cinar" in what the recogniser heard. */
export function fold(s: string): string {
  return s
    .toLocaleLowerCase("tr")
    .replace(/ı/g, "i")
    .normalize("NFD")
    .replace(/[̀-ͯ]/g, "")
    .replace(/[^\p{L}\p{N} ]+/gu, " ")
    .replace(/\s+/g, " ")
    .trim()
}

/**
 * startWake listens in the background for one of names() and calls onWake
 * with the name it heard. The browser's recogniser stops on its own every
 * so often, so it is restarted until the returned function is called.
 */
export function startWake(lang: string, names: () => string[], onWake: (name: string) => void): () => void {
  const Ctor = (window as any).SpeechRecognition ?? (window as any).webkitSpeechRecognition
  if (!Ctor) return () => {}
  let stopped = false
  let rec: Recognition | undefined
  let failures = 0
  const run = () => {
    if (stopped) return
    rec = new Ctor() as Recognition
    rec.lang = lang
    rec.interimResults = true
    rec.continuous = true
    rec.onresult = (e: any) => {
      failures = 0
      for (let i = e.resultIndex; i < e.results.length; i++) {
        const heard = ` ${fold(e.results[i][0].transcript)} `
        const hit = names().find((n) => {
          const f = fold(n)
          return f && (heard.includes(` hey ${f} `) || heard.includes(` ${f} `))
        })
        if (hit) {
          stopped = true
          rec?.abort()
          onWake(hit)
          return
        }
      }
    }
    rec.onerror = (e: any) => {
      // Without permission there is nothing to retry.
      if (e?.error === "not-allowed" || e?.error === "service-not-allowed") stopped = true
      failures++
    }
    rec.onend = () => {
      if (!stopped) setTimeout(run, Math.min(10_000, 250 * 2 ** Math.min(failures, 5)))
    }
    try {
      rec.start()
    } catch {
      setTimeout(run, 1000)
    }
  }
  run()
  return () => {
    stopped = true
    rec?.abort()
  }
}
