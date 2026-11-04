// Creating a dot: pick a look, a name and what it is for. The live
// preview reacts to every choice, so the dot already feels alive before it
// exists.

import { createSignal, For, Show } from "solid-js"
import type { Shape } from "./api"
import { LiveAvatar, MiniAvatar } from "./Avatar"
import { Icon } from "./icons"
import { createDot, state } from "./store"
import { VoicePicker } from "./VoicePicker"

export interface Template {
  id: string
  title: string
  blurb: string
  role: string
  prompt: string
  shape: Shape
  color: string
}

export const templates: Template[] = [
  {
    id: "assistant",
    title: "Kişisel asistan",
    blurb: "E-posta, takvim, hatırlatma ve günlük işler.",
    role: "Kişisel asistan",
    prompt: "Günlük işlerimde yardım et: e-postalar, takvim, hatırlatmalar ve küçük araştırmalar. Önemli bir şey olduğunda kısa ve net haber ver.",
    shape: "orb",
    color: "#8d5cf6",
  },
  {
    id: "research",
    title: "Araştırmacı",
    blurb: "Kaynak tarar, karşılaştırır, kaynaklı özet çıkarır.",
    role: "Araştırmacı",
    prompt: "Web'de araştırma yap, kaynakları karşılaştır ve bulgularını kaynaklarıyla birlikte özetle. Uzun sonuçları çalışma alanına not olarak kaydet.",
    shape: "bloom",
    color: "#3b7cf2",
  },
  {
    id: "code",
    title: "Kodcu",
    blurb: "Hata ayıklar, test çalıştırır, işi bitirene kadar uğraşır.",
    role: "Yazılım geliştirici",
    prompt: "Kod depolarında çalış: hataları bul ve düzelt, testleri çalıştır, yaptığın değişiklikleri açıkla. Testler geçmeden işi bitmiş sayma.",
    shape: "hex",
    color: "#2fb67c",
  },
  {
    id: "social",
    title: "Sosyal medya",
    blurb: "İçerik planı ve gönderi taslakları hazırlar.",
    role: "Sosyal medya yöneticisi",
    prompt: "İçerik planı hazırla ve gönderi taslakları yaz. Hiçbir şeyi onayım olmadan paylaşma.",
    shape: "star",
    color: "#e2498a",
  },
  {
    id: "sales",
    title: "Satış",
    blurb: "Müşteri araştırır, kişisel mesaj taslağı yazar.",
    role: "Satış asistanı",
    prompt: "Potansiyel müşterileri araştır, kişiselleştirilmiş mesaj taslakları yaz ve takipleri hatırlat. Mesajları onayım olmadan gönderme.",
    shape: "shield",
    color: "#f2762e",
  },
]

export function CreateDot(props: { template?: Template; onClose: () => void }) {
  const t = props.template
  const [name, setName] = createSignal("")
  const [shape, setShape] = createSignal<Shape>(t?.shape ?? "orb")
  const [color, setColor] = createSignal(t?.color ?? "#a8693c")
  const [tpl, setTpl] = createSignal<Template | undefined>(t)
  const [voice, setVoice] = createSignal<{ id: string; name: string } | undefined>()
  const [busy, setBusy] = createSignal(false)

  const submit = async () => {
    if (!name().trim() || busy()) return
    setBusy(true)
    const d = await createDot({
      name: name().trim(),
      shape: shape(),
      color: color(),
      role: tpl()?.role ?? "",
      system_prompt: tpl()?.prompt ?? "",
      voice_id: voice()?.id ?? "",
      voice_name: voice()?.name ?? "",
    })
    setBusy(false)
    if (d) props.onClose()
  }

  return (
    <div class="scrim" onClick={(e) => e.target === e.currentTarget && props.onClose()}>
      <div class="dialog" role="dialog" aria-modal="true" aria-labelledby="create-title" style={{ "--accent": color() }}>
        <div class="dialog-head">
          <h2 id="create-title">Yeni dot</h2>
          <button class="icon-btn sm" onClick={props.onClose} aria-label="Kapat">
            {Icon.close(14)}
          </button>
        </div>
        <div class="dialog-body">
          <div style={{ display: "grid", "justify-items": "center", padding: "6px 0 18px" }}>
            <LiveAvatar shape={shape()} color={color()} state={name() ? "idle" : "thinking"} size={112} />
          </div>
          <div class="field">
            <input
              ref={(el) => setTimeout(() => el.focus(), 30)}
              class="input center"
              placeholder="Dot'una bir ad ver"
              maxLength={40}
              value={name()}
              onInput={(e) => setName(e.currentTarget.value)}
              onKeyDown={(e) => e.key === "Enter" && submit()}
            />
          </div>
          <div class="field">
            <span>Şekil</span>
            <div class="shape-grid">
              <For each={state.shapes}>
                {(s) => (
                  <button class="shape-opt" aria-pressed={shape() === s} onClick={() => setShape(s)} title={s}>
                    <MiniAvatar shape={s} color={color()} size={30} />
                  </button>
                )}
              </For>
            </div>
          </div>
          <div class="field">
            <span>Renk</span>
            <div class="swatches">
              <For each={state.colors}>
                {(c) => <button class="swatch" style={{ background: c }} aria-pressed={color() === c} onClick={() => setColor(c)} aria-label={c} />}
              </For>
            </div>
          </div>
          <div class="field">
            <span>Ne için?</span>
            <div class="swatches" style={{ gap: "6px" }}>
              <button class="btn sm" classList={{ primary: !tpl() }} onClick={() => setTpl(undefined)}>
                Genel
              </button>
              <For each={templates}>
                {(x) => (
                  <button class="btn sm" classList={{ primary: tpl()?.id === x.id }} onClick={() => setTpl(x)}>
                    {x.title}
                  </button>
                )}
              </For>
            </div>
            <Show when={tpl()}>
              <small>{tpl()!.prompt}</small>
            </Show>
          </div>
          <div class="field">
            <span>Ses</span>
            <VoicePicker value={voice()?.id ?? ""} onChange={(v) => setVoice(v)} compact />
          </div>
        </div>
        <div class="dialog-foot">
          <button class="btn ghost" onClick={props.onClose}>
            Vazgeç
          </button>
          <button class="btn primary" disabled={!name().trim() || busy()} onClick={submit}>
            {busy() ? "Oluşturuluyor…" : "Oluştur"}
          </button>
        </div>
      </div>
    </div>
  )
}
