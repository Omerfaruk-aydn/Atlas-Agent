import { useEffect, useImperativeHandle, useRef, useState, type Ref } from "react";
import { CSRF_HEADER, csrfToken, http } from "../../api/http";
import { lang, t } from "../../lib/i18n";
import { IconCheck, IconClose, IconMic } from "../../primitives/icons";
import { Tip } from "../../primitives/ui";
import { useApp } from "../../stores/app";

export function pcmWav(blocks: Float32Array[], rate: number): ArrayBuffer {
  const length = blocks.reduce((n, block) => n + block.length, 0);
  const out = new ArrayBuffer(44 + length * 2);
  const view = new DataView(out);
  const text = (at: number, value: string) => [...value].forEach((c, i) => view.setUint8(at + i, c.charCodeAt(0)));
  text(0, "RIFF"); view.setUint32(4, 36 + length * 2, true); text(8, "WAVE"); text(12, "fmt ");
  view.setUint32(16, 16, true); view.setUint16(20, 1, true); view.setUint16(22, 1, true);
  view.setUint32(24, rate, true); view.setUint32(28, rate * 2, true); view.setUint16(32, 2, true); view.setUint16(34, 16, true);
  text(36, "data"); view.setUint32(40, length * 2, true);
  let at = 44;
  for (const block of blocks) for (const value of block) {
    const clamped = Math.max(-1, Math.min(1, value));
    view.setInt16(at, Math.round(clamped * (clamped < 0 ? 32768 : 32767)), true);
    at += 2;
  }
  return out;
}

interface Capture {
  stream: MediaStream;
  context: AudioContext;
  node: AudioWorkletNode;
  analyser: AnalyserNode;
  blocks: Float32Array[];
}

