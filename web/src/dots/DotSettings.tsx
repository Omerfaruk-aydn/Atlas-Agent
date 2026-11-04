// A dot's settings: look, personality, voice, permissions and workspace.
// Edits are kept as a draft and saved together; the persona file Atlas
// reads is rewritten on save, so the next message already uses it.

import { createEffect, createMemo, createResource, createSignal, For, Match, on, onCleanup, Show, Switch } from "solid-js"
import { createStore } from "solid-js/store"
import { dotsApi, fullAccessActive, type Dot, type Personality, type Shape } from "./api"
import type { SettingsPane } from "./commands"
import { MiniAvatar } from "./Avatar"
import { Icon } from "./icons"
import { deleteDot, enableNotifications, grantFullAccess, revokeFullAccess, setPref, state, toast, updateDot } from "./store"
import { Mailbox } from "./Mailbox"
import { VoicePicker } from "./VoicePicker"

type Pane = SettingsPane

const sliders: { key: keyof Personality; label: string; low: string; high: string }[] = [
  { key: "warmth", label: "Sıcaklık", low: "Mesafeli", high: "Sıcak" },
  { key: "humor", label: "Mizah", low: "Ciddi", high: "Esprili" },
  { key: "formality", label: "Resmiyet", low: "Samimi", high: "Resmi" },
  { key: "proactivity", label: "Proaktiflik", low: "Söyleneni yapar", high: "Önden gider" },
]

