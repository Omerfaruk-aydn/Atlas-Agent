# Atlas Dots — Mimari ve Veri Modeli

Durum: taslak, 2026-11-03. Kod yazılmadı. Bu doküman ürünün tamamını
kapsar; planlanan hiçbir özellik çıkarılmadı. Fazlar yalnızca bağımlılık
sırasını belirler.

## 1. Ürün tanımı

Atlas Dots, Atlas Agent'ın web ve CLI sürümünün yanında duran, 7/24 çalışan
kişisel ajan ürünüdür. Her ajana **dot** denir. Bir dotun altı parçası vardır:

| Parça | İçerik |
|---|---|
| Kimlik | ad, sistem promptu, kişilik ayarları, ses, canlı karakter |
| Bilgisayar | kalıcı diski olan kendi Linux masaüstü ve tarayıcısı |
| Hafıza | uzun süreli bilgiler, tercihler, geçmiş işler |
| Tetikleyiciler | cron, e-posta, webhook, dosya değişikliği, fiyat değişimi, mesaj |
| Sınırlar | yetki politikası, günlük bütçe, onay kuralları |
| Dış kimlikler | e-posta adresi, telefon numarası, takvim, cüzdan |

Kullanıcı dotlarını bir ekip gibi yönetir. Dotlar birbirine iş devredebilir.

## 2. Mevcut Atlas'tan alınanlar ve eksikler

Atlas'ın agent çekirdeği olduğu gibi kullanılır. Ancak birkaç paket bugün
tek kullanıcılı, yerel ya da Windows'a özgüdür. Ürün katmanı bu eksikleri
kapatmalıdır.

| İhtiyaç | Atlas'taki karşılığı | Durum | Yapılacak |
|---|---|---|---|
| Hedefe kadar çalışma | `agent` (StartGoal, `decideCompletion`, tamamlama kapısı) | Hazır | Dot çalışan sürecinde aynen kullanılır |
| Araç izinleri | `permission` (5 mod), `hooks` | Hazır | Dot politikası `permission` üzerine eşlenir, onay isteği telefona yönlenir |
| Bilgisayar kullanımı | `computer` | **Yalnız Windows** (`unsupported.go` diğer platformlarda `ErrUnsupportedPlatform` döner) | Linux/X11 backend yazılmalı (bkz. §5) |
| Tarayıcı | `browser` | Hazır | Dot masaüstündeki Chromium'a bağlanır |
| İzolasyon | `sandbox`, `isolation` | Yerel süreç düzeyinde | Dot başına konteyner/mikro-VM ile değiştirilir |
| Zamanlama | `routine` (`ParseSchedule`, `Next`) | Hazır, tek makine | Cron ayrıştırıcı tekrar kullanılır, zamanlayıcı kontrol merkezine taşınır |
| Ses ve telefon | `voice` (Twilio, ElevenLabs, `Budget`, `policy`) | Hazır, test edilmedi | Dot başına numara ve ses kimliği eklenir |
| Konuşma tanıma | `speech` | Hazır | Gerçek zamanlı ses hattında kullanılır |
| Hafıza | `memory`, `factstore` | Yerel | Dot başına ad alanı eklenir |
| Ekip çalışması | `teams` | **Bellek içi**, tek oturum, süreç ölünce kaybolur | Kalıcı, dotlar arası posta kutusuna dönüştürülür |
| Telefondan kontrol | `mobile`, `mobilerelay`, `remotecontrol` | Hazır | Onay ve canlı ekran akışı buradan geçer |
| Sırlar | `vault`, `secretprompt` | Hazır | Dot başına kasa, kullanıcı anahtarıyla şifreli |
| Sunucu | `server` (HTTP, yerel soket) | Tek kullanıcı | Kontrol merkezi bunun üzerine kurulur, kimlik doğrulama ve çok kiracılık eklenir |
| Oturumlar | `session` (SQLite) | Tek kullanıcı | Dot ve kullanıcı kimliği oturumlara bağlanır |

## 3. Genel mimari

