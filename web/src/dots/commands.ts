// Slash commands the terminal palette offers, wired to what Dots can
// actually do. A command either opens a surface, runs an action, or
// returns the prompt the composer should send.

import { createSignal } from "solid-js"
import { fullAccessActive } from "./api"
import { cancel, newConversation, retryLast, revokeFullAccess, setPref, state, summarize, toast } from "./store"
import { stopSpeaking } from "./voice"

export type SideTab = "screen" | "activity" | "auto" | "memory" | "files" | "summary"
export type SettingsPane = "general" | "persona" | "voice" | "skills" | "permissions" | "workspace"

export const [panelTab, setPanelTab] = createSignal<SideTab>("activity")
export const [panelOpenPulse, bumpPanelOpen] = createSignal(0)
export const [panelTogglePulse, bumpPanelToggle] = createSignal(0)
export const [settingsPane, setSettingsPane] = createSignal<SettingsPane>("general")
export const [settingsPulse, bumpSettings] = createSignal(0)
export const [searchPulse, bumpSearch] = createSignal(0)
export const [attachPulse, bumpAttach] = createSignal(0)
export const [modelPulse, bumpModel] = createSignal(0)

function openPanel(tab: SideTab) {
  setPanelTab(tab)
  bumpPanelOpen((n) => n + 1)
}

function openSettings(pane: SettingsPane) {
  setSettingsPane(pane)
  bumpSettings((n) => n + 1)
}

export interface LocalCommand {
  name: string
  aliases?: string[]
  description: string
  group: "Sohbet" | "Panel" | "Ayarlar" | "Terminal"
  needsArgs?: boolean
  /** A string is the prompt to send. Undefined means the command already ran. */
  run: (args: string) => void | string | Promise<void | string>
}