export function VoiceInput({ onText, onActive, startRef }: { onText(text: string): void; onActive(active: boolean): void; startRef?: Ref<{ start(): void }> }) {
  const [phase, setPhase] = useState<"idle" | "starting" | "recording" | "transcribing">("idle");
  const [seconds, setSeconds] = useState(0);
  const capture = useRef<Capture | null>(null);
  const canvas = useRef<HTMLCanvasElement>(null);
  const request = useRef<AbortController | null>(null);
  const mounted = useRef(true);
  const finishing = useRef(false);
  const toast = useApp((s) => s.toast);
  const workspace = useApp((s) => s.workspace?.id ?? "");
  const endpoint = `/api/workspaces/${encodeURIComponent(workspace)}/speech?language=${lang === "tr" ? "tr-TR" : "en-US"}`;

  const release = () => {
    const current = capture.current;
    capture.current = null;
    if (!current) return;
    current.stream.getTracks().forEach((track) => track.stop());
    current.node.disconnect();
    void current.context.close();
  };
  useEffect(() => {
    mounted.current = true;
    return () => { mounted.current = false; request.current?.abort(); release(); };
  }, []);
  useEffect(() => onActive(phase !== "idle"), [phase, onActive]);

  const finish = async (keep: boolean) => {
    if (finishing.current) return;
    finishing.current = true;
    const current = capture.current;
    if (!current || !keep) {
      request.current?.abort();
      release();
      setPhase("idle");
      finishing.current = false;
      return;
    }
    setPhase("transcribing");
    try {
      await new Promise<void>((resolve) => {
        const timeout = setTimeout(resolve, 300);
        const flushed = (event: MessageEvent) => {
          if (event.data !== "flushed") return;
          clearTimeout(timeout);
          current.node.port.removeEventListener("message", flushed);
          resolve();
        };
        current.node.port.addEventListener("message", flushed);
        current.node.port.postMessage("flush");
      });
      const wav = pcmWav(current.blocks, current.context.sampleRate);
      release();
      request.current = new AbortController();
      const res = await fetch(endpoint, { method: "POST", headers: { [CSRF_HEADER]: csrfToken(), "Content-Type": "audio/wav" }, body: wav, signal: request.current.signal });
      const data = await res.json() as { text?: string; error?: string };
      if (!res.ok) throw new Error(data.error ?? res.statusText);
      if (mounted.current) {
        if (data.text?.trim()) onText(data.text.trim());
        else toast(t("compose.noSpeech"), "danger");
      }
    } catch (err) {
      if (mounted.current && !(err instanceof DOMException && err.name === "AbortError")) toast(err instanceof Error ? err.message : String(err), "danger");
    } finally {
      release();
      if (mounted.current) setPhase("idle");
      finishing.current = false;
    }
  };
  const finishRef = useRef(finish);
  finishRef.current = finish;
  useEffect(() => {
    if (phase !== "recording") return;
    const started = performance.now();
    const timer = setInterval(() => {
      const value = Math.floor((performance.now() - started) / 1000);
      setSeconds(value);
      if (value >= 120) void finishRef.current(true);
    }, 200);
    const escape = (event: KeyboardEvent) => {
      if (event.key === "Escape") { event.preventDefault(); event.stopPropagation(); void finishRef.current(false); }
    };
    document.addEventListener("keydown", escape, true);
    let frame = 0;
    const levels = Array<number>(64).fill(0);
    const draw = () => {
      const source = capture.current;
      const el = canvas.current;
      if (source && el) {
        const samples = new Uint8Array(source.analyser.fftSize);
        source.analyser.getByteTimeDomainData(samples);
        const level = Math.sqrt(samples.reduce((sum, v) => sum + ((v - 128) / 128) ** 2, 0) / samples.length);
        levels.push(Math.min(1, level * 7)); levels.shift();
        const ctx = el.getContext("2d");
        if (ctx) {
          el.width = Math.max(1, el.clientWidth * devicePixelRatio); el.height = 28 * devicePixelRatio;
          ctx.scale(devicePixelRatio, devicePixelRatio);
          ctx.fillStyle = getComputedStyle(el).color;
          for (let i = 0; i < levels.length; i++) {
            const h = Math.max(2, levels[i] * 24);
            ctx.globalAlpha = 0.35 + (i / levels.length) * 0.65;
            ctx.fillRect((i / levels.length) * el.clientWidth, (28 - h) / 2, 2, h);
          }
        }
      }
      frame = requestAnimationFrame(draw);
    };
    frame = requestAnimationFrame(draw);
    return () => { clearInterval(timer); cancelAnimationFrame(frame); document.removeEventListener("keydown", escape, true); };
  }, [phase]);

  const start = async () => {
    if (phase !== "idle") return;
    setPhase("starting"); setSeconds(0);
    let stream: MediaStream | null = null;
    let context: AudioContext | null = null;
    try {
      await http.get(endpoint);
      if (!mounted.current) return;
      stream = await navigator.mediaDevices.getUserMedia({ audio: { channelCount: 1, echoCancellation: true, noiseSuppression: true }, video: false });
      if (!mounted.current) { stream.getTracks().forEach((track) => track.stop()); return; }
      context = new AudioContext({ sampleRate: 16000 });
      await context.audioWorklet.addModule("/pcm-capture.js");
      if (!mounted.current) { stream.getTracks().forEach((track) => track.stop()); await context.close(); return; }
      const node = new AudioWorkletNode(context, "atlas-pcm");
      const analyser = context.createAnalyser(); analyser.fftSize = 256;
      const blocks: Float32Array[] = [];
      node.port.onmessage = (event) => { if (event.data instanceof Float32Array) blocks.push(event.data); };
      const source = context.createMediaStreamSource(stream);
      source.connect(analyser); source.connect(node);
      const mute = context.createGain(); mute.gain.value = 0;
      node.connect(mute).connect(context.destination);
      capture.current = { stream, context, node, analyser, blocks };
      await context.resume();
      setPhase("recording");
    } catch (err) {
      stream?.getTracks().forEach((track) => track.stop());
      if (context && context.state !== "closed") void context.close();
      if (mounted.current) { setPhase("idle"); toast(err instanceof DOMException ? t("compose.micError") : err instanceof Error ? err.message : String(err), "danger"); }
    }
  };

  useImperativeHandle(startRef, () => ({ start: () => void start() }));
  if (phase === "idle") return <Tip label={t("compose.record")}><button type="button" className="icon-button voice-trigger" aria-label={t("compose.record")} onClick={() => void start()}><IconMic /></button></Tip>;
  return <div className="voice-capture" data-phase={phase}>
    {phase === "recording" ? <><canvas ref={canvas} className="voice-wave" aria-label={t("compose.recording")} /><span className="voice-time" role="timer">{Math.floor(seconds / 60)}:{String(seconds % 60).padStart(2, "0")}</span></> : <span className="voice-status" role="status">{t(phase === "starting" ? "compose.starting" : "compose.transcribing")}</span>}
    {phase !== "starting" && <button type="button" className="icon-button" aria-label={t("compose.discard")} disabled={phase === "transcribing"} onClick={() => void finish(false)}><IconClose /></button>}
    {phase === "recording" && <button type="button" className="voice-confirm" aria-label={t("compose.confirm")} onClick={() => void finish(true)}><IconCheck /></button>}
  </div>;
}
