# ATLAS-AGENT App: Yapım Planı (v1, 2026-11-04)

Bu belge `DESIGN-ROUTE.md` (§1-7: tasarım temeli) ve `atlas-app-tokens.css`
ile birlikte okunur. Sahip kararları (nihai):

- App, web UI'dan **tamamen ayrı bir üründür**. Yeni frontend sıfırdan yazılır.
  `web/` ve `web/vendor/*` kullanılmaz, kopyalanmaz, fork'lanmaz; `web/` ve
  `atlas-agent web` değiştirilmez. Paylaşılabilen tek şey Go backend'dir
  (server, session'lar, araçlar, `internal/webui` gateway API'si).
- Electron, komut `atlas-agent app`, Ember turuncu yalnız marka anlarında,
  yalnız sessiz metin araç satırları (Cursor/OpenCode tarzı), web'deki
  wordmark korunur, Cursor seviyesinde cila, Cursor tarzı **proje başına bir
  pencere**. Maskot/persona modu ve Dots ertelendi, planlanmadı.

Kanıt notu: Go tarafı bu oturumda `internal/webui/{api,gateway,auth,hub,
assets,control}.go`, `internal/server/server.go` ve `internal/cmd/{app,web}.go`
okunarak çıkarıldı (dosya:satır referansları aşağıda). Paket sürümleri
`npm view ... time` ile doğrulandı; 2026-10-21 ve öncesinde yayınlanmış olanlar
seçildi (bugün 2026-11-04, yani ≥ 2 hafta).

---

## 1. Mimari

### 1.1 Süreç modeli (tek bakışta)

```
atlas-agent app [yol]            (CLI, kısa ömürlü başlatıcı, `code .` gibi)
   └─ Electron'u (veya kurulu Atlas.exe'yi) --project=<yol> ile başlatır, çıkar
        ▲ ikinci çağrı: single-instance kilidi argv'yi mevcut sürece iletir
Electron main  (tek süreç, uygulamanın sahibi)
   ├─ child: atlas-agent app serve --renderer <dir> --port 0   (gateway süpervizörü)
   │     stdout: {"type":"ready","port":53122,"link":"http://127.0.0.1:53122/auth/launch?code=…"}
   │     stdin kapanırsa temiz kapanır (Electron ölürse Go da ölür)
   │     ├─ internal/webui gateway: statik renderer + /api/* + SSE
   │     └─ Atlas server (ensureServer: ortak, oturum/ajan/araçlar burada)
   ├─ BrowserWindow(proje A)  ──http──▶ 127.0.0.1:53122  (cookie + CSRF, bugünkü model)
   ├─ BrowserWindow(proje B)  ──http──▶ aynı gateway, ayrı workspace/hub
   └─ preload → window.atlas  (dar IPC: pencere, diyalog, bildirim, tema, git)
```

Neden böyle:

- **Gateway ömrü = Electron main ömrü.** Bugünkü `atlas-agent app` (CLI
  süreci gateway'i tutup pencerenin kapanmasını bekliyor) çoklu pencereyle
  çakışıyor: ikinci proje penceresi ilk sürecin gateway'ine bağlı kalmak
  zorunda. Gateway'i Electron'un çocuğu yapınca pencere sayısından bağımsız,
  en son pencere kapanınca (`window-all-closed`) hepsi iner.
- **Renderer, gateway ile aynı origin'den yüklenir** (gateway statik dosyayı
  kendi servis eder). Böylece mevcut güvenlik modeli aynen çalışır: HttpOnly
  cookie, CSRF header, Host/Origin kontrolü, CSP (`internal/webui/api.go:101-126`,
  `auth.go:133-191`). Özel protokol + token proxy alternatifi daha fazla kod ve
  yeni saldırı yüzeyi demek; reddedildi.
- **Port 0** (boş port): sabit port çakışması ve "port kullanımda" hataları yok.
  Origin her açılışta değiştiği için `localStorage` kalıcı sayılmaz; kalıcı
  tercihler main-process dosyasına yazılır (§1.5).