export function DotSettings(props: { dot: Dot; initial?: Pane; onClose: () => void }) {
  const [pane, setPane] = createSignal<Pane>(props.initial ?? "general")
  createEffect(on(() => props.initial, (p) => p && setPane(p)))
  const [draft, setDraft] = createStore({
    name: props.dot.name,
    role: props.dot.role,
    shape: props.dot.shape as Shape,
    color: props.dot.color,
    voice_id: props.dot.voice_id,
    voice_name: props.dot.voice_name,
    system_prompt: props.dot.system_prompt,
    personality: { ...props.dot.personality },
    always_ask: [...(props.dot.rules.always_ask ?? [])],
    auto_allow: [...(props.dot.rules.auto_allow ?? [])],
    skills: [...(props.dot.skills ?? [])],
  })
  const [skills] = createResource(
    () => (pane() === "skills" ? props.dot.id : undefined),
    (id) => dotsApi.skills(id),
  )
  const [skillQuery, setSkillQuery] = createSignal("")
  const [saving, setSaving] = createSignal(false)
  const [fullOpen, setFullOpen] = createSignal(false)
  const [confirmDelete, setConfirmDelete] = createSignal(false)
  const live = () => state.dots.find((d) => d.id === props.dot.id) ?? props.dot

  const dirty = createMemo(() => {
    const d = props.dot
    return (
      draft.name !== d.name ||
      draft.role !== d.role ||
      draft.shape !== d.shape ||
      draft.color !== d.color ||
      draft.voice_id !== d.voice_id ||
      draft.system_prompt !== d.system_prompt ||
      JSON.stringify(draft.personality) !== JSON.stringify(d.personality) ||
      JSON.stringify(draft.always_ask) !== JSON.stringify(d.rules.always_ask ?? []) ||
      JSON.stringify(draft.auto_allow) !== JSON.stringify(d.rules.auto_allow ?? []) ||
      JSON.stringify(draft.skills) !== JSON.stringify(d.skills ?? [])
    )
  })

  const save = async () => {
    if (!draft.name.trim()) {
      toast("Dot'un bir adı olmalı", true)
      setPane("general")
      return
    }
    setSaving(true)
    const ok = await updateDot(props.dot.id, {
      name: draft.name,
      role: draft.role,
      shape: draft.shape,
      color: draft.color,
      voice_id: draft.voice_id,
      voice_name: draft.voice_name,
      system_prompt: draft.system_prompt,
      personality: { ...draft.personality },
      rules: { always_ask: [...draft.always_ask], auto_allow: [...draft.auto_allow] },
      skills: [...draft.skills],
    })
    setSaving(false)
    if (ok) {
      toast("Kaydedildi")
      props.onClose()
    }
  }

  const nav: { id: Pane; label: string; icon: () => any }[] = [
    { id: "general", label: "Genel", icon: () => Icon.user(15) },
    { id: "persona", label: "Kişilik", icon: () => Icon.sliders(15) },
    { id: "voice", label: "Ses ve uyarılar", icon: () => Icon.voice(15) },
    { id: "skills", label: "Beceriler", icon: () => Icon.sparkle(15) },
    { id: "permissions", label: "İzinler", icon: () => Icon.shield(15) },
    { id: "workspace", label: "Çalışma alanı", icon: () => Icon.folder(15) },
  ]

  return (
    <div class="scrim" onClick={(e) => e.target === e.currentTarget && !fullOpen() && props.onClose()}>
      <div class="dialog wide" role="dialog" aria-modal="true" aria-label={`${props.dot.name} ayarları`} style={{ height: "min(680px, calc(100dvh - 32px))", "--accent": draft.color }}>
        <div class="dialog-head" style={{ "border-bottom": "1px solid var(--line)", "padding-bottom": "14px" }}>
          <MiniAvatar shape={draft.shape} color={draft.color} size={26} />
          <h2>{draft.name || props.dot.name}</h2>
          <button class="icon-btn sm" onClick={props.onClose} aria-label="Kapat">
            {Icon.close(14)}
          </button>
        </div>
        <div class="settings">
          <nav class="settings-nav" role="tablist">
            <For each={nav}>
              {(n) => (
                <button role="tab" aria-selected={pane() === n.id} onClick={() => setPane(n.id)}>
                  {n.icon()}
                  {n.label}
                </button>
              )}
            </For>
          </nav>
          <div class="settings-pane">
            <Switch>
              <Match when={pane() === "general"}>
                <h3>Genel</h3>
                <label class="field">
                  <span>Ad</span>
                  <input class="input" maxLength={40} value={draft.name} onInput={(e) => setDraft("name", e.currentTarget.value)} />
                </label>
                <label class="field">
                  <span>Rol</span>
                  <input class="input" maxLength={60} placeholder="ör. Araştırmacı" value={draft.role} onInput={(e) => setDraft("role", e.currentTarget.value)} />
                </label>
                <div class="field">
                  <span>Şekil</span>
                  <div class="shape-grid">
                    <For each={state.shapes}>
                      {(s) => (
                        <button class="shape-opt" aria-pressed={draft.shape === s} onClick={() => setDraft("shape", s)}>
                          <MiniAvatar shape={s} color={draft.color} size={30} />
                        </button>
                      )}
                    </For>
                  </div>
                </div>
                <div class="field">
                  <span>Renk</span>
                  <div class="swatches">
                    <For each={state.colors}>
                      {(c) => <button class="swatch" style={{ background: c }} aria-pressed={draft.color === c} onClick={() => setDraft("color", c)} aria-label={c} />}
                    </For>
                  </div>
                </div>
                <div class="group" style={{ "margin-top": "24px" }}>
                  <div class="group-row">
                    <div class="text">
                      <b>Dot'u sil</b>
                      <small>Listeden kaldırılır. Çalışma alanı klasörü ve dosyaları diskte kalır.</small>
                    </div>
                    <Show
                      when={confirmDelete()}
                      fallback={
                        <button class="btn sm" onClick={() => setConfirmDelete(true)}>
                          Sil
                        </button>
                      }
                    >
                      <button
                        class="btn danger sm"
                        onClick={async () => {
                          await deleteDot(props.dot.id)
                          props.onClose()
                        }}
                      >
                        Evet, sil
                      </button>
                    </Show>
                  </div>
                </div>
              </Match>

              <Match when={pane() === "persona"}>
                <h3>Kişilik</h3>
                <div style={{ "margin-bottom": "20px" }}>
                  <For each={sliders}>
                    {(s) => (
                      <label class="slider">
                        <span>{s.label}</span>
                        <input
                          type="range"
                          min="0"
                          max="100"
                          step="1"
                          value={draft.personality[s.key]}
                          style={{ background: `linear-gradient(to right, var(--text) ${draft.personality[s.key]}%, var(--panel-3) ${draft.personality[s.key]}%)` }}
                          onInput={(e) => setDraft("personality", s.key, Number(e.currentTarget.value))}
                        />
                        <small>{draft.personality[s.key] < 34 ? s.low : draft.personality[s.key] < 67 ? "Dengeli" : s.high}</small>
                      </label>
                    )}
                  </For>
                </div>
                <label class="field">
                  <span>Sistem promptu</span>
                  <textarea
                    class="textarea"
                    rows={9}
                    maxLength={8000}
                    placeholder={`${draft.name || "Dot"} ne yapsın, nasıl davransın? ör. "Her sabah 08:00'de rakip fiyatlarını kontrol et, %5'ten büyük değişimi haber ver."`}
                    value={draft.system_prompt}
                    onInput={(e) => setDraft("system_prompt", e.currentTarget.value)}
                  />
                  <small>Dot her mesajda bunu okur. Değişiklik bir sonraki mesajdan itibaren geçerli olur.</small>
                </label>
              </Match>

              <Match when={pane() === "voice"}>
                <h3>Ses</h3>
                <div class="group">
                  <div class="group-row">
                    <div class="text">
                      <b>Cevapları sesli oku</b>
                      <small>Dot bir işi bitirince cevabını seçili sesle okur.</small>
                    </div>
                    <button class="switch" role="switch" aria-checked={state.prefs.speak} onClick={() => setPref("speak", !state.prefs.speak)} />
                  </div>
                  <div class="group-row">
                    <div class="text">
                      <b>Konuşma dili</b>
                      <small>Sesli giriş ve tarayıcı sesi bu dili kullanır.</small>
                    </div>
                    <select class="select" style={{ width: "160px" }} value={state.prefs.lang} onChange={(e) => setPref("lang", e.currentTarget.value)}>
                      <option value="tr-TR">Türkçe</option>
                      <option value="en-US">English</option>
                      <option value="de-DE">Deutsch</option>
                      <option value="fr-FR">Français</option>
                      <option value="es-ES">Español</option>
                    </select>
                  </div>
                </div>
                <div class="group">
                  <div class="group-row">
                    <div class="text">
                      <b>Uyandırma sözü</b>
                      <small>
                        Sekme açıkken "Hey {draft.name || "dot"}" ya da yalnızca "{draft.name || "dot"}" deyince dinlemeye başlar. Mikrofon sürekli açık kalır; ses bu
                        tarayıcının tanıma servisine gider.
                      </small>
                    </div>
                    <button class="switch" role="switch" aria-checked={state.prefs.wake} onClick={() => setPref("wake", !state.prefs.wake)} />
                  </div>
                  <div class="group-row">
                    <div class="text">
                      <b>Bildirim sesi</b>
                      <small>İş bitince, onay gerekince ya da bir hata olunca kısa bir ses çalar.</small>
                    </div>
                    <button class="switch" role="switch" aria-checked={state.prefs.sound} onClick={() => setPref("sound", !state.prefs.sound)} />
                  </div>
                  <div class="group-row">
                    <div class="text">
                      <b>Masaüstü bildirimi</b>
                      <small>Sekme arka plandayken bir dot işi bitirince ya da onay isteyince sistem bildirimi gösterir.</small>
                    </div>
                    <button class="switch" role="switch" aria-checked={state.prefs.notify} onClick={() => void enableNotifications(!state.prefs.notify)} />
                  </div>
                </div>
                <div class="section-label">ElevenLabs sesi</div>
                <VoicePicker
                  workspaceId={state.rt[props.dot.id]?.wsId ?? ""}
                  value={draft.voice_id}
                  onChange={(v) => {
                    setDraft("voice_id", v?.id ?? "")
                    setDraft("voice_name", v?.name ?? "")
                  }}
                />
              </Match>

              <Match when={pane() === "skills"}>
                <h3>Beceriler</h3>
                <p class="muted" style={{ "margin-bottom": "14px" }}>
                  Atlas'ın kurulu becerilerinden {draft.name || "dot"} için açtıkların, görev uygun düştüğünde kullanılır. Yeni beceri eklemek için becerinin
                  klasörünü Atlas'ın beceri dizinine koy; burada hemen görünür.
                </p>
                <input class="input" placeholder="Beceri ara" value={skillQuery()} onInput={(e) => setSkillQuery(e.currentTarget.value)} />
                <Show when={!skills.loading} fallback={<div class="empty"><span class="spinner" /></div>}>
                  <Show
                    when={(skills() ?? []).length}
                    fallback={
                      <div class="empty">
                        <b>Kurulu beceri yok</b>
                        Atlas bir beceri bulduğunda burada listelenir.
                      </div>
                    }
                  >
                    <div class="group" style={{ "margin-top": "12px" }}>
                      <For each={(skills() ?? []).filter((s) => (s.name + " " + s.description).toLocaleLowerCase("tr").includes(skillQuery().toLocaleLowerCase("tr")))}>
                        {(s) => {
                          const on = () => draft.skills.includes(s.name)
                          return (
                            <div class="group-row">
                              <div class="text">
                                <b>
                                  {s.name}
                                  <Show when={s.source}>
                                    <span class="faint skill-src"> · {s.source}</span>
                                  </Show>
                                </b>
                                <small>{s.description}</small>
                              </div>
                              <button
                                class="switch"
                                role="switch"
                                aria-checked={on()}
                                aria-label={s.name}
                                onClick={() => setDraft("skills", on() ? draft.skills.filter((x) => x !== s.name) : [...draft.skills, s.name])}
                              />
                            </div>
                          )
                        }}
                      </For>
                    </div>
                  </Show>
                </Show>
              </Match>

              <Match when={pane() === "permissions"}>
                <h3>İzinler</h3>
                <div class="group">
                  <div class="group-row">
                    <div class="text">
                      <b style={{ display: "flex", "align-items": "center", gap: "8px" }}>
                        Tam yetki
                        <Show when={fullAccessActive(live().full_access)}>
                          <span class="pill full">
                            <i />
                            <FullAccessLeft dot={live()} />
                          </span>
                        </Show>
                      </b>
                      <small>
                        Açıkken {live().name} onay beklemeden her şeyi yapar: şifreyle oturum açar, mesaj gönderir, paylaşım yapar, ayar değiştirir, ödeme yapar.
                        Kapalıyken bunlardan önce sana sorar.
                      </small>
                    </div>
                    <Show
                      when={fullAccessActive(live().full_access)}
                      fallback={
                        <button class="btn sm" onClick={() => setFullOpen(true)}>
                          Aç…
                        </button>
                      }
                    >
                      <button class="btn sm" onClick={() => revokeFullAccess(props.dot.id)}>
                        Kapat
                      </button>
                    </Show>
                  </div>
                </div>
                <RuleList
                  title="Her zaman sor"
                  hint="Bu durumlardan biri olursa dot durur ve sana sorar."
                  placeholder="ör. 500 TL üzeri her ödeme"
                  items={draft.always_ask}
                  onChange={(l) => setDraft("always_ask", l)}
                />
                <RuleList
                  title="Sormadan yap"
                  hint="Bu durumlarda dot onay beklemeden devam eder."
                  placeholder="ör. Kendi çalışma alanındaki dosyaları düzenlemek"
                  items={draft.auto_allow}
                  onChange={(l) => setDraft("auto_allow", l)}
                />
              </Match>

              <Match when={pane() === "workspace"}>
                <h3>Çalışma alanı</h3>
                <div class="group">
                  <div class="group-row">
                    <div class="text">
                      <b>Klasör</b>
                      <small class="mono">{props.dot.workspace}</small>
                    </div>
                    <button class="btn sm" onClick={() => navigator.clipboard?.writeText(props.dot.workspace).then(() => toast("Kopyalandı"))}>
                      Kopyala
                    </button>
                  </div>
                  <div class="group-row">
                    <div class="text">
                      <b>Kişilik dosyası</b>
                      <small>Atlas her mesajda klasördeki AGENTS.md dosyasını okur. Bu ayarlar kaydedilince dosya yeniden yazılır.</small>
                    </div>
                  </div>
                  <div class="group-row">
                    <div class="text">
                      <b>Oturum</b>
                      <small class="mono">{props.dot.session_id || "Henüz yok"}</small>
                    </div>
                  </div>
                </div>
                <Show when={state.rt[props.dot.id]?.wsId}>
                  {(id) => <Mailbox workspaceId={id()} />}
                </Show>
              </Match>
            </Switch>
          </div>
        </div>
        <div class="dialog-foot">
          <span class="faint" style={{ "font-size": "12.5px", "margin-right": "auto", "align-self": "center" }}>
            {dirty() ? "Kaydedilmemiş değişiklikler var" : ""}
          </span>
          <button class="btn ghost" onClick={props.onClose}>
            Kapat
          </button>
          <button class="btn primary" disabled={!dirty() || saving()} onClick={save}>
            {saving() ? "Kaydediliyor…" : "Kaydet"}
          </button>
        </div>
      </div>
      <Show when={fullOpen()}>
        <FullAccessDialog dot={live()} onClose={() => setFullOpen(false)} />
      </Show>
    </div>
  )
}