```
 Tarayıcı / Mobil uygulama
   │  HTTPS + WebSocket (olaylar)   WebRTC (ses, canlı ekran)
   ▼
┌───────────────────────── Kontrol merkezi ─────────────────────────┐
│ API gateway · Kimlik · Dot yönetimi · Tetikleyici zamanlayıcı     │
│ Onay servisi · Bütçe sayacı · Zaman çizelgesi · Yetenek mağazası  │
│ Posta kutusu (dotlar arası) · Medya (kayıt, video)                │
│ Postgres · Nesne deposu (S3) · Kuyruk (NATS JetStream)           │
└──────────────┬─────────────────────────────────┬──────────────────┘
               │ gRPC (iş ver, olay al)           │ WebRTC SFU
               ▼                                  ▼
┌──────── Dot çalışma alanı (dot başına bir tane) ────────┐
│ dot-runner: Atlas agent çekirdeği (sessionAgent, araçlar)│
│ Linux masaüstü (Xvfb + hafif pencere yöneticisi)         │
│ Chromium · Dosya sistemi (kalıcı disk) · Ekran kodlayıcı │
└──────────────────────────────────────────────────────────┘
```

### 3.1 Kontrol merkezi

Go ile yazılır ve `internal/server` paketinin HTTP iskeletini temel alır.
Ayrı bir ikili olarak çalışır (`atlas-dots-server`). Görevleri:

- **Kimlik:** kullanıcı hesabı, oturum, cihaz kaydı (mobil onay için).
- **Dot yönetimi:** oluşturma, düzenleme, uyutma, uyandırma, silme.
- **Zamanlayıcı:** tüm tetikleyicileri dinler, olay gelince dot'u uyandırır.
- **Onay servisi:** riskli eylemleri bekletir, telefona bildirim gönderir.
- **Bütçe sayacı:** her LLM, ses ve araç çağrısını maliyete çevirip sayar.
- **Zaman çizelgesi:** dotun her adımını, ekran görüntüsünü ve kararını kaydeder.
- **Posta kutusu:** dotlar arasında kalıcı mesajlaşma ve iş devri.
- **Orkestratör:** dot çalışma alanlarını açar, kapatır, kaynak ayırır.

### 3.2 Dot çalışma alanı

Her dot ayrı bir izole ortamda çalışır:

- **MVP:** Docker konteyneri (gVisor çalışma zamanıyla).
- **Üretim:** Firecracker mikro-VM. Daha güçlü izolasyon sağlar, anlık
  görüntü (snapshot) ile saniyeler içinde uyanma imkânı verir.

İçinde `dot-runner` adlı ikili çalışır. Bu ikili Atlas'ın `agent`,
`permission`, `hooks`, `browser`, `memory` paketlerini içerir. Kontrol
merkezinden gRPC ile iş alır, olayları geri akıtır.

Dot uyurken VM anlık görüntüsü alınır ve durdurulur. Disk kalıcıdır.
Uyandırma hedefi soğuk başlangıçta 3 saniyenin altıdır.

### 3.3 İstemci

- **Web:** React + TypeScript, Vite. Animasyon için Motion, karakter için
  Rive (`@rive-app/canvas`) ve three.js + `@pixiv/three-vrm`.
- **Mobil:** onay, bildirim, sesli konuşma ve canlı ekran. Mevcut
  `internal/mobile` akışı genişletilir.

## 4. Veri modeli

Postgres üzerinde. Tüm tablolarda `id` (UUIDv7), `created_at`, `updated_at`
bulunur, aşağıda tekrar yazılmadı.