export const localCommands: LocalCommand[] = [
  { name: "new", aliases: ["yeni", "clear"], description: "Yeni bir sohbet başlat; hafıza ve dosyalar kalır", group: "Sohbet", run: () => void newConversation() },
  { name: "compact", aliases: ["ozetle"], description: "Uzayan sohbeti özetleyip yer aç", group: "Sohbet", run: () => void summarize() },
  { name: "tekrar", description: "Son görevi yeniden gönder", group: "Sohbet", run: () => void retryLast() },
  { name: "dur", description: "Çalışan işi durdur", group: "Sohbet", run: () => void cancel() },
  { name: "correct", description: "Çalışan işi durdur, düzeltmeyi yaz", group: "Sohbet", run: () => { void cancel(); toast("Düzeltmeyi yazıp gönder") } },
  { name: "fresh", description: "Takılı kalan işi durdurup yeni sohbet aç", group: "Sohbet", run: async () => { await cancel(); await newConversation() } },
  { name: "handoff", description: "Bu sohbetin devrini yazdır", group: "Sohbet", run: () => "Bu sohbetin devrini yaz: ne kararlaştı, ne kaldı, sıradaki adım ne. Yeni bir oturumun okuyacağı kadar kısa tut." },
  { name: "goal", description: "Bir hedef ver, bitene kadar kendi başına çalışsın", group: "Sohbet", needsArgs: true, run: (args) => `Bu hedefe ulaşana kadar kendi başına adım at: ${args}. Takıldığın yerde sor.` },
  { name: "btw", aliases: ["aside"], description: "Dosyalara dokunmadan kısa bir yan soru sor", group: "Sohbet", needsArgs: true, run: (args) => `Bunu dosyalara dokunmadan, kısa bir yan soru olarak yanıtla: ${args}` },
  { name: "review", description: "Kaydedilmemiş değişiklikleri gözden geçir", group: "Sohbet", run: (args) => `Bu çalışma alanındaki değişiklikleri gözden geçir${args ? `: ${args}` : ""}. Ne değiştiğini ve riskli görüneni söyle.` },
  { name: "init", description: "Bu proje için bir AGENTS.md yaz", group: "Sohbet", run: () => "Bu proje için bir AGENTS.md yaz. Önce mevcut dosyalara bak, sonra kısa ve uygulanabilir kurallar koy." },
  { name: "spec", description: "İstek, tasarım ve görevleri gözden geçir", group: "Sohbet", run: (args) => `İstekleri, tasarımı ve görevleri gözden geçir${args ? `: ${args}` : ""}. Eksik olanı söyle.` },
  { name: "search", aliases: ["find", "sessions", "history"], description: "Sohbetlerde ve dot'larda ara", group: "Sohbet", run: () => { bumpSearch((n) => n + 1) } },
  { name: "help", description: "Kısayolları göster", group: "Sohbet", run: () => toast("Enter gönderir. / komut açar. @ dosya ekler. Esc durdurur. Space basılı tutunca dinler.") },
  {
    name: "sessiz",
    description: "Sesli cevapları aç veya kapat",
    group: "Sohbet",
    run: () => {
      if (state.prefs.speak) stopSpeaking()
      setPref("speak", !state.prefs.speak)
      toast(state.prefs.speak ? "Sesli cevaplar açık" : "Sesli cevaplar kapalı")
    },
  },

  { name: "sidebar", description: "Yan paneli göster veya gizle", group: "Panel", run: () => { bumpPanelToggle((n) => n + 1) } },
  { name: "files", description: "Bu oturumda değişen dosyalar", group: "Panel", run: () => openPanel("files") },
  { name: "usage", aliases: ["context"], description: "Harcanan token ve oturum özeti", group: "Panel", run: () => openPanel("summary") },
  { name: "jobs", aliases: ["work"], description: "Şu ana kadarki adımlar", group: "Panel", run: () => openPanel("activity") },
  { name: "automation", aliases: ["workflow", "board"], description: "Zamanlanmış işler", group: "Panel", run: () => openPanel("auto") },
  { name: "interactions", aliases: ["resume-use"], description: "Ekran ve tarayıcı", group: "Panel", run: () => openPanel("screen") },
  { name: "source-memory", description: "Dot'un hafızası", group: "Panel", run: () => openPanel("memory") },
  { name: "attach", description: "Mesaja dosya ekle", group: "Panel", run: () => { bumpAttach((n) => n + 1) } },

  { name: "permissions", aliases: ["tools", "mode", "computer-use"], description: "İzinler ve onaysız çalışma", group: "Ayarlar", run: () => openSettings("permissions") },
  {
    name: "yolo",
    description: "Onaysız çalışmayı aç veya kapat",
    group: "Ayarlar",
    run: () => {
      const dot = state.dots.find((d) => d.id === state.current)
      if (!dot) return
      if (fullAccessActive(dot.full_access)) {
        void revokeFullAccess(dot.id)
        return
      }
      openSettings("permissions")
      toast("Onaysız çalışma izinler sayfasından, adını yazarak açılır")
    },
  },
  { name: "notifications", description: "Ses ve uyarılar", group: "Ayarlar", run: () => openSettings("voice") },
  { name: "subagents", description: "Dot'un becerileri", group: "Ayarlar", run: () => openSettings("skills") },
  { name: "language", description: "Dot'un adı ve görünümü", group: "Ayarlar", run: () => openSettings("general") },

  { name: "rewind", description: "Bir mesajı düzenleyip o noktadan yeniden başlat", group: "Terminal", run: () => toast("Üst oka bas, mesajı düzelt, gönder. Sohbet o noktadan yeniden başlar.") },
  { name: "back", description: "Önceki oturuma dön", group: "Terminal", run: () => toast("Dots'ta her dot'un kendi sohbeti var. Soldan diğer dot'a geç.") },
  { name: "model", aliases: ["effort", "thinking", "fast", "quality"], description: "Bu dot'un modelini ve düşünme düzeyini seç", group: "Ayarlar", run: () => { bumpModel((n) => n + 1) } },
  { name: "style", description: "Cevap üslubu", group: "Ayarlar", run: () => openSettings("persona") },
  { name: "profiles", aliases: ["roles", "fallbacks"], description: "Model rolleri bu dot'un modeli üzerinden gider", group: "Ayarlar", run: () => { bumpModel((n) => n + 1) } },
  { name: "auto-compact", aliases: ["auto-compact-at"], description: "Otomatik özetleme", group: "Terminal", run: () => toast("Otomatik özet terminal ayarında. Burada /compact ile elle özetlenir.") },
  { name: "editor", description: "İstemi dış editörde yaz", group: "Terminal", run: () => toast("Bu tarayıcıda istek kutusuna yazılır. /editor terminalde $EDITOR açar.") },
  { name: "docker-mcp", description: "Docker MCP kataloğu", group: "Terminal", run: () => toast("Docker MCP kataloğu terminaldeki /docker-mcp ile açılır.") },
  { name: "pills", description: "Yapılacaklar şeridi", group: "Terminal", run: () => openPanel("activity") },
  { name: "mark", description: "Ekranda bir bölge işaretle", group: "Terminal", run: () => toast("Ekran işaretleme terminalde. Burada dosyayı ata ya da ekran sekmesine bak.") },
  { name: "remote-control", aliases: ["rc"], description: "Uzaktan kontrol", group: "Terminal", run: () => toast("Uzaktan kontrol terminaldeki /remote-control ile kurulur.") },
  { name: "integrations", description: "Bağlı araçlar", group: "Terminal", run: () => toast("Bağlı araçlar terminaldeki /integrations ekranında.") },
  { name: "app", aliases: ["apps"], description: "Bu bilgisayardaki uygulamalar", group: "Terminal", run: () => toast("Uygulama listesi terminaldeki /app ile açılır.") },
  { name: "support", description: "Başka bir bilgisayara destek", group: "Terminal", run: () => toast("Uzaktan destek terminaldeki /support ile, tek kullanımlık kodla açılır.") },
  { name: "mobile", description: "Telefon eşleştir", group: "Terminal", run: () => toast("Telefon eşleştirme terminaldeki /mobile ile açılır.") },
  { name: "agents", description: "Çalışan ajanlar", group: "Terminal", run: () => openPanel("activity") },
  { name: "batches", description: "Toplu iş sonuçları", group: "Terminal", run: () => toast("Toplu iş dökümü terminaldeki /batches ekranında.") },
  { name: "snippets", description: "Kayıtlı istem parçaları", group: "Terminal", run: () => toast("Kayıtlı parçalar terminaldeki /snippets ile eklenir.") },
  { name: "transparent", description: "Saydam arka plan", group: "Terminal", run: () => toast("Saydam arka plan terminalin kendi rengi içindir.") },
  { name: "quit", aliases: ["exit"], description: "Atlas'tan çık", group: "Terminal", run: () => toast("Sekmeyi kapatmak sunucuyu durdurmaz. Terminalde Ctrl+C durdurur.") },
]

export function findCommand(name: string): LocalCommand | undefined {
  const n = name.toLocaleLowerCase("tr")
  return localCommands.find((c) => c.name === n || c.aliases?.includes(n))
}