function RuleList(props: { title: string; hint: string; placeholder: string; items: string[]; onChange: (l: string[]) => void }) {
  const [text, setText] = createSignal("")
  const add = () => {
    const t = text().trim()
    if (!t) return
    props.onChange([...props.items, t])
    setText("")
  }
  return (
    <div style={{ "margin-bottom": "20px" }}>
      <div style={{ "font-weight": 500, "font-size": "13.5px" }}>{props.title}</div>
      <div class="faint" style={{ "font-size": "12.5px" }}>
        {props.hint}
      </div>
      <div class="rule-list">
        <For each={props.items}>
          {(r, i) => (
            <div class="rule">
              <span>{r}</span>
              <button class="icon-btn sm" aria-label="Kuralı sil" onClick={() => props.onChange(props.items.filter((_, j) => j !== i()))}>
                {Icon.close(12)}
              </button>
            </div>
          )}
        </For>
      </div>
      <div class="rule-add">
        <input class="input" maxLength={200} placeholder={props.placeholder} value={text()} onInput={(e) => setText(e.currentTarget.value)} onKeyDown={(e) => e.key === "Enter" && add()} />
        <button class="btn" disabled={!text().trim()} onClick={add}>
          Kural ekle
        </button>
      </div>
    </div>
  )
}

