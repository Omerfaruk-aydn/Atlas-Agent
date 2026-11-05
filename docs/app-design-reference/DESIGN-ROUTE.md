# ATLAS-AGENT App: Tasarım Rotası (v1, 2026-11-04)

Kapsam: ATLAS-AGENT masaüstü uygulaması (App edition). CLI, Web ve Atlas Dots
sürümleri hazır; bu belge App'in UI/UX rotasıdır. Kaynaklar: `FINDINGS.md`
(+ ADDENDUM), `shots/*.png`, OpenCode/Grok Bot/Cursor'ın paketlenmiş CSS/JS'i,
ve Atlas'ın kendi kodu (`web/src/oc`, `web/vendor/atlas-ui`,
`web/vendor/atlas-session-ui`).

> **Güncelleme (2026-11-04, sahip kararı):** App, web UI'dan tamamen ayrı bir
> üründür; sıfırdan yeni bir frontend ile yazılır ve `web/`, `web/vendor/*`
> kullanılmaz. Bu yüzden **§0 (zemin tablosu) ve §8 (yol haritası) geçersizdir**
> (yerine `APP-BUILD-PLAN.md`); §1-7 tasarım temeli olarak geçerlidir, şu
> geçersiz kılmalarla: Ember yalnız marka anlarında (odak halkası/bağlantı/
> çalışıyor göstergesi nötr), yalnız sessiz metin araç satırları (§5.1 "Kart"
> modu ve §4.7 Persona/maskot ertelendi), Dots kapsam dışı, Cursor tarzı proje
> başına bir pencere (oturumlar sidebar'da; §7.1'deki sekme şeridi v1'de yok).
> CSS'teki Kobalte/vendored seçiciler `APP-BUILD-PLAN.md` §3.2'deki tabloyla
> Radix/`data-atlas` seçicilerine çevrilir.

Destek dosyaları (aynı klasörde):

- `atlas-app-tokens.css`: renk/tip/radius/spacing/motion token'ları ve
  vendored `--v2-*` token'larına köprü. Yapıştırmaya hazır.
- `atlas-marks.svg`: Atlas işaret (logo) önerileri A/B/C, kaba çizim.

Kanıt düzeyi etiketleri (bu belgede her yerde):

- **[canlı]**: statik ekran görüntüsünde gözlendi (OpenCode home/model/
  arama, Grok Bot sohbet/bağla, Cursor Agents sohbet/araç listesi).
- **[canlı-akış]**: gerçek test sohbetinde (web araması) ~0.7 sn arayla
  alınan kare dizilerinde **hareket halinde** gözlendi (`shots/oc-live*`,
  `shots/grok-live*`, FINDINGS ADDENDUM 2). Gözlenenler: streaming kelime
  fade'i ve blok blok markdown (OpenCode), araç grup satırı anatomisi ve
  canlı artan sayaçlar (OpenCode), composer'ın yerini alan izin dock'u
  (OpenCode), gönder → durdur morph'u (OpenCode), başlıkta token halkası
  (OpenCode), maskot durum göstergesi + değişen durum metni (Grok), anlık
  "ack" balonu (Grok), inline atıf çipi (Grok).
- **[CSS]**: referans uygulamanın paketlenmiş CSS/JS'inden birebir okundu
  (değerler kesin, hareket canlı görülmedi).
- **[çıkarım]**: canlı gözlenmedi, CSS + davranış mantığından türetildi.
  **Cursor hiç canlı sürülemedi** (IDE sınıfı uygulamada yazma engelli):
  Cursor'a dair her şey statik ekran + CSS'tir. Ayrıca çok kaynaklı
  "kaynak listesi" paneli, tarayıcı önizleme çerçevesi ve uzun süren
  shell kuyruğu hiçbir uygulamada canlı görülmedi: **[çıkarım]**.

---

## 0. Atlas kod tabanında bugün ne var (zemin)

| Alan | Bugünkü durum | Not |
|---|---|---|
| Framework | SolidJS + Vite + Tailwind 4, Kobalte, `motion`, `solid-sonner`, `@tanstack/solid-virtual` | `web/package.json` |
| UI çekirdeği | OpenCode 2.0.32 portu, değiştirilmeden vendored: `web/vendor/atlas-ui`, `web/vendor/atlas-session-ui` | `vite.config.ts` alias'ları `@atlas/ui/*`, `@atlas/session-ui/*` |
| Token'lar | `web/vendor/atlas-ui/styles/tokens/{colors,theme}.css` (`--v2-*`), köprü `web/src/styles/tokens-bridge.css` | Saf gri + mavi aksan (#3b5cf6), yani OpenCode'un paleti |
| Araç satırları | `tool-renderer.tsx` (BasicTool, ToolRegistry), `basic-tool.css`, `tool-status-title.css` (active/done morph), `TextShimmer` | Kart/collapsible yoğunluğu var, Cursor tarzı düz satır yok |
| Oturum ekranı | `web/src/oc/session.tsx` (Timeline, `reasoningMode`, `disclosure` store, ComposerDock, ReviewRegion) | |
| Dock'lar | `oc/app/session/requests/session-permission-dock.tsx`, `session-question-dock.tsx` | |
| Titlebar/sekme | `oc/titlebar.tsx`, `oc/app/shell/titlebar/*` (scroll-timeline kenar fade'leri) | |
| Platform | `oc/app/runtime/platform/platform.tsx`: `platform: "web" \| "desktop"` | Masaüstü kabuğu yok (repo'da electron/tauri/wails yok) |
| Marka | `oc/atlas-wordmark.tsx` (CLI'daki ANSI Shadow "ATLAS-AGENT", gökkuşağı tarama, canvas) | Atlas'a ait, korunur |
| **Logo** | `oc/app/providers/atlas-logo.tsx` | **Bu SVG path'i OpenCode'un kendi işaretiyle byte-byte aynı** (OpenCode bundle `list-*.js` içinde doğrulandı). App yayınından önce değiştirilmeli (bkz. 1.3, Faz 1). |
| Dots | `web/src/dots/dots.css`: sıcak "sand" nötrler, Ember aksan `#f2762e`, Plex Sans | App'in kimliği buna akraba olacak |

Sonuç: App sıfırdan bir UI değil. Aynı Solid bundle'ı `platform: "desktop"`
ile çalışır. İş üç katmanda yürür: (1) Atlas token katmanı ve kimlik,
(2) Atlas'a özgü bileşenler (`web/src/oc/app/...` altında, vendored
dosyalara dokunmadan), (3) ince bir native kabuk.

---

## 1. Tasarım ilkeleri ve referanslardan ne alıyoruz

### 1.1 İlkeler

1. **Sessiz yüzey, canlı durum.** Krom (çerçeve) neredeyse görünmez:
   hairline çizgiler, 2 seviye yüzey. Renk ve hareket yalnızca durum
   taşır (çalışıyor, bekliyor, hata, izin). Dekoratif hareket yok.
2. **Ajan işi okunur olmalı, gürültülü değil.** Uzun ajan turları
   (100+ araç çağrısı) tek satıra katlanır ("2 dk 34 sn çalıştı"),
   ama her adım bir tıkla açılır. Cursor'ın en güçlü yanı bu [canlı].
3. **Tek hareket dili.** Tüm UI 7 easing ve 6 süre token'ı ile konuşur
   (bkz. 2.5). Ad-hoc `0.3s ease` yasak.
4. **Klavye birinci sınıf.** Her eylemin kısayolu ve görünür focus'u var.
5. **Hareket azaltılabilir, bilgi kaybolmaz.** Reduced-motion'da her
   animasyonun statik eşdeğeri tanımlı.
6. **Kimlik bizim.** Desen alırız, varlık (asset) almayız.
7. **Masaüstü hissi.** Butonlarda `cursor: default` (OpenCode böyle yapıyor
   [CSS]), native pencere kontrolleri, metin seçimi yalnızca içerikte,
   `user-select: none` kromda.

### 1.2 Kimden ne alıyoruz

| Referans | Alıyoruz (desen) | Almıyoruz |
|---|---|---|
| **Cursor** (Agents penceresi) | Düz metin araç satırı (fiil + soluk argüman) [canlı]; turu "Worked for 2m 34s" altında katlama + "Edited 3 files, explored 9 files, 7 searches, ran 1 command +21 −71" özet satırı [canlı]; "3 Files Changed" kartı + Review linki [canlı]; sağ ray (Changes/Browser/Terminal/Files) [canlı]; sol kenarda tur tick-scrubber [canlı]; composer üstü yüzen çipler (Changes, 1 Terminal, Create Branch & Commit) [canlı]; kullanıcı mesajının turun tepesinde sticky kart olması [canlı]; tur altı eylem satırı (👍 👎 kopyala fork + zaman) [canlı]; `scale-in` reduced-motion'da `fade-in`'e düşer [CSS] | "blob" ajan küresi, Cursor logosu, `--cursor-*` token adları, VS Code workbench yoğunluğu, koyu-varsayılan tema |
| **OpenCode** desktop | Araç grup satırı: soluk önek + sayı + koyu başlık, ikon/kutu yok, sayı canlı artar, anlatım paragrafları satırların **arasına** girer [canlı-akış]; izin isteğinde composer'ın yerini alan dock (başlık, açıklama, kapsam dropdown'u, metin buton + koyu pill) [canlı-akış]; gri ok → siyah yuvarlatılmış kare durdur [canlı-akış]; başlıkta dönen halka + token sayacı ("7 b → 25 b") [canlı-akış]; ilk mesajdan otomatik sekme başlığı [canlı-akış]; en yeni kelimeler gri gelip koyulaşır, markdown blok blok [canlı-akış]; sticky kullanıcı balonunun tek satırlık şeride küçülmesi [canlı-akış]; İçe gömülü (inset) içerik kartı + kabuk arka planı [canlı]; titlebar'da oturum sekmeleri [canlı]; merkez composer + altında meta satırı (workspace / Yerel / branch / path) [canlı]; per-char text-shimmer (45 ms adım, 1.2 s) [CSS]; streaming kelime fade (`markdown-word-enter` .18 s) [CSS]; sources panel/chip/site-icon (favicon yığını −3 px örtüşme) [CSS]; compaction gather/glint [CSS]; `:root[data-motion=reduce]` kullanıcı anahtarı [CSS]; toast stack (.28 s emphasized) [CSS]; 640 px model diyaloğu, 480 px arama paleti [canlı] | Piksel "opencode" wordmark, OpenCode kare işareti (bugün `atlas-logo.tsx`'te duruyor!), "Big Pickle" vb. ürün dili, mavi aksan |
| **Grok Bot** | Araçları tamamen gizleyip **maskotu durum göstergesi** yapma: sidebar karosu ve son balonun altındaki 26 px maskot faza göre ifade değiştirir (yükleniyor üç nokta → yüz, göz kırpma, yana bakma, "pause" çubukları, kısılmış göz) ve yanındaki gri metin faza göre değişir ("Web'de arama yapıyor" → "Web'de arıyor" → "Yazıyor…") [canlı-akış]; iş başlamadan anlık "Hemen bakıyorum." balonu [canlı-akış]; kaynak listesi yerine metin içi favicon'lu atıf linki [canlı-akış]; Yumuşak gri balon + gruplu mesaj (2 px aralık) [canlı]; inline kartlar: soru kartı, connector kartı (40 px logo, doğrulanmış rozet, siyah pill "Bağlan") [canlı]; araç → insan dili aktivite fiili + glyph eşlemesi ("Searching the web" / magnifying-glass, "Reading the web" / globe, 60 karakter kırpma) [CSS/JS]; ikon morph-out `blur(2px) scale(.6)` [CSS]; `grid-template-rows 0fr↔1fr` collapse [CSS]; pill composer [canlı]; 900 px "uygulama bağla" modalı, filtre çipleri (aktif = siyah pill) [canlı] | "Fıstık" avatarı/yıldız rozeti, maskotun kendisi (siyah daire + hap gözler; Atlas'ta Dots'un kendi `LiveAvatar` şekilleri kullanılır), Grok/Cursor "sand" tasarım sistemi adları, ses dalga logosu, sohbet asistanı tonu (App bir kodlama aracı) |

### 1.3 Atlas kimliği

- **İsim metaforu:** Atlas göğü omuzlarında taşır. İşaret bunu taşır.
- **İşaret önerileri** (`atlas-marks.svg`):
  - **A · Omuz (önerilen):** iki çizgiden bir "A", tepesine oturan bir yay
    (gök). 24 px grid, 2 px stroke, yuvarlak uç. Yay Ember, gövde ink.
    Stroke tabanlı olduğu için ilk açılış "draw" animasyonuna uygun.
  - **B · Meridyen:** küre + eğik meridyen + yörünge noktası. Daha "global",
    ama küre ikonları sektörde çok yaygın.
  - **C · Piksel A:** CLI'daki ANSI banner'ın 5×5 piksel "A"sı. Terminal
    mirasını taşır, Dots ile uyumsuz.
- **Wordmark:** `atlas-wordmark.tsx` (ANSI Shadow, gökkuşağı tarama) CLI
  mirası olarak home'da kalır, ama App'te **varsayılan monokrom**
  (`--atlas-ink-4` dolgu) + tek seferlik Ember tarama; gökkuşağı modu
  Ayarlar > Görünüm'de "Terminal wordmark" anahtarı.
- **Renk:** sıcak grafit nötrler (Dots ile aynı aile) + **Ember** aksan.
  Ember yalnızca: marka anları, focus ring, çalışıyor göstergesi, link
  metni, birincil CTA (onboarding). Gönder butonu Ember değil, ink
  (Grok/Cursor/OpenCode konsensüsü: koyu yuvarlak buton).
- **Tip:** Inter (vendored bileşenler Inter metriklerine göre ayarlı:
  440/530 ağırlıklar, 13/16 compact metrik). Kod: JetBrains Mono Nerd
  (zaten `atlas-app.css`'te).

---

## 2. Design token'ları

Tam CSS: `atlas-app-tokens.css`. Burada tablolar.

### 2.1 Renk

| Token | Light | Dark | Kullanım / kontrast |
|---|---|---|---|
| `--atlas-shell` | `#f5f4f1` | `#0b0b0c` | Pencere zemini, sidebar zemini |
| `--atlas-surface` | `#ffffff` | `#141415` | İçerik kartı, diyalog, composer |
| `--atlas-surface-1` | `#f9f8f6` | `#18181a` | İç paneller, hover zemin |
| `--atlas-surface-2` | `#f1efeb` | `#1f1f22` | Kullanıcı mesajı, araç yüzeyi, çip |
| `--atlas-surface-3` | `#e8e5df` | `#29292d` | Basılı, pasif gönder butonu |
| `--atlas-ink` | `#1a1917` | `#ececed` | Metin (17.1:1 / 15.6:1) |
| `--atlas-ink-2` | `#5a5650` | `#a6a6ad` | İkincil metin (7.3:1 / 7.6:1) |
| `--atlas-ink-3` | `#7a766f` | `#808088` | Meta, placeholder, araç argümanı (4.5:1 / 4.7:1) |
| `--atlas-ink-4` | `#a9a59d` | `#55555c` | Yalnızca dekor (wordmark, ayraç) |
| `--atlas-line-1/2/3` | ink %7 / %11 / %20 | beyaz %6 / %10 / %18 | Hairline, kart kenarı, güçlü kenar |
| `--atlas-hover` / `--atlas-press` | ink %4.5 / %8 | beyaz %5 / %9 | Overlay |
| `--atlas-ember-500` | `#f2762e` | `#f2762e` | Marka dolgu |
| `--atlas-ember-600` / focus | `#e0621f` (4.2:1, non-text) | `#ff9a5c` | Focus ring, çalışıyor noktası |
| `--atlas-accent-text` | `#b8481a` (5.3:1) | `#ff9a5c` (8.6:1) | Link, aksan metin |
| `--atlas-success` | `#17804f` | `#3dd68c` | Başarı, diff + |
| `--atlas-warning` | `#9a6400` | `#f5b83d` | Uyarı, izin bekliyor |
| `--atlas-danger` | `#c9362f` | `#ff6b63` | Hata, reddedildi, diff − |
| `--atlas-info` | `#2f5bd3` | `#7ea2ff` | Bilgi, okunmamış noktası |

Kurallar: metin asla `ink-4`; 12 px altı metin yok (kbd 11 px hariç, o da
`ink-2`); durum renkleri her zaman ikon/metin ile birlikte (yalnız renk ile
anlam taşımıyoruz).

### 2.2 Tipografi (Inter, variable)

| Rol | Token | px / lh | Ağırlık |
|---|---|---|---|
| Caption (kbd, rozet) | `--atlas-text-caption` | 11 / 14 | 530 |
| Small (meta satırı, çip) | `--atlas-text-small` | 12 / 16 | 440 |
| UI (araç satırı, sidebar, menü) | `--atlas-text-ui` | 13 / 16 | 440, başlık 530 |
| UI loose (reasoning, ayar açıklaması) | `--atlas-text-ui-loose` | 13 / 20 | 440 |
| Body (sohbet, composer) | `--atlas-text-body` | 14 / 22 | 440, kalın 620 |
| H3 / H2 / H1 | | 16/24 · 20/28 · 28/34 | 620 |
| Display (onboarding) | | 40 / 44 | 620, tracking −0.4px |
| Code | `--atlas-text-code` | 12.5 / 20 | 400 |

`vendor/atlas-session-ui/AGENTS.md` kuralı geçerli: 13 px metin 16 px
line-height ile; `leading-none` yok (Inter descender kırpılır).

### 2.3 Radius

`xs 4` (kbd, inline code) · `sm 6` (buton, satır) · `md 8` (menü öğesi, araç
yüzeyi, toast) · `lg 12` (kart, kullanıcı mesajı, popover) · `xl 14`
(composer) · `2xl 16` (diyalog) · `window 10` (inset içerik kartı) ·
`pill 9999` (çip, gönder butonu). Konsensüs: OpenCode 6 baskın, Grok 8/12/999
[CSS].

### 2.4 Spacing ve layout

4 pt grid: 2, 4, 6, 8, 12, 16, 20, 24, 32, 40, 56.

| Ölçü | Değer |
|---|---|
| Titlebar | 40 px |
| Pencere boşluğu (shell → kart) | 8 px (sol sidebar açıkken sidebar shell üzerinde, kart sağda) |
| Sidebar | 264 px (220–360, sürüklenebilir) |
| Sağ ray | 380 px (320 – %50) |
| Sohbet ölçüsü | 720 px metin, 736 px composer |
| Turlar arası | 24 px; tur içi blok arası 12 px; araç satırları arası 0 (satır yüksekliği 24) |

### 2.5 Gölge

`raised` (kart, çip), `floating` (menü, popover, toast), `overlay`
(diyalog), `composer`. Hepsi 0.5 px hairline içerir (OpenCode v2 deseni
[CSS]), sıcak tonlu. Değerler `atlas-app-tokens.css`'te.

### 2.6 Motion token'ları

| Token | Değer | Ne için |
|---|---|---|
| `--atlas-dur-micro` | 90 ms | Press scale, checkbox tick |
| `--atlas-dur-fast` | 120 ms | Hover renk/zemin, ikon değişimi, küçük çıkışlar |
| `--atlas-dur-base` | 160 ms | Tooltip, menü, popover girişi |
| `--atlas-dur-moderate` | 220 ms | Collapsible (≤240 px), toast, mesaj girişi |
| `--atlas-dur-slow` | 280 ms | Sidebar/ray, büyük collapse, toast yığını |
| `--atlas-dur-dialog` | 240 ms | Diyalog girişi |
| loop: shimmer / skeleton / spin | 1.2 s / 1.8 s / 0.9 s | |
| `--atlas-dur-brand` | 1000 ms | İlk açılış çizimi |

| Easing | Eğri | Kaynak konsensüsü |
|---|---|---|
| `--atlas-ease-out` | `cubic-bezier(.22,1,.36,1)` | OpenCode ×16, Grok ×7 (giriş) |
| `--atlas-ease-standard` | `cubic-bezier(.2,.8,.2,1)` | OpenCode ×15 (UI) |
| `--atlas-ease-emphasized` | `cubic-bezier(.2,0,0,1)` | Grok ×11, OpenCode toast (layout) |
| `--atlas-ease-in` | `cubic-bezier(.4,0,1,1)` | OpenCode çıkışları |
| `--atlas-ease-in-out` | `cubic-bezier(.45,0,.55,1)` | OpenCode skeleton döngüsü |
| `--atlas-ease-spring` | `cubic-bezier(.34,1.56,.64,1)` | Grok pop |
| `--atlas-ease-spring-soft` | `cubic-bezier(.34,1.08,.64,1)` | OpenCode text-reveal |

Kurallar:
- Çıkış = girişin ~%70'i ve `ease-in` (ör. popover 160 → 110 ms).
- Yalnız `transform` ve `opacity` animasyon edilir. İstisnalar: collapsible
  `grid-template-rows` (aynı anda tek bir tane), sidebar `width` yerine
  `grid-template-columns`.
- **Reduced motion** (`:root[data-motion="reduce"]`, OS sorgusu veya
  Ayarlar > Görünüm > Hareket: Sistem / Tam / Azaltılmış):
  - translate/scale kaldırılır, opacity fade kalır ve ≤120 ms olur
    (Cursor'ın `scale-in → fade-in` yaklaşımı [CSS]).
  - Sonsuz döngüler durur: shimmer → statik `ink-2`; skeleton → sabit
    `surface-2`; spinner → 2 s `pulse-opacity` (dönmeyen nokta). Durum
    bilgisi metinle zaten var.
  - Streaming kelime fade kapalı; caret yanıp sönmez, sabit.
  - Marka anları atlanır (son kare gösterilir).

---

## 3. Animasyon kataloğu (yapıştırmaya hazır)

Hedef dosya: `web/src/styles/atlas-motion.css`. Seçiciler vendored
bileşenlerin `data-component`/`data-slot` adlarını ve yeni Atlas
bileşenlerini (`data-atlas="..."`) kullanır.

### 3.1 Buton hover / press

```css
[data-atlas-button],
button[data-component="button-v2"] {
  transition:
    background-color var(--atlas-dur-fast) var(--atlas-ease-standard),
    color var(--atlas-dur-fast) var(--atlas-ease-standard),
    box-shadow var(--atlas-dur-fast) var(--atlas-ease-standard),
    transform var(--atlas-dur-micro) var(--atlas-ease-standard);
  cursor: default;
}
[data-atlas-button]:hover:not(:disabled) { background-color: var(--atlas-hover); }
[data-atlas-button]:active:not(:disabled) { transform: scale(0.98); background-color: var(--atlas-press); }
[data-atlas-button][data-variant="primary"] { background: var(--atlas-ink); color: var(--atlas-surface); }
[data-atlas-button][data-variant="primary"]:hover:not(:disabled) {
  background: color-mix(in oklab, var(--atlas-ink) 88%, var(--atlas-surface));
}
:root[data-motion="reduce"] [data-atlas-button]:active { transform: none; }
```

### 3.2 İkon buton (28 × 28, ikon 16)

```css
[data-atlas-icon-button] {
  inline-size: 28px; block-size: 28px; border-radius: var(--atlas-radius-sm);
  display: inline-grid; place-items: center; color: var(--atlas-ink-3);
  transition: background-color var(--atlas-dur-fast) var(--atlas-ease-standard),
              color var(--atlas-dur-fast) var(--atlas-ease-standard),
              transform var(--atlas-dur-micro) var(--atlas-ease-standard);
}
[data-atlas-icon-button]:hover { background: var(--atlas-hover); color: var(--atlas-ink); }
[data-atlas-icon-button]:active { transform: scale(0.92); background: var(--atlas-press); }
[data-atlas-icon-button][data-size="sm"] { inline-size: 24px; block-size: 24px; } /* meta satırı */
```

### 3.3 Gönder butonu durum değişimi (pasif → aktif → durdur)

32 px daire. Pasif: `surface-3` zemin, `ink-4` ok. Aktif (metin var):
`ink` zemin, `surface` ok, spring pop. Streaming: ok → kare (durdur),
ikon morph (Grok morph-out blur+scale [CSS], Cursor `ui-icon-morph-in`
[CSS]). OpenCode'da gri ok butonunun çalışırken **siyah, yuvarlatılmış
kare bir durdur butonuna** dönüştüğü gözlendi [canlı-akış]; ara kareler
0.7 sn aralıklı olduğundan geçişin eğrisi görülmedi, morph spec'i CSS'ten
[CSS]. Atlas: buton şekli daire kalır (Atlas imzası), içindeki ikon
10 × 10 px radius 2 kareye morph eder; streaming süresince buton
`ink` zeminde sabit kalır (OpenCode ile aynı "koyu = durdurulabilir" dili).

```css
[data-atlas="send"] {
  inline-size: 32px; block-size: 32px; border-radius: var(--atlas-radius-pill);
  background: var(--atlas-surface-3); color: var(--atlas-ink-4);
  display: grid; place-items: center;
  transition: background-color var(--atlas-dur-base) var(--atlas-ease-standard),
              color var(--atlas-dur-base) var(--atlas-ease-standard),
              transform var(--atlas-dur-micro) var(--atlas-ease-standard);
}
[data-atlas="send"][data-state="ready"],
[data-atlas="send"][data-state="streaming"] {
  background: var(--atlas-ink); color: var(--atlas-surface);
  animation: atlas-pop var(--atlas-dur-moderate) var(--atlas-ease-spring);
}
[data-atlas="send"]:active:not(:disabled) { transform: scale(0.92); }
[data-atlas="send"] > svg { grid-area: 1 / 1; }
[data-atlas="send"] > svg[data-leaving] { animation: atlas-icon-out var(--atlas-dur-fast) var(--atlas-ease-in) forwards; }
[data-atlas="send"] > svg[data-entering] { animation: atlas-icon-in var(--atlas-dur-base) var(--atlas-ease-out) both; }

@keyframes atlas-pop { 0% { transform: scale(0.86); } 100% { transform: scale(1); } }
@keyframes atlas-icon-in { 0% { opacity: 0; transform: scale(0.94); filter: blur(2px); } 100% { opacity: 1; transform: none; filter: none; } }
@keyframes atlas-icon-out { 0% { opacity: 1; transform: none; filter: none; } 100% { opacity: 0; transform: scale(0.6); filter: blur(2px); } }

:root[data-motion="reduce"] [data-atlas="send"],
:root[data-motion="reduce"] [data-atlas="send"] > svg { animation: none !important; }
```

Durum makinesi: `disabled` (boş) → `ready` (metin/ek var) → `streaming`
(tıklama = durdur, Esc ile aynı) → `ready`/`disabled`. Streaming'de
basılı tutunca (600 ms) "Kuyruğa ekle" tooltip'i: yazılan mesaj bir
sonraki tura sıraya girer (OpenCode `session-queue-panel` deseni [CSS]).

### 3.4 Popover / tooltip / menü

```css
[data-atlas-pop],
[data-component="menu-v2-content"],
[data-component="popover-content"],
[data-component="tooltip-v2"] {
  transform-origin: var(--kb-popover-content-transform-origin, var(--kb-menu-content-transform-origin, center));
}
[data-atlas-pop][data-expanded],
[data-component="menu-v2-content"][data-expanded],
[data-component="popover-content"][data-expanded] {
  animation: atlas-pop-in var(--atlas-dur-base) var(--atlas-ease-out) both;
}
[data-atlas-pop][data-closed],
[data-component="menu-v2-content"][data-closed],
[data-component="popover-content"][data-closed] {
  animation: atlas-pop-out 110ms var(--atlas-ease-in) both;
}
[data-component="tooltip-v2"][data-expanded] { animation: atlas-pop-in 120ms var(--atlas-ease-out) both; }

@keyframes atlas-pop-in { 0% { opacity: 0; transform: scale(0.96); } 100% { opacity: 1; transform: scale(1); } }
@keyframes atlas-pop-out { 0% { opacity: 1; transform: scale(1); } 100% { opacity: 0; transform: scale(0.98); } }
/* Composer'dan açılan paneller (model, ek, mod) — OpenCode composer-pill-panel-in [CSS]. */
@keyframes atlas-panel-up { 0% { opacity: 0; transform: translateY(4px) scale(0.98); } 100% { opacity: 1; transform: none; } }

:root[data-motion="reduce"] :is([data-atlas-pop], [data-component="menu-v2-content"], [data-component="popover-content"], [data-component="tooltip-v2"]) {
  animation-name: atlas-fade-in !important; animation-duration: 100ms !important;
}
@keyframes atlas-fade-in { 0% { opacity: 0; } 100% { opacity: 1; } }
```

Zamanlama: tooltip açılış gecikmesi 500 ms; bir tooltip kapandıktan sonra
300 ms "sıcak pencere" içinde komşu tooltip gecikmesiz açılır. Hover card
(oturum satırı detayı, Cursor [canlı]) 450 ms gecikme, 320 px genişlik.

### 3.5 Diyalog

```css
[data-component="dialog-overlay"],
[data-atlas="dialog-overlay"] {
  background: var(--atlas-scrim);
  animation: atlas-fade-in 200ms var(--atlas-ease-standard) both;
}
[data-component="dialog-v2"][data-expanded],
[data-atlas="dialog"][data-expanded] {
  animation: atlas-dialog-in var(--atlas-dur-dialog) var(--atlas-ease-out) both;
}
[data-component="dialog-v2"][data-closed],
[data-atlas="dialog"][data-closed] {
  animation: atlas-dialog-out 150ms var(--atlas-ease-in) both;
}
@keyframes atlas-dialog-in { 0% { opacity: 0; transform: translateY(8px) scale(0.985); } 100% { opacity: 1; transform: none; } }
@keyframes atlas-dialog-out { 0% { opacity: 1; transform: none; } 100% { opacity: 0; transform: scale(0.985); } }
:root[data-motion="reduce"] :is([data-component="dialog-v2"], [data-atlas="dialog"]) { animation-name: atlas-fade-in !important; animation-duration: 120ms !important; }
```

Pencere kontrolleri karartmanın **üstünde** kalır (Grok [canlı]): titlebar
`z-index` > overlay; overlay titlebar'ın altından başlar (`inset-block-start:
var(--atlas-titlebar-h)`) ya da titlebar drag alanı overlay'de de aktif.

### 3.6 Collapsible (araç gövdesi, "Worked for", reasoning)

JS ölçümü gerektirmeyen `grid-template-rows` yöntemi (Grok [CSS]).
Kobalte collapsible'ı kullanan vendored yerlerde `--kb-collapsible-content-height`
ile `slideDown` (OpenCode [CSS]) aynı süre/eğriyle eşlenir.

```css
[data-atlas="collapse"] {
  display: grid; grid-template-rows: 0fr;
  transition: grid-template-rows var(--atlas-dur-moderate) var(--atlas-ease-emphasized);
}
[data-atlas="collapse"][data-open] { grid-template-rows: 1fr; }
[data-atlas="collapse"] > [data-slot="inner"] {
  min-block-size: 0; overflow: hidden; opacity: 0;
  transition: opacity 140ms var(--atlas-ease-standard);
}
[data-atlas="collapse"][data-open] > [data-slot="inner"] { opacity: 1; transition-delay: 40ms; }
[data-atlas="collapse"][data-size="large"] { transition-duration: var(--atlas-dur-slow); } /* >240px içerik */

[data-atlas="chevron"] { transition: transform 200ms var(--atlas-ease-emphasized); }
[aria-expanded="true"] > [data-atlas="chevron"] { transform: rotate(90deg); }      /* sağ → aşağı */
[aria-expanded="true"] > [data-atlas="chevron"][data-axis="y"] { transform: rotate(180deg); }

[data-component="collapsible"] [data-slot="collapsible-content"][data-expanded] {
  animation: atlas-slide-down var(--atlas-dur-moderate) var(--atlas-ease-emphasized);
}
[data-component="collapsible"] [data-slot="collapsible-content"][data-closed] {
  animation: atlas-slide-up 160ms var(--atlas-ease-in);
}
@keyframes atlas-slide-down { 0% { height: 0; opacity: 0; } 100% { height: var(--kb-collapsible-content-height); opacity: 1; } }
@keyframes atlas-slide-up { 0% { height: var(--kb-collapsible-content-height); opacity: 1; } 100% { height: 0; opacity: 0; } }

:root[data-motion="reduce"] [data-atlas="collapse"],
:root[data-motion="reduce"] [data-atlas="chevron"] { transition: none; }
:root[data-motion="reduce"] [data-component="collapsible"] [data-slot="collapsible-content"] { animation: none; }
```

Yüksekliği 600 px'i aşan gövdeler (uzun diff/terminal) açılışta önce
240 px'lik bir "önizleme" yüksekliğine animasyon yapar, kalan kısmı
"N satır daha" ile açılır (layout thrash'i sınırlar).

### 3.7 Mesaj girişi / çıkışı

```css
[data-atlas="turn"][data-enter] { animation: atlas-message-in 260ms var(--atlas-ease-out) both; }
[data-atlas="user-message"][data-enter] { animation: atlas-user-in 220ms var(--atlas-ease-out) both; transform-origin: 100% 100%; }
[data-atlas="turn"][data-leaving] { animation: atlas-message-out 160ms var(--atlas-ease-in) both; }

@keyframes atlas-message-in { 0% { opacity: 0; transform: translateY(8px); } 100% { opacity: 1; transform: none; } }
@keyframes atlas-user-in { 0% { opacity: 0; transform: translateY(6px) scale(0.99); } 100% { opacity: 1; transform: none; } }
@keyframes atlas-message-out { 0% { opacity: 1; transform: none; } 100% { opacity: 0; transform: translateY(-6px); } }
:root[data-motion="reduce"] [data-atlas="turn"],
:root[data-motion="reduce"] [data-atlas="user-message"] { animation-name: atlas-fade-in !important; animation-duration: 100ms !important; }
```

`data-enter` yalnızca **bu oturumda canlı gelen** turlara konur; geçmiş
yüklenirken (hydration, oturum açma, sanal liste geri kaydırma) animasyon
yok. Revert/geri alma: tur önce `atlas-message-out`, sonra yüksekliği
220 ms'de 0'a iner.

### 3.8 Streaming metin: kelime fade + "Çalışıyor" etiketi (+ opsiyonel caret)

Gözlenen [canlı-akış, OpenCode]: markdown **blok blok** belirir (başlık,
kalın, kod, liste öğesi ayrı ayrı); **en yeni kelimeler gri gelir ve tam
metin rengine koyulaşır**; sayfa otomatik kayar, konuşmanın tepesi yumuşak
bir fade altına girer; son bloğun altında shimmer'lı "Çalışıyor" etiketi
durur. **Caret gözlenmedi.** Grok'ta yanıt parça parça (chunk) ve tek
balon olarak geldi, kelime fade'i görülmedi [canlı-akış].
CSS karşılığı birebir [CSS]: `[data-markdown-word][data-markdown-enter]`
`animation: .18s ease-out both markdown-word-enter` (opacity 0 → 1;
beyaz zeminde ara kareler "gri → koyu" görünür). Vendored markdown
bileşeni bunu zaten üretir; Atlas süre/eğriyi bağlar. Varsayılan: caret
**yok**, OpenCode gibi son bloğun altında `TextShimmer("Çalışıyor")`.
Caret yalnız Ayarlar > Sohbet > "Yazma imleci" açıkken (aşağıdaki CSS) [çıkarım].

```css
[data-component="markdown"] :is([data-markdown-word], code)[data-markdown-enter] {
  animation: atlas-word-in 180ms var(--atlas-ease-standard) both;
}
@keyframes atlas-word-in { 0% { opacity: 0; } 100% { opacity: 1; } }

/* Caret (opsiyonel, html[data-caret]): yalnız son blok streaming iken; 2px x 1.05em, ink-2. */
:root[data-caret] [data-atlas="turn"][data-streaming] [data-component="markdown"] > :last-child::after {
  content: ""; display: inline-block; inline-size: 2px; block-size: 1.05em;
  margin-inline-start: 2px; vertical-align: -0.15em; border-radius: 1px;
  background: var(--atlas-ink-2);
  animation: atlas-caret 1s steps(1, end) infinite;
}
/* Token akışı 400ms'den kısa aralıklarla geliyorsa caret sabit görünür (yanıp sönme yalnız beklemede). */
:root[data-caret] [data-atlas="turn"][data-streaming][data-flowing] [data-component="markdown"] > :last-child::after { animation: none; }

/* Otomatik kaydırmada tepe fade'i [canlı-akış]: kaydırma alanının üst 24px'i. */
[data-atlas="thread-scroll"] {
  mask-image: linear-gradient(to bottom, transparent 0, #000 24px, #000 calc(100% - 48px), transparent 100%);
}
@keyframes atlas-caret { 0%, 100% { opacity: 1; } 50% { opacity: 0; } }

:root[data-motion="reduce"] [data-component="markdown"] [data-markdown-enter] { animation: none; }
:root[data-motion="reduce"] [data-atlas="turn"][data-streaming] [data-component="markdown"] > :last-child::after { animation: none; }
```

Performans kuralı: aynı anda en fazla 64 kelime span'i `data-markdown-enter`
taşır; `animationend`'de attribute kaldırılır. Kod bloğu streaming'i
kelime değil satır bazında fade eder (shiki stream).

### 3.9 Düşünme (thinking) shimmer

Vendored `TextShimmer` (per-char, `--text-shimmer-step: 45ms`, 1.2 s,
spread 5.2ch [CSS]) "Düşünüyor", "Çalışıyor", aktif araç fiilleri için
kullanılır. Atlas token bağlaması + hafif CSS-only varyant (sidebar ve
sekme gibi yoğun yerler için, per-char DOM maliyeti olmadan):

```css
[data-component="text-shimmer"] {
  --text-shimmer-duration: var(--atlas-dur-loop-shimmer);
  --text-shimmer-base-color: var(--atlas-ink-3);
  --text-shimmer-peak-color: var(--atlas-ink);
}
[data-atlas="shimmer-lite"] {
  background: linear-gradient(90deg, var(--atlas-ink-3) 0%, var(--atlas-ink-3) 40%, var(--atlas-ink) 50%, var(--atlas-ink-3) 60%, var(--atlas-ink-3) 100%);
  background-size: 250% 100%; background-clip: text; -webkit-background-clip: text; color: transparent;
  animation: atlas-shimmer var(--atlas-dur-loop-shimmer) linear infinite;
}
@keyframes atlas-shimmer { 0% { background-position: 100% 0; } 100% { background-position: -150% 0; } }
:root[data-motion="reduce"] [data-atlas="shimmer-lite"] { animation: none; background: none; color: var(--atlas-ink-2); }
```

Düşünme tamamlanınca etiket "Düşündü 8 sn" olur (Cursor "Thought 8s"
[canlı]): fiil `ink-2` 530, süre `ink-3`. Geçiş vendored
`tool-status-title` active/done morph'u ile (blur 0.9 px, 160 ms).

### 3.10 Yükleyiciler, iskelet, ilerleme

```css
/* Spinner: OpenCode loader-v2 ile aynı hız [CSS]. */
[data-atlas="spinner"] { animation: atlas-spin var(--atlas-dur-loop-spin) linear infinite; color: var(--atlas-ink-3); }
@keyframes atlas-spin { to { transform: rotate(360deg); } }

/* Üç nokta (bekliyor/bağlanıyor). */
[data-atlas="dots"] > i { inline-size: 4px; block-size: 4px; border-radius: 50%; background: currentColor; display: inline-block;
  animation: atlas-dot 1.2s var(--atlas-ease-in-out) infinite; }
[data-atlas="dots"] > i:nth-child(2) { animation-delay: 0.15s; }
[data-atlas="dots"] > i:nth-child(3) { animation-delay: 0.3s; }
@keyframes atlas-dot { 0%, 80%, 100% { opacity: 0.3; transform: translateY(0); } 40% { opacity: 1; transform: translateY(-2px); } }

/* İskelet: mask sweep, OpenCode home-session-skeleton [CSS]. */
[data-atlas="skeleton"] {
  background: var(--atlas-surface-2); border-radius: var(--atlas-radius-sm);
  mask-image: linear-gradient(90deg, #000 30%, rgb(0 0 0 / 0.55) 50%, #000 70%);
  mask-size: 300% 100%;
  animation: atlas-skeleton var(--atlas-dur-loop-skeleton) var(--atlas-ease-in-out) infinite;
}
@keyframes atlas-skeleton { 0% { mask-position: 100% 0; } 100% { mask-position: 0 0; } }

/* Belirsiz ilerleme: 35% dolgu, 1.3s [CSS]. 2px yükseklik. */
[data-atlas="progress"] { block-size: 2px; background: var(--atlas-line-2); overflow: hidden; border-radius: 1px; }
[data-atlas="progress"] > [data-slot="fill"] { block-size: 100%; background: var(--atlas-ink-2);
  transition: inline-size var(--atlas-dur-slow) var(--atlas-ease-emphasized); }
[data-atlas="progress"][data-indeterminate] > [data-slot="fill"] { inline-size: 35%; animation: atlas-indeterminate 1.3s var(--atlas-ease-in-out) infinite; }
@keyframes atlas-indeterminate { 0% { transform: translateX(-100%); } 100% { transform: translateX(300%); } }

/* Context ring (oc/context-ring.tsx): stroke-dashoffset 400ms emphasized; %80 üstü warning, %95 danger. */

:root[data-motion="reduce"] [data-atlas="spinner"] { animation: atlas-pulse 2s var(--atlas-ease-in-out) infinite; }
:root[data-motion="reduce"] :is([data-atlas="dots"] > i, [data-atlas="skeleton"]) { animation: none; }
:root[data-motion="reduce"] [data-atlas="progress"][data-indeterminate] > [data-slot="fill"] { animation: atlas-pulse 2s var(--atlas-ease-in-out) infinite; inline-size: 100%; }
@keyframes atlas-pulse { 0%, 100% { opacity: 0.4; } 50% { opacity: 1; } }
```

### 3.11 Toast

Konum: sağ alt, composer'ın üstünde değil; 16 px kenar boşluğu, 320 px
genişlik, en fazla 3 görünür, fazlası "+2" yığını. `solid-sonner` zaten
bağımlılıkta; OpenCode `.toast-v2` geçişleri [CSS] referans.

```css
.toast-v2, [data-atlas="toast"] {
  border-radius: var(--atlas-radius-md); background: var(--atlas-surface); box-shadow: var(--atlas-shadow-floating);
  transition: transform var(--atlas-dur-slow) var(--atlas-ease-emphasized),
              opacity var(--atlas-dur-base) var(--atlas-ease-standard),
              height var(--atlas-dur-slow) var(--atlas-ease-emphasized);
}
[data-atlas="toast"][data-enter] { animation: atlas-toast-in var(--atlas-dur-slow) var(--atlas-ease-emphasized) both; }
[data-atlas="toast"][data-leaving] { animation: atlas-toast-out var(--atlas-dur-base) var(--atlas-ease-in) both; }
@keyframes atlas-toast-in { 0% { opacity: 0; transform: translateY(8px) scale(0.98); } 100% { opacity: 1; transform: none; } }
@keyframes atlas-toast-out { 0% { opacity: 1; transform: none; } 100% { opacity: 0; transform: translateX(12px); } }
:root[data-motion="reduce"] :is(.toast-v2, [data-atlas="toast"]) { transition: opacity 100ms linear !important; animation-name: atlas-fade-in !important; }
```

Süreler: bilgi 4 s, başarı 3 s, hata kalıcı (kapat butonu). Hover süreyi
durdurur. Hata toast'ı aynı anda `aria-live="assertive"`, diğerleri `polite`.

### 3.12 Sekme ve sidebar geçişleri

```css
/* Sidebar: genişlik yerine grid kolonu animasyonu. */
[data-atlas="window"] {
  display: grid; grid-template-columns: var(--atlas-sidebar-w) minmax(0, 1fr) var(--atlas-rail-open, 0px);
  transition: grid-template-columns var(--atlas-dur-slow) var(--atlas-ease-emphasized);
}
[data-atlas="window"][data-sidebar="closed"] { grid-template-columns: 0px minmax(0, 1fr) var(--atlas-rail-open, 0px); }
[data-atlas="window"][data-rail="open"] { --atlas-rail-open: var(--atlas-rail-w); }
[data-atlas="window"][data-resizing] { transition: none; } /* sürüklerken anlık */
[data-atlas="sidebar"] > * { transition: opacity var(--atlas-dur-fast) var(--atlas-ease-standard); }
[data-atlas="window"][data-sidebar="closed"] [data-atlas="sidebar"] > * { opacity: 0; }

/* Sekme: aktif zemin crossfade, kapanınca genişlik çöker. */
[data-slot="titlebar-tab-item"] { transition: background-color var(--atlas-dur-fast) var(--atlas-ease-standard); }
[data-slot="titlebar-tab-item"][data-enter] { animation: atlas-tab-in var(--atlas-dur-moderate) var(--atlas-ease-out) both; }
[data-slot="titlebar-tab-item"][data-leaving] { animation: atlas-tab-out var(--atlas-dur-base) var(--atlas-ease-in) both; overflow: hidden; }
@keyframes atlas-tab-in { 0% { opacity: 0; max-inline-size: 0; } 100% { opacity: 1; max-inline-size: 240px; } }
@keyframes atlas-tab-out { 0% { opacity: 1; max-inline-size: 240px; } 100% { opacity: 0; max-inline-size: 0; } }

/* Sağ ray sekme içerikleri arası: 120ms crossfade, kaydırma yok. */
[data-atlas="rail-panel"][data-enter] { animation: atlas-fade-in var(--atlas-dur-fast) var(--atlas-ease-standard) both; }

:root[data-motion="reduce"] [data-atlas="window"] { transition: none; }
:root[data-motion="reduce"] [data-slot="titlebar-tab-item"] { animation: none !important; }
```

Kenar fade'leri: `titlebar.css`'teki scroll-timeline tabanlı sekme fade'i
aynen korunur.

### 3.13 Bağlam sıkıştırma (compaction) anı

OpenCode gather/glint'in [CSS] Atlas sürümü: bir ayraç çizgisi üzerinde
soldan sağa kayan ışık + etiket üzerinde tek parıltı, 3.2 s döngü,
işlem bitince tek kez `fade`.

```css
[data-atlas="compaction"] {
  --cycle: 3.2s; display: grid; grid-template-columns: 1fr auto 1fr; align-items: center; gap: 12px;
  color: var(--atlas-ink-3); font: 440 var(--atlas-text-small) / var(--atlas-lh-small) var(--atlas-font-sans);
  margin-block: 16px; animation: atlas-fade-in 320ms var(--atlas-ease-standard) both;
}
[data-atlas="compaction"] > [data-slot="line"] { block-size: 1px; background: var(--atlas-line-2); position: relative; overflow: hidden; }
[data-atlas="compaction"][data-running] > [data-slot="line"]::after {
  content: ""; position: absolute; inset: 0; inline-size: 40%;
  background: linear-gradient(90deg, transparent, var(--atlas-ember-500), transparent);
  animation: atlas-compaction-gather var(--cycle) linear infinite backwards;
}
[data-atlas="compaction"][data-running] > [data-slot="label"] { animation: atlas-compaction-glint var(--cycle) linear infinite backwards; }
@keyframes atlas-compaction-gather {
  0% { opacity: 0; transform: translateX(-100%); animation-timing-function: cubic-bezier(.4,0,.6,1); }
  14%, 49.5% { opacity: 1; }
  70%, 100% { opacity: 0; transform: translateX(250%); }
}
@keyframes atlas-compaction-glint {
  0%, 49% { color: var(--atlas-ink-3); }
  57% { color: var(--atlas-ink); }
  82%, 100% { color: var(--atlas-ink-3); }
}
:root[data-motion="reduce"] [data-atlas="compaction"] * { animation: none !important; }
```

Metin: çalışırken "Bağlam sıkıştırılıyor…", bitince "Bağlam sıkıştırıldı ·
128k → 22k token" + "Özeti gör" linki (özet bir collapsible'da).

### 3.14 İlk açılış / marka anı

1. İlk kurulum (yalnız bir kez, `localStorage` + ayar): işaret ortada 64 px,
   stroke-draw 1000 ms ease-out (OpenCode `first-launch-wordmark-draw`
   tekniği [CSS], `pathLength="1"`). Yay (gök) 200 ms gecikmeyle çizilir.
2. 300 ms bekle, işaret 600 ms'de titlebar'daki 16 px yerine "uçar"
   (FLIP, `ease-emphasized`), onboarding kartı `atlas-dialog-in` ile gelir.
3. Yeni oturum (home): wordmark üzerinde tek Ember mask taraması 1.4 s
   (`new-session-wordmark-shimmer` deseni [CSS]), sonra statik.

```css
[data-atlas="mark-draw"] path { stroke-dasharray: 1; stroke-dashoffset: 1; animation: atlas-draw var(--atlas-dur-brand) var(--atlas-ease-out) forwards; }
[data-atlas="mark-draw"] path[data-slot="sky"] { animation-delay: 200ms; }
@keyframes atlas-draw { to { stroke-dashoffset: 0; } }

[data-atlas="wordmark-sweep"] {
  -webkit-mask-image: linear-gradient(100deg, transparent 35%, #000 50%, transparent 65%);
  mask-image: linear-gradient(100deg, transparent 35%, #000 50%, transparent 65%);
  mask-size: 300% 100%; background: var(--atlas-ember-500);
  animation: atlas-wordmark-sweep 1.4s var(--atlas-ease-in-out) 1 both;
}
@keyframes atlas-wordmark-sweep { 0% { opacity: 0; mask-position: 100% 0; } 20%, 80% { opacity: 0.9; } 100% { opacity: 0; mask-position: 0 0; } }
:root[data-motion="reduce"] :is([data-atlas="mark-draw"] path, [data-atlas="wordmark-sweep"]) { animation: none; stroke-dashoffset: 0; opacity: 0; }
```

### 3.15 Diğer küçük anlar

| An | Spec |
|---|---|
| Aşağı kaydır butonu (32 px daire) | Görünür olurken `atlas-pop` 220 ms spring; kaybolurken 120 ms fade. 120 px'ten fazla yukarıdaysa görünür. |
| Composer üstü yüzen çipler | Her çip `atlas-panel-up` 160 ms, 30 ms stagger; sayı değişimi `animated-number` (vendored, 280 ms width). |
| Sürükle-bırak bölgesi | Mevcut `session-dropzone` şerit kayması korunur; renk `--atlas-ember-soft`. |
| Okunmamış noktası | 6 px `--atlas-info`, giriş `atlas-pop` 220 ms. |
| Çalışıyor noktası (sidebar) | 6 px `--atlas-ember-600`, halka ripple: `box-shadow 0 0 0 0 → 0 0 0 5px transparent`, 1.6 s döngü (Cursor `composer-status-ripple` [CSS]). |
| İzin bekliyor | Sekme ve sidebar satırında `--atlas-warning` noktası, ripple yok (dikkat dağıtmaz), pencere odakta değilse OS bildirimi + taskbar flash. |
| Review vurgusu | Değişen dosyaya atlayınca 1.5 s tek halka pulse (`review-changes-border-pulse` [CSS]) Ember ile. |

---

## 4. Sohbet tasarımı

### 4.1 Mesaj düzeni

```
┌ sol tick scrubber (24px) ┐┌──────── 720px sohbet sütunu ────────┐┌ sağ ray ┐
│ ╴ ╴ ╴ ━ ╴ ╴              ││        [kullanıcı balonu — sticky] ↶ ││ Changes │
│                          ││ ⌄ 2 dk 34 sn çalıştı               ││ Browser │
│                          ││   Düşündü 8 sn                      ││ Terminal│
│                          ││   <ara metin>                       ││ Files   │
│                          ││   Düzenledi 3 dosya, 9 dosya ...  ⌄ ││         │
│                          ││ <final yanıt, düz metin, 14/22>     ││         │
│                          ││ 👍 👎 ⧉ ⑂ ↻  · 1 sa önce · model    ││         │
│                          ││ ┌ 3 dosya değişti ────── İncele ┐   ││         │
│                          ││ └ content.go           +5 −10  ┘   ││         │
└──────────────────────────┘└─────────────────────────────────────┘└─────────┘
```

- **Kullanıcı mesajı**: üç referans üç farklı yol izliyor: Cursor tam
  genişlik koyu kart [canlı], OpenCode **sağa hizalı açık gri balon**
  (radius ~12, max ~570 px) [canlı-akış], Grok **siyah pill, beyaz metin,
  sağa hizalı**, radius ~18 [canlı-akış]. Atlas (sessiz mod): OpenCode
  gibi sağa hizalı `surface-2` balon, radius 12, padding 10/14, max
  560 px, 14/22. Turun tepesine **sticky** (`position: sticky; top: 8px`);
  tur kaydıkça balon **tek satırlık şeride küçülür** ("… dosyaları
  listele", 32 px, ellipsis) [canlı-akış, OpenCode; Cursor'da da sticky
  kart [canlı]]; küçülme 220 ms `ease-emphasized` (`max-height` yerine
  `grid-template-rows`), şerite tıklama turun başına kaydırır. Persona
  modunda (4.7) Grok'a benzer koyu pill: `ink` zemin, `surface` metin,
  radius 18.
  Sağda 24 px ikon butonu: "Buradan geri al" (revert, ↶) ve hover'da
  "Düzenle" (kalem). Ekler kartın içinde 48 px küçük resim / dosya çipi.
  Uzun mesaj 6 satırda kesilir, "Devamını göster".
- **Asistan metni**: balonsuz, düz (Cursor/OpenCode [canlı]); kodlama
  bağlamında balon okumayı bozar. Grok tarzı gri balon **yalnız** "Dots /
  persona" modunda ve kısa sohbet yanıtlarında (`data-density="chat"`).
- **Avatar yok** (üç referansın ortak kararı [canlı]). Ajan kimliği
  oturum başlığındaki pill'de. Tur içinde ajan değişirse (alt ajan,
  `task`, takım ajanı) turun başında 16 px ajan glyph'i + ad satırı.
- **Gruplu mesajlar**: aynı rolden ardışık bloklar arası 12 px; ayrı
  turlar arası 24 px; Dots modunda balonlar arası 2 px (Grok [canlı]).
- **Zaman damgası**: tur meta satırında göreli ("1 sa önce", hover'da tam
  tarih). 30 dk'dan uzun aralıklarda sütun ortasında küçük `ink-3` gün/saat
  ayracı ("Dün 18:35", Grok [canlı]).
- **Sistem notları** (ad değişti, model değişti, izin verildi): sağa
  hizalı değil, sütun ortası 12 px `ink-3` tek satır (Grok "Adı Fıstık
  olarak değiştirildi" [canlı] deseni, ama ortalanmış).

### 4.2 Tur yapısı: "çalıştı" katlaması [canlı, Cursor] + canlı akış [canlı-akış, OpenCode]

- **200 ms kuralı** [canlı-akış]: gönder'e basıldıktan sonra 200 ms içinde
  ekranda bir şey olmalı. OpenCode: gönder → durdur + "Çalışıyor" shimmer;
  Grok: "Hemen bakıyorum." balonu. Atlas sessiz modda istemci tarafında
  anında `TextShimmer("Çalışıyor")` satırı + gönder → durdur; persona
  modunda maskot "dinledi" ifadesine geçer + durum metni "Bakıyorum…".
  **Sahte asistan metni üretilmez**: anlık ack, model çıktısı değil,
  durum satırıdır (Grok'un balonu model çıktısı gibi görünüyor; Atlas'ta
  dürüstlük için durum satırı stilinde, balon değil).
- Tur çalışırken: OpenCode'da gözlendiği gibi **anlatım paragrafları
  araç satırlarının arasına girer** ("Web araması ilk denemede başarısız
  oldu; resmi sürüm sayfasını çekmeyi deneyeyim.") [canlı-akış]; en altta
  `TextShimmer("Çalışıyor")` + canlı süre ("0:42", `tabular-nums`) + o anki
  aktivite fiili ("· go.dev okunuyor"). Ara adımlar açık akar.
- Tur bitince başlık "2 dk 34 sn çalıştı ⌄" olur ve ara adımlar (düşünme,
  araç satırları, ara metinler) **otomatik katlanır**; final yanıt metni ve
  "Dosya değişti" kartı açık kalır. Chevron yalnız hover'da görünür
  (Cursor [canlı]), klavye focus'unda da görünür.
- Katlı halde bile **özet satırı** görünür: "Düzenledi 3 dosya, 9 dosya
  inceledi, 7 arama, 1 komut çalıştırdı +21 −71" (fiil `ink-2` 530, kalan
  `ink-3`, +/− `diff` renkleri). Özet satırına tıklamak araç listesini
  açar.
- Kısa turlar (< 3 araç çağrısı ve < 15 sn) katlanmaz.
- Ayar: Ayarlar > Sohbet > "Tamamlanan turları katla" (varsayılan açık).
  Mevcut `prefs().showReasoning` → `reasoningMode` ile birlikte çalışır.

### 4.3 Tur meta satırı ve geri bildirim

24 px yükseklik, 24 px ikon butonları (ikon 14 px), aralık 2 px:
`👍 Beğen` · `👎 Beğenme` · `⧉ Kopyala` (markdown) · `⑂ Buradan çatalla`
(fork) · `↻ Yeniden dene` · ayraç · `1 sa önce` · hover'da `model · 12.4k
token · $0.03 · 38 sn`. Son turda her zaman görünür, eski turlarda tur
hover/focus'unda `opacity 0 → 1` 120 ms. Beğenmeme tıklanınca satırın
altında 3 hızlı neden çipi ("Yanlış", "Talimata uymadı", "Çok yavaş") +
serbest metin; gönderim `atlas-panel-up`. Kopyalandı: ikon `check`'e
morph (3.3 ikon morph), 1.2 s sonra geri.

### 4.4 Chat ikon / avatar sistemi

- **İkon seti**: vendored katalog (`vendor/atlas-ui/icons/catalog.ts` +
  `additional-icons.ts`) temel alınır: 16 px grid, 1.5 px stroke (outline
  ailesi). Yeni ikonlar aynı ölçülerle **Atlas'a özgü** çizilir (yeni dosya
  `web/src/oc/app/icons/atlas-icons.ts`):
  `atlas-mark` (A·Omuz), `thinking` (yay + 3 nokta), `worked` (saat
  çeyreği), `sources` (iki üst üste daire), `citation`, `compaction`
  (iki ok içe), `browser-frame`, `computer-cursor`, `routine` (döngü ok),
  `node-remote` (Atlas remote node), `voice-wave` (Dots ile ortak).
- **Ajan glyph'leri** (avatar yerine, 16/20 px, `currentColor`): `build`
  (çekiç-ok), `plan` (pusula), `explore` (dürbün), `review` (büyüteç-onay),
  `task` (vendored `task`), takım ajanları için isim baş harfi daire
  (`--v2-avatar-bg-*` paleti mevcut). Agent renkleri mevcut
  `--v2-agent-*-solid` token'larından.
- **Durum rozetleri** (ikonun sağ altına 8 px, 1.5 px `surface` halka):
  çalışıyor (Ember nokta + ripple), bekliyor (warning nokta), hata
  (danger nokta), tamamlandı-okunmamış (info nokta), uzak node (küçük
  bulut). Proje avatarlarında (`project-avatar-state.ts`) aynı sistem.

### 4.5 Boş durumlar

- **Home (yeni oturum)**: wordmark (monokrom, 3.14 sweep) + composer +
  altında 3 ipucu pill'i (Cursor [canlı] deseni, Atlas içeriği):
  `Planla  ⇧Tab`, `Kodu incele`, `Rutin kur`. Altta tek satır ipucu
  link (OpenCode [canlı] "75+ sağlayıcı" deseni): "Sağlayıcı bağla ›"
  yalnız hiç sağlayıcı yoksa; varsa "Son oturuma dön ›".
- **Proje yok**: 28/34 başlık "Bir klasörle başla", iki büyük kart
  (Klasör aç Ctrl+O / Git deposu klonla), sürükle-bırak bölgesi.
- **Arama sonuçsuz** (palet): ortada 16 px ikon + "Sonuç bulunamadı" +
  "Tüm oturumlarda ara" linki.
- **Sağ ray boş** (Changes): "Henüz değişiklik yok" + mevcut
  `session-review-empty-*` bileşenleri.

### 4.6 Inline kartlar

Hepsi: `surface` zemin, `line-2` 1 px kenar, radius 12, padding 12/14,
sütun genişliği, `atlas-message-in` ile girer.

- **İzin kartı/dock'u** (`session-permission-dock.tsx` genişletilir):
  OpenCode'da izin gereken araçta **composer kaybolur, yerine aynı
  konumda kenarlıklı bir kart (radius ~14) gelir**: başlık ("Üçüncü taraf
  web araması"), gri açıklama, kapsam dropdown'u ("Herhangi biri ▾"),
  ikincil metin butonu ("Aramayı kullanma") + koyu pill birincil
  ("Etkinleştir") [canlı-akış]. Modal değil, sohbet okunabilir kalır.
  Atlas aynı yeri kullanır: composer → izin dock'u geçişi 220 ms
  crossfade + yükseklik (`grid-template-rows`), dock kapanınca composer
  taslağıyla geri gelir. Timeline'da çözüldükten sonra tek satırlık iz
  bırakır ("İzin verildi · bash `go test ./...`").
  Anatomi: üst satır kategori ikonu (`permission/category.go`:
  read_only / edit / execute / network → glasses / pencil-line / console /
  outline-globe) + başlık "Atlas bu komutu çalıştırmak istiyor" (13/16 530);
  gövde mono önizleme (12.5/20, `surface-1`, radius 8, en fazla 6 satır,
  fazlası fade + "Tamamını göster"); edit için mini diff; network için
  host çipi. Alt satır, OpenCode düzeninde [canlı-akış]: solda kapsam
  dropdown'u (`Bir kez ▾` / bu oturum / bu proje / bu komut deseni),
  sağda `Reddet` (metin buton, Esc) + `İzin ver` (koyu `ink` pill, Enter;
  etiket seçili kapsama göre "Bu oturumda izin ver" olur). Klavye `1/2/3`
  kapsamı seçer. Hook'lar izinden önce çalıştığı için
  bir hook kararı varsa kartın altında `ink-3` satır: "Hook: allow
  (pre-commit-guard)". Bekleme süresi > 60 sn ise kenar `warning-line`.
- **Soru kartı** (`session-question-dock.tsx`): Grok soru kartı [canlı]
  deseni: soru metni `ink-2`, altında seçenekler 32 px satırlar (harf
  rozeti `A/B/C` 18 px kare radius 4 + metin), serbest metin satırı;
  seçilen satır `ember-soft` zemin. Çoklu soru: üstte segment ilerleme
  (mevcut `question-progress-segment`).
- **Connector kartı** (Grok [canlı] deseni, Atlas içeriği: MCP sunucusu,
  entegrasyon, OAuth hesap): 40 px logo kutusu radius 10, ad + doğrulanmış
  rozet (14 px `info`), açıklama `ink-3`, sağda pill `Bağlan` (primary
  ink), altında 12 px `ink-3` güven notu ("Atlas değişiklik yapmadan önce
  her zaman sorar."). Bağlanırken buton → spinner + "Bağlanıyor"; bitince
  `check` morph + "Bağlandı".
- **Hata kartı**: vendored `tool-error-card` + sağlayıcı hataları için
  "Yeniden dene" + "Modeli değiştir"; rate limit'te geri sayım halkası.
- **Plan / todo kartı**: mevcut `todos` aracı; tur içinde değil, composer
  üstünde sticky "Plan 3/7" çipi, açılınca liste (Cursor todo tray [CSS]).

### 4.7 İki sunum modu: Sessiz satırlar vs Persona (Dots maskot durumu)

Canlı akışta iki geçerli felsefe görüldü [canlı-akış]: OpenCode/Cursor
araçları **sessiz metin satırları** olarak gösterip açılabilir bırakıyor;
Grok araçları **tamamen gizleyip** bir maskot + değişen durum cümlesiyle
anlatıyor. Atlas'ın iki kitlesi var (kodlama ajanı ve Dots persona
ajanları), o yüzden ikisini de **aynı tur veri modelinden** sunarız.
Oturum başına seçilir (`session.presentation = "quiet" | "persona"`),
Dots ajanıyla açılan oturumlar varsayılan persona, proje oturumları
varsayılan sessiz.

| | **Sessiz (quiet)** | **Persona** |
|---|---|---|
| Kitle | Kod, proje oturumları | Dots ajanları, kişisel asistan, sesli |
| Kullanıcı mesajı | Sağda `surface-2` balon (OpenCode) | Sağda `ink` pill, radius 18 (Grok benzeri) |
| Asistan metni | Balonsuz düz metin | `surface-2` balon, radius 14, ardışık balonlar 2 px |
| Araçlar | Satırlar (5.1 düz/kart yoğunluk) | **Gizli**; yerine maskot + durum cümlesi. Tur sonunda `ink-3` "Ayrıntılar · 4 adım" linki sessiz satırları açar |
| Çalışıyor göstergesi | `TextShimmer("Çalışıyor")` + süre | 26 px maskot + değişen durum metni (pulse) |
| Kaynaklar | Atıf çipleri + kaynak çipi | Yalnız metin içi favicon'lu atıf linki (Grok) |
| Anlık ack | Durum satırı | Maskot ifadesi + "Bakıyorum…" durum satırı |
| İzin | Dock (composer yerine) | Aynı dock, metni insan diliyle ("Gmail'den bir e-posta göndermek istiyorum") |

**Persona maskotu** (Atlas'a ait: `web/src/dots/Avatar.tsx` `LiveAvatar`,
8 şekil `orb/pebble/bloom/hex/drop/cloud/star/shield`, kendi göz/kırpma
fiziği; Grok'un siyah daire maskotu kopyalanmaz). Bugünkü `AvatarState`
(`idle|listening|thinking|speaking|working|waiting|sleeping`) şu
faz eşlemesiyle genişletilir (Grok'ta faz başına ifade değiştiği gözlendi
[canlı-akış]; Atlas ifadeleri [çıkarım]):

| Faz (araç kategorisi, 5.3) | `AvatarState` | İfade | Durum metni (TR) |
|---|---|---|---|
| gönderildi, ilk token yok | `listening` (yeni değil) | gözler composer'a bakar, tek kırpma | "Bakıyorum…" |
| thinking | `thinking` | gözler yukarı-yana, yavaş kırpma | "Düşünüyor" |
| web-search | **`searching`** (yeni) | gözler sola-sağa tarar, 1.2 s döngü | "Web'de arıyor" |
| fetch / read | **`reading`** (yeni) | gözler hafif aşağı, satır satır kayma | "Okuyor" / "go.dev'i okuyor" |
| edit / write / metin akışı | **`writing`** (yeni) | gözler kısık (`lid 0.6`) | "Yazıyor…" |
| shell / computer / browser | `working` | gözler hızlı küçük sıçrama | "Bilgisayarında çalışıyor" |
| izin / soru bekliyor | `waiting` | gözler kullanıcıya, `--warn` halka | "Onayını bekliyor" |
| hata | `waiting` + `worried` | düşük göz kapakları | "Bir sorun çıktı" |
| bitti | `idle` + `happy` (1.2 s) | mutlu sıçrama, sonra idle | (metin kaybolur) |

- **Konum:** (1) sidebar'daki ajan karosu (48 px), (2) son balonun altında
  inline 26 px maskot + durum metni, sola hizalı, 8 px boşluk [canlı-akış,
  Grok], (3) başlık pill'inde 20 px.
- **Durum metni geçişi:** eski metin `opacity 1 → 0, translateY(0 → -4px)`
  120 ms `ease-in`; yeni metin `translateY(4px → 0)` + fade 180 ms
  `ease-out`. Metin 13/16 `ink-3`, `atlas-pulse` 2 s (shimmer değil,
  persona tonu daha sakin). Aynı metin < 1.5 sn içinde tekrar değişmez
  (titreme önleme: faz değişimleri 600 ms debounce).
- **Reduced motion:** maskot statik ifade karesi (göz yönü durumu anlatır),
  durum metni crossfade 100 ms, döngüler yok.
- Uygulama: `LiveAvatar`'ı Dots bundle'ından paylaşılan bir modüle taşı
  (`web/src/shared/avatar/`), faz → durum eşlemesi `tool-row-model.ts`
  (Faz 3) ile ortak kategori tablosunu kullanır.

---

## 5. Araç çağrısı tasarımı

### 5.1 İki yoğunluk modu (öneri: evet, gerekçeli)

Gerekçe: Atlas'ın 100'den fazla aracı var (`internal/agent/tools/*.md`) ve
uzun ajan turları sık. Cursor'ın düz satırları [canlı] 30+ çağrılık bir
turu tek ekranda okutuyor; OpenCode'un kartları [CSS] az ama ağır
çağrılarda (diff, shell çıktısı) daha zengin. İkisi aynı veri modelinden
render edilebildiği için maliyet düşük: tek bir `ToolRowModel`, iki görünüm.

| | **Düz (`plain`, varsayılan)** | **Kart (`card`)** |
|---|---|---|
| Esin | Cursor [canlı] | OpenCode [CSS] |
| Satır | 24 px, ikon yok, `Fiil` 530 `ink-2` + `argüman` 440 `ink-3` + sağda meta | 28 px tetik, 16 px kategori ikonu + başlık + alt başlık, collapsible gövde, sol ray çizgisi (12 px girinti) |
| Gruplama | Ardışık okuma/arama → "İnceledi 2 dosya, 2 arama" | `context-tool-group` (vendored) |
| Çalışan durum | Fiil shimmer + süre | Spinner + shimmer + süre |
| Gövde | Yalnız tıklanınca; edit/shell/web için kart gövdesi açılır | Varsayılan kapalı, hata otomatik açık |

Ayar: Ayarlar > Sohbet > "Araç satırı yoğunluğu: Düz / Kart"
(`<html data-tool-density>`). İki modda da düzenleme (edit/write/patch),
shell ve web gövdeleri aynı bileşenler. Bu yoğunluk ayarı yalnız sessiz
sunum modunda geçerlidir; persona modunda araçlar gizlidir (4.7).

**Gözlenen grup satırı anatomisi** [canlı-akış, OpenCode]: tek satır,
ikon/kutu yok: `[soluk önek] [sayı] [koyu başlık]`, ör. soluk "İkinci el"
+ "2" + koyu "Web araması ve Kabuk"; ayrı türde iş yeni satır açar
("1 Web içeriğini getir"); sayı ve başlık **canlı güncellenir** ("1 Web
araması" → "2 Web araması ve Kabuk"); anlatım paragrafları grup
satırlarının arasında akar. Cursor'daki karşılığı (statik) "Explored 2
files, 2 searches" ve genişletilince çağrı başına "Read assistant.go
L700-819" [canlı]. Atlas düz modu ikisini birleştirir:

- Ardışık aynı aile çağrılar → **grup satırı**: `Fiil` (koyu, 530) +
  sayılı özet (`ink-3`): "İnceledi 3 dosya, 2 arama". Sayılar
  `animated-number` ile canlı artar; ailenin ilk çağrısı geldiğinde satır
  `atlas-message-in` ile girer, sonraki çağrılar yalnız sayıyı günceller
  (satır zıplamaz).
- Bir anlatım paragrafı araya girince grup kapanır, sonraki çağrılar yeni
  grup açar (OpenCode'daki serpiştirilmiş düzen).
- Grup satırına tıklayınca çağrı başına satırlar açılır (Cursor düzeni:
  `Okudu assistant.go L700-819`).
- Önek kuralı: OpenCode'un soluk öneki (rol/ajan) Atlas'ta yalnız alt
  ajan ya da takım ajanı çağrılarında görünür ("explore ·"), ana ajanda yok.

### 5.2 Birleşik satır anatomisi

```
[ind 16][gap 8][Fiil][gap 6][argüman · · · · · · · ellipsis][gap 8][meta][chevron 16]
```

- `ind`: düz modda yalnız running/error/denied durumunda görünür (16 px
  kutu, aksi halde 0 genişlik, 160 ms genişlik geçişi); kart modda
  her zaman kategori ikonu.
- `Fiil`: insan dili, şimdiki zaman (çalışırken) → geçmiş zaman (bitince);
  vendored `tool-status-title` active/done morph'u ile (OpenCode [CSS]).
  Grok'un "Searching the web / Reading the web" eşlemesi [JS] desenini
  Atlas fiilleriyle uygularız; argüman 60 karakterde değil, satıra göre
  CSS ellipsis + `mask-image` fade (son 24 px).
- `argüman`: dosya adı + aralık (`assistant.go L700-819`), desen + kapsam
  (`refusal|REFUSED in ui`), komut (mono 12.5 değil, sans 13; Cursor
  [canlı] böyle), URL için domain.
- `meta`: süre (> 2 sn ise), sonuç sayısı (`12 eşleşme`), `+21 −71`,
  çıkış kodu (≠ 0 ise danger çip).
- `chevron`: yalnız hover/focus (Cursor [canlı]); satır `role="button"`,
  `aria-expanded`, Enter/Space açar.
- Satır hover: `--atlas-hover` zemin, radius 6, −6 px yatay taşma (metin
  hizası bozulmaz).

### 5.3 Fiil ve ikon eşlemesi (kategori tabanlı)

Kategori kaynağı: `internal/permission/category.go` + araç adı önekleri.
İkon adları mevcut kataloğa göre. Metinler i18n anahtarı
(`atlas.tool.<kategori>.active|done`), EN kaynak + TR çeviri.

| Kategori | Araçlar (örnek) | TR çalışıyor → bitti | EN | İkon |
|---|---|---|---|---|
| read | view, inspect_file, read_mcp_resource | Okuyor → Okudu | Reading → Read | `glasses` |
| search | grep, glob, semantic_code_search, code_query, references | Arıyor → Aradı | Searching → Grepped/Searched | `magnifying-glass-menu` |
| list | ls, code_map, project_model | Listeliyor → Listeledi | Listing → Listed | `bullet-list` |
| lsp | lsp_* (okuma) | İnceliyor → İnceledi | Inspecting → Inspected | `code-slash` |
| edit | edit, multiedit, lsp_rename, lsp_replace_symbol | Düzenliyor → Düzenledi | Editing → Edited | `pencil-line` |
| write | write | Oluşturuyor → Oluşturdu | Creating → Created | `open-file` |
| shell | bash, job_output, job_kill | Çalıştırıyor → Çalıştırdı | Running → Ran | `console` |
| test | test_run, lint_run, coverage_report, mutation_test | Test ediyor → Test etti | Testing → Tested | `flask` |
| git | git_*, github_pr_view, pr_describe | Git: … | Git: … | `branch` |
| web-search | web_search, design_search | Web'de arıyor → Web'de aradı | Searching the web → Searched | `outline-globe` |
| fetch | fetch, api_probe, site_monitor | Sayfayı okuyor → Sayfayı okudu | Reading page → Read page | `window-cursor` |
| browser | browser, tool_pipeline_browser | Tarayıcıda → Tarayıcıda gezindi | Browsing → Browsed | `outline-browser-annotate` |
| computer | computer, isolated_desktop, android, local_app, app_loop, screen_mark | Bilgisayarda çalışıyor → Bilgisayarda çalıştı | Using the computer → Used | `monitor` |
| agent | agent, subagent, task, team_send | Alt ajan çalışıyor → Alt ajan bitti | Delegating → Delegated | `task` |
| plan | todos, exit_plan_mode | Planı güncelliyor → Planı güncelledi | Updating plan → Updated | `checklist` |
| ask | question | Soruyor → Sordu | Asking → Asked | `bubble-5` |
| memory | memory, facts, session_search | Hatırlıyor → Hatırladı | Recalling → Recalled | `brain` |
| security | security_scan, scan_secrets, dep_audit | Tarıyor → Taradı | Scanning → Scanned | `shield` |
| mcp | `mcp_<server>_<tool>` | `<server>` · `<tool>` | same | `mcp` |
| other | geri kalan | `<tool_name>` humanized | same | `status` |

Düşünme satırı ayrı: "Düşünüyor" (shimmer) → "Düşündü 4 sn".

### 5.4 Durumlar

Kanıt: grup satırında canlı artan sayı ve "Çalışıyor" shimmer'ı
[canlı-akış]; aşağıdaki satır bazlı durum görselleri (spinner, hata çipi,
reddedildi, süre sayacı) hiçbir uygulamada çalışırken canlı görülmedi,
vendored CSS ve Grok fiil eşlemesinden [CSS/çıkarım].

| Durum | Düz mod | Kart mod | Hareket |
|---|---|---|---|
| pending (argüman akıyor) | Fiil `ink-3` shimmer, argüman harf harf (vendored `text-reveal`, spring-soft .45 s [CSS]) | İkon `ink-4`, aynı | [çıkarım] |
| running | Fiil shimmer `ink-3→ink`; 2 sn sonra meta'da süre sayacı | Spinner 16 px yerine ikon, süre sayacı | spin .9 s |
| success | Fiil `ink-2` 530 geçmiş zaman, argüman `ink-3` | İkon `ink-3` | active→done morph 160 ms |
| error | Fiil `danger`, sağda `Hata` çip (danger-bg), satır otomatik açılır (ToolErrorCard) | İkon → `circle-exclamation` danger | ikon morph |
| denied | Fiil `ink-3`, `circle-ban-sign` 14 px + "Reddedildi" çip; üstü çizili **değil** | aynı | — |
| cancelled / interrupted | Fiil `ink-3` + "Durduruldu" | aynı | — |
| waiting-permission | Fiil `warning` + "İzin bekliyor" + dock'a odak linki | ikon `warning` nokta | nokta pulse 2 s |
| background (uzun iş) | Meta'da "arka planda" + composer üstü "1 Terminal" çipi | aynı | — |

Satır durumu `data-state` attribute'u ile (`pending|running|success|error|denied|cancelled|waiting|background`).

### 5.5 Genişlet / daralt

- Tek satır: tıklama → gövde `atlas="collapse"` (3.6). Açık durum
  `session.tsx`'teki mevcut `disclosure` store'unda tutulur (oturuma özel).
- Tur başlığı "çalıştı" → tüm ara adımlar. Özet satırı → yalnız araç listesi.
- `Alt+Click` bir satıra: o turdaki tüm aynı kategori satırlarını açar/kapar.
- Kısayol: timeline'da focus varken `Space` açar, `[`/`]` tur atlar.

### 5.6 Gövde önizlemeleri

- **Diff (edit/write/patch)**: başlık satırı dosya yolu (ad 530 + klasör
  `ink-3`) + `+5 −10` + "İncele" (sağ rayda açar). Gövde `@pierre/diffs`
  (zaten bağımlılık) unified, 12.5/20 mono, satır numarası `ink-4`,
  ekleme `diff-add-bg`, silme `diff-del-bg`. Kapalı halde ilk 12 satır,
  alt 24 px fade + "N satır daha". `write` yeni dosyada ilk 20 satır.
- **Terminal (shell)**: komut satırı `$ go test ./...` mono 12.5 `ink-2`;
  çıktı `surface-1` radius 8, max-height 240 px, kendi scroll'u, ANSI
  renkleri (`strip-ansi` yalnız kopyalarken), alt satırda çıkış kodu çipi
  + süre + "Kopyala" + "Terminalde aç" (sağ ray Terminal). Çalışırken
  **canlı kuyruk**: kapalı satırda bile son çıktı satırı 12 px `ink-3`
  tek satır mask fade ile akar (uzun işte "ölü" hissini önler) [çıkarım].
- **Dosya (read)**: satır aralığı önizlemesi (mono, 8 satır, satır no),
  "Dosyayı aç" (sağ ray Files / IDE'de aç).
- **Arama (grep/glob)**: dosya başına gruplu eşleşme listesi, eşleşen
  parça `ember-soft` vurgulu, en fazla 20 satır.
- **"Dosya değişti" kartı** [canlı, Cursor]: tur sonunda; başlık
  "3 dosya değişti" 13/16 530 + sağda "İncele"; satırlar 28 px: dosya
  ikonu (vendored `file-icon`) + ad + sağda `+N −N` (tabular). Satıra
  tıklama → sağ rayda o dosyanın diff'i + 1.5 s Ember halka pulse.

### 5.7 Paralel araç çağrıları

Aynı adımda birden fazla çağrı (ör. 3 `view`): tek grup satırı
"3 dosyayı okuyor · 2/3" (sayı `animated-number`), altında her biri kendi
durumuyla girintili (12 px sol ray). Hepsi bitince grup "Okudu 3 dosya"
olarak katlanır. Bir tanesi hata verirse grup açık kalır, hatalı satır
ilk sıraya. Alt ajanlar (`task`) paralelse her biri ayrı `task-tool-card`
(vendored) satırı, ajan rengiyle; tıklayınca alt oturuma gider (mevcut
`navigateToSession`).

### 5.8 Uzun süren işler

- 2 sn: süre sayacı görünür.
- 10 sn: shell için canlı kuyruk satırı; diğerleri için belirsiz 2 px
  progress satırın altında [çıkarım].
- 60 sn: satır sağında "Arka plana al" (shell → job) ve "Durdur" ikon
  butonları; composer üstünde "1 Terminal" çipi (Cursor [canlı]).
  OpenCode çalışan shell'in altında "Arka plana taşı CTRL+B" ipucu
  gösteriyor [canlı-akış]; Atlas'ta Ctrl+B sidebar'a ayrıldığı için
  kısayol **Ctrl+Shift+B**, ipucu 10. saniyeden itibaren 12 px `ink-3`.
- Belirli ilerleme bilinen işler (test sayısı, indirme): `progress-circle`
  (vendored) 14 px + yüzde.
- Pencere odakta değilken 30 sn'den uzun tur biterse OS bildirimi.

---

## 6. Web search / fetch / browser tasarımı

Canlı gözlenen [canlı-akış] (test istemi: "Web'de arama yap: Go dilinin
son sürümünde neler yeni?"):

- **OpenCode:** arama ve fetch yalnız sessiz grup satırları olarak
  ("2 Web araması ve Kabuk", "1 Web içeriğini getir"); ilk arama
  başarısız olunca model bunu araya giren bir paragrafla anlattı ve
  fetch'e geçti. Kaynak kartı/listesi **yok**. İlk çağrıda üçüncü taraf
  web araması için izin dock'u composer'ın yerine geldi.
- **Grok:** hiç araç satırı yok; maskot + "Web'de arama yapıyor" →
  "Web'de arıyor" → "Yazıyor…" durum metni; kaynak yalnız final metin
  içinde **favicon'lu inline link** ("Ayrıntılar [favicon] Go 1.27 sürüm
  notlarında").
- İki uygulamada da kaynak kartı, favicon yığını ve sonuç listesi reveal
  animasyonu **görülmedi**. Aşağıdaki 6.1–6.2 ve 6.5 bu yüzden
  **[çıkarım]**dır (OpenCode `sources-panel`, `sources-chip`, `site-icon`,
  `session-websearch-dock`, `markdown-link-favicon` CSS'i [CSS] + Grok
  "Searching the web" / "Reading the web" fiil eşlemesi [JS]). 6.3 fetch
  satırı ve 6.4 inline atıf gözleme dayanır.

Karar: varsayılan sunum gözlenen sadelikte (satır + inline atıf), zengin
kaynak listesi **isteğe bağlı genişletme** olarak.

### 6.1 Arama sürüyor [çıkarım; grup satırı + shimmer kısmı canlı-akış]

```
[globe] Web'de arıyor  ⟨go 1.26 release notes⟩ ⟨greenteagc⟩         0:03
         (favicon yığını büyüyor)  ●●● 6 kaynak
```

- Fiil `TextShimmer`. Sorgu(lar) **çip** olarak: 20 px pill, `surface-2`,
  12 px 440 `ink-2`, padding 2/8, aralık 4; her yeni sorgu çipi
  `atlas-panel-up` 160 ms, 40 ms stagger.
- Sonuç geldikçe favicon yığını: 16 px daire `site-icon`, −3 px örtüşme +
  radial mask kesik (OpenCode [CSS]), en fazla 5 + "+N". Yeni favicon
  `atlas-pop` 220 ms; görsel yüklenince `opacity 0→1` 150 ms
  (`data-state=loaded`). Favicon yoksa domain baş harfi, `--v2-avatar-bg-*`
  paletinden domain hash'iyle renk.
- Sayı `animated-number` ile artar.

### 6.2 Arama bitti [çıkarım]

Satır: "Web'de aradı · go 1.26 release notes · 8 kaynak [●●●]".
Genişletince **kaynak listesi** (sources panel deseni):

- Satır 2 kolon grid `16px 1fr`, padding 6/8, radius 8, hover `--atlas-hover`.
- Başlık 13/16 530 `ink` tek satır mask fade; altında `domain · tarih`
  12/16 `ink-3`; opsiyonel 2 satır snippet 12/16 `ink-2` (ayarla).
- Açılışta **stagger reveal**: her satır `atlas-message-in` varyantı
  (`translateY(4px)` + fade) 180 ms, 30 ms stagger, ilk 8 satır; sonrası
  anlık. Reduced-motion: stagger yok.
- Satır tıklama: varsayılan tarayıcıda açar; `Alt+Click` sağ ray Browser'da.

### 6.3 Fetch / "sayfayı okuyor" [satır canlı-akış; progress ve gövde çıkarım]

- Gözlenen: OpenCode'da "1 Web içeriğini getir" grup satırı (sayı +
  başlık, ikon yok), satır tamamlanınca yerinde kalır [canlı-akış].
- Satır (kart modu): "[window-cursor] Sayfayı okuyor · [favicon] go.dev/doc/go1.26".
  Düz modda ikon yok: "Sayfayı okudu go.dev/doc/go1.27".
  Domain çipi favicon'lu. Altında 2 px belirsiz progress (3.10) yalnız
  okuma sürerken.
- Bitince: "Sayfayı okudu · Go 1.26 Release Notes · 4.2k kelime".
  Genişletince: sayfa başlığı, URL (accent link, hover'da altı çizili +
  dış ok ikonu, vendored `webfetch-link`), alınan özetin ilk 12 satırı
  markdown olarak, "Ham içeriği göster".
- Hata (403/timeout): danger çip + "Tarayıcıyla dene" eylemi.

### 6.4 Atıflar (citation)

- **Varsayılan: inline favicon'lu link** [canlı-akış, Grok]: metnin
  kendisi link olur ("Go 1.27 sürüm notlarında"), önünde 14 px favicon
  (yüklenince 150 ms fade, yoksa globe glyph; `markdown-link-favicon`
  deseni [CSS]), renk `--atlas-accent-text`, hover'da alt çizgi.
- **Cümle sonu atıf çipi** (model kaynak id'li atıf ürettiğinde) [çıkarım]:
  18 px pill, `surface-2`,
  12 px 440 `ink-2`, içinde 12 px favicon + kısa domain (`go.dev`),
  aynı cümlede birden fazla kaynak → `go.dev +2`. Hover (300 ms) → 320 px
  hover card: favicon + site, başlık 13/16 530, 3 satır snippet, URL
  `ink-3`; `atlas-pop-in` 160 ms.
- Markdown içindeki normal linkler: `markdown-link-favicon` deseni
  (14 px favicon önde, yüklenince fade) [CSS].
- Tur sonunda **kaynaklar çipi** meta satırında: "[●●●] 8 kaynak"
  (OpenCode `sources-chip` [CSS]: 20 px pill, `--atlas-hover` zemin,
  hover'da `--atlas-press`). Tıklama → sources panel popover (420 px,
  arama adımları + sonuçlar, 6.2 düzeni).

### 6.5 Tarayıcı / bilgisayar önizleme çerçevesi [çıkarım]

- Kart (inline, sütun genişliği, max 560 px yükseklik): üstte 32 px
  "krom" çubuğu `surface-1`: geri/ileri (pasif görsel), URL pill'i
  (favicon + domain `ink-2` + path `ink-3`), sağda "Canlı izle" (sağ ray
  Browser). **Üç renkli mac noktaları kullanılmaz** (taklit olmasın).
- Gövde: ekran görüntüsü 16:10, radius 0 alt köşeler 12; yeni kare
  geldiğinde eskisinin üstünde 180 ms crossfade (flash yok).
- Eylem işaretleri: tıklama koordinatında 24 px Ember halka ripple
  (`scale .6 → 1.4`, opacity 0.6 → 0, 500 ms ease-out), yazma için
  koordinatta küçük "⌨ yazıyor" etiketi; `screen_mark` aracının işaretleri
  numaralı 18 px rozetler.
- Alt caption: adım fiili ("'Giriş' butonuna tıkladı") + adım sayacı
  "Adım 4/12"; kaydırıcıyla adımlar arasında gezinme (sağ rayda).
- Bilgisayar kullanımı (computer/isolated_desktop) aynı çerçeve, krom
  çubuğunda URL yerine pencere/uygulama adı + "İzole masaüstü" rozeti.

---

## 7. Ekran ekran UX haritası

### 7.1 Pencere kromu / titlebar (40 px)

- Windows: sağda native boyutlu kontroller 46×40 (min/max/close, close
  hover `#c42b1c` beyaz ikon, Windows standardı). macOS: solda trafik
  ışıkları için 78 px boşluk. Linux: sağ, 32 px.
- Sol → sağ: sidebar aç/kapa (Ctrl+B) · geri/ileri (Alt+←/→) · arama
  (Ctrl+K) · **oturum sekmeleri** (mevcut `tab-nav`; aktif sekme
  `surface` zemin + `shadow-raised`, pasif `ink-2`; sekmede durum noktası,
  kapatma × hover'da) · `+` yeni sekme · drag alanı · sağda "IDE'de aç ↗",
  `⋯` menü, sağ ray aç/kapa (Ctrl+Alt+B).
- Native menü çubuğu (File/Edit/View/Help, Cursor [canlı]) yalnız
  Windows/Linux'ta `Alt` ile görünür; macOS'ta sistem menüsü.

### 7.2 Sidebar (264 px, shell zemininde)

- Üst: `Yeni oturum` (aktif pill, Ctrl+N) · `Rutinler` (Atlas routines) ·
  `Otomasyon` · `Özelleştir` (ajanlar, skill'ler, MCP).
- `Projeler` başlığı + filtre + klasör ekle ikonları (Cursor [canlı]).
  Proje satırı (28 px, proje avatarı 16 px) → altında oturumlar (28 px):
  durum noktası (çalışıyor Ember ripple / bekliyor warning / okunmamış
  info / hata danger) + başlık + sağda göreli zaman `ink-3` 12 px; hover'da
  zamanın yerine pin + arşiv ikon butonları (24 px); 450 ms hover card
  (başlık, repo, yol, Yerel/uzak node, model) [canlı, Cursor].
- Gruplama: Sabitlenenler / Bugün / Bu hafta / Daha eski; 20'den fazla
  oturumda "Tümünü göster".
- Alt: kurulum ilerleme kartı (yalnız onboarding bitmediyse: "Başlangıç
  2/4" + halka) · kullanıcı/hesap çipi + ayarlar dişlisi.
- Yeniden boyutlandırma: kenar 4 px handle (vendored `resize-handle`),
  çift tık varsayılana döner.

### 7.3 Home

Ortada (dikey %38): wordmark (max 520 px) → **bağlam satırı composer'ın
üstünde** (Cursor [canlı]): proje ▾ · branch ▾ · çalışma yeri ▾ (Yerel /
İzole / uzak node) → composer (736 px) → ipucu pill'leri (4.5) → sayfa
altı tek satır link. OpenCode'un composer altı meta satırı [canlı]
yerine Cursor'ın üst satırını seçiyoruz: bağlamı gönder'den önce okutur.

### 7.4 Sohbet

4.1 düzeni. Başlık satırı (40 px, içerik kartının üstünde): breadcrumb
`proje / oturum başlığı ⋯` (OpenCode [canlı-akış]; başlık ilk mesajdan
otomatik üretilir ve sekmeye de yazılır [canlı-akış], çift tık düzenle) ·
ajan pill'i · sağda **token halkası** (14 px, çalışırken döner, yanında
`tabular-nums` token sayacı "25 b"; OpenCode başlığında gözlendi
[canlı-akış]; Atlas'ta `oc/context-ring.tsx`: boşta bağlam doluluğunu
gösterir, çalışırken 0.9 s dönen yay + artan sayaç, hover'da context ring
kartı) · paylaş/çatalla/`⋯` · sağ ray aç/kapa. Sol 24 px **tick scrubber** [canlı, Cursor]: her tur 1 px × 8 px
`ink-4` çizgi, aktif tur `ink` 2 px × 12 px; hover'da tur başlığı tooltip,
tıklama turun başına kaydırır. Alt 48 px fade maskesi composer üstünde.
Yüzen çipler (composer'ın 8 px üstünde, sola hizalı): `Değişiklikler +N −N`,
`1 Terminal`, `Commit oluştur ▾`, sağda aşağı-kaydır butonu.

### 7.5 Composer

- Kart: `surface`, `line-2` 1 px, radius 14, `shadow-composer`; focus'ta
  kenar `line-3` + `0 0 0 3px var(--atlas-ember-soft)` (160 ms).
- Satır 1: editör 14/22, min 1 satır, max %40 viewport, placeholder
  "Bir şey sor, / komutlar, @ bağlam" (`ink-3`).
- Satır 2 (32 px): `+` ek (24 px ikon buton) · mod çipi `Build ▾` (Shift+Tab
  ile Plan/Build döngü; mod rengi `--v2-agent-*`) · model çipi
  `Claude Sonnet ▾` (model popover, `atlas-panel-up`) · efor çipi · boşluk ·
  context ring 16 px · mikrofon · gönder (3.3).
- Ekler: composer içinde üstte 56 px çip şeridi, yatay kaydırma kenar
  fade'leri (mevcut `composer-attachments-fade`).
- `@` ve `/` öneri listesi: composer'ın üstünde açılır, 320 px, satır 32 px,
  `atlas-panel-up`; mevcut `composer-suggestions`.
- Streaming sırasında yazılan mesaj **kuyruğa** girer: composer üstünde
  "Sırada 1 mesaj" çipi, düzenlenebilir.
- Çalışırken [canlı-akış]: OpenCode'da composer altta sabit kalır,
  placeholder "Takip iletisi ekleyin…" olur, gönder butonu durdur'a döner;
  Grok'ta composer hiç değişmez. Atlas: OpenCode gibi placeholder
  "Takip mesajı ekle…" + gönder → durdur (3.3).
- **İzin anında composer'ın yerini dock alır** (4.6) [canlı-akış]; taslak
  korunur ve dock kapanınca geri gelir.

### 7.6 Komut paleti (Ctrl+K / Ctrl+Shift+P)

640 px genişlik, üstten 14vh, radius 16, `shadow-overlay`. Arama input 44 px
(16 px ikon + 14/22). Gruplar: Son oturumlar · Komutlar · Dosyalar (@) ·
Ayarlar · Modeller. Satır 32 px: ikon 16 + başlık + `ink-3` açıklama + sağda
`kbd` (11 px 530, `surface-2`, radius 4, padding 1/5). Fuzzy eşleşme
(`fuzzysort` mevcut) harfleri `ink` 620, kalan `ink-2`. Seçili satır
`--atlas-hover`, klavye ile hareket anlık (animasyonsuz), fare ile 60 ms.
Giriş 160 ms `atlas-pop-in` (scale .98). Oturum arama (Ctrl+P) aynı kabuk,
480 px (OpenCode [canlı]). Mevcut `oc/palette.tsx` üzerine.

### 7.7 Ayarlar

Diyalog 880 × min(680, 90vh), radius 16; sol nav 208 px (Genel, Görünüm,
Sohbet, Modeller & Sağlayıcılar, Ajanlar, İzinler, Hook'lar, MCP &
Entegrasyonlar, Kısayollar, Gelişmiş). Mevcut `oc/settings.tsx` +
`app/settings/*` (search-field, connections). Yeni satırlar: Görünüm >
Tema (Sistem/Açık/Koyu), Hareket (Sistem/Tam/Azaltılmış), Terminal
wordmark; Sohbet > Araç satırı yoğunluğu (Düz/Kart), Tamamlanan turları
katla, Akıl yürütmeyi göster, Kaynak snippet'leri. Sağlayıcı ekleme modalı
Grok "Uygulamaları bağla" [canlı] düzeninde: 900 px, arama + filtre çipleri
(aktif = ink pill), 2 kolon satır (40 px logo radius 10, ad, açıklama,
`Bağlan` pill). Model seçici OpenCode [canlı] düzeninde: 640 px, satırda
"Ücretsiz" rozeti, seçili olanda check, altta sağlayıcı kartları paneli.

### 7.8 Onboarding (ilk açılış)

1. Marka anı (3.14).
2. "Atlas'a hoş geldin": 40/44 display, tek cümle, `Başla` (Ember primary,
   yalnız burada Ember dolgu buton).
3. Sağlayıcı bağla: abonelik girişleri (ChatGPT / Claude / Antigravity
   planlı) + API anahtarı + "Sonra"; kartlar 2 kolon.
4. Proje seç: klasör aç / klonla / son klasörler.
5. Tercihler: tema, araç yoğunluğu (canlı önizlemeli küçük mock), izin
   modu (Sor / Okumalara izin ver / Otonom).
Adımlar arası: içerik `translateX(24px) → 0` + fade 280 ms emphasized,
üstte 4 nokta ilerleme. `Esc` atlar, her adım Ayarlar'dan geri alınabilir.

### 7.9 Bildirimler

- In-app toast (3.11): "Tur tamamlandı", "Commit oluşturuldu", hatalar.
- OS bildirimi: pencere odakta değilken tur biterse (>30 sn), izin/soru
  beklerse (hemen), hata. Tıklama ilgili sekmeyi açar. Ayarlar'da tür bazlı.
- Taskbar/dock rozeti: bekleyen izin + okunmamış tur sayısı.

### 7.10 Çevrimdışı / hata durumları

- Sunucu (yerel `atlas-agent` süreci) bağlantısı koptu: içerik kartının
  tepesinde 32 px şerit `warning-bg`, "Atlas sunucusuna yeniden
  bağlanılıyor · 3 sn" + `Şimdi dene`; composer gönder pasif, taslak
  korunur (mevcut `persistence/drafts.ts`). Kapanma: şerit yükseklik
  çökmesi 220 ms.
- İnternet yok: sağlayıcı çağrısı başarısızsa inline hata kartı; web
  araçları "Çevrimdışı" çipiyle pasif.
- Sağlayıcı rate-limit: kart içinde geri sayım halkası + "Başka modelle
  devam et".
- Sunucu sürümü uyumsuz: tam ekran boş durum, "Güncelle" butonu.
- Çökme sonrası açılış: "Son oturum geri yüklendi" toast'ı.

### 7.11 Klavye kısayolları (Windows/Linux; macOS'ta Ctrl→Cmd)

| Eylem | Kısayol |
|---|---|
| Komut paleti | Ctrl+K, Ctrl+Shift+P |
| Oturum ara | Ctrl+P |
| Yeni oturum / sekme | Ctrl+N / Ctrl+T |
| Sekme kapat / geri aç | Ctrl+W / Ctrl+Shift+T |
| Sekme değiştir | Ctrl+Tab, Ctrl+1…9 |
| Sidebar / sağ ray | Ctrl+B / Ctrl+Alt+B |
| Ray sekmeleri (Changes/Browser/Terminal/Files) | Ctrl+Alt+1…4 |
| Composer'a odak | Ctrl+L |
| Gönder / yeni satır | Enter / Shift+Enter |
| Durdur | Esc (streaming'de), Ctrl+C composer boşken |
| Çalışan komutu arka plana taşı | Ctrl+Shift+B |
| Sunum modu (Sessiz / Persona) | Ctrl+Shift+M |
| Mod değiştir (Plan/Build) | Shift+Tab |
| Model seç | Ctrl+/ |
| Önceki/sonraki tur | Alt+↑ / Alt+↓ |
| Turu katla/aç | Ctrl+. |
| İzin: reddet / bir kez / her zaman | Esc / Enter / Ctrl+Enter (ve 1/2/3) |
| Ayarlar | Ctrl+, |
| Kısayol listesi | Ctrl+? (mevcut HelpDialog) |

---

## 8. Uygulama yol haritası (GEÇERSİZ: yerine APP-BUILD-PLAN.md)

> Bu bölüm "Solid bundle'ı `platform: desktop` ile çalıştır" varsayımına
> dayanıyordu; sahip kararıyla App sıfırdan yeni bir renderer'dır. Güncel
> mimari, backend sözleşmesi, bileşen envanteri, fazlar ve paketleme:
> `APP-BUILD-PLAN.md`. Aşağıdaki performans/erişilebilirlik kontrol listesi
> (Faz 6) geçerliliğini korur ve plan tarafından devralınır.

İlke: vendored `web/vendor/*` dosyalarına dokunulmaz; değişiklik token
köprüsü, Atlas CSS katmanı ve `web/src/oc/app/...` altında yeni
bileşenlerle yapılır. Vendored davranış değişmek zorundaysa (ör. ToolRegistry
renderer'ı) Atlas tarafında `ToolRegistry.register` ile **üzerine kayıt**
yapılır, vendored dosya düzenlenmez.

### Faz 0: Kabuk kararı ve iskelet (1 hafta)

- Native kabuk (açık soru 1). Öneri: `app/` (repo kökü) altında ince
  kabuk, `atlas-agent web --app` sürecini başlatır, aynı Solid bundle'ını
  `platform: "desktop"` ile yükler, frameless pencere + native kontroller.
- Dosyalar: yeni `app/` (kabuk), `web/src/oc/app/runtime/platform/platform.tsx`
  (desktop tespiti: preload'un koyduğu `window.atlasDesktop`), yeni
  `web/src/oc/app/shell/window-controls.tsx`, `internal/cmd/web.go`
  (`--app` bayrağı: rastgele port, token, tarayıcı açmama).
- Kabul: Windows'ta pencere açılır, titlebar drag/çift tık/snap çalışır,
  kontroller native boyutta, kapatma süreci temiz sonlandırır; soğuk
  açılış < 1.5 s (SSD, release build).

### Faz 1: Token'lar, hareket, kimlik (1 hafta)

- Yeni `web/src/styles/atlas-app-theme.css` (`atlas-app-tokens.css`'ten),
  yeni `web/src/styles/atlas-motion.css` (bölüm 3), `web/src/styles/atlas-app.css`
  içinden import (vendored CSS'ten **sonra**).
- `oc/app/providers/atlas-logo.tsx` → yeni Atlas işareti (A·Omuz);
  favicon/app ikonları (`app/` altında ico/icns/png 16–1024).
- Yeni `web/src/oc/app/runtime/motion.ts`: `data-motion` ve
  `data-color-scheme`'i `<html>`'e yazar (OS sorgusu + tercih), boot inline
  script ile ilk paint öncesi.
- `state/prefs.ts`: `motion`, `toolDensity`, `collapseTurns`,
  `terminalWordmark` tercihleri.
- Kabul: tüm ekranlarda OpenCode mavisi/logosu kalmadı (grep: `#3b5cf6`,
  `M11.2 3.2H4.79998` sıfır sonuç `web/src` içinde); light/dark kontrast
  tablosu (2.1) axe ile doğrulandı; `data-motion="reduce"` ile hiçbir
  transform animasyonu çalışmıyor (Playwright: `getAnimations()` kontrolü).

### Faz 2: Sohbet (2 hafta)

- Yeni bileşenler (`web/src/oc/app/session/turn/`): `turn-header.tsx`
  ("Çalışıyor/çalıştı" + süre), `turn-summary.tsx` (özet satırı sayacı),
  `turn-meta-row.tsx` (geri bildirim), `files-changed-card.tsx`,
  `user-message-card.tsx` (sticky + revert), `turn-scrubber.tsx`
  (tick timeline), `floating-chips.tsx`, `scroll-to-bottom.tsx`.
- `oc/session.tsx` Timeline `Row`: turları bu bileşenlerle sarar;
  `disclosure` store'u tur katlamayı da tutar.
- `oc/app/session/requests/*`: izin dock'u yeni anatomi (4.6).
- Persona modu: `web/src/shared/avatar/` (Dots `LiveAvatar` taşınır,
  `searching/reading/writing` durumları eklenir), yeni
  `web/src/oc/app/session/persona/persona-status.tsx` (maskot + durum
  metni), `session.presentation` tercihi (`state/prefs.ts`).
- Kabul: gönder'den sonra ≤ 200 ms içinde durum satırı görünür (Playwright
  zaman damgası); 200 turluk oturum 60 fps kaydırma (4× CPU throttle'da ≥ 50 fps);
  tamamlanan tur otomatik katlanır, özet sayıları araç listesiyle birebir;
  sticky kullanıcı kartı turun sonunda serbest kalır; meta satırı klavyeyle
  erişilebilir; geçmiş yüklemede giriş animasyonu yok.

### Faz 3: Araç satırları (2 hafta)

- Yeni `web/src/oc/app/session/tools/`: `tool-row-model.ts` (araç adı →
  kategori/fiil/ikon/argüman/meta, 5.3 tablosu; `internal/permission/category.go`
  ile senkron test), `plain-tool-row.tsx`, `tool-group.tsx` (ardışık okuma
  ve paralel grup), `shell-tail.tsx` (canlı kuyruk), `tool-density.css`.
- Kart modu vendored `BasicTool`'u kullanır; düz mod Atlas render'ı.
- i18n: `web/src/oc/i18n/en.ts` + `tr.ts` içine `atlas.tool.*` anahtarları.
- Kabul: tüm 8 durum Storybook-benzeri fixture sayfasında (`web/src/oc/app/dev/tool-gallery.tsx`,
  yalnız dev build) iki yoğunlukta render; 50 çağrılı turda DOM düğümü
  < 1500 (katlı); hata satırı otomatik açılır; Enter/Space/Alt+Click
  davranışları test edildi.

### Faz 4: Web, kaynak, tarayıcı (1.5 hafta)

- Yeni `web/src/oc/app/session/web/`: `search-row.tsx` (sorgu çipleri,
  favicon yığını), `sources-panel.tsx`, `citation-chip.tsx` (+ hover card),
  `fetch-row.tsx`, `browser-frame.tsx` (ekran görüntüsü + eylem ripple),
  `favicon.ts` (önbellekli, yerel sunucu üzerinden proxy; doğrudan üçüncü
  taraf favicon servisi yok, gizlilik).
- Markdown atıf eşlemesi: `internal/webui` tarafında kaynak id'leri mesaj
  metadata'sına (`internal/message/content.go`) — backend ile koordineli.
- Kabul: favicon yüklenemediğinde baş harf yedeği; 30 kaynaklı aramada
  reveal ilk 8'den sonra anlık; atıf hover card 300 ms; tüm linkler harici
  tarayıcıda açılır (kabuk `shell.openExternal` benzeri, allowlist'siz URL
  şeması yok).

### Faz 5: Ekranlar (2 hafta)

- Sidebar yeniden düzen (`oc/app/home/sessions/view.tsx`, `oc/titlebar.tsx`),
  sağ ray (`ReviewRegion` → `web/src/oc/app/shell/rail/` Changes/Browser/
  Terminal/Files), palet (`oc/palette.tsx`), ayarlar yeni satırlar
  (`oc/settings.tsx`), onboarding (yeni `web/src/oc/app/onboarding/`),
  bildirimler (`oc/app/shell/notifications/toast.tsx` + kabuk köprüsü),
  çevrimdışı şeridi (yeni `oc/app/shell/connection-banner.tsx`).
- Kabul: 7.11 kısayollarının tamamı çalışır ve HelpDialog'da listelenir;
  onboarding 5 adımda tamamlanır ve atlanabilir; sunucu öldürülünce 1 sn
  içinde şerit, geri gelince otomatik kapanır ve taslak kaybolmaz.

### Faz 6: Cila, performans, erişilebilirlik (1 hafta)

Kontrol listesi:

- [ ] Yalnız `transform`/`opacity` animasyonu (istisnalar 2.6'da);
  `will-change` yalnız animasyon süresince.
- [ ] Timeline > 200 parça: `@tanstack/solid-virtual` sanallaştırma;
  `content-visibility: auto` turlarda (vendored zaten araç tetiklerinde).
- [ ] Streaming: aynı anda ≤ 64 animasyonlu kelime; markdown yeniden
  parse 16 ms bütçesi (mevcut `markdown-cache`).
- [ ] Büyük yüzeylerde `backdrop-filter` yok.
- [ ] Shimmer per-char yalnız görünür satırlarda; ekran dışı
  `IntersectionObserver` ile `data-run=false`.
- [ ] `prefers-reduced-motion` + `data-motion` her animasyon için test.
- [ ] Kontrast: metin ≥ 4.5:1, UI/ikon ≥ 3:1 (2.1 tablosu); focus ring
  2 px `--atlas-focus`, offset 2 px, `:focus-visible` (fare tıklamasında
  görünmez); kartlarda `outline-offset: -1px`.
- [ ] Hedef boyutu: ikon buton ≥ 24 px (meta), varsayılan 28 px.
- [ ] Ekran okuyucu: streaming her token değil, tur başı "Atlas
  yanıtlıyor" ve sonu "Yanıt tamamlandı" `aria-live=polite`; izin kartı
  `role="alertdialog"`; araç satırı `role="button" aria-expanded`;
  tick scrubber `role="navigation"`.
- [ ] RTL: vendored `:dir(rtl)` desenleri korunur.
- [ ] i18n: görünür her metin anahtarlı (vendored AGENTS.md kuralı).
- Kabul: Lighthouse a11y ≥ 95 (desktop profile), axe sıfır kritik;
  Playwright görsel regresyon (light/dark × plain/card × reduce/full).

---

## 9. Sahibine açık sorular (5)

1. **Native kabuk teknolojisi:** Electron (OpenCode ve Grok Bot ile aynı,
   en öngörülebilir, ~150 MB) mı, Tauri v2 (küçük, Rust toolchain) mı,
   yoksa Go içinden webview (Windows'ta CGO'suz `go-webview2`; ama
   macOS/Linux'ta CGO gerekir ve `CGO_ENABLED=0` kuralıyla çakışır) mı?
2. **Aksan rengi:** Ember (`#f2762e`, Dots ile ortak marka) etkileşim
   aksanı olarak mı kalsın, yoksa App nötr kalıp Ember yalnız marka
   anlarında mı kullanılsın?
3. **Araç sunumu varsayılanı:** Proje oturumlarında Sessiz + Düz satır
   (Cursor/OpenCode canlı akış tarzı, önerilen), Dots oturumlarında Persona
   (maskot durumu) eşlemesi uygun mu? Web UI da aynı varsayılanlara geçsin mi?
4. **İşaret:** A·Omuz / B·Meridyen / C·Piksel A'dan hangisi? Home'daki
   ANSI gökkuşağı wordmark App'te varsayılan monokrom olsun mu?
5. **Pencere modeli:** Tek pencere + oturum sekmeleri (OpenCode) mi, proje
   başına pencere (Cursor) mu? Atlas Dots App içinde bir mod/sekme mi
   olacak, ayrı uygulama mı kalacak?

### Sahibin kararları (2026-11-04)

1. **Kabuk: Electron.** Terminalde `atlas-agent web` / `atlas-agent dots` gibi `atlas-agent app` komutu masaüstü uygulamasını açar.
2. **Ember turuncusu yalnız marka anlarında** (logo, onboarding, boş durum). Etkileşim aksanı nötr. Beğenilmezse değiştirilir.
3. **Araç sunumu: yalnız sessiz satırlar (Cursor/OpenCode tarzı).** Maskot/Persona modu Dots ile birlikte sonraya bırakıldı.
4. **Wordmark kalsın**, web'deki gibi; Cursor gibi sade ve profesyonel görünüm hedefi. İşaret (A/B/C) seçimi henüz yok.
5. **Pencere modeli: Cursor gibi** (proje başına pencere). Dots'un App içindeki yeri sonra düşünülecek.