- **Başlatıcı ayrı, gateway ayrı:** `atlas-agent app` yalnız bulur-başlatır
  (Windows'ta `DETACHED_PROCESS`); Ctrl+C/terminal kapanması uygulamayı
  öldürmez. `atlas-agent app serve` gizli alt komuttur, yalnız Electron
  çağırır.

### 1.2 Frontend yığını (sıfırdan, bağımlılık kararları)

| Katman | Seçim | Sürüm (≥ 2 hafta eski) | Gerekçe |
|---|---|---|---|
| Çerçeve | **React** + `react-dom` | 19.1.10 (2026-10-14) | Radix/test/virtualization ekosistemi ve Electron + Playwright olgunluğu; ekip dili. Streaming hassasiyeti blok-bazlı memo ve harici store seçicileriyle çözülür (§1.4), Solid'in ince taneli avantajına ihtiyaç bırakmaz. `@types/react` 19.3.0, `@types/react-dom` 19.3.0 |
| Dil | **TypeScript** | 5.9.3 | TS 7 (native) çok yeni; Vite/Vitest araç zinciri 5.9'da oturmuş |
| Bundler/dev | **Vite** + `@vitejs/plugin-react` | 7.3.7 / 6.1.2 | HMR, hızlı build; çıktı `app/renderer/dist` |
| State | **Zustand** | 5.0.15 | Küçük, harici store; `useSyncExternalStore` ile seçici bazlı abonelik. Redux/Query katmanı eklenmez: veri zaten SSE ile itilen entity'ler + az sayıda GET |
| Stil | **Düz CSS** (CSS değişkenleri + `@layer`) + CSS Modules | (araç içinde) | `atlas-app-tokens.css` ve hareket kataloğu zaten `data-*`/değişken tabanlı; Tailwind ihtiyaç yok, runtime CSS-in-JS yok |
| Primitifler | **Radix** (`radix-ui` birleşik paket) | 1.8.1 (2026-10-12) | Dialog/Popover/Menu/Tooltip/Select erişilebilirliği hazır; görünüm bizim CSS |
| Sanal liste | **@tanstack/react-virtual** | 3.14.21 (2026-10-16) | Dinamik yükseklik; sohbette alt-sabitleme (anchor-bottom) mantığını biz yazarız (§1.4) |
| Markdown | **marked** (yalnız `lexer`) + **react-markdown** + **remark-gfm** | 18.1.1 / 10.1.0 / 4.0.1 | `marked.lexer` üst düzey blokları ayırır (tamamlanan blok memo'lanır, yalnız son blok yeniden render); `react-markdown` ham HTML basmaz (XSS yüzeyi küçük, `dompurify` gerekmez) |
| Eksik markdown onarımı | **remend** | 1.4.0 | Akış sırasında kapanmamış `**`, ``` ``` ``` vb. toparlar |
| Kod renklendirme | **shiki** (`shiki/core` + JS regex motoru, dile göre lazy yükleme) | 4.5.0 (2026-10-01) | Yalnız **tamamlanan** kod bloklarını renklendir; akan blok düz mono |
| Diff | **diff** (jsdiff) hesap için + kendi unified görünümümüz; ihtiyaç olursa `@pierre/diffs` | 8.0.4 / 1.6.0 | Önce küçük ve bizim kontrolümüzde (araç satırı içi diff 12-40 satır); inceleme rayında büyük diff için `@pierre/diffs` React girişi Faz 5 başında spike ile doğrulanır |
| Terminal | **@xterm/xterm** + `addon-fit` (+ ops. `addon-webgl`) | 6.0.0 / 0.11.0 / 0.19.0 (2025-12-22) | Yalnız Faz 5 "Terminal" rayı/job çıktısı için; v1 PTY yok |
| Fuzzy arama | **fuzzysort** | 3.1.0 | Palet ve `@` listesi |
| İkon | Kendi SVG bileşenlerimiz (16 px, 1.5 px stroke); ilk 40 yaygın ikon için `lucide-react` 1.60.0 kaynak olarak (ISC) | 1.60.0 (2026-10-20) | Atlas'a özgü ikonlar (marka, düşünme, compaction vb.) elle çizilir; tree-shake edilmiş tekil import |
| i18n | Kendi tip-güvenli `t()` (TR + EN, ~60 satır) | (bağımlılık yok) | Kütüphane gerekmiyor; anahtar eksikse EN'e düşer |
| Lint/format | **Biome** | 2.5.15 | Tek araç, hızlı |
| Test | **Vitest** 3.2.7, **@testing-library/react** 16.3.3, **user-event** 14.6.8, **happy-dom** 21.0.4 (jsdom 26.1.0 yedek) | | Birim/bileşen testleri |
| E2E | **@playwright/test** (Electron `_electron` API) | 1.64.1 (2026-10-14) | Gerçek pencere + gerçek Go gateway |
| Node tipleri | `@types/node` | 22.20.5 | Electron 45 Node 24 taşıyor; tipler 22 (main kodu basit) |

Electron ve paketleme:

| Parça | Seçim | Sürüm | Not |
|---|---|---|---|
| Electron | **electron** (pin, `^` yok) | 45.2.2 (halihazırda `app/node_modules`'ta) | **Bilinçli istisna:** tarayıcı motoru güvenlik yamaları 2 hafta kuralından önemli. Kural-uyumlu en yeni 45.x yok (45.0.0 = 2026-10-21, tam 14 gün); sıkı uyum istenirse 44.9.3 (2026-10-21) |
| Paketleyici | **electron-builder** | 26.18.0 (2026-10-20) | NSIS hedefi, `extraResources`, fuse ayarı |
| Otomatik güncelleme | **electron-updater** | 6.8.11 (2026-10-20) | Faz 7, opsiyonel |
| Fuses | electron-builder `electronFuses` (altta `@electron/fuses` 2.1.3) | | §1.6 |

Reddedilenler (tek satır): Tailwind (token'lı düz CSS yeterli), TanStack Query
(SSE + zustand yeterli), CodeMirror/Lexical/TipTap (v1 composer `textarea`,
chip'li editör Faz 5'te ihtiyaç doğarsa), Redux, framer-motion (hareket CSS
öncelikli; FLIP gerekirse küçük bir yardımcı).

### 1.3 Klasör yapısı (`D:\Atlas\app`)

```
app/
  package.json            (name: "atlas-app" KALIR: Go `isAtlasAppDir` buna bakıyor)
  electron-builder.yml
  main/                   (düz CJS, bundler yok, `// @ts-check` + `tsc --checkJs`)
    index.cjs             (app yaşam döngüsü, single-instance, gateway child)
    gateway.cjs           (spawn atlas-agent app serve, handshake, yeniden başlatma)
    windows.cjs           (proje→pencere kaydı, bounds kalıcılığı, titleBarOverlay)
    ipc.cjs               (kanal tanımları + gönderen doğrulama)
    security.cjs          (navigasyon/izin/istek kısıtları, CSP kontrolü)
    prefs.cjs             (userData/prefs.json: tema, genişlikler, son pencereler)
    menu.cjs              (Windows/Linux: Alt ile görünen menü; macOS sistem menüsü)
    paths.cjs             (dev/packaged yol çözümü: resources/bin, resources/renderer)
  preload/
    index.cjs             (contextBridge → window.atlas; sandbox uyumlu CJS)
  renderer/               (Vite kökü; saf web kodu, Node API yok)
    index.html
    public/boot.js        (ilk boyama öncesi data-color-scheme/data-motion; inline script CSP'ye takılır, o yüzden harici dosya)
    vite.config.ts  tsconfig.json
    src/
      main.tsx  app.tsx
      design/             (tokens.css = atlas-app-tokens.css, motion.css, base.css, icons/)
      primitives/         (Button, IconButton, Tooltip, Dialog, ... §3.1)
      components/         (Markdown, CodeBlock, DiffView, ToolRow, ... §3.1)
      features/           (chrome, sidebar, thread, composer, permissions, rail, settings, palette, onboarding)
      stores/             (workspace, sessions, messages, pending, models, ui, prefs)
      api/                (http.ts, sse.ts, endpoints.ts, types.ts, fixtures/)
      lib/                (i18n, shortcuts, format, scroll-anchor, markdown-blocks)
    dist/                 (build çıktısı; git'e girmez)
  build/                  (ikon: .ico/.icns/.png, NSIS görselleri, entitlements)
  scripts/                (dev.mjs, stage-go.mjs, gen-fixtures.mjs)
```

Yasaklar (CI'da `rg` ile denetlenir): `renderer/` içinde `web/` yolu veya
`@atlas/ui`, `@atlas/session-ui` import'u; `renderer/` içinde `require(`,
`process.`, `electron` import'u (yalnız `window.atlas`).

### 1.4 Renderer iç mimarisi (performans kritik kısımlar)

- **Veri akışı:** `api/sse.ts` fetch-stream tabanlı SSE istemcisi (EventSource
  değil: `Last-Event-ID`/`instance` parametresini kendimiz yönetiriz,
  401'de ana sürece "gateway yeniden başlat" isteği atarız). Olaylar tek bir
  `reducer.ts` içinden zustand store'larına girer. `hello.resync` veya
  instance değişimi → snapshot yeniden yükleme (bugünkü gateway sözleşmesi).
- **Akış yazma:** gateway `message` olaylarında **tüm mesajı** gönderiyor
  (idempotent upsert). Token başına render yapılmaz: olaylar `requestAnimationFrame`
  ile birleştirilir (en fazla kare başına bir store güncellemesi).
- **Thread render:** tur (turn) bazlı sanal liste; her tur `React.memo`. Bir
  turun içinde markdown blokları `marked.lexer` ile ayrılır, tamamlanan bloklar
  referans eşitliğiyle memo'lanır; yalnız son blok akar. Hedef: 200 tur,
  4× CPU throttle'da ≥ 50 fps kaydırma.
- **Alt-sabitleme:** kullanıcı en alttaysa yeni içerik görünümü alta çeker;
  kullanıcı yukarı kaydırdıysa "aşağı kaydır" butonu çıkar ve akış görünümü
  oynatmaz (`lib/scroll-anchor.ts`, ResizeObserver tabanlı; sanal liste yüksekliği
  ölçümleri bu mantıkla uyumlu).
- **Reduced motion/tema:** `boot.js` `data-color-scheme` ve `data-motion`'ı
  ilk boyamadan önce yazar (main'in `nativeTheme` + prefs sonucunu
  `window.atlas.prefsSync` ile okur; yoksa `matchMedia`).
- **Hata sınırları:** her özellik alanı (thread, rail, sidebar) kendi
  `ErrorBoundary`'sine sahip; çökerse alan "Yeniden yükle" kartı gösterir,
  uygulama çökmez.

### 1.5 Electron main / preload / renderer ayrımı ve IPC yüzeyi

Kural: renderer **hiçbir** Node/Electron API'si görmez; ağ işleri (gateway)
doğrudan HTTP ile yapılır, IPC yalnız masaüstüne özgü işler içindir.

`window.atlas` (preload, her çağrı main'de gönderen çerçeve URL'si gateway
origin'ine eşit mi diye doğrulanır, şema/yol/uzunluk kontrolü yapılır):

| Grup | Çağrı | Main'de yaptığı | Faz |
|---|---|---|---|
| app | `app.info()` | `{version, platform, packaged, gatewayVersion}` | 0 |
| chrome | `chrome.setOverlay({color, symbolColor})`, `chrome.onMaximizeChange(cb)` | `win.setTitleBarOverlay` (Windows native kontroller 46×40 kalır, renkler tema ile eşlenir) | 1 |
| windows | `windows.openProject(path)`, `windows.pickFolder()`, `windows.recent()`, `windows.closeSelf()` | `dialog.showOpenDialog`, pencere kaydı (`Map<proje, BrowserWindow>`; varsa odakla), bounds `prefs.json`'a | 1 |
| prefs | `prefs.get(key)`, `prefs.set(key, value)`, `prefs.getSync(key)` (yalnız boot) | `userData/prefs.json` (şema doğrulamalı) | 1 |
| theme | `theme.set("system"\|"light"\|"dark")`, `theme.onChange(cb)` | `nativeTheme.themeSource` | 1 |
| notify | `notify({title, body, sessionId})`, `notify.onClick(cb)` | `new Notification`; tıklama → pencere odak + renderer'a olay | 6 |
| badge | `badge.set(n)` | Windows: overlay ikon; mac/linux: `app.setBadgeCount` | 6 |
| shell | `shell.reveal(path)`, `shell.openExternal(url)` | `showItemInFolder`; yalnız `https:`/`http:`/`mailto:` ve proje yolu doğrulaması | 2 |
| git | `git.status(path)` → `{branch, dirty, ahead, behind}` | `execFile("git", [...])` (zaman aşımı 3 sn, yalnız okuma komutları) | 3 |
| ide | `ide.open(path, line?)` | `code`/`cursor` varsa `--goto`, yoksa `shell.openPath` | 5 |
| favicon | `favicon.get(host)` → data URL | main'de `net.fetch` + disk önbelleği, yalnız https, özel IP reddi | 5 |
| updater | `updater.check()`, `updater.onState(cb)` | electron-updater | 7 |

Pencere kaydı ve `second-instance`: `argv`'den `--project=<yol>` ayrıştırılır;
yol zaten açıksa o pencere odaklanır, değilse yeni pencere
`http://127.0.0.1:<port>/?project=<kodlanmış yol>` ile açılır. Renderer açılışta
`POST /api/workspaces {path}` çağırır (gateway workspace'i açar/yeniden kullanır).
Pencere kapanırken `pagehide` içinde `fetch(..., {keepalive:true, method:"DELETE"})`
ile `/api/workspaces/{id}` çağrılır (gateway'in workspace sahipliği bırakılır,
`api.go:367` `closeWorkspace`; çalışan koşular server'da devam eder).

### 1.6 Güvenlik

- `BrowserWindow`: `contextIsolation: true`, `sandbox: true`,
  `nodeIntegration: false`, `webSecurity` varsayılan (açık), `webviewTag: false`,
  `spellcheck: false`, `partition: "atlas-app"` (kalıcı olmayan; cookie diske
  yazılmaz, gateway her açılışta yeni instance).
- **CSP** gateway'den gelir (`api.go:119-126`, `script-src 'self'`, inline script
  yok → `boot.js` harici). App için Go'da küçük bir farklılık: `media-src`'den
  `storage.googleapis.com` çıkarılır (§2.3, G3). Uzak resimler (markdown) CSP
  ile zaten engelli; link olarak gösterilir.
- **Ağ kısıtı (derinlik savunması):** main'de `session.webRequest.onBeforeRequest`
  gateway origin'i dışındaki her isteği iptal eder; `setPermissionRequestHandler`
  yalnız `clipboard-sanitized-write` ve `notifications` verir, gerisini reddeder.
- **Navigasyon:** `will-navigate`/`setWindowOpenHandler` mevcut kodda doğru
  (`main.js:64-73`): origin dışı → varsayılan tarayıcı (yalnız http/https/mailto).
  `will-attach-webview` her zaman iptal.
- **Launch link:** argv yerine **ortam değişkeni/handshake** ile taşınır
  (argv diğer yerel süreçlerce okunabilir). Link tek kullanımlık, 10 dk ömürlü
  (`auth.go`); ilk pencere `loadURL(link)` ile cookie'yi alır, gateway
  `?next=/?project=…` ile yönlendirir (`auth.go:211-215` yalnız `/` ile
  başlayan `next` kabul eder).
- **Paket:** `asar` açık, fuse'lar: `RunAsNode=off`,
  `EnableNodeOptionsEnvironmentVariable=off`, `EnableNodeCliInspectArguments=off`,
  `OnlyLoadAppFromAsar=on`, `EnableEmbeddedAsarIntegrityValidation=on`.
  DevTools yalnız `ATLAS_APP_DEVTOOLS=1` ile.
- **Uzak içerik yok:** renderer hiçbir CDN/font/analitik çağırmaz; yazı tipleri
  paketlenir (Inter + JetBrains Mono, `renderer/public/fonts`).
- **Telemetri:** App v1'de yok (CLI'nın PostHog'u ayrı; renderer'a taşınmaz).

---

## 2. Backend sözleşmesi

### 2.1 Gateway uçları, App'in kullanacakları (kaynak: `internal/webui/api.go:38-96`)

Auth: `GET /auth/launch?code=&next=` (cookie `atlas_web_<port>`, HttpOnly,
SameSite=Strict) → tüm `/api/*` için cookie; durum değiştiren isteklerde
`X-CSRF` header (token `GET /api/bootstrap`'tan) + `Origin` = gateway.

| Alan | Uç | App kullanımı | Faz |
|---|---|---|---|
| Açılış | `GET /api/bootstrap` | csrf, instance, sunucu erişilebilirliği, açık workspace'ler | 0 |
| | `POST /api/auth/signout` | (nadiren) | 6 |
| Projeler | `GET /api/projects` | son projeler (ana ekran, "Open Recent") | 1 |
| Workspace | `POST /api/workspaces {path}`, `GET /api/workspaces/{id}`, `DELETE /api/workspaces/{id}` | pencere ↔ proje | 0 |
| **Akış** | `GET /api/workspaces/{id}/events?after=&instance=` (SSE) | tek canlı kanal | 0 |
| | `GET /api/workspaces/{id}/status` | snapshot (`busy/queued` + seq) | 2 |
| Oturumlar | `GET/POST /sessions`, `GET/PATCH/DELETE /sessions/{sid}`, `GET /sessions?q=` | liste, yeni, yeniden adlandır, sil, ara | 2-3 |
| Mesajlar | `GET /sessions/{sid}/messages`, `/history`, `/user-messages` | thread snapshot, geri alma noktaları | 2 |
| Prompt | `POST /sessions/{sid}/prompt {prompt, run_id(uuid), follow_up?, attachments[]}` | gönder (idempotent `run_id`; meşgulken `follow_up`) | 2 |
| | `POST /sessions/{sid}/cancel`, `GET/DELETE /sessions/{sid}/queue` | durdur, kuyruk | 2 |
| | `POST /sessions/{sid}/shell {command}`, `/summarize`, `/goal`, `/rewind[/preview]` | `!` komutu, compaction, hedef, geri al | 2-5 |
| İzin/soru | `GET /pending`, `POST /permissions/{pid} {action: allow\|allow_session\|deny}`, `POST /questions/{qid}/answer`, `/cancel` | dock'lar | 2 |
| | `POST /permission-mode`, `POST /session-mode` | mod çipleri | 4 |
| Model | `GET/POST /models`, `GET /settings` | model seçici, ayarlar özeti (mod listesi, MCP durumu) | 4 |
| Bağlantı | `GET/POST /connections[/{c}[/test\|/oauth]]` | sağlayıcı anahtarı/OAuth | 4 |
| Dosya | `GET /files?path=`, `GET /files/content?path=&max_bytes=`, `GET /sessions/{sid}/files` | dosya ağacı, görüntüleyici, "değişen dosyalar" | 5 |
| İş | `GET /jobs`, `GET /jobs/{jid}/output`, `DELETE /jobs/{jid}` | arka plan komutları | 5 |
| Komut | `GET /commands`, `POST /commands/expand` | `/` slash komutları | 5 |
| Diğer | `GET /sessions/{sid}/agents`, `/workflow`, `/subagents` | alt ajan satırı | 5 |

SSE sözleşmesi (`hub.go:9-30`, `api.go:381-452`): ilk olay `hello`
(`{instance, seq, workspace_id, resync}`), sonra numaralı olaylar
(`id: <seq>`), 15 sn `: keep-alive`. Sentetik tipler: `hello`, `resync`,
`session_status {session_id, busy, queued}`, `connection`,
`workspace_replaced`. Sunucudan geçenler: `message`, `session`, `run_complete`,
`agent_event`, `permission_request`, `permission_notification`,
`question_batch_request`, `question_batch_notification`, `file`, `job_event`,
`config_changed`, `mcp_event`, `skills_event`, `lsp_event` (kaynak:
`web/src/state/workspace.ts` yalnız **olay adlarını doğrulamak için okundu**;
renderer'a hiçbir şey taşınmaz). Pubsub olayları `{type:"created"|"updated"|
"deleted", payload}` zarfında gelir. Halka tamponu 4096 olay; geride kalan
abone atılır ve `Last-Event-ID` ile devam eder.

Veri tipleri `internal/proto` kaynaklıdır (mesaj parçaları: `text`,
`reasoning`, `image_url`, `binary`, `tool_call`, `tool_result`, `finish`,
`shell_command`, `message.go:509-516`). Renderer tipleri `api/types.ts`'te
**Go kaynağından elle** yazılır; `scripts/gen-fixtures.mjs` Go test
harness'inden (`internal/webui/harness_test.go`) gerçek JSON örnekleri kaydeder,
Vitest bu fikstürlerle tipleri/reducer'ı doğrular (sözleşme kayması CI'da kırılır).

### 2.2 Eksikler ve App için çözüm

| # | Eksik | Önerilen çözüm | Go değişikliği | Faz |
|---|---|---|---|---|
| E1 | Gateway statik dosyayı gömülü web build'inden servis ediyor; `ErrAssetsMissing` ile başlamıyor (`gateway.go:122-140`) | `Options.Assets` zaten var: App `os.DirFS(<renderer dist>)` verir | **Yok** (yalnız `app serve`te kullanım) | 0 |
| E2 | `@` dosya araması: `GET /files` yalnız dizin listeler | Yeni uç `GET /api/workspaces/{id}/files/search?q=&limit=` (gateway'de yürüyüş + `.gitignore` uyumu) **veya** istemci tarafı tam ağaç önbelleği | **Küçük** (yeni uç, `webui` içinde yeni dosya `search.go`) | 5 |
| E3 | İzin kapsamı: backend yalnız `allow`/`allow_session`/`deny` biliyor; "bu proje/komut deseni" yok | v1'de bir kez / bu oturum / reddet. Proje/desen kapsamı server tarafı iş (`permission` paketi) | Server (ertelenebilir) | 4+ |
| E4 | Oturum sabitleme/arşiv/çatallama (fork) yok | v1: sabitleme/arşiv istemci tarafı (`prefs.json`, proje başına); fork yok (tur meta satırında gösterilmez) | Yok | 3 |
| E5 | Git dalı/durumu | Main-process `git.status` IPC (Go'ya dokunmaz) | Yok | 3 |
| E6 | Favicon: CSP uzak görseli engeller | Main-process `favicon.get` (önbellekli data URL) | Yok | 5 |
| E7 | Etkileşimli terminal (PTY) yok | v1: yalnız `shell` komutu çıktısı + `jobs` (salt-okunur xterm). PTY ayrı proje | (sonra) | 5+ |
| E8 | Ayar yazma: hook'lar, izin allow-list'leri, MCP/skill düzenleme gateway'de yok (`config/set` server'da var, gateway'e açık değil) | Faz 4'te yalnız okuma + sağlayıcı/model/mod yazma; düzenleme uçları ihtiyaç anında gateway'e eklenir (`relayPost` tek satır) | Küçük, ayrı ayrı | 4+ |
| E9 | Hata sayfası/mesajlar "run `atlas-agent web`" diyor (`auth.go:177`, `expiredLinkPage`) | `Options.Product` ("web"/"app") ile metin ayrımı (kozmetik) | Çok küçük, opsiyonel | 6 |
| E10 | Tarayıcı/ekran önizleme rayı için canlı kare akışı yok | v1 kapsam dışı; web/bilgisayar araçları yalnız satır + sonuç gösterir | (sonra) | - |

### 2.3 `atlas-agent app` için minimal Go değişiklikleri (web'den izole)

Hedef: `internal/cmd/web.go` **hiç** App'e özgü satır içermez; App kendi
başlangıç yolunu kullanır.

**G1. `internal/cmd/app.go` yeniden yazılır (yeni, kendi başına):**
- `appCmd` (başlatıcı): `atlas-agent app [yol]` → yolu çöz (varsayılan cwd),
  kabuğu bul (`findDesktopShell`: `ATLAS_APP_DIR`, exe yanı `app/`, **kurulu
  `Atlas.exe`**, kaynak ağacı), `--project=<yol>` ile **ayrık** başlat, hemen
  çık. `--browser` bayrağı: Electron yoksa/istenirse gateway'i ön planda
  çalıştırıp (aşağıdaki `serve` mantığı) tarayıcıda açar (dev/headless için).
- `appServeCmd` (gizli, `app serve`): `--renderer <dizin>` (zorunlu),
  `--port` (varsayılan 0), `--dev-origin` (gizli). Akış: `ensureServer` →
  `client.NewClient` → `webui.New(Options{Upstream, ServerAddress, Version,
  Env, RecentProjects: recentWebProjects, Reconnect, Assets: os.DirFS(renderer),
  DevOrigins, Logger})` (**Dots/Automation/Voices/Notify/Approvals nil**) →
  `gw.Listen("127.0.0.1:<port>")` → `WriteControlFile` → `Serve` → stdout'a tek
  satır JSON `ready` (port + `gw.LaunchURL()`) → stdin EOF/SIGINT/`serveErr`
  bekle → `gw.Shutdown(5 sn)`. Hata → `{"type":"error","message":…}` ve çıkış
  kodu ≠ 0. ~120 satır; `recentWebProjects` ve `ensureServer` mevcut yardımcıları
  olduğu gibi çağrılır (değiştirilmez).
- Kendi bayrak değişkenleri (`appPort`, `appDevOrigins`): `web`/`dots` ile
  paylaşılan `webPort`/`webDevOrigins`/`webOpen` bağı kalkar (bugünkü
  "varsayılanlar sonra kaydedilir" sorunu da biter).
- `waitAndShutdown` (Dots yeniden düzenlemesiyle `web.go`'ya taşındı) App
  tarafından **kullanılmaz**; `serve` kendi 15 satırlık bekleme döngüsünü taşır.

**G2. Sahibin `internal/cmd/web.go`'ya App için eklediği küçük düzenlemeler
(geri alınacaklar):** yalnız şu iki blok + paylaşılan değişken:

1. `runWeb` içinde "zaten çalışıyor" dalında (`web.go:133-140`):
   ```go
   if webApp {
       closed, err := launchDesktopApp(ctx, link)
       if err != nil { return err }
       <-closed
       return nil
   }
   ```
   → **silinir** (yalnız `openWebBrowser(cmd, link); return nil` kalır).
2. `runWeb` sonunda (`web.go:253-255`):
   ```go
   if webApp {
       return runApp(cmd, gw, serveErr, link)
   }
   ```
   → **silinir**.
3. `internal/cmd/app.go:22-24` içindeki `var webApp bool` ve `RunE`'deki
   `webApp = true; webOpen = false; ... return runWeb(cmd, args)` → G1 ile
   değişir.

`web.go`'daki **geri kalan** fark (dots komutu, `dotStore`/`automation`/
`voices` kurulumu, `dotApprovals`, `dotNotifier`, `elevenLabsAccessToken`,
`runDots`, `waitAndShutdown` çıkarımı, `product` metni, ilgili import'lar)
**Atlas Dots'a aittir; App ile ilgisi yok, geri alınmaz.** (App planı bu
parçalara bağımlı değil.)

**G3. `internal/webui` (opsiyonel, kozmetik):** `Options.Product` yoksa hiç
dokunulmaz. İstenirse `contentSecurityPolicy` için `media-src` farkı ve hata
sayfası metni `Product=="app"` ile ayrılır (bugünkü davranış `web` için aynen
kalır). Statik işleyici `dots`/`dots.html` yolunu App dist'inde bulamayıp 500
döner; App renderer'ı o yola gitmez, zararsız.

**G4. `launchDesktopApp`:** `--url=` yerine `--project=<yol>` ve
`--atlas-bin=<os.Executable()>` geçer (main, gateway child'ını bu ikiliyle
başlatır: aynı sürüm garantisi). Kurulu `Atlas.exe` modunda `--atlas-bin`
yoksa `resources/bin/atlas-agent.exe` kullanılır.

**Doğrulama (Go):** `go build ./... && go test ./internal/cmd/... -run App
-p 1` (hafızadaki test ortamı notu: `-p 1`, `AI_AGENT` env'i bir cmd testini
bozuyor). Yeni test: `app serve` handshake'i (httptest benzeri, sahte
upstream) ve `Assets` boşken hata mesajı.

### 2.4 Pencere başına proje: sözleşme özeti

- Bir gateway, N workspace/hub (`gateway.go:91-112`, hub başına SSE
  sahipliği). Pencere başına bir SSE akışı (`/api/workspaces/{id}/events`).
- Aynı projeyi iki pencere açmaz (pencere kaydı); ikinci istek mevcut pencereyi
  odaklar.
- `workspace_replaced` (server yeniden başladı) → renderer yeni id'yi alır,
  snapshot yeniler. `connection:offline` → bağlantı şeridi (§4 Faz 3).
- Gateway çökerse main child'ı bir kez yeniden başlatır, tüm pencereleri yeni
  link ile yeniler; ikinci çöküşte "Atlas çekirdeği başlatılamadı" tam ekran
  hata + günlük konumu.

---

## 3. Tasarım sistemi (sıfırdan)

### 3.1 Bileşen envanteri ve token/animasyon eşlemesi

Token kaynağı: `atlas-app-tokens.css` (renk, tip, radius, gölge, motion
token'ları). Animasyon kaynağı: `DESIGN-ROUTE.md` §3. Sütun "Rota" bu
bölümlere gönderir.

**L0 Temel (Faz 1):** `tokens.css`, `motion.css` (§3.1-3.15 CSS'i), `base.css`
(reset, `cursor: default` butonlarda, seçim yalnız içerikte, kaydırma çubuğu
8 px ince, `:focus-visible` 2 px ink halka, `prefers-reduced-motion` +
`[data-motion=reduce]`), yazı tipi yükleme, `boot.js`, `t()` i18n, ikon seti
(`icons/`: `AtlasMark`, `Send`, `Stop`, `Chevron`, `Check`, `Copy`, `Folder`,
`Plus`, `Search`, `Settings`, `Terminal`, `Branch`, `File`, `Globe`, `Worked`,
`Thinking`, `Compaction`...).

**L1 Primitifler (Faz 1-2, Radix üstünde):**

| Bileşen | Rota | Notlar |
|---|---|---|
| `Button` (primary ink / secondary / ghost / danger, 28/32/36) | 3.1 | primary = `--atlas-ink` zemin; Ember yok |
| `IconButton` (24/28) | 3.2 | |
| `SendButton` (disabled→ready→streaming/stop) | 3.3 | ikon morph, daire kalır |
| `Tooltip` (500 ms gecikme, 300 ms sıcak pencere) | 3.4 | Radix Tooltip |
| `Popover`, `Menu`, `ContextMenu`, `Select` | 3.4 | `[data-state]` animasyonları |
| `Dialog` (+ `AlertDialog`) | 3.5 | pencere kontrolleri overlay üstünde |
| `Collapse`, `Chevron` | 3.6 | `grid-template-rows` |
| `Toast` + `ToastRegion` (kendi, ~100 satır) | 3.11 | 320 px, sağ alt, en çok 3 |
| `Spinner`, `Dots`, `Skeleton`, `Progress`, `ContextRing` | 3.10 | |
| `ShimmerText` (per-char yok; "lite" gradient) | 3.9 | tek DOM düğümü |
| `Kbd`, `Chip`, `Badge`, `StatusDot`, `Avatar` (proje harf avatarı) | 2.x | |
| `Switch`, `Segmented`, `TextField`, `SearchField` | 2.x | |
| `ScrollArea` (üst/alt fade maskesi), `ResizeHandle` | 3.8, 3.12 | |
| `Disclosure` (tur/tool katlama için üst düzey) | 3.6 | |

**L2 Kompozitler (Faz 2-5):**

| Bileşen | Rota | Faz |
|---|---|---|
| `Markdown` (blok-bazlı akış, kelime fade) | 3.8 | 2 |
| `CodeBlock` (başlık, kopyala, shiki yalnız tamamlanınca) | 5.6 | 2 |
| `UserBubble` (sağda `surface-2`, sticky→tek satır şerit) | 4.1 | 2 |
| `TurnHeader` ("Çalışıyor" shimmer + süre / "2 dk 34 sn çalıştı ⌄"), `TurnSummary` | 4.2 | 2 |
| `ToolRow` (düz metin: fiil + soluk argüman, 8 durum), `ToolGroup` (canlı sayaç) | 5.2-5.4, 5.7 | 2 |
| `ToolBody`: `DiffView`, `TerminalOutput`, `FilePreview`, `SearchResults` | 5.6 | 2 (diff+shell), 5 (kalan) |
| `ThinkingRow` ("Düşündü 8 sn") | 3.9, 4.2 | 2 |
| `PermissionDock` (composer yerine), `QuestionDock` | 4.6 | 2 |
| `FilesChangedCard` ("3 dosya değişti", İncele) | 5.6 | 2 |
| `TurnMeta` (👍👎 kopyala, yeniden dene; fork yok) | 4.3 | 3 |
| `SystemNote`, `DaySeparator` | 4.1 | 3 |
| `ScrollToBottom`, `FloatingChips` ("Changes", "1 Terminal") | 3.15 | 2 / 5 |
| `ComposerShell` + `Composer` (textarea, ek çip şeridi, kuyruk çipi) | 7.5 | 2 |
| `ModelPicker`, `ModeChip`, `ContextRing` | 7.5 | 4 |
| `SuggestionList` (`/`, `@`) | 7.5 | 5 |
| `CompactionDivider` | 3.13 | 5 |
| `ConnectionBanner` | 7.10 | 3 |
| `EmptyState` (+ `Wordmark`) | 4.5, 3.14 | 2 |

**L3 Kabuk ve ekranlar:**

| Ekran/parça | Rota | Faz |
|---|---|---|
| `WindowFrame` (40 px titlebar, `-webkit-app-region`, native kontrol overlay, `AtlasMark` solda, proje adı + dal çipi) | 7.1 | 1 |
| `Home` (wordmark + composer + proje/dal satırı + ipucu pill'leri) | 7.3 | 2 |
| `Thread` (sanal liste, 720 px sütun, sol tick scrubber sonra) | 7.4 | 2 (scrubber 5) |
| `Sidebar` (264 px; Yeni oturum, oturum listesi, hover kartı, alt kullanıcı/ayar) | 7.2 | 3 |
| `CommandPalette` (640 px; son oturumlar, komutlar, dosyalar, ayarlar) | 7.6 | 4 |
| `SettingsDialog` (880×680; Genel, Görünüm, Sohbet, Modeller, İzinler, Kısayollar, Gelişmiş) | 7.7 | 4 |
| `RightRail` (Changes / Files / Jobs) | 7.4 | 5 |
| `Onboarding` (5 adım) + marka anı | 7.8, 3.14 | 6 |
| `Notifications` (OS + toast) | 7.9 | 6 |

**Wordmark:** "web'deki wordmark korunur": ANSI Shadow "ATLAS-AGENT" gökkuşağı
taraması. Bileşen `web/`'den kopyalanmaz; terminal UI'nin kaynağı
(`internal/ui/logo/pixelbanner.go`) esas alınarak renderer'da yeniden yazılır
(canvas, hücre başına renk, 14 sütun/sn, reduced-motion'da statik, ~60 satır).
Varsayılan **monokrom** (`--atlas-ink-4`), gökkuşağı/Ember taraması yalnız
marka anlarında (ilk açılış, yeni oturum ana ekranının ilk görünümü).

**App ikonu / logo:** DESIGN-ROUTE §1.3'teki "A · Omuz" (`atlas-marks.svg`)
çizimi son hâline getirilir; `.ico` (16-256), `.icns`, `.png` üretimi
`app/build/` altında script ile.

### 3.2 Hareket kataloğu seçici çevirisi (DESIGN-ROUTE §3 → React/Radix)

Rota CSS'i vendored Kobalte/OpenCode seçicileriyle yazıldı. App'te:

| Rota'daki | App'te |
|---|---|
| `[data-component="menu-v2-content"]`, `popover-content`, `tooltip-v2` + `[data-expanded]`/`[data-closed]` | `[data-atlas="pop"][data-state="open"]`/`[data-state="closed"]` (Radix) |
| `--kb-popover-content-transform-origin`, `--kb-menu-content-transform-origin` | `--radix-popover-content-transform-origin`, `--radix-dropdown-menu-content-transform-origin`, `--radix-tooltip-content-transform-origin` |
| `[data-component="dialog-v2"]`, `dialog-overlay` | `[data-atlas="dialog"]`, `[data-atlas="dialog-overlay"]` |
| `[data-component="collapsible"] [data-slot="collapsible-content"]` + `--kb-collapsible-content-height` | `[data-atlas="collapse"]` (grid-rows); Radix Collapsible kullanılacaksa `--radix-collapsible-content-height` |
| `[data-component="markdown"] [data-markdown-word]` | `[data-atlas="markdown"] [data-word-enter]` (kendi renderer'ımız ekler; `animationend`'de kaldırılır) |
| `[data-component="text-shimmer"]` | `[data-atlas="shimmer-lite"]` (per-char yok) |
| `.toast-v2` | `[data-atlas="toast"]` |
| `[data-slot="titlebar-tab-item"]` | v1'de yok (sekme şeridi yok) |
| `data-motion="reduce"` | aynı (boot.js yazar) |

Ember kullanan katalog satırları nötrleştirilir: compaction çizgisi `ink-2`
gradient (Ember yok); çalışıyor noktası `ink-2` halka ripple; review vurgusu
`ink` halka; composer odak parıltısı yok (kenar `line-3`). Ember yalnız:
`mark-draw`, `wordmark-sweep`, onboarding "Başla" düğmesi, uygulama ikonu.

### 3.3 Araç satırı: tek mod (sessiz metin)

Rota §5.1'deki "Kart" modu ve yoğunluk ayarı **yok**. §5.2-5.8'deki düz satır
anatomisi, durumlar, gruplama ve gövde önizlemeleri aynen uygulanır;
`tool-row-model.ts` (araç adı → kategori/fiil/argüman/meta) Go'daki
`internal/permission/category.go` ile bir test üzerinden senkron tutulur
(araç listesi fikstürü). Bilinmeyen araç → "other" fiili: araç adı humanize.

---

## 4. Faz planı (en kısa yoldan ilk kullanılabilir sohbet)

Takvim tahmini tek geliştirici + AI yardımıyla; ilk kullanılabilir sohbet
**Faz 2 sonunda (~3. hafta)**.

### Faz 0: İskelet ve dikey dilim (3-4 gün)

Yapılacaklar:
- `app/renderer` iskeleti (Vite + React + TS), `dist` çıktısı.
- Go: G1 (`app serve`), G2 (web.go'dan App düzenlemelerinin silinmesi), G4.
- `main/` yeniden düzenleme: gateway child + handshake, tek pencere
  `loadURL(link)`, `window.atlas.app.info`.
- Renderer: `bootstrap` + SSE `hello` + workspace adı ekranı.

Kabul:
- `atlas-agent app` bir pencere açar ve boş projede workspace adını +
  "bağlı" göstergesini yazar; komut hemen döner, kapatınca `atlas-agent` süreçleri
  (gateway) kalmaz (görev yöneticisi/`Get-Process` kontrolü).
- `rg "webApp|runApp|launchDesktopApp" internal/cmd/web.go` boş.
- `go build ./... && go test ./internal/cmd/... -p 1` yeşil (mevcut kırık test
  varsa değişikliğimizden bağımsız olduğu not edilir).
- CSP ihlali konsol hatası yok; `web/` ve `atlas-agent web` davranışı değişmedi
  (`atlas-agent web` hâlâ açılıyor).

### Faz 1: Temel + pencere kromu + tek proje penceresi (1 hafta)

Yapılacaklar: L0 + L1 çekirdek (Button, IconButton, Tooltip, Dialog, Toast,
Spinner, ShimmerText, Collapse, Kbd), `WindowFrame` (titleBarOverlay, sürükleme
alanı, tema eşlemeli overlay), tema/`data-motion` boot, i18n, pencere kaydı
(`--project`, `second-instance`, bounds), klasör seç diyaloğu, proje yoksa
"Klasör aç" ana ekranı, `prefs.json`.

Kabul:
- Açık/koyu/sistem tema ilk boyamada beyaz flaş olmadan uygulanır; OS tema
  değişince overlay renkleri 1 sn içinde güncellenir.
- İkinci `atlas-agent app <başka proje>` ikinci pencere açar; aynı proje
  mevcut pencereyi odaklar. Pencere boyutu/konumu proje başına hatırlanır.
- `[data-motion=reduce]` iken tüm transform animasyonları kapalı (Playwright
  `getAnimations()` testi). Axe: kritik ihlal 0.

### Faz 2: Sohbet MVP (2 hafta)

Yapılacaklar: stores + SSE reducer (resync, rAF birleştirme), oturum oluştur +
otomatik başlık, `Composer` + `SendButton` (gönder↔durdur, kuyruk/follow-up),
`Thread` sanal liste + alt-sabitleme, `UserBubble`, `Markdown` (blok-bazlı,
kelime fade, "Çalışıyor" shimmer), `CodeBlock`, sessiz `ToolRow`/`ToolGroup`
(8 durum, canlı sayaç), `ThinkingRow`, `TurnHeader` katlama + özet satırı,
`DiffView` (edit/write) + shell çıktısı, `PermissionDock` + `QuestionDock`
(composer yerine geçer, taslak korunur), `FilesChangedCard`, `ScrollToBottom`,
`Home` boş durum + wordmark, hata/iptal durumları.

Kabul:
- Gerçek bir modelle uçtan uca: istem → akan metin → araç satırları (okuma,
  grep, edit, shell) → izin dock'u (izin ver/reddet) → "çalıştı" katlaması →
  "N dosya değişti" kartı.
- Gönder'den **≤ 200 ms** içinde durum satırı ("Çalışıyor") görünür
  (Playwright zaman damgası; sahte asistan metni yok).
- 200 turluk fikstür oturumunda 4× CPU throttle'da ≥ 50 fps kaydırma; akış
  sırasında ana iş parçacığı uzun görevi (> 50 ms) kare başına ortalama < 1.
- SSE kesilip gelince (gateway `resync`) thread tutarlı; çift mesaj yok.
- Meşgulken yazılan mesaj follow-up olarak girer; Esc turu iptal eder;
  iptal edilen turda "Durduruldu" satırı.
- Reduced-motion'da kelime fade/shimmer döngüleri kapalı, durum metin olarak
  okunur.

### Faz 3: Sidebar, geçmiş, dayanıklılık (1 hafta)

Yapılacaklar: `Sidebar` (oturum listesi: Bugün/Bu hafta/Daha eski, durum
noktaları, okunmamış, hover kartı), arama (`?q=`), yeniden adlandır/sil/sabitle
(sabitle/arşiv istemci tarafı), oturum açıp devam etme, `TurnMeta`,
`ConnectionBanner` (çevrimdışı, yeniden bağlanma, taslak korunur), `git.status`
IPC (dal çipi), klavye: Ctrl+N, Ctrl+B, Alt+↑/↓, Esc.

Kabul:
- 500 oturumlu projede sidebar açılışı < 100 ms; arama yazarken kare düşmez.
- Gateway/sunucu öldürülünce 1 sn içinde şerit, geri gelince otomatik kapanır;
  yazılmış taslak kaybolmaz.
- Başka istemcide (terminal TUI) çalışan oturum sidebar'da canlı durum noktası
  gösterir.

### Faz 4: Palet, modeller, ayarlar (1.5 hafta)

Yapılacaklar: `CommandPalette` (Ctrl+K), kısayol kayıt sistemi + Ayarlar >
Kısayollar listesi, `ModelPicker` (640 px), sağlayıcı bağlantıları (connections
uçları), izin modu çipi, oturum modu, `SettingsDialog` (Genel/Görünüm/Sohbet/
Modeller/İzinler/Kısayollar), Hareket (Sistem/Tam/Azaltılmış) ve tema ayarı.

Kabul: palet 100 ms altında açılır, fuzzy sonuçlar klavye ile tam
yönetilir; model değişimi bir sonraki turda geçerli; API anahtarı ayarı ve
test düğmesi çalışır; ayarlar diyaloğu odak tuzağı + Esc + geri dönüş odağı
doğru (axe + manuel).

### Faz 5: Sağ ray, dosyalar, ekler (1.5 hafta)

Yapılacaklar: `RightRail` (Changes: dosya listesi + `DiffView`; Files: ağaç +
görüntüleyici; Jobs/Terminal: salt-okunur çıktı), `@` dosya araması (E2),
`/` komutlar, dosya/resim ekleri (sürükle-bırak, yapıştır, 10 MB/10 adet
sınırı gateway ile aynı), compaction anı + `ContextRing`, geri al (rewind)
akışı, `ide.open`, favicon IPC + atıf linki, sol tick scrubber.

Kabul: 40 dosyalı değişiklik setinde Changes rayı akıcı (sanal liste);
ek gönderimi gateway limitleriyle uyumlu hata mesajları; compaction sırasında
animasyon + bitince özet; rewind önizleme + onay.

### Faz 6: Onboarding, bildirim, cila (1 hafta)

Yapılacaklar: onboarding (marka anı: işaret çizimi 1 sn, Ember sweep; 5 adım),
OS bildirimleri + görev çubuğu rozeti, boş/hata durumları, performans bütçesi
(aşağıda), erişilebilirlik denetimi (DESIGN-ROUTE §8/Faz 6 kontrol listesi
devralınır), görsel regresyon (light/dark × reduce/full).

Kabul: Lighthouse a11y ≥ 95, axe kritik 0; soğuk açılış (pencere görünür →
etkileşimli) < 1.5 sn SSD; boşta bellek < 350 MB (tek pencere); 30 sn+ tur
bitince odak dışındayken OS bildirimi, tıklayınca doğru pencere/oturum.

### Faz 7: Paketleme ve yayın (1 hafta, Faz 2'den sonra paralel başlayabilir)

Aşağıda §5.

**Performans bütçesi (CI'da ölçülür):** renderer JS ilk yükleme (gzip) ≤ 350 kB
(shiki/xterm/diff lazy), akış sırasında bir token olayı → ekran ≤ 1 kare,
thread'de DOM düğümü (katlı turlar) ≤ 1500, ana iş parçacığı uzun görev
(> 50 ms) akışta ≤ %1.

---

## 5. Paketleme (Windows) ve Go ikilisi ile birlikte gönderim

> **Güncel durum (2026-11-04):** Sahip kararıyla ayrı kurulum paketi yok; uygulama
> imzasız olarak **CLI paketinin içinde** gelir. Sürüm
> `atlas-app-windows-x64.zip` varlığını ekler; `install.ps1` ve npm postinstall
> onu ikilinin yanındaki `app\` klasörüne açar, `atlas-agent app` oradaki
> `Atlas.exe`'yi başlatır. Ayrıntı `app/README.md`, `app/electron-builder.yml`,
> `.github/workflows/app.yml`. Aşağıdaki NSIS anlatımı geçersizdir ve tarihsel
> kayıt için durur. `ide.open`, `--browser` yedek modu ve otomatik güncelleme
> kapsam dışı bırakıldı.

Dağıtım biçimi: **tek kurulum paketi** `Atlas-Setup-<sürüm>.exe` (NSIS,
kullanıcı başına kurulum, yönetici gerekmez, `%LOCALAPPDATA%\Programs\Atlas`).

Kurulu düzen:
```
Atlas\
  Atlas.exe                      (Electron, fuse'lı)
  resources\
    app.asar                     (main/, preload/, package.json)
    renderer\                    (extraResources; AÇIK dosya, Go os.DirFS ile servis eder)
    bin\atlas-agent.exe          (aynı sürüm Go ikilisi, CGO_ENABLED=0)
```

`electron-builder.yml` (özet):
```yaml
appId: dev.atlas.agent
productName: Atlas
directories: { output: ../dist/app, buildResources: build }
files: [main/**, preload/**, package.json]
extraResources:
  - { from: renderer/dist, to: renderer }
  - { from: build/bin/atlas-agent.exe, to: bin/atlas-agent.exe }
asar: true
win:
  target: [{ target: nsis, arch: [x64] }]
  signtoolOptions: {}            # Faz 7: sertifika gelince doldurulur
nsis: { oneClick: false, perMachine: false, allowToChangeInstallationDirectory: true }
electronFuses:
  runAsNode: false
  enableNodeOptionsEnvironmentVariable: false
  enableNodeCliInspectArguments: false
  onlyLoadAppFromAsar: true
  enableEmbeddedAsarIntegrityValidation: true
publish: { provider: github }    # opsiyonel, electron-updater
```

Derleme komut zinciri (`Taskfile.yaml`'a `app:*` görevleri, `web:*` değişmez):
1. `npm --prefix app/renderer run build` → `app/renderer/dist`
2. `go build -trimpath -ldflags "-X …/version.Version=<v>" -o app/build/bin/atlas-agent.exe .`
   (`CGO_ENABLED=0 GOEXPERIMENT=greenteagc`)
3. `npx electron-builder --config app/electron-builder.yml --win`

Çalışma zamanı yol çözümü (`main/paths.cjs`): packaged → `resources/bin`,
`resources/renderer`; dev → `ATLAS_AGENT_BIN` (varsayılan PATH'teki
`atlas-agent`) ve `app/renderer/dist`.

`atlas-agent app` ile kurulum ilişkisi: `findDesktopShell` kurulu
`%LOCALAPPDATA%\Programs\Atlas\Atlas.exe`'yi (ve kayıt defterindeki kurulum
yolunu) arar; bulursa onu `--project=<cwd>` ile ayrık başlatır. Kurulu değilse
kaynak ağacındaki `app/` + `node_modules/electron` (geliştirici modu), o da yoksa
"Atlas masaüstü uygulaması kurulu değil: <indirme/kurulum yönergesi>" hatası
(ve `--browser` seçeneği). npm ile gelen CLI paketi masaüstü uygulamayı
**içermez** (Electron ~200 MB); yalnız bulur-başlatır.

CI (`.github/workflows`): yeni `app.yml` (windows-latest): renderer lint+test,
Go build, `electron-builder --win --publish never`, kurulum paketini artifact
yap, Playwright Electron duman testi (paketlenmiş uygulama açılır, `atlas-agent`
çocuğu `ready` verir, pencere başlığı doğru, kapanışta çocuk süreç kalmaz).
Sürüm etiketi: `app-v*` (CLI `v*` etiketlerinden ayrı; hafızadaki yayın
sürecindeki "artifacts/ 100 MB itme tuzağı" gereği çıktı `dist/app` git'e
girmez, `.gitignore`a eklenir).

Güncelleme: Faz 7 sonunda `electron-updater` + GitHub Releases (imza şart);
imza yoksa otomatik güncelleme kapalı, "Yeni sürüm var" bildirimi + indirme
bağlantısı. CLI güncellemesi ayrı kanal (`atlas-agent` ikilisi paket içindeki
sürüme kilitli; App güncellenince `resources/bin` birlikte güncellenir).

---

## 6. Riskler

| Risk | Etki | Önlem |
|---|---|---|
| Akış sırasında render maliyeti (tam mesaj snapshot + büyük markdown) | Takılma, düşük fps | rAF birleştirme, blok-bazlı memo, son blok dışı dondurma, Faz 2 perf kabulü; gerekirse gateway'e `message` delta'sı (server değişikliği) |
| Sanal liste + dinamik yükseklik + alt-sabitleme zorluğu | Zıplayan scroll | Erken prototip (Faz 0-1'de fikstürle), kendi `scroll-anchor` mantığı, gerekirse liste dışı render (son N tur normal DOM) |
| Gateway çocuk süreç yaşam döngüsü (zombi süreç, çift başlatma, çökme) | Takılı süreç, bozuk oturum | stdin-EOF yaşam çizgisi, `window-all-closed` → kill, tek yeniden başlatma, duman testi, görev yöneticisi kontrolü |
| Port/origin her açılışta değişiyor | `localStorage` kaybı, cookie karışması | Kalıcı tercih main `prefs.json`'da; `partition` kalıcı değil; cookie adı port taşıyor (mevcut) |
| Başka istemci (TUI) ile eşzamanlı oturum, `workspace_replaced` | Eski id ile 404 | Reducer `workspace_replaced`'ı işler, tüm isteklerde id çözümü gateway'de (`hubFor` alias) |
| `webui` paketinin Dots ile aynı anda değişiyor olması (`api.go`, `gateway.go`, `assets.go` kirli) | Birleştirme çatışması | App `webui`'ye dokunmaz (E1 çözümü Options ile); yalnız E2 gibi yeni dosyalar eklenir |
| Windows imzası olmadan SmartScreen uyarısı | Kurulum sürtünmesi | Faz 7 öncesi sertifika kararı (Soru 1); o zamana dek iç dağıtım |
| Electron major güncellemesi (3 majör desteklenir) | Güvenlik açığı | Pin + aylık yama güncellemesi, Dependabot grubu |
| Model/araç çeşitliliği (100+ araç) → satır fiil eşlemesi eksik | "other" satırlar çirkin | Kategori tablosu testi + bilinmeyen araç için humanize yedeği; telemetri yok, yerel günlük |
| Tek geliştirici kapasitesi / kapsam büyümesi | Gecikme | Faz 2 sonu "kullanılabilir" noktası; terminal PTY, tarayıcı önizleme, sekme şeridi, fork ertelenmiş |
| Sözleşme kayması (Go proto değişir, TS tipleri eskir) | Sessiz hata | Fikstür tabanlı Vitest + Go harness kaydı CI'da |

---

## 7. Açık sorular (3)

1. **Kod imzalama ve güncelleme kanalı:** Windows kod imzalama sertifikası
   (OV/EV veya Azure Trusted Signing) alınacak mı? Alınmazsa kurulum
   SmartScreen uyarısı verir ve otomatik güncelleme (electron-updater) pratik
   olarak kapalı kalır; GitHub Releases kanalı kabul mü?
2. **Platform kapsamı:** v1 yalnız Windows x64 mi (önerim), yoksa macOS/Linux
   da aynı döngüde mi? (macOS için imza+notarization ve trafik ışığı yerleşimi
   ek iş; kod tabanı buna hazır olacak şekilde yazılır.)
3. **Dağıtım biçimi:** App yalnız ayrı kurulum paketi olarak mı (önerim; CLI
   `atlas-agent app` kurulu uygulamayı bulur-başlatır), yoksa CLI yayın
   arşivine/npm paketine de gömülsün mü (≈ +200 MB)?

### Sahibin cevapları (2026-11-04)
- İlk sürüm: yalnız Windows.
- Dağıtım: app CLI paketinin içine konacak (ayrı kurulum yok).
- İmzalama: sahip terimi bilmiyordu; ilk sürüm için imzasız önerildi, karar bekliyor.