/** FullAccessLeft shows how long full access has left, updating itself. */
export function FullAccessLeft(props: { dot: Dot }) {
  const [now, setNow] = createSignal(Date.now())
  const t = setInterval(() => setNow(Date.now()), 15000)
  onCleanup(() => clearInterval(t))
  const label = () => {
    const f = props.dot.full_access
    if (f.permanent) return "Tam yetki · kalıcı"
    const left = Math.max(0, Date.parse(f.until ?? "") - now())
    const h = Math.floor(left / 3_600_000)
    const m = Math.floor(left / 60_000) % 60
    return h > 23 ? `Tam yetki · ${Math.ceil(h / 24)} gün` : `Tam yetki · ${String(h).padStart(2, "0")}:${String(m).padStart(2, "0")}`
  }
  return <>{label()}</>
}

/** FullAccessDialog is the warning the user must accept, and the dot's
 * name they must type, before a dot may act without asking. */
export function FullAccessDialog(props: { dot: Dot; onClose: () => void }) {
  const [minutes, setMinutes] = createSignal(1440)
  const [ack, setAck] = createSignal(false)
  const [typed, setTyped] = createSignal("")
  const [busy, setBusy] = createSignal(false)
  const ok = () => ack() && typed().trim().toLocaleLowerCase("tr") === props.dot.name.toLocaleLowerCase("tr")

  const confirm = async () => {
    if (!ok() || busy()) return
    setBusy(true)
    try {
      await grantFullAccess(props.dot.id, minutes(), minutes() === 0, typed().trim())
      toast(`${props.dot.name} için tam yetki açıldı`)
      props.onClose()
    } catch (err) {
      toast((err as Error).message, true)
    } finally {
      setBusy(false)
    }
  }

  return (
    <div class="scrim" style={{ "z-index": 50 }} onClick={(e) => e.target === e.currentTarget && props.onClose()}>
      <div class="dialog" role="alertdialog" aria-modal="true" aria-labelledby="fa-title" style={{ width: "min(480px, 100%)", "--accent": props.dot.color }}>
        <div class="dialog-body" style={{ "padding-top": "22px" }}>
          <div style={{ width: "38px", height: "38px", "border-radius": "11px", background: "color-mix(in oklab, var(--danger) 12%, transparent)", color: "var(--danger)", display: "grid", "place-items": "center", "margin-bottom": "14px" }}>
            {Icon.shield(18)}
          </div>
          <h2 id="fa-title" style={{ "font-size": "17px", "font-weight": 600, "letter-spacing": "-0.01em" }}>
            {props.dot.name}'e tam yetki ver
          </h2>
          <p class="muted" style={{ "margin-top": "6px" }}>
            Bu mod açıkken {props.dot.name} senden onay beklemeden aşağıdakilerin hepsini yapar. Yaptığı her şey etkinlik geçmişinde görünür.
          </p>
          <ul class="risk-list">
            <li>Kayıtlı şifrelerle sitelerde oturum açar</li>
            <li>Senin adına e-posta ve mesaj gönderir, paylaşım yapar</li>
            <li>Hesap ayarlarını değiştirir, dosya siler</li>
            <li>Kart limitleri içinde ödeme yapar</li>
            <li>Bilgisayarında komut çalıştırır</li>
          </ul>
          <div class="field" style={{ "margin-bottom": "0" }}>
            <span>Süre</span>
          </div>
          <div class="seg">
            <For each={[{ m: 60, l: "1 saat" }, { m: 1440, l: "24 saat" }, { m: 10080, l: "7 gün" }, { m: 0, l: "Kalıcı" }]}>
              {(o) => (
                <button aria-pressed={minutes() === o.m} onClick={() => setMinutes(o.m)}>
                  {o.l}
                </button>
              )}
            </For>
          </div>
          <label class="check">
            <input type="checkbox" checked={ack()} onChange={(e) => setAck(e.currentTarget.checked)} />
            Riskleri anladım. Modu istediğim an tek tıkla kapatabileceğimi biliyorum.
          </label>
          <label class="field">
            <span>
              Onaylamak için <b style={{ color: "var(--text)" }}>{props.dot.name}</b> yaz
            </span>
            <input class="input" autocomplete="off" spellcheck={false} value={typed()} onInput={(e) => setTyped(e.currentTarget.value)} onKeyDown={(e) => e.key === "Enter" && confirm()} />
          </label>
        </div>
        <div class="dialog-foot">
          <button class="btn ghost" onClick={props.onClose}>
            Vazgeç
          </button>
          <button class="btn danger" disabled={!ok() || busy()} onClick={confirm}>
            Tam yetkiyi aç
          </button>
        </div>
      </div>
    </div>
  )
}