```sql
users            (email, display_name, locale, plan, timezone)
devices          (user_id, kind[web|ios|android], push_token, last_seen_at)

dots             (user_id, name, slug, status[sleeping|waking|working|
                  waiting_approval|error], avatar_id, voice_id,
                  system_prompt, model, workspace_id, archived_at)
dot_personality  (dot_id, warmth, humor, formality, proactivity  -- 0..100
                  , language, extra_rules)
avatars          (owner_id NULL=hazır, kind[rive|vrm], asset_url,
                  state_machine, expressions jsonb)
voices           (provider[elevenlabs], provider_voice_id, name,
                  language, preview_url)  -- ElevenLabs ses kütüphanesi

workspaces       (dot_id, runtime[docker|firecracker], image, disk_gb,
                  cpu, memory_mb, snapshot_ref, host_id, state)

triggers         (dot_id, kind[cron|email|webhook|file|price|message|
                  calendar], config jsonb, goal_template, enabled,
                  last_fired_at, next_fire_at)
runs             (dot_id, trigger_id NULL, goal, status[queued|running|
                  waiting_approval|complete|incomplete|blocked|failed],
                  completion_decision jsonb, cost_cents, started_at,
                  ended_at, atlas_session_id)
run_steps        (run_id, seq, kind[thought|tool_call|tool_result|
                  message|screenshot|approval], payload jsonb,
                  media_key, cost_cents, at)

policies         (dot_id, rule jsonb)    -- izinli araçlar, alan adları,
                                         -- onay gerektiren eylemler
full_access      (dot_id, granted_by, warning_version, typed_name,
                  expires_at NULL=kalıcı, revoked_at)  -- tam yetki modu
approvals        (run_id, step_seq, action_kind, summary, payload jsonb,
                  status[pending|approved|denied|expired], decided_by,
                  decided_at, expires_at)
budgets          (dot_id, daily_cents, monthly_cents, hard_stop bool)
usage_ledger     (dot_id, run_id, kind[llm|voice|phone|compute|card],
                  units, cost_cents, at)

memories         (dot_id, kind[fact|preference|episode], text,
                  embedding vector, source_run_id, pinned)

identities       (dot_id, kind[email|phone|calendar], address,
                  provider, credentials_vault_ref, verified_at)
messages_inbox   (identity_id, direction, from, to, subject, body_ref,
                  thread_id, at)

mailbox          (team_id, from_dot_id, to_dot_id NULL=yayın, kind[note|
                  handoff|result], body, run_id, read_at)
teams            (user_id, name, lead_dot_id)
team_members     (team_id, dot_id, role)

skills           (publisher_id, slug, version, manifest jsonb,
                  permissions jsonb, review_status)
dot_skills       (dot_id, skill_id, config jsonb, vault_ref)

wallets          (dot_id, provider, card_ref, per_tx_limit_cents,
                  daily_limit_cents, frozen bool)
wallet_tx        (wallet_id, approval_id, merchant, amount_cents,
                  status, at)

recordings       (run_id, kind[video|timelapse], media_key, duration_s)
audit_log        (user_id, actor[user|dot|system], action, target, at)
```

Önemli kurallar:

- `runs.completion_decision` Atlas'ın `decideCompletion` çıktısını saklar.
  Kanıt yoksa run "complete" sayılmaz.
- `wallet_tx` ya `approved` durumda bir `approvals` satırına ya da o anda
  geçerli bir `full_access` kaydına bağlı olmalıdır. Bu kural veritabanında
  kısıt olarak da uygulanır.

## 5. Bilgisayar ve tarayıcı

`internal/computer` bugün yalnızca Windows'ta çalışıyor. Dot çalışma alanı
Linux olduğu için yeni bir backend gerekir:

- `computer/backend_linux.go`: X11 üzerinde ekran görüntüsü (XShm),
  fare ve klavye (XTest), pencere listesi (EWMH). `Backend` arayüzü
  korunur, böylece agent tarafında değişiklik gerekmez.
- Ekran canlı yayını: Xvfb çıktısı GStreamer ile H.264'e kodlanır,
  WebRTC üzerinden istemciye gider. Yedek olarak noVNC.
- Kullanıcı istediği an "kontrolü al" diyebilir. Bu sırada dot duraklar,
  kullanıcı bırakınca devam eder.
- Tarayıcı profili kalıcı diskte tutulur.
- Oturum açma ve şifre girişi: kullanıcı izin verirse dot bunları kendisi
  yapar. Şifreler `vault`'ta durur, modele düz metin olarak gösterilmez;
  dot yalnızca "şu alana şu kasa kaydını yaz" der, değeri `dot-runner`
  doğrudan forma yazar. İzin site bazında verilir: "her seferinde sor"
  veya "bu site için her zaman izinli". Tam yetki modunda (bkz. §7)
  dot her sitede sormadan oturum açar. Kayıtlara şifre değil, yalnızca
  hangi kasa kaydının kullanıldığı yazılır.

## 6. 7/24 çalışma modeli

Sürekli dönen bir LLM döngüsü yoktur. Dot olay gelince uyanır:

1. Tetikleyici ateşlenir (cron, gelen e-posta, webhook, fiyat eşiği vb.).
2. Zamanlayıcı `runs` satırı oluşturur, bütçe ön kontrolü yapar.
3. Orkestratör çalışma alanını uyandırır.
4. `dot-runner`, `goal_template` ile Atlas hedef modunu başlatır.
5. Her adım `run_steps`'e ve canlı olay akışına yazılır.
6. Tamamlama kapısı karar verir. "incomplete" ise yeniden denenir,
   "blocked" ise kullanıcıya bildirim gider.
7. İş bitince dot uyur.

Tekrar deneme ve yeniden başlatma: run'lar kuyrukta tutulur. Çalışma alanı
çökerse run son tamamlanan adımdan devam eder.

## 7. Güvenlik, onay ve bütçe

- **Onay gerektiren eylemler (varsayılan):** para harcama, mesaj/e-posta
  gönderme, paylaşım yapma, hesap ayarı değiştirme, dosya silme, form
  gönderme, oturum açma. Kullanıcı bunları dot ve site bazında
  gevşetebilir ("her zaman izinli").
- **Tam yetki modu:** kullanıcı isterse bir dot için açar. Açıldığında
  dot onay beklemeden her şeyi yapar: şifre girer, oturum açar, mesaj
  gönderir, paylaşım yapar, ayar değiştirir, ödeme yapar. Açılış akışı:
  1. Kullanıcı ayarlardan "Tam yetki" anahtarına basar.
  2. Uyarı ekranı çıkar: dotun neleri onaysız yapacağı tek tek listelenir
     (şifreler, hesaplar, mesajlar, ödemeler, silme).
  3. Kullanıcı "Riskleri anladım" kutusunu işaretler ve dotun adını yazar.
  4. Mod açılır. Süre seçilebilir: 1 saat, 24 saat, kalıcı.
  Mod açıkken dotun üstünde sürekli görünen bir "Tam yetki" rozeti olur.
  Tek dokunuşla kapatılır, kapatınca bekleyen işler eski onay kurallarına
  döner. Bütçe ve kart limitleri tam yetki modunda da geçerlidir. Her
  eylem yine zaman çizelgesine yazılır, kullanıcı sonradan hepsini görür.
- **Akış:** Atlas'ın `permission.Request` noktası bir hook üzerinden onay
  servisine bağlanır. Dot `waiting_approval` durumuna geçer, telefona
  bildirim gider, kullanıcı tek dokunuşla onaylar veya reddeder.
- **Bütçe:** her çağrı `usage_ledger`'a yazılır. Günlük sınırın %80'inde
  uyarı, %100'ünde `hard_stop` ise run durdurulur. `voice.Budget`
  mantığı genelleştirilerek kullanılır.
- **İzolasyon:** her dot kendi VM'inde, ağ çıkışı politika ile sınırlı.
  Sırlar `vault`'ta, kullanıcı anahtarıyla şifreli tutulur.
- **İzlenebilirlik:** tüm eylemler `audit_log` ve `run_steps`'te.
  Kayıtlar silinemez, sadece kullanıcı hesabıyla birlikte silinir.

## 8. Ses ve canlı karakter

### 8.1 Gerçek zamanlı ses

- İstemci ile kontrol merkezi arasında WebRTC.
- Konuşma tanıma `speech`, ses üretimi ElevenLabs akışlı TTS.
- Söz kesme: kullanıcı konuşmaya başlayınca TTS akışı keser, model
  çıktısı iptal edilir.
- Telefon görüşmeleri mevcut `voice` paketi ve Twilio ile yapılır.

### 8.2 Karakter motoru

Ortak bir arayüz tanımlanır, Rive ve VRM bunu uygular:

```ts
interface Character {
  setState(s: "idle" | "listening" | "thinking" | "speaking" | "working"): void
  setEmotion(e: Emotion, intensity: number): void
  speak(visemes: VisemeFrame[]): void   // TTS zaman damgalarından
  lookAt(x: number, y: number): void    // imleç takibi
}
```

- Model her cevapta bir duygu etiketi de döner, karakter ifadesini buna
  göre değiştirir.
- Dudak senkronu: ElevenLabs karakter zaman damgaları vizemlere çevrilir.
- Boşta animasyonlar: nefes, göz kırpma, ara sıra imlece bakış.
- Karakterler bizim tasarımımızdır. Grok veya başka bir ürünün
  karakteri, adı ya da görünümü kopyalanmaz.

## 9. Özellik modülleri

| Modül | Bağlı olduğu altyapı | Not |
|---|---|---|
| E-posta kimliği | identities, tetikleyiciler | Kendi alan adımızda posta kutusu veya kullanıcının Gmail'ine OAuth |
| Telefon numarası | `voice`, Twilio | Numara maliyeti bütçeye yazılır |
| Takvim ve toplantı | identities, ses | Toplantı botu not alır, aksiyonları Jira'ya açar (onaylı) |
| Araştırma dotu | cron, browser | Örn. her sabah 08:00 fiyat kontrolü, %5 eşik |
| Sosyal medya dotu | onay, görsel üretimi | Paylaşım her zaman onaylı |
| Kod dotu | çalışma alanı, `ghpr`, `cicdx` | PR açar, CI kırmızıysa düzeltir |
| Satış dotu | browser, e-posta | Taslak yazar, gönderim onaylı |
| Ajan ofisi | mailbox, teams | Yönetici dot görev böler, ofis görünümü |
| Yetenek mağazası | skills, dot_skills | Manifest ile izin beyanı, inceleme süreci |
| Kamera ve görme | WebRTC, görsel model | Kullanıcı açıkça başlatır, kayıt yapılmaz |
| Tekrar izleme | recordings, run_steps | Hızlandırılmış video + adım listesi |
| Kişilik ve ses | dot_personality, voices | Her dota ElevenLabs kütüphanesinden ses atanır, seçerken önizlenir |
| Cüzdan | wallets, approvals | Sanal kart, her ödeme onaylı, en son faz |

## 10. Tasarım sistemi

"AI slop" yasağı somut kurallara bağlanır:

- Fontlar: Inter Display (arayüz), JetBrains Mono (kod, kayıt).
- 4/8 px ızgara, tek vurgu rengi, sınırlı gri skala, koyu tema öncelikli.
- Yasak: mor-mavi gradyan, her yerde cam efekti, ✨, ikon yerine emoji,
  lorem ipsum.
- Animasyon: yay fiziği (Motion), 150–300 ms, her animasyonun bir anlamı
  olur (geçiş, geri bildirim, durum değişimi).
- Her ekranın boş, yükleniyor ve hata hali tasarlanır.
- Token'lar tek kaynaktan gelir (CSS değişkenleri), bileşenler bu
  token'lar dışında renk kullanmaz.

## 11. Yapım sırası

Hiçbir madde çıkarılmadı. Sıra bağımlılıkları izler.

1. Tasarım sistemi, kontrol merkezi iskeleti, kimlik, veri modeli.
2. Dot çalışma alanı (Docker), Linux `computer` backend'i, canlı ekran.
3. Dot kimliği: kişilik ayarları ve ElevenLabs'tan ses atama.
4. Rive karakter, duygu etiketleri, dudak senkronu; ardından VRM.
5. Gerçek zamanlı ses, hedef modunda iş bitirme.
6. Tetikleyiciler, onay servisi, bütçe, zaman çizelgesi, tekrar izleme.
7. E-posta, telefon, takvim kimlikleri.
8. Hazır dot şablonları (araştırma, sosyal medya, kod, satış).
9. Ajan ofisi, kalıcı posta kutusu, yetenek mağazası, kamera ve görme.
10. Firecracker'a geçiş, ölçekleme.
11. Cüzdan.

## 12. Açık kararlar

- Barındırma: kendi sunucumuz mu, bulut (Fly.io, AWS) mu?
- Tek kiracılı kurulum (kullanıcının kendi makinesi) de desteklenecek mi?
- LLM maliyeti kullanıcıya mı yansıtılacak (kendi anahtarı) yoksa
  abonelik mi?
- Marka: "Atlas" adı OpenAI ChatGPT Atlas ve MongoDB Atlas ile çakışıyor,
  marka ve alan adı araştırması gerekli.
